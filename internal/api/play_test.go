package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"trackferry/internal/library"
	"trackferry/internal/routing"
	"trackferry/internal/store"
	"trackferry/internal/stream"
	"trackferry/internal/takeover"
)

func TestBrowserCollectionIsIndependentOfTheDisplayPage(t *testing.T) {
	s, tok := newServer(t)
	ctx := context.Background()
	const count = 205
	rows := make([]store.TrackRow, 0, count)
	for i := range count {
		title := fmt.Sprintf("歌%d", i+1)
		rows = append(rows, store.TrackRow{Path: title + ".flac", Title: title, Artist: "测试歌手", Favorite: true})
	}
	if err := s.Store.SaveTracks(ctx, 1, rows, nil); err != nil {
		t.Fatal(err)
	}
	s.Library = library.New(t.TempDir(), s.Store, s.Log)
	s.Router = &routing.Router{Library: s.Library}
	s.Stream = stream.New("http://nas", t.TempDir())
	if err := s.Library.Load(ctx); err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	ids := func(path, field string) []int64 {
		t.Helper()
		rec := call(h, "GET", path, tok, "")
		var out []struct{ ID int64 }
		data := map[string]json.RawMessage{}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &data) != nil || json.Unmarshal(data[field], &out) != nil {
			t.Fatal(path, rec.Code, rec.Body)
		}
		got := []int64{}
		for _, x := range out {
			got = append(got, x.ID)
		}
		return got
	}
	favs := ids("/api/library/favorites", "tracks")
	allFavs := ids("/api/here?source=favorites", "songs")
	if len(favs) != 60 || len(allFavs) != count || !slices.Equal(favs, allFavs[:len(favs)]) {
		t.Fatal("favorites did not preserve every track in display order", len(favs), len(allFavs))
	}
	const artist = "%E6%B5%8B%E8%AF%95%E6%AD%8C%E6%89%8B"
	artists := ids("/api/library/artist?name="+artist, "tracks")
	allArtist := ids("/api/here?source=artist&name="+artist, "songs")
	if len(artists) != count || !slices.Equal(artists, allArtist) {
		t.Fatal("artist list truncated or reordered", len(artists), len(allArtist))
	}
}

func TestPlayingSettings(t *testing.T) {
	s, tok := newServer(t)
	h := s.Handler()
	for _, c := range []struct {
		body string
		code int
		want takeover.Prefs
	}{
		{`{"hours":4}`, 200, takeover.Prefs{Single: true, Pool: "library", Hours: 4}},
		{`{"single":false,"hours":0}`, 200, takeover.Prefs{Pool: "library"}},
		{`{"pool":"favorites"}`, 200, takeover.Prefs{Pool: "favorites"}},
		{`{"pool":"online"}`, 400, takeover.Prefs{Pool: "favorites"}},
		{`{"hours":25}`, 400, takeover.Prefs{Pool: "favorites"}},
		{`{"hours":-1}`, 400, takeover.Prefs{Pool: "favorites"}},
	} {
		if rec := call(h, "PUT", "/api/playing", tok, c.body); rec.Code != c.code {
			t.Fatal(c.body, rec.Code, rec.Body)
		}
		if got := takeover.LoadPrefs(context.Background(), s.Store); got != c.want {
			t.Fatal(c.body, got)
		}
	}
	if c := call(h, "GET", "/api/playing", "", "").Code; c != 401 {
		t.Fatal(c)
	}
}

func TestDeviceHush(t *testing.T) {
	s, tok, _ := homeServer(t)
	h := s.Handler()
	for body, want := range map[string]int{`{"hush":"loud"}`: 400, `{"hush":""}`: 400, `{"hush":"strong"}`: 409, `{"hush":"normal"}`: 200} {
		if rec := call(h, "PATCH", "/api/devices/d1", tok, body); rec.Code != want {
			t.Fatal(body, rec.Code, rec.Body)
		}
	}
	if b := call(h, "GET", "/api/devices", tok, "").Body.String(); !strings.Contains(b, `"hush": "normal"`) {
		t.Fatal(b)
	}
}

// Only an artist, an album, the favourites or a playlist is a list of its own; anything else,
// the few songs shown at random among them, goes on with the library at random.
func TestPicksAndSingleSongsGoOnAtRandom(t *testing.T) {
	s, _ := newServer(t)
	ctx := context.Background()
	rows := make([]store.TrackRow, 0, 40)
	for i := range 40 {
		title := fmt.Sprintf("歌%d", i+1)
		rows = append(rows, store.TrackRow{Path: title + ".flac", Title: title, Artist: "测试歌手"})
	}
	if err := s.Store.SaveTracks(ctx, 1, rows, nil); err != nil {
		t.Fatal(err)
	}
	s.Library = library.New(t.TempDir(), s.Store, s.Log)
	if err := s.Library.Load(ctx); err != nil {
		t.Fatal(err)
	}
	all := s.Library.RandomIDs(100)
	picks := []int64{all[3], all[5]}
	_, ids, ok := s.collect(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "picks", "", "", picks)
	if !ok || len(ids) != len(all) || !slices.Equal(ids[:2], picks) {
		t.Fatal("picks did not go on with the library", len(ids), ids[:2])
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatal("twice", id)
		}
		seen[id] = true
	}
	one := s.withRandom(all[7])
	if len(one) != len(all) || one[0] != all[7] || slices.Contains(one[1:], all[7]) {
		t.Fatal("a single song did not go on with the library", len(one))
	}
	// The settings decide for every output at once: nothing follows, or the favourites do.
	if err := takeover.SavePrefs(ctx, s.Store, takeover.Prefs{Pool: "library", Hours: 3}); err != nil {
		t.Fatal(err)
	}
	if got := s.withRandom(all[7]); len(got) != 1 {
		t.Fatal("followed although turned off", len(got))
	}
	if _, ids, _ := s.collect(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "picks", "", "", picks); !slices.Equal(ids, picks) {
		t.Fatal("picks followed although turned off", ids)
	}
	if err := takeover.SavePrefs(ctx, s.Store, takeover.Prefs{Single: true, Pool: "favorites", Hours: 3}); err != nil {
		t.Fatal(err)
	}
	if got := s.withRandom(all[7]); len(got) != len(all) { // no favourites: the library still follows
		t.Fatal("nothing followed without favourites", len(got))
	}
}
