package takeover

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"trackferry/internal/routing"
)

func TestButtonsMoveThroughAList(t *testing.T) {
	d, sp, q, asked := queueSetup(t)
	ctx := context.Background()
	old := d.turn(q)
	for _, by := range []int{1, 1, -1} { // 2, then 4 as 3 cannot be read, and back
		if err := d.Skip(ctx, "d1", by); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(*asked, []int64{2, 3, 4, 3, 2}) || len(sp.started) != 3 || sp.last().Follows {
		t.Fatal(*asked)
	}
	old(EndPlayed) // ended while it was being skipped
	sp.started[0].Ended(EndOther)
	if len(sp.started) != 3 || d.queued("d1") != q {
		t.Fatal("an old song moved the list")
	}
	d.Drop("d1")
	if err := d.Skip(ctx, "d1", 1); !errors.Is(err, ErrNoQueue) {
		t.Fatal(err)
	}
}

func TestOneSongRepeats(t *testing.T) {
	d, sp, _, asked := queueSetup(t)
	ctx := context.Background()
	d.Random = func(int) []int64 { return []int64{8, 9} }
	if err := d.SetPlayMode(ctx, "d1", ModeOne); err != nil {
		t.Fatal(err)
	}
	// A song that is not in the library.
	end := d.After("d1", "LX06", 0, routing.ForTest("在线的", "人"), false)
	for range 3 {
		end(EndPlayed)
		end = sp.last().Ended
	}
	if len(sp.started) != 3 || len(*asked) != 0 || sp.last().Song.Title != "在线的" || !sp.last().Follows {
		t.Fatal(sp.started, *asked)
	}
	// Skipping still moves on, and back to it.
	if err := d.Skip(ctx, "d1", 1); err != nil || !slices.Equal(*asked, []int64{8}) {
		t.Fatal(err, *asked)
	}
	if err := d.Skip(ctx, "d1", -1); err != nil || sp.last().Song.Title != "在线的" {
		t.Fatal(err)
	}
	// Its time is limited, and so are failures.
	d.queued("d1").until = time.Now().Add(-time.Second)
	n := len(sp.started)
	sp.last().Ended(EndPlayed)
	if len(sp.started) != n || d.queued("d1") != nil {
		t.Fatal("went on")
	}
}

func TestOneSongThatStaysSilentIsGivenUp(t *testing.T) {
	d, sp, _, _ := queueSetup(t)
	d.SetPlayMode(context.Background(), "d1", ModeOne)
	end := d.After("d1", "LX06", 1, nil, true)
	for range 10 {
		n := len(sp.started)
		end(EndUnplayed)
		if len(sp.started) == n {
			break
		}
		end = sp.last().Ended
	}
	if len(sp.started) != maxFails-1 || d.queued("d1") != nil {
		t.Fatal(len(sp.started))
	}
}

func TestModeChangesKeepTheSong(t *testing.T) {
	d, _, _, _ := queueSetup(t)
	ctx := context.Background()
	if d.PlayMode(ctx, "d1") != ModeLoop {
		t.Fatal("untouched speakers should play in list order")
	}
	q := &queue{dev: "d1", list: "x", ids: []int64{5, 2, 9, 1}, pos: 2, order: []int64{1, 2, 5, 9}}
	d.enqueue(q)
	d.SetPlayMode(ctx, "d1", ModeLoop)
	if !slices.Equal(q.ids, []int64{1, 2, 5, 9}) || q.pos != 3 {
		t.Fatal(q.ids, q.pos)
	}
	if _, ok := d.step(q, 1); !ok || q.ids[q.pos] != 1 || !slices.Equal(q.ids, q.order) {
		t.Fatal("not in order", q.ids)
	}
	d.SetPlayMode(ctx, "d1", ModeShuffle)
	if q.ids[q.pos] != 1 || len(q.ids) != 4 || d.Queue("d1").Total != 4 {
		t.Fatal(q.ids)
	}
}
