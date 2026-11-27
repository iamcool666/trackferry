package stream

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeFFmpeg copies its input ("-" = stdin) to its last argument, prefixed, or fails on "bad".
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ffmpeg")
	script := `#!/bin/sh
in=""; prev=""; for a in "$@"; do [ "$prev" = "-i" ] && in="$a"; prev="$a"; out="$a"; done
sleep 0.1
if [ "$in" = "-" ]; then data="$(cat)"; else data="$(cat "$in")"; fi
case "$data" in *bad*) echo "Invalid data" >&2; exit 1;; esac
printf 'MP3:%s' "$data" > "$out"
`
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMP3ConvertsOnceAndKeepsIt(t *testing.T) {
	m := NewMP3(fakeFFmpeg(t), t.TempDir(), 1<<20)
	src := filepath.Join(t.TempDir(), "a.flac")
	os.WriteFile(src, []byte("song"), 0o644)
	var wg sync.WaitGroup
	paths := make([]string, 3)
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := m.Convert(context.Background(), "k", src, nil)
			if err != nil {
				t.Error(err)
			}
			paths[i] = p
		}(i)
	}
	wg.Wait()
	if paths[0] == "" || paths[0] != paths[1] || paths[1] != paths[2] {
		t.Fatal(paths)
	}
	if b, _ := os.ReadFile(paths[0]); string(b) != "MP3:song" {
		t.Fatalf("%q", b)
	}
	os.Remove(src) // from now on only the kept file serves
	if p, err := m.Convert(context.Background(), "k", src, nil); err != nil || p != paths[0] {
		t.Fatal(p, err)
	}
}

func TestMP3FailureLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	m := NewMP3(fakeFFmpeg(t), dir, 1<<20)
	_, err := m.Convert(context.Background(), "k", "-", io.NopCloser(strings.NewReader("bad")))
	if err == nil || !strings.Contains(err.Error(), "Invalid data") {
		t.Fatal(err)
	}
	if es, _ := os.ReadDir(dir); len(es) != 0 {
		t.Fatal("left behind", es)
	}
	if _, ok := m.Cached("k"); ok {
		t.Fatal("a failure was kept")
	}
}

func TestMP3StaysUnderItsCap(t *testing.T) {
	dir := t.TempDir()
	m := NewMP3(fakeFFmpeg(t), dir, 25)
	for _, k := range []string{"a", "b", "c"} {
		if _, err := m.Convert(context.Background(), k, "-", io.NopCloser(strings.NewReader("0123456789"))); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := m.Cached("a"); ok {
		t.Fatal("the oldest should be gone")
	}
	if _, ok := m.Cached("c"); !ok {
		t.Fatal("the newest must stay")
	}
}

func TestMP3CallerGivesUpButTheConversionIsKept(t *testing.T) {
	m := NewMP3(fakeFFmpeg(t), t.TempDir(), 1<<20)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := m.Convert(ctx, "k", "-", io.NopCloser(strings.NewReader("x"))); err == nil {
		t.Fatal("should not wait")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := m.Cached("k"); ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the conversion was not kept")
}

func TestSongMP3OnlyForLossless(t *testing.T) {
	music := t.TempDir()
	os.WriteFile(filepath.Join(music, "a.mp3"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(music, "b.flac"), []byte("y"), 0o644)
	s := New("http://nas:1", music)
	s.MP3 = NewMP3(fakeFFmpeg(t), t.TempDir(), 1<<20)
	if _, err := s.SongMP3(context.Background(), "a.mp3", "", time.Minute); err != ErrNotLossless {
		t.Fatal(err)
	}
	h, err := s.SongMP3(context.Background(), "b.flac", "", time.Minute)
	if err != nil || !strings.HasSuffix(h.URL, ".mp3") || !h.Seekable() {
		t.Fatal(h, err)
	}
}
