package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOnlineSongsSurviveReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RememberOnline(ctx, map[string]string{"kuwo:1": `{"source":"kuwo","id":"1"}`}); err != nil {
		t.Fatal(err)
	}
	if err := s.RememberOnline(ctx, map[string]string{"kuwo:1": `{"source":"kuwo","id":"1","title":"晴天"}`}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	song, ok, err := s.RecallOnline(ctx, "kuwo:1")
	if err != nil || !ok || song != `{"source":"kuwo","id":"1","title":"晴天"}` {
		t.Fatalf("recall = %q %v %v", song, ok, err)
	}
	if _, ok, err := s.RecallOnline(ctx, "kuwo:2"); ok || err != nil {
		t.Fatalf("unknown key: %v %v", ok, err)
	}
}
