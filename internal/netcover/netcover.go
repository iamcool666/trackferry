// Package netcover keeps the platforms' cover pictures on disk, so a page full of online songs
// asks a platform for each picture once, a few at a time, and a missing one is not asked again
// at once.
package netcover

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"trackferry/internal/online"
)

// ErrNoCover means the platform gave no picture for the ID.
var ErrNoCover = errors.New("没有封面")

const (
	maxPicture = 5 << 20
	maxDisk    = 256 << 20
	trimTo     = 192 << 20
	missFor    = 30 * time.Minute
	fetching   = 4 // pictures fetched at once
)

// Cache fetches and keeps cover pictures under Dir.
type Cache struct {
	dir    string
	client *http.Client
	slots  chan struct{}
	writes atomic.Int64
	trim   sync.Mutex

	mu     sync.Mutex
	missed map[string]time.Time
	flight map[string]*call
}

type call struct {
	done chan struct{}
	mime string
	b    []byte
	err  error
}

// New keeps pictures in dir; client is used with a short time limit and no redirect off the
// platforms' image hosts.
func New(dir string, client *http.Client) *Cache {
	c := *client
	c.Timeout = 10 * time.Second
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || online.CoverID(req.URL.String()) == "" {
			return errors.New("redirect off the image hosts")
		}
		return nil
	}
	return &Cache{dir: dir, client: &c, slots: make(chan struct{}, fetching), missed: map[string]time.Time{}, flight: map[string]*call{}}
}

func (c *Cache) path(id string) string {
	h := sha1.Sum([]byte(id))
	k := hex.EncodeToString(h[:])
	return filepath.Join(c.dir, k[:2], k)
}

// Get returns the picture an ID names: from disk, or fetched once by however many ask.
func (c *Cache) Get(ctx context.Context, id string) (string, []byte, error) {
	u, ok := online.CoverURL(id)
	if !ok {
		return "", nil, ErrNoCover
	}
	p := c.path(id)
	if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
		now := time.Now()
		_ = os.Chtimes(p, now, now) // last used, for trimming
		return http.DetectContentType(b), b, nil
	}
	c.mu.Lock()
	if t, ok := c.missed[id]; ok && time.Since(t) < missFor {
		c.mu.Unlock()
		return "", nil, ErrNoCover
	}
	if f, ok := c.flight[id]; ok {
		c.mu.Unlock()
		select {
		case <-f.done:
			return f.mime, f.b, f.err
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	}
	f := &call{done: make(chan struct{})}
	c.flight[id] = f
	c.mu.Unlock()

	// The fetch is not tied to the first asker: others may be waiting for it.
	f.mime, f.b, f.err = c.fetch(context.WithoutCancel(ctx), u, p)
	c.mu.Lock()
	delete(c.flight, id)
	if errors.Is(f.err, ErrNoCover) {
		if len(c.missed) > 5000 {
			c.missed = map[string]time.Time{}
		}
		c.missed[id] = time.Now()
	}
	c.mu.Unlock()
	close(f.done)
	return f.mime, f.b, f.err
}

func (c *Cache) fetch(ctx context.Context, u, p string) (string, []byte, error) {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-time.After(15 * time.Second):
		return "", nil, errors.New("封面排队太久")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", nil, ErrNoCover
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return "", nil, ErrNoCover
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, errors.New(resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPicture+1))
	if err != nil {
		return "", nil, err
	}
	mime := http.DetectContentType(b)
	if len(b) > maxPicture || len(b) < 64 {
		return "", nil, ErrNoCover
	}
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
	default:
		return "", nil, ErrNoCover
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
		tmp := p + ".part"
		if os.WriteFile(tmp, b, 0o644) == nil && os.Rename(tmp, p) == nil && c.writes.Add(1)%200 == 0 {
			go c.trimDisk()
		}
	}
	return mime, b, nil
}

// trimDisk drops the pictures used longest ago once the folder grows past its bound.
func (c *Cache) trimDisk() {
	if !c.trim.TryLock() {
		return
	}
	defer c.trim.Unlock()
	type file struct {
		p    string
		size int64
		at   time.Time
	}
	var all []file
	var total int64
	filepath.WalkDir(c.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			all = append(all, file{p, fi.Size(), fi.ModTime()})
			total += fi.Size()
		}
		return nil
	})
	if total <= maxDisk {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })
	for _, f := range all {
		if total <= trimTo {
			break
		}
		if os.Remove(f.p) == nil {
			total -= f.size
		}
	}
}
