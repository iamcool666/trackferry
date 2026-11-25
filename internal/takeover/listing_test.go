package takeover

import (
	"context"
	"errors"
	"slices"
	"testing"

	"trackferry/internal/routing"
)

func TestListingJumpAndRemove(t *testing.T) {
	d, sp, q, asked := queueSetup(t)
	ctx := context.Background()
	q.ids, q.order, q.pos = []int64{1, 2, 5, 4}, []int64{1, 2, 4, 5}, 1
	songs := func() (out []int64) {
		l, _ := d.Listing(ctx, "d1")
		for _, s := range l.Songs {
			out = append(out, s.ID)
		}
		return out
	}
	if got := songs(); !slices.Equal(got, []int64{5, 4}) { // a shuffled list starts over in another order
		t.Fatal(got)
	}
	if err := d.Store.SetSetting(ctx, settingMode+"d1", ModeLoop); err != nil {
		t.Fatal(err)
	}
	if got := songs(); !slices.Equal(got, []int64{5, 4, 1}) {
		t.Fatal(got)
	}
	// One that came before: the song that is on stays the same.
	if err := d.Remove("d1", Pick{Slot: Slot{0, 1}}); err != nil || q.ids[q.pos] != 2 || !slices.Equal(q.order, []int64{2, 4, 5}) {
		t.Fatal(err, q.ids, q.pos, q.order)
	}
	// What the page knows is out of date by one place.
	if err := d.Remove("d1", Pick{Slot: Slot{3, 4}}); err != nil || !slices.Equal(q.ids, []int64{2, 5}) {
		t.Fatal(err, q.ids)
	}
	if err := d.Remove("d1", Pick{Slot: Slot{1, 4}}); !errors.Is(err, ErrMoved) {
		t.Fatal(err)
	}
	if err := d.Remove("d1", Pick{Slot: Slot{0, 2}}); err == nil {
		t.Fatal("took off the song that is on")
	}
	if err := d.Jump(ctx, "d1", Pick{Slot: Slot{1, 5}}); err != nil || q.pos != 1 || (*asked)[len(*asked)-1] != 5 || len(sp.started) != 1 {
		t.Fatal(err, q.pos, *asked)
	}
	if err := d.Jump(ctx, "d1", Pick{Slot: Slot{0, 9}}); !errors.Is(err, ErrMoved) || q.pos != 1 {
		t.Fatal(err)
	}
	if err := d.RemoveCurrent(ctx, "d1"); err != nil || !slices.Equal(q.ids, []int64{2}) || q.pos != 0 || len(sp.started) != 2 {
		t.Fatal(err, q.ids, q.pos)
	}
	if err := d.RemoveCurrent(ctx, "d1"); err == nil || len(q.ids) != 1 {
		t.Fatal("emptied the list", q.ids)
	}
	if _, ok := d.Listing(ctx, "d2"); ok {
		t.Fatal("a list on a speaker that plays nothing")
	}
}

func TestWishesCanBePickedAndTakenBack(t *testing.T) {
	d, sp, q, _ := queueSetup(t)
	ctx := context.Background()
	var took []string
	for _, n := range []string{"甲", "乙", "丙"} {
		if ok, err := d.Want("d1", Wish{Title: n, Route: func(context.Context) (*routing.Route, error) {
			took = append(took, n)
			return routing.ForTest(n, "人"), nil
		}}); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	l, _ := d.Listing(ctx, "d1")
	if len(l.Wishes) != 3 || l.Wishes[0].ID == l.Wishes[1].ID || l.Aside {
		t.Fatalf("%+v", l)
	}
	if err := d.Remove("d1", Pick{Wish: l.Wishes[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err := d.Remove("d1", Pick{Wish: l.Wishes[0].ID}); !errors.Is(err, ErrMoved) {
		t.Fatal(err)
	}
	if err := d.Jump(ctx, "d1", Pick{Wish: l.Wishes[2].ID}); err != nil || !slices.Equal(took, []string{"丙"}) || !q.aside || q.pos != 0 || len(sp.started) != 1 {
		t.Fatal(err, took, q.aside, q.pos)
	}
	if l, _ = d.Listing(ctx, "d1"); len(l.Wishes) != 1 || l.Wishes[0].Title != "乙" || !l.Aside {
		t.Fatalf("%+v", l)
	}
}
