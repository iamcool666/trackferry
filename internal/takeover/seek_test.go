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

func TestQueuedSeekHonorsCancellationAndRun(t *testing.T) {
	e, speaker, _ := quick(t)
	request := req(speaker)
	request.Track.Length, request.Track.Seekable = time.Hour, true
	calls := make(chan time.Duration, 2)
	unblock := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(unblock)
		}
		e.Press(context.Background(), "d1", ControlStop)
	}()
	request.Track.Resume = func(ctx context.Context, at time.Duration) (Track, time.Duration, error) {
		calls <- at
		if at == time.Minute {
			select {
			case <-unblock:
			case <-ctx.Done():
				return Track{}, 0, ctx.Err()
			}
		}
		fetched := make(chan struct{})
		close(fetched)
		return Track{URL: "http://nas/stream/new.wav", Fetched: fetched}, at, nil
	}
	run := playing(t, e, speaker, request)
	if err := e.Seek(context.Background(), "d1", time.Minute, run.ID+1); !errors.Is(err, ErrSeekMoved) {
		t.Fatal(err)
	}
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- e.Seek(context.Background(), "d1", time.Minute, run.ID) }()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("first seek did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { second <- e.Seek(ctx, "d1", 2*time.Minute, run.ID) }()
	until(t, "second seek did not enter the queue", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return len(e.devs["d1"].seeks) > 0
	})
	cancel()
	if err := <-second; !errors.Is(err, ErrSeekLate) {
		t.Fatal(err)
	}
	close(unblock)
	released = true
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := e.Seek(context.Background(), "d1", 3*time.Minute, run.ID); err != nil {
		t.Fatal(err)
	}
	if at := <-calls; at != 3*time.Minute {
		t.Fatalf("cancelled seek reached the speaker: %v", at)
	}
}

func TestSeekDoesNotSucceedOnFetchAlone(t *testing.T) {
	e, speaker, _ := quick(t)
	request := req(speaker)
	request.Track.Length, request.Track.Seekable = time.Hour, true
	resume := make(chan struct{})
	request.Track.Resume = func(context.Context, time.Duration) (Track, time.Duration, error) {
		speaker.mu.Lock()
		speaker.statusOK = false
		speaker.mu.Unlock()
		close(resume)
		fetched := make(chan struct{})
		close(fetched)
		return Track{URL: "http://nas/stream/new.wav", Fetched: fetched}, time.Minute, nil
	}
	run := playing(t, e, speaker, request)
	defer e.Press(context.Background(), "d1", ControlStop)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- e.Seek(ctx, "d1", time.Minute, run.ID) }()
	select {
	case <-resume:
	case <-time.After(time.Second):
		t.Fatal("seek did not start")
	}
	select {
	case err := <-result:
		t.Fatalf("seek completed without a successful readback: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	cancel()
	if err := <-result; !errors.Is(err, ErrSeekLate) {
		t.Fatal(err)
	}
}
