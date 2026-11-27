package takeover

import (
	"testing"
	"time"
)

func TestListEndsWhenItsTimeIsUp(t *testing.T) {
	d, sp, q, _ := queueSetup(t)
	q.until = time.Now().Add(-time.Second)
	d.turn(q)(EndPlayed)
	if len(sp.started) != 0 || d.queues["d1"] != nil {
		t.Fatal("went on")
	}
}

func TestRandomSongsAlwaysFollowASingleOne(t *testing.T) {
	d, sp, _, asked := queueSetup(t)
	d.Random = func(int) []int64 { return []int64{7, 8} }
	d.After("d1", "LX06", 7, nil, false)(EndPlayed)
	if len(sp.started) != 1 || (*asked)[0] != 8 { // 7 has just been heard
		t.Fatal(*asked)
	}
	if d.queues["d1"].until.IsZero() {
		t.Fatal("no end")
	}
}
