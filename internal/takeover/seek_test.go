package takeover

import (
	"context"
	"errors"
	"testing"
	"time"
)

func seekable(t *testing.T, e *Engine, sp *fakeSpeaker) chan time.Duration {
	t.Helper()
	r := req(sp)
	r.Track.Length, r.Track.Seekable = time.Hour, true
	asked := make(chan time.Duration, 4)
	r.Track.Resume = func(_ context.Context, at time.Duration) (Track, time.Duration, error) {
		asked <- at
		if at > 50*time.Minute {
			return Track{}, 0, errors.New("no frame")
		}
		f := make(chan struct{})
		close(f)
		return Track{URL: "http://nas/stream/y.mp3", Fetched: f}, at, nil
	}
	playing(t, e, sp, r)
	return asked
}

func TestSeekMovesTheSong(t *testing.T) {
	e, sp, _ := quick(t)
	asked := seekable(t, e, sp)
	ctx := context.Background()
	if err := e.Seek(ctx, "d1", 20*time.Minute); err != nil || <-asked != 20*time.Minute {
		t.Fatal(err)
	}
	// What cannot be started there plays on.
	if err := e.Seek(ctx, "d1", 55*time.Minute); !errors.Is(err, ErrCannotSeek) || <-asked != 55*time.Minute {
		t.Fatal(err)
	}
	e.Press(ctx, "d1", ControlStop)
	until(t, "the run goes on", func() bool { return errors.Is(e.Seek(ctx, "d1", time.Minute), ErrNoSeek) })
}

func TestSeekWhilePausedWaitsForPlay(t *testing.T) {
	e, sp, _ := quick(t)
	asked := seekable(t, e, sp)
	ctx := context.Background()
	if err := e.Press(ctx, "d1", ControlPause); err != nil {
		t.Fatal(err)
	}
	until(t, "not paused", func() bool { sp.mu.Lock(); defer sp.mu.Unlock(); return sp.playing == "" })
	time.Sleep(50 * time.Millisecond)
	if err := e.Seek(ctx, "d1", 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
		t.Fatal("started by itself")
	case <-time.After(60 * time.Millisecond):
	}
	if err := e.Press(ctx, "d1", ControlPlay); err != nil {
		t.Fatal(err)
	}
	select {
	case at := <-asked:
		if at != 10*time.Minute {
			t.Fatal(at)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("not continued")
	}
	e.Press(ctx, "d1", ControlStop)
}
