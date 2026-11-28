package takeover

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"trackferry/internal/intent"
	"trackferry/internal/routing"
	"trackferry/internal/store"
	"trackferry/internal/stream"
)

// onlineQueueSetup is a queue of online songs: negative ids with the keys of the songs in
// outside, and an Online that resolves them anew the way a handover does.
func onlineQueueSetup(t *testing.T, gone map[string]bool) (*Dispatcher, *queueStarter, *queue) {
	t.Helper()
	d, sp, q, _ := queueSetup(t)
	q.ids, q.order = []int64{-1, -2}, []int64{-1, -2}
	q.outside = map[int64]Item{
		-1: {ID: -1, Online: "lx:kw_1", Title: "最炫民族风", Artist: "凤凰传奇"},
		-2: {ID: -2, Online: "lx:kw_2", Title: "自由飞翔", Artist: "凤凰传奇"},
	}
	d.Online = func(_ context.Context, key string) (*routing.Route, error) {
		if gone[key] {
			return nil, errors.New("找不到这首在线歌了，请重新搜索")
		}
		return routing.ForTest(map[string]string{"lx:kw_1": "最炫民族风", "lx:kw_2": "自由飞翔"}[key], "凤凰传奇"), nil
	}
	return d, sp, q
}

// After the first song the next one comes from its key, not from the library.
func TestOnlineQueueContinues(t *testing.T) {
	d, sp, q := onlineQueueSetup(t, nil)
	d.turn(q)(EndPlayed)
	if r := sp.last(); r.Song.Title != "自由飞翔" || r.Song.List != q.list {
		t.Fatalf("%+v, want the next online song under the list's name", r.Song)
	}
	if d.queued("d1") != q {
		t.Fatal("the queue did not go on")
	}
}

// A song whose key died is passed over: the queue falls back to the song it still has.
func TestOnlineQueueGone(t *testing.T) {
	d, sp, q := onlineQueueSetup(t, map[string]bool{"lx:kw_2": true})
	d.turn(q)(EndPlayed)
	if r := sp.last(); r.Song.Title != "最炫民族风" {
		t.Fatalf("%+v, want the song the queue still has", r.Song)
	}
	if d.queued("d1") != q {
		t.Fatal("one dead key ended the list")
	}
}

// Keys nobody can resolve end the list instead of looping on silence.
func TestOnlineQueueAllGone(t *testing.T) {
	d, sp, q := onlineQueueSetup(t, map[string]bool{"lx:kw_1": true, "lx:kw_2": true})
	d.turn(q)(EndPlayed)
	if len(sp.started) != 0 || d.queued("d1") != nil {
		t.Fatal(len(sp.started), d.queued("d1"))
	}
}

// artistDecider always answers with a singer's online list, the way the router does for a
// singer the library cannot fill.
type artistDecider struct{ calls atomic.Int32 }

func (f *artistDecider) Decide(_ context.Context, _ intent.Intent, _ string) *routing.Route {
	f.calls.Add(1)
	return routing.ForArtistTest("凤凰传奇", "最炫民族风", "自由飞翔")
}

// The voice path builds the queue from the route: the keys of a singer's online list ride
// with it, and each song is found anew when its turn comes.
func TestVoiceArtistListContinuesOnline(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.SyncDevices(ctx, []store.DiscoveredDevice{{DeviceID: "d1", Name: "音箱", Hardware: "LX06"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetDeviceMode(ctx, "d1", store.ModeTakeover); err != nil {
		t.Fatal(err)
	}
	fs := &queueStarter{}
	d := &Dispatcher{Engine: fs, Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Router: &artistDecider{}, Streams: stream.New("http://nas", t.TempDir()), Allowed: func(string) bool { return true },
		Open: func(*routing.Route) stream.Opener { return nil },
		Next: func(int64, string) (*routing.Route, bool) { return routing.ForTest("曲库歌", "人"), true },
		Online: func(_ context.Context, key string) (*routing.Route, error) {
			title := map[string]string{"lx:kw_1": "最炫民族风", "lx:kw_2": "自由飞翔"}[key]
			return routing.ForTest(title, "凤凰传奇"), nil
		}}
	c := store.Command{DeviceID: "d1", RequestKey: "a", Query: "播放凤凰传奇的歌", UpstreamMS: 1, PrevPollMS: 1, SeenMS: 2, IntentKind: "artist", Stage: store.StageObserved}
	if _, err := st.AddCommand(ctx, &c); err != nil {
		t.Fatal(err)
	}
	d.OnCommand(ctx, c, "LX06", store.ModeTakeover)
	if len(fs.started) != 1 || fs.started[0].Song.Title != "最炫民族风" || fs.started[0].Song.List != "凤凰传奇的歌" {
		t.Fatalf("%+v", fs.started)
	}
	fs.started[0].Ended(EndPlayed)
	if len(fs.started) != 2 || fs.started[1].Song.Title != "自由飞翔" || fs.started[1].Song.List != "凤凰传奇的歌" {
		t.Fatalf("%+v", fs.started)
	}
}

// In loop mode the online list starts over, never twice in a row.
func TestOnlineQueueLoop(t *testing.T) {
	ctx := context.Background()
	d, sp, q := onlineQueueSetup(t, nil)
	if err := d.Store.SetSetting(ctx, settingMode+"d1", ModeLoop); err != nil {
		t.Fatal(err)
	}
	if d.PlayMode(ctx, "d1") != ModeLoop {
		t.Fatal("loop mode did not stick")
	}
	d.turn(q)(EndPlayed)
	want := []string{"自由飞翔", "最炫民族风", "自由飞翔"}
	for i, w := range want {
		if sp.last().Song.Title != w {
			t.Fatalf("round %d: %s, want %s", i, sp.last().Song.Title, w)
		}
		if i < len(want)-1 {
			sp.last().Ended(EndPlayed)
		}
	}
	if d.queued("d1") != q {
		t.Fatal("the list stopped looping")
	}
}
