package online

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// artistServer answers a singer search with songs and hands out bodies per song id and
// rate: bodies[id][rate], with "*" standing for any rate. asked keeps every rate a
// stream was asked for, in order.
func artistServer(t *testing.T, bodies map[string]map[string][]byte, songs []map[string]any) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/search3":
			q := r.URL.Query().Get("query")
			if strings.HasPrefix(q, "kw:") { // the plain search has its own test
				songs = []map[string]any{}
			}
			json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{"status": "ok",
				"searchResult3": map[string]any{"song": songs}}})
		case "/rest/stream":
			id, rate := r.URL.Query().Get("id"), r.URL.Query().Get("maxBitRate")
			asked = append(asked, id+"@"+rate)
			b, ok := bodies[id][rate]
			if !ok {
				b = bodies[id]["*"]
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(b)))
			w.Write(b)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	return srv, &asked
}

func artistService(srv *httptest.Server) *Service {
	l := &Lx{BaseURL: srv.URL + "/rest", User: "u", Password: "p", Client: srv.Client()}
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), srv.Client(), l)
}

func artistSongs(ids ...string) []map[string]any {
	out := make([]map[string]any, len(ids))
	for i, id := range ids {
		out[i] = map[string]any{"id": id, "title": "歌" + id, "artist": "凤凰传奇", "duration": 294, "suffix": "mp3"}
	}
	return out
}

// A singer the library cannot fill comes from the online source: the first song that
// really plays is probed and taken, the rest are named so the queue can play them after.
func TestFindArtistFallsThroughToAPlayableSong(t *testing.T) {
	srv, asked := artistServer(t, map[string]map[string][]byte{
		"kw_101": {"*": {0x29, 0x17, 0x55, 0x67, 0x33, 0x46, 0x79, 0x6e}}, // the encrypted container
		"kw_102": {"*": flacHead(285)},
		"kw_103": {"*": mp3Head()},
	}, artistSongs("kw_101", "kw_102", "kw_103"))
	defer srv.Close()
	svc := artistService(srv)
	f, rest, outs := svc.FindArtist(context.Background(), "凤凰传奇", 10)
	if f == nil {
		t.Fatalf("nothing found: %+v", outs)
	}
	if f.Candidate.Key != "lx:kw_102" {
		t.Fatalf("taken %s, want the first song that plays", f.Candidate.Key)
	}
	if f.Candidate.Member {
		t.Fatal("a singer's song is marked member and would never be cached")
	}
	if len(rest) != 1 || rest[0].Key != "lx:kw_103" {
		t.Fatalf("rest %+v, want the songs after the taken one", rest)
	}
	if _, err := svc.Take(context.Background(), rest[0].Key); err != nil {
		t.Fatalf("a rest song was not named for the queue: %v", err)
	}
	// the bad song tried every rate, the taken one its own, the taken rest one more
	if len(*asked) != 5 {
		t.Fatalf("asked %v, want three bad rates, one good, one for the rest", *asked)
	}
}

// A singer nobody has says so without probing anything.
func TestFindArtistNobodyHome(t *testing.T) {
	srv, asked := artistServer(t, map[string]map[string][]byte{}, nil)
	defer srv.Close()
	svc := artistService(srv)
	f, rest, _ := svc.FindArtist(context.Background(), "查无此人", 10)
	if f != nil || len(rest) != 0 {
		t.Fatalf("found %+v rest %+v, want nothing", f, rest)
	}
	if len(*asked) != 0 {
		t.Fatalf("asked %v, want no probe at all", *asked)
	}
}

// Songs that play at no rate are not offered: probing gives up after a few, so one bad
// singer cannot burn the whole quota.
func TestFindArtistAllBad(t *testing.T) {
	bodies := map[string]map[string][]byte{}
	for _, id := range []string{"kw_201", "kw_202", "kw_203", "kw_204", "kw_205"} {
		bodies[id] = map[string][]byte{"*": {0x29, 0x17, 0x55, 0x67}}
	}
	srv, asked := artistServer(t, bodies, artistSongs("kw_201", "kw_202", "kw_203", "kw_204", "kw_205"))
	defer srv.Close()
	svc := artistService(srv)
	f, rest, _ := svc.FindArtist(context.Background(), "凤凰传奇", 10)
	if f != nil || len(rest) != 0 {
		t.Fatalf("found %+v rest %+v, want nothing", f, rest)
	}
	if want := 3 * len(lxFalls); len(*asked) != want {
		t.Fatalf("asked %v (%d), want %d", *asked, len(*asked), want)
	}
}
