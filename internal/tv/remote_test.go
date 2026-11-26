package tv

import (
	"errors"
	"testing"
	"time"
)

func TestRemoteOnlyExposesLivePlaybackAndRequiresAcknowledgment(t *testing.T) {
	m := NewRemote()
	now := time.Now()
	m.now = func() time.Time { return now }
	h := Heartbeat{Epoch: "boot", Sequence: 1, Playback: Playback{Entry: "b", Revision: "list", Index: 1, Count: 2, Phase: "paused", Queue: []Ref{{ID: 1}, {ID: 2}}}}
	if _, err := m.Submit("tv", Command{Action: "play"}); !errors.Is(err, ErrOffline) {
		t.Fatal(err)
	}
	m.Sync("tv", h)
	if _, err := m.Submit("tv", Command{Action: "seek", Entry: "stale"}); !errors.Is(err, ErrMoved) {
		t.Fatal(err)
	}
	cmd, err := m.Submit("tv", Command{Action: "play", Entry: "b"})
	if err != nil {
		t.Fatal(err)
	}
	h.Sequence++
	h.Playback.Queue = nil
	if got := m.Sync("tv", h); got.Command == nil || got.Command.ID != cmd.ID || got.NeedQueue {
		t.Fatal(got)
	}
	if p, online := m.State("tv", true); !online || p.Queue[1].ID != 2 {
		t.Fatal(p, online)
	}
	if got := m.Status("tv", cmd.ID); got.State != "pending" {
		t.Fatal(got)
	}
	h.Result = &Result{ID: cmd.ID, State: "done"}
	m.Sync("tv", h) // duplicate sequence cannot acknowledge a command
	if got := m.Status("tv", cmd.ID); got.State != "pending" {
		t.Fatal(got)
	}
	h.Sequence++
	h.Playback.Phase = "playing"
	if got := m.Sync("tv", h); !got.ResultAccepted {
		t.Fatal("TV must know its confirmation was accepted", got)
	}
	next, err := m.Submit("tv", Command{Action: "pause"})
	if err != nil {
		t.Fatal(err)
	}
	m.Cancel("tv", next.ID)
	if got := m.Status("tv", cmd.ID); got.State != "done" {
		t.Fatal(got)
	}
	if got := m.Sync("tv", h); !got.ResultAccepted {
		t.Fatal("repeated confirmation should survive another command", got)
	}
	now = now.Add(OnlineFor + time.Millisecond)
	if _, online := m.State("tv", false); online {
		t.Fatal("offline TV is still available")
	}
}

func TestRemoteExpiryReconnectAndCancellation(t *testing.T) {
	m := NewRemote()
	now := time.Now()
	m.now = func() time.Time { return now }
	h := Heartbeat{Epoch: "old", Sequence: 1}
	m.Sync("tv", h)
	a, _ := m.Submit("tv", Command{Action: "load"})
	b, _ := m.Submit("tv", Command{Action: "load"})
	if got := m.Status("tv", a.ID); got.State != "superseded" {
		t.Fatal(got)
	}
	m.Cancel("tv", a.ID)
	if got := m.Status("tv", b.ID); got.State != "pending" {
		t.Fatal(got)
	}
	now = now.Add(CommandFor + 1)
	h.Sequence++
	h.Result = &Result{ID: b.ID, State: "done"}
	if got := m.Sync("tv", h); got.Command != nil || got.ResultAccepted {
		t.Fatal(got)
	}
	if got := m.Status("tv", b.ID); got.State != "failed" {
		t.Fatal(got)
	}
	c, _ := m.Submit("tv", Command{Action: "play"})
	m.Sync("tv", Heartbeat{Epoch: "new", Sequence: 1})
	h.Sequence++
	h.Result = &Result{ID: c.ID, State: "done"}
	m.Sync("tv", h)
	if got := m.Status("tv", c.ID); got.State != "failed" {
		t.Fatal(got)
	}
}
