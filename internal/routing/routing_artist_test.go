package routing

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"trackferry/internal/intent"
	"trackferry/internal/library"
	"trackferry/internal/online"
	"trackferry/internal/store"
)

// flacStart is the start of a real FLAC file: the marker and a STREAMINFO block header.
func flacStart() []byte {
	b := []byte("fLaC\x00\x00\x00\x22\x10\x00\x10\x00\x00\x00\x10\x00")
	for range 34 {
		b = append(b, 0)
	}
	return b
}

// onlineLibrary makes a library with one singer the online source does not have.
func onlineLibrary(t *testing.T) *library.Library {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "任贤齐 - 心太软.mp3"), []byte("no tags"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.DiscardHandler)
	lib := library.New(dir, st, log)
	if err := lib.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	return lib
}

// lxSinger points an online service at a fake server that answers a singer search with
// two songs; bad makes every stream no audio at all. searched counts every search the
// server saw, so a test can tell the online layer was never asked.
func lxSinger(t *testing.T, bad bool) (*online.Service, *atomic.Int64) {
	t.Helper()
	var searched, asked atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/search3":
			searched.Add(1)
			songs := []map[string]any{
				{"id": "kw_1", "title": "最炫民族风", "artist": "凤凰传奇", "duration": 294, "suffix": "mp3"},
				{"id": "kw_2", "title": "自由飞翔", "artist": "凤凰传奇", "duration": 254, "suffix": "mp3"},
			}
			json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{"status": "ok",
				"searchResult3": map[string]any{"song": songs}}})
		case "/rest/stream":
			asked.Add(1)
			b := flacStart()
			if bad {
				b = []byte{0x29, 0x17, 0x55, 0x67, 0x33, 0x46, 0x79, 0x6e} // the encrypted container
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(b)))
			w.Write(b)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	l := &online.Lx{BaseURL: srv.URL + "/rest", User: "u", Password: "p", Client: srv.Client()}
	return online.New(slog.New(slog.DiscardHandler), srv.Client(), l), &searched
}

// A singer the library cannot fill comes from the online source, the first song that
// plays taken and the rest queued under negative ids that carry their online keys.
func TestArtistOnlineFallback(t *testing.T) {
	ctx := context.Background()
	svc, _ := lxSinger(t, false)
	r := &Router{Library: onlineLibrary(t), Log: slog.New(slog.DiscardHandler), Openable: func(string) bool { return true }, Online: svc}
	rt := r.Decide(ctx, intent.Parse("播放凤凰传奇的歌"), "播放凤凰传奇的歌")
	if rt.Decision != Online || rt.Source != "lx" {
		t.Fatalf("%s %s (%s)", rt.Decision, rt.Source, rt.Reason)
	}
	if rt.Title != "最炫民族风" {
		t.Fatalf("taken %s, want the singer's first song", rt.Title)
	}
	if rt.List != "凤凰传奇的歌" {
		t.Fatalf("list %q, want the singer's own", rt.List)
	}
	q := rt.Queue()
	if len(q) != 2 {
		t.Fatalf("queue %v, want one id per queued song", q)
	}
	for _, id := range q {
		if id >= 0 {
			t.Fatalf("queue %v, want negative ids for online songs", q)
		}
	}
	rest := rt.OnlineQueue()
	if len(rest) != 2 || rest[0].Key != rt.Online {
		t.Fatalf("online queue %+v, want the taken song first with its key", rest)
	}
}

// A singer the library has never goes online: the library keeps the list.
func TestArtistLibraryHitNoOnline(t *testing.T) {
	ctx := context.Background()
	svc, searched := lxSinger(t, false)
	r := &Router{Library: onlineLibrary(t), Log: slog.New(slog.DiscardHandler), Openable: func(string) bool { return true }, Online: svc}
	rt := r.Decide(ctx, intent.Parse("播放任贤齐的歌"), "播放任贤齐的歌")
	if rt.Decision != Local {
		t.Fatalf("%s (%s)", rt.Decision, rt.Reason)
	}
	if n := searched.Load(); n != 0 {
		t.Fatalf("the online layer was asked %d times", n)
	}
}

// Without an online layer a singer the library lacks stays what it was: Xiao AI keeps it.
func TestArtistOnlineDisabled(t *testing.T) {
	ctx := context.Background()
	r := &Router{Library: onlineLibrary(t), Log: slog.New(slog.DiscardHandler), Openable: func(string) bool { return true }}
	rt := r.Decide(ctx, intent.Parse("播放凤凰传奇的歌"), "播放凤凰传奇的歌")
	if rt.Decision != Native || rt.Reason == "" {
		t.Fatalf("%s reason=%q", rt.Decision, rt.Reason)
	}
}

// A source that is switched off is not asked: what pick said stands.
func TestArtistOnlineSourceOff(t *testing.T) {
	ctx := context.Background()
	svc, searched := lxSinger(t, false)
	r := &Router{Library: onlineLibrary(t), Log: slog.New(slog.DiscardHandler), Openable: func(string) bool { return true }, Online: svc}
	svc.SetEnabled("lx", false)
	rt := r.Decide(ctx, intent.Parse("播放凤凰传奇的歌"), "播放凤凰传奇的歌")
	if rt.Decision != Native {
		t.Fatalf("%s (%s)", rt.Decision, rt.Reason)
	}
	if rt.Reason != "曲库里没有凤凰传奇的歌" {
		t.Fatalf("reason %q, want what pick said", rt.Reason)
	}
	if n := searched.Load(); n != 0 {
		t.Fatalf("a disabled source was asked %d times", n)
	}
}

// When the online source has the singer but nothing that plays, what pick said stands.
func TestArtistOnlineNothingPlaysKeepsReason(t *testing.T) {
	ctx := context.Background()
	svc, _ := lxSinger(t, true)
	r := &Router{Library: onlineLibrary(t), Log: slog.New(slog.DiscardHandler), Openable: func(string) bool { return true }, Online: svc}
	rt := r.Decide(ctx, intent.Parse("播放凤凰传奇的歌"), "播放凤凰传奇的歌")
	if rt.Decision != Native || rt.Reason != "曲库里没有凤凰传奇的歌" {
		t.Fatalf("%s reason=%q, want what pick said", rt.Decision, rt.Reason)
	}
}
