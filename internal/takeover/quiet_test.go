package takeover

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"trackferry/internal/store"
)

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal(what)
		}
	}
}

func TestQuietMutesAtOnceAndGivesBackWhatNobodyTook(t *testing.T) {
	e, sp, st := setup(t)
	done := e.Quiet("d1")
	until(t, "not muted", func() bool { return sp.lastVolume() == 0 })
	done(true)
	until(t, "not given back", func() bool { return sp.lastVolume() == 40 })
	until(t, "still pending", func() bool { _, ok, _ := st.PendingVolume(context.Background(), "d1"); return !ok })
}

func TestQuietLeavesTheVolumeToTheRun(t *testing.T) {
	e, sp, _ := setup(t)
	done := e.Quiet("d1")
	until(t, "not muted", func() bool { return sp.lastVolume() == 0 })
	e.Start(context.Background(), req(sp))
	done(false)
	until(t, "not restored by the run", func() bool { return sp.lastVolume() == 40 })
}

func TestTwoQuietsGiveBackOnce(t *testing.T) {
	e, sp, _ := setup(t)
	a, b := e.Quiet("d1"), e.Quiet("d1")
	until(t, "not muted", func() bool { return sp.lastVolume() == 0 })
	a(true)
	time.Sleep(50 * time.Millisecond)
	if sp.lastVolume() != 0 {
		t.Fatal("given back while the other still decides")
	}
	b(true)
	until(t, "not given back", func() bool { return sp.lastVolume() == 40 })
}

func TestKnownVolumeNeedsNoAnswerFirst(t *testing.T) {
	e, sp, st := setup(t)
	e.know("d1", 33)
	m := e.mute(context.Background(), "d1")
	if !m.muted || m.volume == 0 || sp.lastVolume() != 0 {
		t.Fatalf("%+v", m)
	}
	if v, ok, _ := st.PendingVolume(context.Background(), "d1"); !ok || v != m.volume {
		t.Fatal(v, ok)
	}
}

func TestStrongMuteIsLiftedAfterwards(t *testing.T) {
	e, sp, _ := setup(t)
	var on atomic.Bool
	var calls atomic.Int32
	e.Style = func(string) string { return HushStrong }
	e.Deep = func(_ context.Context, _ string, v bool) error {
		if v {
			time.Sleep(50 * time.Millisecond) // slower than what follows
		}
		on.Store(v)
		calls.Add(1)
		return nil
	}
	e.Run(context.Background(), req(sp))
	if calls.Load() < 2 || on.Load() || sp.lastVolume() != 40 {
		t.Fatal(calls.Load(), on.Load(), sp.lastVolume())
	}
}

// LX06 drops the stream it plays when its mute is lifted: nothing of the mute may reach the song.
func TestMuteIsOverBeforeTheSongIsPushed(t *testing.T) {
	e, sp, _ := setup(t)
	e.Style = func(string) string { return HushStrong }
	fails := 1
	e.Deep = func(_ context.Context, _ string, on bool) error {
		sp.mu.Lock()
		defer sp.mu.Unlock()
		if !on && fails > 0 {
			fails--
			return errors.New("lost")
		}
		sp.log(map[bool]string{true: "deep", false: "lift"}[on])
		return nil
	}
	e.Run(context.Background(), req(sp))
	sp.mu.Lock()
	defer sp.mu.Unlock()
	lift, play := slices.Index(sp.calls, "lift"), slices.Index(sp.calls, "play")
	if lift < 0 || play < lift || slices.Contains(sp.calls[play:], "lift") || slices.Contains(sp.calls[play:], "deep") || sp.volume != 40 {
		t.Fatal(sp.calls, sp.volume)
	}
}

