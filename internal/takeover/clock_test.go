package takeover

import (
	"testing"
	"time"

	"trackferry/internal/live"
	"trackferry/internal/xiaomi"
)

// The clock is read before the speaker is asked, and asking takes time: what is published must be
// where the song is when it is published, or a page that takes the song over starts behind it.
func TestPublishedClockIsCurrent(t *testing.T) {
	now := time.Unix(1000, 0)
	e := &Engine{now: func() time.Time { return now }}
	hub := live.New()
	tr := hub.Begin(live.Flow{DeviceID: "d"})
	r := &runner{e: e, req: Request{DeviceID: "d", Trace: tr}, player: &xiaomi.PlayStatus{Status: 1}}
	r.elapsed, r.elapsedAt = 10*time.Second, now
	now = now.Add(1200 * time.Millisecond) // the speaker took this long to answer
	r.publishClock()
	if got := hub.Flows()[0].Player.Elapsed; got != 11200 {
		t.Fatalf("published %d ms, want 11200", got)
	}
	// A song that does not sound stands still.
	r.held = true
	now = now.Add(5 * time.Second)
	r.publishClock()
	if got := hub.Flows()[0].Player.Elapsed; got != 10000 {
		t.Fatalf("held clock moved to %d ms", got)
	}
}
