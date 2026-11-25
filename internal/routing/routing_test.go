package routing

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"trackferry/internal/intent"
	"trackferry/internal/library"
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
