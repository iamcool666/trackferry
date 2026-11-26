package api

import (
	"context"
	"encoding/json"
	"fmt"
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
		{`{"after":true}`, 200, takeover.Prefs{Artist: true, After: true, Hours: 3}},
		{`{"artist":false,"hours":0}`, 200, takeover.Prefs{After: true}},
		{`{"hours":25}`, 400, takeover.Prefs{After: true}},
		{`{"hours":-1}`, 400, takeover.Prefs{After: true}},
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
