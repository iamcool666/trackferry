// Package tv manages the limited credentials and service identity for TrackFerry TVs.
package tv

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"trackferry/internal/store"
)

const PairTTL = 5 * time.Minute
const maxPending = 24
const maxPendingPerAddress = 4

var (
	ErrInvalid = errors.New("配对信息无效")
	ErrExpired = errors.New("配对码已失效，请在电视上重试")
	ErrBusy    = errors.New("配对请求太多，请稍后再试")
)

type pairing struct {
	id, name, address     string
	commitment, tokenHash []byte
	expires               time.Time
	code                  string
	approved              bool
}

type PairPrompt struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Prompt advertises the newest verified waiting screen to signed-in web clients.
func (p *Pairer) Prompt() *PairPrompt {
	p.mu.Lock()
	defer p.mu.Unlock()
	var latest *pairing
	now := p.now()
	for _, item := range p.pending {
		if !item.approved && item.code != "" && item.expires.After(now) &&
			(latest == nil || item.expires.After(latest.expires)) {
			latest = item
		}
	}
	if latest == nil {
		return nil
	}
	return &PairPrompt{ID: latest.id, Name: "电视"}
}

type Pairer struct {
	Pin     string
	Name    string
	Store   *store.Store
	Remote  *Remote
	now     func() time.Time
	mu      sync.Mutex
	pending map[string]*pairing
}

func NewPairer(pin, name string, st *store.Store) *Pairer {
	return &Pairer{Pin: pin, Name: name, Store: st, Remote: NewRemote(), now: time.Now, pending: make(map[string]*pairing)}
}

func secretHex(raw string) ([]byte, bool) {
	if len(raw) != sha256.Size*2 {
		return nil, false
	}
	b, err := hex.DecodeString(raw)
	return b, err == nil
}

func (p *Pairer) Start(address, name, commitment, tokenHash string) (string, error) {
	name = strings.TrimSpace(name)
	committed, cOK := secretHex(commitment)
	token, tOK := secretHex(tokenHash)
	if name == "" || utf8.RuneCountInString(name) > 64 || !cOK || !tOK {
		return "", ErrInvalid
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(random[:])
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	from := 0
	for id, item := range p.pending {
		if !item.expires.After(now) {
			delete(p.pending, id)
			continue
		}
		if item.address == address && !item.approved {
			from++
		}
	}
	if len(p.pending) >= maxPending || from >= maxPendingPerAddress {
		return "", ErrBusy
	}
	p.pending[id] = &pairing{id: id, name: name, address: address, commitment: committed, tokenHash: token, expires: now.Add(PairTTL)}
	return id, nil
}

// Code binds the approved login to the server key. The TV commits to the seed before the
// challenge is fixed, then reveals it on the connection pinned to the key it observed.
func Code(pin, id, seed, tokenHash string) string {
	digest := sha256.Sum256([]byte("TrackFerry TV v1\n" + pin + "\n" + id + "\n" + seed + "\n" + tokenHash))
	return fmt.Sprintf("%06d", binary.BigEndian.Uint64(digest[:8])%1_000_000)
}

func (p *Pairer) Status(ctx context.Context, id, seed string) (string, error) {
	if _, ok := secretHex(seed); !ok {
		return "", ErrInvalid
	}
	proof := sha256.Sum256([]byte(seed))
	p.mu.Lock()
	defer p.mu.Unlock()
	item := p.pending[id]
	if item == nil || !item.expires.After(p.now()) || subtle.ConstantTimeCompare(item.commitment, proof[:]) != 1 {
		return "", ErrExpired
	}
	if !item.approved {
		item.code = Code(p.Pin, id, seed, hex.EncodeToString(item.tokenHash))
		return "pending", nil
	}
	ok, err := p.Store.TVAuthorized(ctx, item.tokenHash)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrExpired
	}
	return "approved", nil
}

func (p *Pairer) Cancel(id, seed string) {
	proof := sha256.Sum256([]byte(seed))
	p.mu.Lock()
	defer p.mu.Unlock()
	item := p.pending[id]
	if item != nil && !item.approved && subtle.ConstantTimeCompare(item.commitment, proof[:]) == 1 {
		delete(p.pending, id)
	}
}

func (p *Pairer) Approve(ctx context.Context, code string) (store.TVDevice, error) {
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return store.TVDevice{}, ErrInvalid
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var hit *pairing
	now := p.now()
	for _, item := range p.pending {
		if item.approved || item.code == "" || !item.expires.After(now) || subtle.ConstantTimeCompare([]byte(item.code), []byte(code)) != 1 {
			continue
		}
		if hit != nil {
			return store.TVDevice{}, ErrExpired
		}
		hit = item
	}
	if hit == nil {
		return store.TVDevice{}, ErrExpired
	}
	d, err := p.Store.AddTV(ctx, hit.id, hit.name, now.UTC().Format(time.RFC3339), hit.tokenHash)
	if err != nil {
		return store.TVDevice{}, err
	}
	hit.approved = true
	return d, nil
}

func (p *Pairer) Authorized(ctx context.Context, token string) (bool, error) {
	if _, ok := secretHex(token); !ok {
		return false, nil
	}
	hash := sha256.Sum256([]byte(token))
	return p.Store.TVAuthorized(ctx, hash[:])
}

// Device identifies the single TV associated with the credential.
func (p *Pairer) Device(ctx context.Context, token string) (string, error) {
	if _, ok := secretHex(token); !ok {
		return "", nil
	}
	hash := sha256.Sum256([]byte(token))
	return p.Store.TVByToken(ctx, hash[:])
}
