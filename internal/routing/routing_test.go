package routing

import (
	"context"
	"fmt"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"path/filepath"
	"testing"

	"trackferry/internal/intent"
	"trackferry/internal/library"
	"trackferry/internal/online"
	"trackferry/internal/store"
)

func TestLocalRouting(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	for _, n := range []string{"周杰伦 - 晴天", "Beyond - 海阔天空", "信乐团 - 海阔天空", "南征北战 - 我的天空"} {
		os.WriteFile(filepath.Join(dir, n+".mp3"), []byte("no tags in this little file"), 0o644)
	}
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := slog.New(slog.DiscardHandler)
	lib := library.New(dir, st, log)
	if err := lib.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	openable := true
	r := &Router{Library: lib, Log: log, Openable: func(string) bool { return openable }}
	for q, want := range map[string][2]string{
		"播放周杰伦的晴天":      {Local, "周杰伦"},
		"播放晴天周杰伦":       {Local, "周杰伦"},
		"播放海阔天空":        {Local, "Beyond"},
		"播放Beyond的海阔天空": {Local, "Beyond"},
		"播放我的天空":        {Local, "南征北战"},
		"播放周杰伦的歌":       {Local, "周杰伦"},
		"播放陈奕迅的晴天":      {Native, ""},
		"播放周结伦的晴天":      {Local, "周杰伦"}, // misheard by a character
		"播放海扩天空":        {Local, "Beyond"},
		"播放信乐团的海扩天空":    {Local, "信乐团"},
		"播放我的天宫":        {Local, "南征北战"},
		"播放周结伦的歌":       {Local, "周杰伦"},
		"播放周结伦的睛天":      {Native, ""}, // both: too far
		"播放不存在的歌名":      {Native, ""},
		"下一首":           {Native, ""},
	} {
		rt := r.Decide(ctx, intent.Parse(q), q)
		if rt.Decision != want[0] || rt.Artist != want[1] {
			t.Errorf("%s: %s %s (%s)", q, rt.Decision, rt.Artist, rt.Reason)
		}
		if rt.Decision == Native && rt.Reason == "" {
			t.Errorf("%s: native without a reason", q)
		}
	}
	if rt := r.Decide(ctx, intent.Parse("播放海阔天空"), ""); len(rt.Others) != 1 || rt.Others[0] != "信乐团" || rt.Layers[0].State != "hit" {
		t.Errorf("the other singer stays on offer: %+v", rt)
	}
	after := After(ctx, Previous{Title: "海阔天空", Artist: "Beyond", Tried: []string{"Beyond"}})
	for q, want := range map[string][3]string{
		"放信乐团的":      {Local, "海阔天空", "信乐团"},
		"不是这个，放信乐队的": {Local, "海阔天空", "信乐团"}, // one character misheard
		"换个版本":       {Local, "海阔天空", "信乐团"},
		"换成周杰伦的版本":   {Native, "", ""},
	} {
		if rt := r.Decide(after, intent.Parse(q), q); rt.Decision != want[0] || rt.Title != want[1] || rt.Artist != want[2] {
			t.Errorf("after 海阔天空, %s: %+v", q, rt)
		}
	}
	if rt := r.Decide(ctx, intent.Parse("放周杰伦的"), ""); rt.Artist != "周杰伦" {
		t.Errorf("without a song before, a singer is a singer: %+v", rt)
	}
	if rt := r.Decide(ctx, intent.Parse("换个版本"), ""); rt.Decision != Native {
		t.Errorf("%+v", rt)
	}
	rivals := []library.Match{{TrackRow: store.TrackRow{Title: "辞·九门回忆", Artist: "浅影阿"}}, {TrackRow: store.TrackRow{Title: "辞九门回忆", Artist: "等什么君"}}}
	if who := (Layer{Candidates: []Candidate{{Title: "辞九门回忆", Artist: "邓寓君(等什么君)"}}}).vote(rivals); who != "等什么君" {
		t.Errorf("vote %q", who)
	}
	openable = false
	if rt := r.Decide(ctx, intent.Parse("播放周杰伦的晴天"), ""); rt.Decision != Native || rt.Layers[0].State != "error" {
		t.Errorf("unreadable file: %+v", rt)
	}
}

