package feiniu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sha256("pw")
const pwHash = "30c952fab122c3f9759f02a6d95c3758b246b4fee239957b2d4fee46e26170c4"

func nas(t *testing.T, valid *bool) *httptest.Server {
	reply := func(w http.ResponseWriter, code int, data any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": "", "data": data})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/music/api/v1/sys/config", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 0, map[string]any{"serverName": "nas", "serverVersion": "1.1.19"})
	})
	mux.HandleFunc("/music/api/v1/user/password-login", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		if in["username"] != "me" || in["password"] != pwHash || len(in["deviceId"]) != 32 {
			reply(w, 100001, nil)
			return
		}
		reply(w, 0, map[string]any{"userToken": "tok", "user": map[string]any{}})
	})
	authed := func(p string, f http.HandlerFunc) {
		mux.HandleFunc("/music/api/v1"+p, func(w http.ResponseWriter, r *http.Request) {
			if c, err := r.Cookie("music-token"); err != nil || c.Value != "tok" || !*valid {
				reply(w, 99999, nil)
				return
			}
			f(w, r)
		})
	}
	authed("/track/list", func(w http.ResponseWriter, r *http.Request) {
		var list []map[string]any
		for i := range 3 {
			list = append(list, map[string]any{"guid": fmt.Sprint("g", i), "title": "歌", "duration": 1000, "createdAt": 5,
				"artists": []any{map[string]any{"name": "甲", "coverId": nil}}, "album": nil, "coverId": nil,
				"audioSpec": map[string]any{"path": "/vol1/1000/音乐/a.flac", "bitDepth": nil}})
		}
		reply(w, 0, map[string]any{"list": list, "total": 3})
	})
	authed("/lyric/list", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 0, map[string]any{"list": []any{map[string]any{"guid": "a", "content": "x"}, map[string]any{"guid": "b", "content": "[00:01.00]y"}}, "preferred": "b"})
	})
	authed("/track/stream", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.flac", time.Time{}, strings.NewReader("0123456789"))
	})
	authed("/favorite-track/list", func(w http.ResponseWriter, r *http.Request) {
		// Two pages.
		list := []any{map[string]any{"guid": "f" + r.URL.Query().Get("page"), "title": "歌"}}
		reply(w, 0, map[string]any{"list": list, "total": 2, "sort": r.URL.Query().Get("sort")})
	})
	authed("/playlist/list", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 0, map[string]any{"list": []any{map[string]any{"guid": "p1", "name": "测试", "createdAt": 1, "coverId": nil}}})
	})
	in := []string{"g0"}
	authed("/track/playlist-detail/list", func(w http.ResponseWriter, r *http.Request) {
		list := []any{}
		for _, g := range in {
			if r.URL.Query().Get("playlistGUID") == "p1" {
				list = append(list, map[string]any{"guid": g, "title": "歌"})
			}
		}
		reply(w, 0, map[string]any{"list": list, "total": len(list)})
	})
	authed("/playlist/add-track", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			GUID   string   `json:"guid"`
			Tracks []string `json:"trackGUIDs"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		if r.Method != http.MethodPost || b.GUID != "p1" {
			reply(w, 100001, nil)
			return
		}
		in = append(in, b.Tracks...)
		reply(w, 0, nil)
	})
	authed("/playlist/remove-track", func(w http.ResponseWriter, r *http.Request) {
		in = in[:1]
		reply(w, 0, nil)
	})
	s := httptest.NewTLSServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestLinkUseAndExpiry(t *testing.T) {
	ctx := context.Background()
	valid := true
	s := nas(t, &valid)
	file := filepath.Join(t.TempDir(), "private", "feiniu.json")
	m, err := NewManager(file)
	if err != nil {
		t.Fatal(err)
	}
	var ae *APIError
	if err := m.Login(ctx, s.URL, "me", "wrong", ""); !errors.As(err, &ae) || m.Linked() {
		t.Fatalf("wrong password: %v", err)
	}
	if err := m.Login(ctx, s.URL, "me", "pw", ""); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o600 || strings.Contains(string(raw), pwHash) {
		t.Errorf("session file mode %v", fi.Mode())
	}
	if m, err = NewManager(file); err != nil || !m.Ready() {
		t.Fatalf("reload: %v", err)
	}
	if songs, n, err := m.Songs(ctx, 1, 200); err != nil || n != 3 || songs[0].Artists[0].Name != "甲" {
		t.Errorf("songs: %v %d %v", songs, n, err)
	}
	if s, err := m.Favorites(ctx); err != nil || len(s) != 2 || s[1].GUID != "f2" {
		t.Errorf("favourites: %v %v", s, err)
	}
	if p, err := m.Playlists(ctx); err != nil || len(p) != 1 || p[0] != (Playlist{GUID: "p1", Name: "测试"}) {
		t.Errorf("playlists: %v %v", p, err)
	}
	for _, x := range []struct{ on, changed bool }{{true, true}, {true, false}, {false, true}, {false, false}} {
		if changed, err := m.PutInPlaylist(ctx, "p1", "g1", x.on); err != nil || changed != x.changed {
			t.Errorf("put %v: %v %v", x, changed, err)
		}
	}
	if s, err := m.PlaylistSongs(ctx, "p1"); err != nil || len(s) != 1 || s[0].GUID != "g0" {
		t.Errorf("playlist: %v %v", s, err)
	}
	if l, err := m.Lyrics(ctx, "g0"); err != nil || l != "[00:01.00]y" {
		t.Errorf("lyrics %q %v", l, err)
	}
	resp, err := m.Open(ctx, "g0", "bytes=2-4")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "234" || resp.StatusCode != 206 {
		t.Errorf("stream %q %d", b, resp.StatusCode)
	}
	if _, err := m.Cover(ctx, "../x", "600"); err == nil {
		t.Error("bad cover id accepted")
	}
	valid = false
	if _, err := m.Open(ctx, "g0", ""); !errors.Is(err, ErrAuth) || m.Ready() || !m.Status().Expired {
		t.Errorf("expired: %v", err)
	}
}

func TestChangedCertificateIsRefused(t *testing.T) {
	valid := true
	s := nas(t, &valid)
	c := newClient(s.URL, "tok", strings.Repeat("0", 64))
	if _, err := c.songs(context.Background(), 1, 1); !errors.Is(err, ErrCert) {
		t.Errorf("got %v", err)
	}
}

func TestBase(t *testing.T) {
	for in, want := range map[string]string{
		"10.0.0.2":             "https://10.0.0.2:5667",
		"http://10.0.0.2:5666": "https://10.0.0.2:5667",
		"https://nas:8443/x":   "https://nas:8443",
		"ftp://x":              "",
		"":                     "",
	} {
		if got, err := Base(in); got != want || (want == "") != (err != nil) {
			t.Errorf("%q -> %q %v", in, got, err)
		}
	}
}
