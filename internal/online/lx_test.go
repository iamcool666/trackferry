package online

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A 洛雪 server answers in Subsonic's shape, wrapped once. Each platform is asked on its own,
// and the first with songs answers: the id says which platform a song came from.
func TestLxSearchAsksPlatformsAtOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if r.URL.Path != "/rest/search3" || r.URL.Query().Get("u") != "u" {
			t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		platform, _, _ := strings.Cut(q, ":")
		var songs []map[string]any
		if platform == "kg" {
			songs = []map[string]any{{"id": "kg_1", "title": "模特", "artist": "李荣浩", "duration": 308, "suffix": "flac"}}
		}
		json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{"status": "ok",
			"searchResult3": map[string]any{"song": songs}}})
	}))
	defer srv.Close()
	l := &Lx{BaseURL: srv.URL + "/rest", User: "u", Password: "p", Client: srv.Client()}
	cs, err := l.Search(context.Background(), "李荣浩 模特")
	if err != nil || len(cs) != 1 {
		t.Fatalf("search = %+v, %v", cs, err)
	}
	if cs[0].ID != "kg_1" || cs[0].Duration != 308_000_000_000 {
		t.Fatalf("candidate = %+v", cs[0])
	}
	if got := LxName(lxPlatformOf(cs[0].ID)); got != "酷狗" {
		t.Fatalf("platform = %q", got)
	}
	if u, err := l.Resolve(context.Background(), cs[0]); err != nil || u == "" {
		t.Fatalf("resolve = %q, %v", u, err)
	}
}

// A server on this network is reached by its own client, which is allowed a private address.
func TestFindArtistUsesLXAndResolvesPlayableSong(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/search3":
			if got := r.URL.Query().Get("query"); got != "周杰伦" {
				t.Errorf("artist query = %q", got)
			}
			json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{
				"status": "ok",
				"searchResult3": map[string]any{"song": []map[string]any{
					{"id": "kw_123", "title": "晴天", "artist": "周杰伦", "duration": 269, "suffix": "flac"},
				}},
			}})
		case "/rest/stream":
			w.Header().Set("Content-Type", "audio/flac")
			_, _ = w.Write([]byte("fLaC"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	lx := &Lx{BaseURL: srv.URL + "/rest", User: "u", Password: "p", Client: srv.Client()}
	svc := New(nil, Client(), lx)
	found, outcomes := svc.FindArtist(context.Background(), "周杰伦", 20)
	if found == nil {
		t.Fatalf("FindArtist returned no song: %+v", outcomes)
	}
	if found.Candidate.Title != "晴天" || found.Candidate.Key != "lx:kw_123" || !found.Candidate.Member {
		t.Fatalf("found candidate = %+v", found.Candidate)
	}
	if len(outcomes) != 1 || outcomes[0].State != StateHit {
		t.Fatalf("outcomes = %+v", outcomes)
	}
}

func TestLxClientReachesAPrivateAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{"status": "ok"}})
	}))
	defer srv.Close()
	c := ClientWith(LxHosts(srv.URL + "/rest"))
	if _, err := c.Get(srv.URL + "/rest/ping"); err != nil {
		t.Fatalf("private address refused: %v", err)
	}
	// The platforms' own client still refuses anything it does not know.
	if _, err := Client().Get(srv.URL + "/rest/ping"); err == nil {
		t.Fatal("the shared client reached a private address")
	}
}


func TestLxMapsPublicOnlineListIDs(t *testing.T) {
	// All four requested music platforms expose both shared playlists and charts.
	tests := []struct {
		input string
		want  string
	}{
		{"public:lx:tx:playlist:7769010449", "pl_tx_7769010449"},
		{"public:lx:wy:playlist:123456", "pl_wy_123456"},
		{"public:lx:kw:playlist:123456", "pl_kw_123456"},
		{"public:lx:kg:playlist:123456", "pl_kg_123456"},
		{"public:lx:tx:chart:4", "lb_tx_4"},
		{"public:lx:wy:chart:19723756", "lb_wy_19723756"},
		{"public:lx:kw:chart:16", "lb_kw_16"},
		{"public:lx:kg:chart:8888", "lb_kg_8888"},
	}
	for _, tt := range tests {
		got, err := lxPlaylistID(tt.input)
		if err != nil || got != tt.want {
			t.Errorf("lxPlaylistID(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
		}
	}
}

func TestLxReadsPublicOnlinePlaylist(t *testing.T) {
	const playlistID = "public:lx:tx:playlist:7769010449"
	const subsonicID = "pl_tx_7769010449"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/getPlaylist" || r.URL.Query().Get("id") != subsonicID {
			t.Errorf("unexpected playlist request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{
			"status": "ok",
			"playlist": map[string]any{"id": subsonicID, "name": "[QQ] 英文经典", "songCount": 1,
				"entry": []map[string]any{{"id": "tx_123", "title": "Hello", "artist": "Adele", "duration": 295, "suffix": "mp3"}}},
		}})
	}))
	defer srv.Close()
	l := &Lx{BaseURL: srv.URL + "/rest", User: "u", Password: "p", Client: srv.Client()}
	p, err := l.Playlist(context.Background(), playlistID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "[QQ] 英文经典" || len(p.Songs) != 1 {
		t.Fatalf("playlist = %+v", p)
	}
	if p.Songs[0].ID != "tx_123" || p.Songs[0].Source != "lx" || p.Songs[0].DurationMS != 295000 {
		t.Fatalf("song = %+v", p.Songs[0])
	}
}
