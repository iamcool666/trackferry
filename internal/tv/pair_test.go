package tv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPairingExpiresAndServerIdentityPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tv-key.der")
	cert, pin, err := Identity(path)
	if err != nil {
		t.Fatal(err)
	}
	rotated, after, err := Identity(path)
	if err != nil || pin != after || string(cert.Certificate[0]) == string(rotated.Certificate[0]) {
		t.Fatal("certificate renewal changed the identity", err)
	}
	now := time.Now()
	p := NewPairer(pin, "TrackFerry", nil)
	p.now = func() time.Time { return now }
	seed := strings.Repeat("12", 32)
	hash := sha256.Sum256([]byte(seed))
	id, err := p.Start("address", "电视", hex.EncodeToString(hash[:]), strings.Repeat("de", 32))
	if err != nil {
		t.Fatal(err)
	}
	wrong := strings.Repeat("34", 32)
	if _, err := p.Status(context.Background(), id, wrong); !errors.Is(err, ErrExpired) {
		t.Fatal(err)
	}
	if _, err := p.Status(context.Background(), id, seed); err != nil {
		t.Fatal(err)
	}
	now = now.Add(PairTTL)
	if _, err := p.Status(context.Background(), id, seed); !errors.Is(err, ErrExpired) {
		t.Fatal(err)
	}
}