func TestArtistRoutingFallsBackToLX(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := slog.New(slog.DiscardHandler)
	lib := library.New(dir, st, log)
	if err := lib.Scan(ctx); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/search3":
			json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{
				"status": "ok",
				"searchResult3": map[string]any{"song": []map[string]any{
					{"id": "kw_123", "title": "晴天", "artist": "周杰伦", "duration": 269, "suffix": "flac"},
				}},
			}})
		case "/rest/stream":
			w.Header().Set("Content-Type", "audio/flac")
			_, _ = w.Write([]byte("fLaC"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	lx := &online.Lx{BaseURL: srv.URL + "/rest", User: "u", Password: "p", Client: srv.Client()}
	r := &Router{Library: lib, Online: online.New(log, online.Client(), lx), Log: log}
	rt := r.Decide(ctx, intent.Parse("播放周杰伦的歌"), "播放周杰伦的歌")
	if rt.Decision != Online || rt.Source != "lx" || rt.Title != "晴天" || rt.Artist != "周杰伦" {
		t.Fatalf("artist request did not fall back to LX: %+v", rt)
	}
	if len(rt.Layers) < 2 || rt.Layers[1].Name != "lx" || rt.Layers[1].State != online.StateHit {
		t.Fatalf("LX route layer = %+v", rt.Layers)
	}
	if len(rt.OnlineQueue()) != 1 || rt.OnlineQueue()[0].Online != "lx:kw_123" {
		t.Fatalf("online artist queue = %+v", rt.OnlineQueue())
	}
}

func TestArtistRoutingKeepsLocalFirstAndAddsLXQueue(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	for _, name := range []string{"周杰伦 - 晴天", "周杰伦 - 稻香"} {
		if err := os.WriteFile(filepath.Join(dir, name+".mp3"), []byte("no tags in this little file"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := slog.New(slog.DiscardHandler)
	lib := library.New(dir, st, log)
	if err := lib.Scan(ctx); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/search3" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{
			"status": "ok",
			"searchResult3": map[string]any{"song": []map[string]any{
				{"id": "kw_1", "title": "晴天", "artist": "周杰伦", "duration": 269, "suffix": "flac"},
				{"id": "kw_2", "title": "七里香", "artist": "周杰伦", "duration": 300, "suffix": "flac"},
				{"id": "kg_3", "title": "青花瓷", "artist": "周杰伦", "duration": 240, "suffix": "mp3"},
			}},
		}})
	}))
	defer srv.Close()

	lx := &online.Lx{BaseURL: srv.URL + "/rest", User: "u", Password: "p", Client: srv.Client()}
	r := &Router{Library: lib, Online: online.New(log, online.Client(), lx), Log: log}
	rt := r.Decide(ctx, intent.Parse("播放周杰伦的歌"), "播放周杰伦的歌")
	if rt.Decision != Local || rt.Source != "library" || rt.Artist != "周杰伦" {
		t.Fatalf("local artist should remain first: %+v", rt)
	}
	if len(rt.Queue()) != 2 {
		t.Fatalf("local queue = %+v", rt.Queue())
	}
	onlineQueue := rt.OnlineQueue()
	if len(onlineQueue) != 2 {
		t.Fatalf("LX should supplement the local list and skip duplicate 晴天: %+v", onlineQueue)
	}
	if onlineQueue[0].Title != "七里香" || onlineQueue[1].Title != "青花瓷" {
		t.Fatalf("online queue order = %+v", onlineQueue)
	}
	if rt.List != "周杰伦的歌" || rt.Reason != "" {
		t.Fatalf("artist list metadata = %+v", rt)
	}
}

func TestArtistQueueCountsOnlyPlayableLocalSongs(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("周杰伦 - 本地歌曲%02d", i)
		if err := os.WriteFile(filepath.Join(dir, name+".mp3"), []byte("no tags in this little file"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := slog.New(slog.DiscardHandler)
	lib := library.New(dir, st, log)
	if err := lib.Scan(ctx); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/search3" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{
			"status": "ok",
			"searchResult3": map[string]any{"song": []map[string]any{
				{"id": "kw_20", "title": "在线补充曲", "artist": "周杰伦", "duration": 240, "suffix": "flac"},
			}},
		}})
	}))
	defer srv.Close()

	lx := &online.Lx{BaseURL: srv.URL + "/rest", User: "u", Password: "p", Client: srv.Client()}
	r := &Router{
		Library: lib,
		Online:  online.New(log, online.Client(), lx),
		Log:     log,
		Openable: func(path string) bool {
			return filepath.Base(path) != "周杰伦 - 本地歌曲00.mp3"
		},
	}
	rt := r.Decide(ctx, intent.Parse("播放周杰伦的歌"), "播放周杰伦的歌")
	if rt.Decision != Local || len(rt.Queue()) != 19 {
		t.Fatalf("playable local queue should contain 19 tracks: decision=%s queue=%d", rt.Decision, len(rt.Queue()))
	}
	onlineQueue := rt.OnlineQueue()
	if len(onlineQueue) != 1 || onlineQueue[0].Title != "在线补充曲" {
		t.Fatalf("LX should fill the one missing playable slot: %+v", onlineQueue)
	}
	if !strings.Contains(rt.Layers[0].Detail, "19 首本地歌曲 + 1 首 LX 在线歌曲") {
		t.Fatalf("queue detail should report playable local and LX counts: %+v", rt.Layers[0])
	}
}

func TestArtistSearchRunsLXAlongsideFullLocalLibrary(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	for i := 0; i < artistQueueTarget; i++ {
		name := fmt.Sprintf("周杰伦 - 本地歌曲%02d", i)
		if err := os.WriteFile(filepath.Join(dir, name+".mp3"), []byte("no tags in this little file"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := slog.New(slog.DiscardHandler)
	lib := library.New(dir, st, log)
	if err := lib.Scan(ctx); err != nil {
		t.Fatal(err)
	}

	requested := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/search3" {
			http.NotFound(w, r)
			return
		}
		select {
		case requested <- struct{}{}:
		default:
		}
		json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{
			"status": "ok",
			"searchResult3": map[string]any{"song": []map[string]any{
				{"id": "kw_online", "title": "在线歌曲", "artist": "周杰伦", "duration": 240, "suffix": "flac"},
			}},
		}})
	}))
	defer srv.Close()

	lx := &online.Lx{BaseURL: srv.URL + "/rest", User: "u", Password: "p", Client: srv.Client()}
	r := &Router{Library: lib, Online: online.New(log, online.Client(), lx), Log: log}
	rt := r.Decide(ctx, intent.Parse("播放周杰伦的歌"), "播放周杰伦的歌")
	select {
	case <-requested:
	default:
		t.Fatal("LX singer search was skipped because the local queue was already full")
	}
	if rt.Decision != Local || len(rt.Queue()) != artistQueueTarget {
		t.Fatalf("local queue should still be playable: decision=%s queue=%d", rt.Decision, len(rt.Queue()))
	}
}
