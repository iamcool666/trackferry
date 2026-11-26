package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"trackferry/internal/feiniu"
	"trackferry/internal/library"
	"trackferry/internal/routing"
	"trackferry/internal/store"
	"trackferry/internal/stream"
	"trackferry/internal/tv"
)

type tvTestLists struct {
	favorite bool
	added    bool
}

func (*tvTestLists) Linked() bool { return true }
func (*tvTestLists) Songs(context.Context, int, int) ([]feiniu.Song, int, error) {
	return nil, 0, nil
}
func (*tvTestLists) Favorites(context.Context) ([]feiniu.Song, error)     { return nil, nil }
func (*tvTestLists) Playlists(context.Context) ([]feiniu.Playlist, error) { return nil, nil }
func (*tvTestLists) PlaylistSongs(context.Context, string) ([]feiniu.Song, error) {
	return nil, nil
}
func (x *tvTestLists) Favorite(_ context.Context, _ string, on bool) error {
	x.favorite = on
	return nil
}
func (x *tvTestLists) PutInPlaylist(_ context.Context, _, _ string, on bool) (bool, error) {
	changed := x.added != on
	x.added = on
	return changed, nil
}

func TestTVPairingScopeAndRevocation(t *testing.T) {
	ctx := context.Background()
	s, admin := newServer(t)
	_, pin, err := tv.Identity(filepath.Join(t.TempDir(), "tv-key.der"))
	if err != nil {
		t.Fatal(err)
	}
	s.TV = tv.NewPairer(pin, "测试服务器", s.Store)
	root := t.TempDir()
	file := filepath.Join(root, "demo.wav")
	if err := os.WriteFile(file, []byte("fixture audio bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SaveTracks(ctx, 1, []store.TrackRow{{Path: "demo.wav", GUID: "fixture-track", Title: "测试曲目", Artist: "歌手", Favorite: true}}, nil); err != nil {
		t.Fatal(err)
	}
	s.Library = library.New(root, s.Store, s.Log)
	if err := s.Library.Load(ctx); err != nil {
		t.Fatal(err)
	}
	lists := &tvTestLists{favorite: true}
	s.Library.Remote = lists
	s.Router = &routing.Router{Library: s.Library, Lists: lists}
	s.Stream = stream.New("", root)
	web, television := s.Handler(), s.TVHandler()
	check := func(h http.Handler, method, path, token, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := call(h, method, path, token, body)
		if r.Code != want {
			t.Fatalf("%s %s = %d, want %d: %s", method, path, r.Code, want, r.Body)
		}
		return r
	}
	hash := func(text string) string { h := sha256.Sum256([]byte(text)); return hex.EncodeToString(h[:]) }
	seed, token := strings.Repeat("12", 32), strings.Repeat("34", 32)
	started := check(television, "POST", tvAPI+"/pair", "", `{"name":"客厅电视","commitment":"`+hash(seed)+`","token_hash":"`+hash(token)+`"}`, 201)
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(started.Body.Bytes(), &out); err != nil || out.ID == "" {
		t.Fatal("missing pairing ID", err)
	}
	code := `{"code":"` + tv.Code(pin, out.ID, seed, hash(token)) + `"}`
	status := `{"id":"` + out.ID + `","seed":"` + seed + `"}`
	check(web, "POST", "/api/tv/devices", admin, code, 410) // code unavailable until the committed seed is revealed
	check(television, "POST", tvAPI+"/pair/status", "", status, 200)
	check(web, "GET", "/api/tv/outputs", "", "", 401)
	prompt := check(web, "GET", "/api/tv/outputs", admin, "", 200)
	var announced struct {
		Pairing map[string]string `json:"pairing"`
	}
	if err := json.Unmarshal(prompt.Body.Bytes(), &announced); err != nil ||
		len(announced.Pairing) != 2 || announced.Pairing["id"] != out.ID || announced.Pairing["name"] != "电视" {
		t.Fatal("pairing prompt should expose a generic device label and ID")
	}
	check(television, "GET", tvAPI+"/shelf", token, "", 401)
	favorite := tvAPI + "/tracks/1/favorite"
	add := tvAPI + "/playlists/fixture-list/tracks"
	check(television, "PUT", favorite, token, `{"on":false}`, 401)
	check(television, "POST", add, token, `{"track_id":1}`, 401)
	check(television, "GET", tvAPI+"/cover/demo", token, "", 401)
	check(web, "POST", "/api/tv/devices", "", code, 401)
	approved := check(web, "POST", "/api/tv/devices", admin, code, 201)
	var named store.TVDevice
	if err := json.Unmarshal(approved.Body.Bytes(), &named); err != nil || named.Name != "电视" {
		t.Fatal("first TV should receive a friendly name", approved.Body)
	}
	secondHash := sha256.Sum256([]byte("another TV"))
	if second, err := s.Store.AddTV(ctx, "second-tv", "ANOTHER_MODEL", named.PairedAt, secondHash[:]); err != nil || second.Name != "电视 2" {
		t.Fatal("second TV should receive a distinct default name", second, err)
	}
	devPath := "/api/tv/devices/" + out.ID
	check(web, "PUT", devPath, token, `{"name":"客厅电视"}`, 401)
	check(web, "PUT", devPath, admin, `{"name":" "}`, 400)
	check(web, "PUT", devPath, admin, `{"name":" 客厅电视 "}`, 200)
	visible := check(web, "GET", "/api/tv/outputs", admin, "", 200)
	var outputs struct{ Devices []tvOutput }
	if err := json.Unmarshal(visible.Body.Bytes(), &outputs); err != nil ||
		!slices.ContainsFunc(outputs.Devices, func(d tvOutput) bool { return d.ID == out.ID && d.Name == "客厅电视" }) {
		t.Fatal("renamed device should be offered with the new label", visible.Body)
	}
	prompt = check(web, "GET", "/api/tv/outputs", admin, "", 200)
	if err := json.Unmarshal(prompt.Body.Bytes(), &announced); err != nil || announced.Pairing != nil {
		t.Fatal("approved pairing prompt still visible")
	}
	check(web, "POST", "/api/tv/devices", admin, code, 410)
	check(television, "POST", tvAPI+"/pair/status", "", status, 200)
	check(television, "PUT", favorite, token, `{"on":false}`, 200)
	if lists.favorite {
		t.Fatal("favorite removal was not applied")
	}
	res := check(television, "PUT", favorite, token, `{"on":true}`, 200)
	if !lists.favorite || !strings.Contains(strings.ReplaceAll(res.Body.String(), " ", ""), `"favorite":true`) {
		t.Fatal("favorite was not confirmed", res.Body)
	}
	for _, want := range []bool{true, false} {
		res = check(television, "POST", add, token, `{"track_id":1}`, 200)
		var got struct{ Changed bool }
		if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil || got.Changed != want {
			t.Fatal("playlist addition must report whether it changed", res.Body)
		}
	}
	queue := check(television, "GET", tvAPI+"/queue?source=favorites", token, "", 200)
	if !strings.Contains(queue.Body.String(), "测试曲目") || strings.Contains(queue.Body.String(), "demo.wav") {
		t.Fatal(queue.Body.String())
	}
	check(television, "GET", tvAPI+"/queue?source=favorites", admin, "", 401)
	// A song put on alone goes on with random ones, as in the browser.
	drift := check(television, "GET", tvAPI+"/queue?source=random", token, "", 200)
	if !strings.Contains(drift.Body.String(), "接着随机放") || !strings.Contains(drift.Body.String(), "测试曲目") {
		t.Fatal(drift.Body.String())
	}
	// Lyrics are moved per song, and the browser sees the same offset.
	check(television, "PUT", tvAPI+"/lyrics/1/offset", admin, `{"offset_ms":800}`, 401)
	check(television, "PUT", tvAPI+"/lyrics/1/offset", token, `{"offset_ms":800}`, 200)
	check(television, "PUT", tvAPI+"/lyrics/1/offset", token, `{"offset_ms":61000}`, 400)
	check(television, "PUT", tvAPI+"/lyrics/999/offset", token, `{"offset_ms":0}`, 404)
	if v, _ := s.Store.Setting(context.Background(), settingOffset+"1"); v != "800" {
		t.Fatal("offset not kept per song:", v)
	}
	check(television, "GET", tvAPI+"/app?abi=arm64-v8a", "", "", 401)
	check(web, "GET", "/api/devices", token, "", 401)
	check(television, "GET", "/api/devices", token, "", 404)
	check(television, "GET", tvAPI+"/cover/demo", token, "", 404)
	req := httptest.NewRequest("GET", tvAPI+"/audio?track=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=0-6")
	part := httptest.NewRecorder()
	television.ServeHTTP(part, req)
	if part.Code != 206 || part.Body.String() != "fixture" {
		t.Fatal(part.Code, part.Body.String())
	}
	output := "/api/tv/outputs/" + out.ID
	check(web, "POST", output+"/commands", admin, `{"action":"load","items":[{"id":1}]}`, 409)
	beat := `{"epoch":"tv-fixture","sequence":1,"playback":{"phase":"idle","revision":"rev"}}`
	check(television, "POST", tvAPI+"/sync", admin, beat, 401)
	check(television, "POST", tvAPI+"/sync", token, beat, 200)
	queued := check(web, "POST", output+"/commands", admin, `{"action":"load","source":"favorites"}`, 202)
	var cmd tv.Result
	if err := json.Unmarshal(queued.Body.Bytes(), &cmd); err != nil || cmd.State != "pending" {
		t.Fatal(queued.Body.String(), err)
	}
	get := check(television, "POST", tvAPI+"/sync", token, strings.Replace(beat, `"sequence":1`, `"sequence":2`, 1), 200)
	var update tv.Update
	if err := json.Unmarshal(get.Body.Bytes(), &update); err != nil || update.Command == nil || update.Command.ID != cmd.ID || len(update.Command.Items) != 1 || update.Command.Items[0].ID != 1 {
		t.Fatal(get.Body.String(), err)
	}
	report := `{"epoch":"tv-fixture","sequence":3,"playback":{"phase":"playing","revision":"rev","entry":"entry","index":0,"count":1,"queue":[{"id":1}]},"result":{"id":"` + cmd.ID + `","state":"done"}}`
	check(television, "POST", tvAPI+"/sync", token, report, 200)
	ack := check(web, "GET", output+"/commands/"+cmd.ID, admin, "", 200)
	if !strings.Contains(ack.Body.String(), `"done"`) {
		t.Fatal(ack.Body.String())
	}
	check(web, "POST", output+"/commands", admin, `{"action":"pause","entry":"old"}`, 409)
	check(web, "DELETE", "/api/tv/devices/"+out.ID, admin, "", 204)
	check(web, "PUT", devPath, admin, `{"name":"客厅电视"}`, 404)
	check(television, "GET", tvAPI+"/audio?track=1", token, "", 401)
	check(television, "POST", tvAPI+"/pair/status", "", status, 410)
}