// A mute still on afterwards is lifted before the volume returns, which the lift may change.
func TestLiftingAMuteThatBringsBackSilence(t *testing.T) {
	e, sp, _ := setup(t)
	e.device("d1").deepOn = true
	e.Deep = func(context.Context, string, bool) error {
		time.AfterFunc(100*time.Millisecond, func() { sp.mu.Lock(); sp.volume = 0; sp.mu.Unlock() })
		return nil
	}
	e.putBack("d1", 40)
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.volume != 40 {
		t.Fatal(sp.volume, sp.volumes)
	}
}

func TestWaitsForTheAnswerToEnd(t *testing.T) {
	e, sp, _ := setup(t)
	e.timing.Speak, e.timing.Pause = 10*time.Millisecond, 0
	e.Style = func(string) string { return HushWait }
	e.Deep = func(context.Context, string, bool) error { return nil }
	r := req(sp)
	r.Answer, r.UpstreamMS = 50, time.Now().UnixMilli()
	run, _ := e.Run(context.Background(), r)
	if s := steps(run); !strings.Contains(s, "stop2,wait,stop2,play") {
		t.Fatal(s)
	}
}

func playing(t *testing.T, e *Engine, sp *fakeSpeaker, r Request) store.TakeoverRun {
	t.Helper()
	run, _ := e.Start(context.Background(), r)
	d := e.device("d1")
	until(t, "never played", func() bool { e.mu.Lock(); defer e.mu.Unlock(); return d.watching })
	return run
}

func quick(t *testing.T) (*Engine, *fakeSpeaker, *store.Store) {
	e, sp, st := setup(t)
	e.timing.Look, e.timing.LookClose, e.timing.Pause = 10*time.Millisecond, 10*time.Millisecond, 30*time.Millisecond
	return e, sp, st
}

func TestStopEndsTheSongAndTheRun(t *testing.T) {
	e, sp, _ := quick(t)
	r := req(sp)
	r.Track.Length = time.Hour
	ended := make(chan End, 1)
	r.Ended = func(h End) { ended <- h }
	playing(t, e, sp, r)
	if err := e.Press(context.Background(), "d1", ControlStop); err != nil {
		t.Fatal(err)
	}
	select {
	case h := <-ended:
		if h != EndOther {
			t.Fatal(h)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the run goes on")
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.playing != "" {
		t.Fatal("still playing", sp.playing)
	}
}

func TestStopWhileTheSongIsBeingStarted(t *testing.T) {
	e, sp, st := setup(t)
	e.timing.Settle = 300 * time.Millisecond
	run, _ := e.Start(context.Background(), req(sp))
	time.Sleep(20 * time.Millisecond)
	e.Press(context.Background(), "d1", ControlPause) // too early to pause: it stops
	until(t, "the run goes on", func() bool {
		x, _ := st.Run(context.Background(), run.ID)
		return x.Outcome == store.OutcomeCancelled && strings.Contains(x.Detail, "停止")
	})
	until(t, "volume lost", func() bool { return sp.lastVolume() == 40 })
}

// The fake speaker obeys stop only.
func TestPauseThatIsIgnoredStopsAndPlayContinues(t *testing.T) {
	e, sp, _ := quick(t)
	r := req(sp)
	r.Track.Length = time.Hour
	resumed := make(chan time.Duration, 1)
	r.Track.Resume = func(_ context.Context, at time.Duration) (Track, time.Duration, error) {
		resumed <- at
		return Track{URL: "http://nas/stream/y.wav"}, at, nil
	}
	playing(t, e, sp, r)
	if err := e.Press(context.Background(), "d1", ControlPause); err != nil {
		t.Fatal(err)
	}
	sp.mu.Lock()
	if sp.playing != "" {
		t.Fatal("still playing")
	}
	sp.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	select {
	case <-resumed:
		t.Fatal("continued by itself")
	default:
	}
	if err := e.Press(context.Background(), "d1", ControlPlay); err != nil {
		t.Fatal(err)
	}
	select {
	case <-resumed:
	case <-time.After(2 * time.Second):
		t.Fatal("not continued")
	}
	e.Press(context.Background(), "d1", ControlStop)
}
