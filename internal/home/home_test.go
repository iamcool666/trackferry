package home

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"trackferry/internal/live"
	"trackferry/internal/routing"
	"trackferry/internal/store"
	"trackferry/internal/xiaomi"
)

type world struct {
	mu      sync.Mutex
	devs    []store.Device
	pending map[string]int
	status  xiaomi.PlayStatus
	err     error
	asked   int
}

func start(t *testing.T) (*Tracker, *live.Hub, *world) {
	t.Helper()
	w := &world{pending: map[string]int{}, status: xiaomi.PlayStatus{Volume: 30, Position: -1},
		devs: []store.Device{{DeviceID: "d1", Alias: "客厅", Hardware: "LX06", MiotDID: "9", Presence: "online", Mode: store.ModeTakeover, InAccount: true}}}
	hub := live.New()
	tr := New(Source{Hub: hub,
		Devices: func(context.Context) ([]store.Device, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			return append([]store.Device(nil), w.devs...), nil
		},
		Pending: func(context.Context) (map[string]int, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			out := map[string]int{}
			for k, v := range w.pending {
				out[k] = v
			}
			return out, nil
		},
		Status: func(context.Context, string) (xiaomi.PlayStatus, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.asked++
			return w.status, w.err
		}}, slog.New(slog.DiscardHandler), Cadence{Tick: 10 * time.Millisecond, Merge: 5 * time.Millisecond, Busy: 20 * time.Millisecond, Idle: 30 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go tr.Run(ctx)
	return tr, hub, w
}

func begin(hub *live.Hub, title string, run int64) *live.Trace {
	tr := hub.Begin(live.Flow{Kind: "command", DeviceID: "d1", Said: "播放" + title, UpstreamMS: time.Now().UnixMilli()})
	tr.Route(routing.Route{Decision: "take", Source: "library", Title: title, Artist: "周杰伦", TrackID: run, Cover: "c" + title, DurationMS: 200000})
	tr.Run(store.TakeoverRun{ID: run, DeviceID: "d1", StartedMS: time.Now().UnixMilli()})
	return tr
}

// next waits for a message whose speaker satisfies ok.
func next(t *testing.T, ch <-chan []byte, event string, ok func(Speaker) bool) {
	t.Helper()
	end := time.After(3 * time.Second)
	for {
		select {
		case m, open := <-ch:
			if !open {
				t.Fatal("closed")
			}
			_, data, _ := strings.Cut(string(m), "data: ")
			var got struct {
				Speaker  Speaker
				Speakers []Speaker
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &got); err != nil {
				t.Fatal(err)
			}
			s := got.Speaker
			if len(got.Speakers) > 0 {
				s = got.Speakers[0]
			}
			if strings.Contains(string(m), "event: "+event+"\n") && ok(s) {
				return
			}
		case <-end:
			t.Fatal("nothing like it arrived")
		}
	}
}

func title(s Speaker) string {
	if s.Title == nil {
		return ""
	}
	return *s.Title
}

func TestOursFromStartToEnd(t *testing.T) {
	tr, hub, w := start(t)
	ch, stop := tr.Subscribe(context.Background())
	defer stop()
	next(t, ch, "snapshot", func(s Speaker) bool { return s.State == Idle && s.Volume != nil && *s.Volume == 30 && s.Title == nil })

	w.mu.Lock()
	w.pending["d1"] = 42 // muted for the start
	w.mu.Unlock()
	f := begin(hub, "晴天", 7)
	next(t, ch, "speaker", func(s Speaker) bool {
		return title(s) == "晴天" && s.State == Idle && s.Activity != nil && *s.Activity == "starting" && *s.Volume == 42 &&
			*s.Lyrics == "/api/ha/lyrics/7" && s.Request != nil && s.CoverVersion != nil && s.Request.Text == "播放晴天" && *s.CoverVersion == "c晴天" && s.PositionMS == nil
	})
	f.Player(live.Player{Status: 1, Volume: 0, Ours: true, Elapsed: 5000, Phase: "playing", AtMS: time.Now().UnixMilli()})
	next(t, ch, "speaker", func(s Speaker) bool {
		return s.State == Playing && s.Activity == nil && *s.PositionMS == 5000 && s.Rate == 1 && *s.SourceKind == Ours && *s.Volume == 42 && !*s.Muted
	})
	// Moving on as expected is no news; a jump is.
	f.Player(live.Player{Status: 1, Ours: true, Elapsed: 60000, Phase: "playing"})
	next(t, ch, "speaker", func(s Speaker) bool { return *s.PositionMS == 60000 })
	f.Player(live.Player{Status: 2, Held: true, Elapsed: 61000, Phase: "talking"})
	next(t, ch, "speaker", func(s Speaker) bool { return s.State == Paused && *s.Activity == "talking" && s.Rate == 0 })

	// The next song takes everything over.
	f.Done("")
	g := begin(hub, "七里香", 8)
	next(t, ch, "speaker", func(s Speaker) bool { return title(s) == "七里香" && s.PositionMS == nil && *s.PlayID == 8 })
	g.Done("")
	next(t, ch, "speaker", func(s Speaker) bool {
		return s.State == Idle && s.Title == nil && s.Cover == nil && s.Lyrics == nil && s.PlayID == nil && s.Request != nil
	})
}

func TestOthersAreNotGuessed(t *testing.T) {
	tr, _, w := start(t)
	w.status = xiaomi.PlayStatus{Status: xiaomi.PlayPlaying, Volume: 20, AudioID: "77", Position: 1000, Duration: 90000}
	ch, stop := tr.Subscribe(context.Background())
	defer stop()
	next(t, ch, "snapshot", func(s Speaker) bool {
		return s.State == Playing && *s.SourceKind == XiaoAI && s.Title == nil && *s.DurationMS == 90000
	})
	w.mu.Lock()
	w.status = xiaomi.PlayStatus{Status: xiaomi.PlayPaused, Volume: 20}
	w.mu.Unlock()
	next(t, ch, "speaker", func(s Speaker) bool { return s.State == Paused && *s.SourceKind == Unknown && s.DurationMS == nil })
	w.mu.Lock()
	w.devs[0].Mode = store.ModeOff
	w.mu.Unlock()
	next(t, ch, "speaker", func(s Speaker) bool { return s.State == Off && s.Volume == nil && len(s.Controls) == 0 })
	w.mu.Lock()
	w.devs = append(w.devs, store.Device{DeviceID: "d2", InAccount: true, Mode: store.ModeOff})
	w.mu.Unlock()
	next(t, ch, "snapshot", func(Speaker) bool { return true })
}

func TestAsksOnlyWhileListenedTo(t *testing.T) {
	tr, _, w := start(t)
	time.Sleep(100 * time.Millisecond)
	if w.asked != 0 {
		t.Fatal("asked without a listener")
	}
	ch, stop := tr.Subscribe(context.Background())
	time.Sleep(150 * time.Millisecond)
	tr.Drop()
	for range ch {
	}
	stop() // closing twice is fine
	time.Sleep(30 * time.Millisecond)
	w.mu.Lock()
	n := w.asked
	w.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	w.mu.Lock()
	defer w.mu.Unlock()
	if n < 2 || w.asked != n {
		t.Fatal(n, w.asked)
	}
}

func TestReconnectBeginsWithEverything(t *testing.T) {
	tr, hub, _ := start(t)
	_, stop := tr.Subscribe(context.Background())
	stop()
	begin(hub, "晴天", 7)
	ch, stop := tr.Subscribe(context.Background())
	defer stop()
	next(t, ch, "snapshot", func(s Speaker) bool { return title(s) == "晴天" })
}

func TestSilentSpeakerIsStale(t *testing.T) {
	tr, _, w := start(t)
	w.err = errors.New("no")
	if s := tr.Snapshot(context.Background()).Speakers[0]; !s.Stale || s.Volume != nil || s.State != Idle {
		t.Fatalf("%+v", s)
	}
}

func TestLyrics(t *testing.T) {
	ok, l := Lyrics("\ufeff[ti:x]\n[offset:500]\n[00:10.00][00:01.5]甲\r\n[00:05.123]乙\n[00:06]\n")
	if !ok || len(l) != 3 || *l[0].T != 1000 || l[0].Text != "甲" || *l[1].T != 4623 || *l[2].T != 9500 {
		t.Fatal(ok, l)
	}
	if ok, l = Lyrics("甲\n\n乙"); ok || len(l) != 2 || l[0].T != nil {
		t.Fatal(ok, l)
	}
	if ok, l = Lyrics(""); ok || l == nil {
		t.Fatal("want an empty list")
	}
	ok, l = Lyrics("[00:01.25][00:02.5]<00:01.25>原文\r\n[00:01:250]译文\r\n[00:02.5]原文\r\n[offset:+150]")
	if !ok || len(l) != 2 || *l[0].T != 1100 || l[0].Text != "原文\n译文" || *l[1].T != 2350 || l[1].Text != "原文" {
		t.Fatal(ok, l)
	}
}

func TestColors(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := range 100 {
		for x := range 100 {
			c := color.RGBA{200, 30, 30, 255}
			if x > 60 {
				c = color.RGBA{20, 60, 180, 255}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	if got := Colors(b.Bytes()); len(got) != 2 || got[0] != "#c81e1e" || got[1] != "#143cb4" {
		t.Fatal(got)
	}
	if Colors([]byte("RIFF")) != nil {
		t.Fatal("unreadable")
	}
}
