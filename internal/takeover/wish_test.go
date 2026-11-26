package takeover

import (
	"context"
	"errors"
	"testing"

	"trackferry/internal/routing"
)

func wishOf(title string, err error) Wish {
	return Wish{Title: title, Route: func(context.Context) (*routing.Route, error) { return routing.ForTest(title, "人"), err }}
}

func TestWishesComeFirst(t *testing.T) {
	d, sp, q, asked := queueSetup(t)
	ctx := context.Background()
	if err := d.SetPlayMode(ctx, "d1", ModeLoop); err != nil {
		t.Fatal(err)
	}
	q.ids, q.order, q.pos = []int64{1, 2, 4}, []int64{1, 2, 4}, 0
	if ok, _ := d.Want("other", wishOf("x", nil)); ok {
		t.Fatal("queued on a speaker that plays nothing")
	}
	for _, w := range []Wish{wishOf("乙", nil), wishOf("甲", nil), wishOf("坏的", errors.New("没了"))} {
		if ok, err := d.Want("d1", w); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	if n := d.Queue("d1").Next; len(n) != 3 || n[0] != "坏的" || n[1] != "甲" {
		t.Fatal(n)
	}
	d.turn(q)(EndPlayed) // the bad one is passed over
	if r := sp.last(); r.Song.Title != "甲" || r.Song.List != "" || q.pos != 0 {
		t.Fatalf("%+v", r.Song)
	}
	if err := d.Skip(ctx, "d1", 1); err != nil || sp.last().Song.Title != "乙" {
		t.Fatal(err, sp.last().Song)
	}
	// Back from a wish is where the list stands.
	if err := d.Skip(ctx, "d1", -1); err != nil || (*asked)[len(*asked)-1] != 1 || sp.last().Song.List == "" {
		t.Fatal(err, *asked)
	}
	sp.last().Ended(EndPlayed)
	if (*asked)[len(*asked)-1] != 2 || q.pos != 1 {
		t.Fatal(*asked, q.pos)
	}
}

func TestWishAfterSingleSong(t *testing.T) {
	d, sp, _, _ := queueSetup(t)
	end := d.After("d1", "LX06", 1, nil, true)
	d.Want("d1", wishOf("甲", nil))
	end(EndPlayed)
	if sp.last().Song.Title != "甲" {
		t.Fatal(sp.last().Song)
	}
	sp.last().Ended(EndPlayed)
	if len(sp.started) != 1 || d.Queue("d1") != nil {
		t.Fatal("a single song and its wish are all")
	}
}
