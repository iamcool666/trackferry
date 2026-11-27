package cast

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"trackferry/internal/tv"
)

// fakeDLNA is a renderer that keeps what it was told.
func fakeDLNA(t *testing.T) (*httptest.Server, *sync.Map) {
	got := &sync.Map{}
	state := "STOPPED"
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/desc.xml", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<?xml version="1.0"?><root xmlns="urn:schemas-upnp-org:device-1-0"><device>
<deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType><friendlyName>外壳</friendlyName><UDN>uuid:outer</UDN>
<deviceList><device><deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType><friendlyName>客厅音响</friendlyName><modelName>R1</modelName><UDN>uuid:ABC_123</UDN>
<serviceList><service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>/av</controlURL></service>
<service><serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType><controlURL>rc</controlURL></service></serviceList>
</device></deviceList></device></root>`)
	})
	answer := func(w http.ResponseWriter, action string, fields string) {
		io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:`+action+`Response xmlns:u="x">`+fields+`</u:`+action+`Response></s:Body></s:Envelope>`)
	}
	soap := func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("SOAPAction")
		action = strings.Trim(action[strings.Index(action, "#")+1:], `"`)
		body, _ := io.ReadAll(r.Body)
		got.Store(action, string(body))
		mu.Lock()
		defer mu.Unlock()
		switch action {
		case "Play":
			state = "PLAYING"
		case "Pause":
			state = "PAUSED_PLAYBACK"
		case "Stop":
			state = "STOPPED"
		case "Seek":
			if state != "PLAYING" {
				w.WriteHeader(500)
				io.WriteString(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>701</errorCode><errorDescription>Transition not available</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
				return
			}
		case "GetTransportInfo":
			answer(w, action, "<CurrentTransportState>"+state+"</CurrentTransportState><CurrentTransportStatus>OK</CurrentTransportStatus>")
			return
		case "GetPositionInfo":
			answer(w, action, "<RelTime>0:01:05.5</RelTime><TrackDuration>0:03:20</TrackDuration><TrackURI>http://nas/stream/x.mp3</TrackURI>")
			return
		case "GetVolume":
			answer(w, action, "<CurrentVolume>35</CurrentVolume>")
			return
		}
		answer(w, action, "")
	}
	mux.HandleFunc("/av", soap)
	mux.HandleFunc("/rc", soap)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, got
}

func TestDLNADescribesLoadsAndReads(t *testing.T) {
	srv, got := fakeDLNA(t)
	ctx := context.Background()
	d, ok, err := describeDLNA(ctx, srv.Client(), srv.URL+"/desc.xml")
	if err != nil || !ok {
		t.Fatalf("describe: %v %v", ok, err)
	}
	if d.Device.ID != "dlna-abc-123" || d.Device.Name != "客厅音响" || !strings.HasSuffix(d.transport, "/av") || !strings.HasSuffix(d.volumeAt, "/rc") {
		t.Fatalf("device = %+v %s %s", d.Device, d.transport, d.volumeAt)
	}
	r := newDLNA(srv.Client(), srv.URL+"/desc.xml")
	m := Media{URL: "http://nas/stream/x.mp3?a=1&b=2", Type: "audio/mpeg", Title: "晴天 & 雨", Artist: "周杰伦", Duration: 200 * time.Second}
	if err := r.Load(ctx, m, 65*time.Second); err != nil {
		t.Fatal(err)
	}
	set, _ := got.Load("SetAVTransportURI")
	// The address and the metadata go escaped, the metadata twice: once as XML, once inside SOAP.
	for _, want := range []string{"x.mp3?a=1&amp;b=2", "&lt;dc:title&gt;晴天 &amp;amp; 雨", "audio/mpeg"} {
		if !strings.Contains(set.(string), want) {
			t.Fatalf("SetAVTransportURI lacks %q: %s", want, set)
		}
	}
	seek, _ := got.Load("Seek")
	if !strings.Contains(seek.(string), "<Target>0:01:05</Target>") {
		t.Fatalf("seek = %v", seek)
	}
	st, err := r.Status(ctx)
	if err != nil || st.State != Playing || st.Position != 65500*time.Millisecond || st.Duration != 200*time.Second || st.Volume != 35 {
		t.Fatalf("status = %+v %v", st, err)
	}
	if err := r.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Seek(ctx, time.Second); err == nil || !strings.Contains(err.Error(), "Transition not available") {
		t.Fatalf("a refusal must be told: %v", err)
	}
}

func TestSSDPLocation(t *testing.T) {
	ok := "HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nLOCATION: http://192.168.1.5:49152/desc.xml\r\nST: urn:schemas-upnp-org:device:MediaRenderer:1\r\n\r\n"
	if got := ssdpLocation([]byte(ok)); got != "http://192.168.1.5:49152/desc.xml" {
		t.Fatalf("location = %q", got)
	}
	if got := ssdpLocation([]byte(strings.Replace(ok, "http://", "file://", 1))); got != "" {
		t.Fatalf("only http: %q", got)
	}
}

func TestCastFramesRoundTrip(t *testing.T) {
	m := castMessage{Source: "a", Dest: "receiver-0", Namespace: nsMedia, Payload: strings.Repeat("歌", 100)}
	got, err := decodeCast(m.encode())
	if err != nil || got != m {
		t.Fatalf("got %+v %v", got, err)
	}
}

// fakeCast answers like a Chromecast with the media app.
func fakeCast(t *testing.T, conn net.Conn) *sync.Map {
	got := &sync.Map{}
	go func() {
		defer conn.Close()
		state, at, sid := "IDLE", 0.0, 0
		launched := false
		for {
			m, err := readFrame(conn)
			if err != nil {
				return
			}
			var p map[string]any
			json.Unmarshal([]byte(m.Payload), &p)
			got.Store(p["type"], p)
			reply := func(v map[string]any) {
				v["requestId"] = p["requestId"]
				b, _ := json.Marshal(v)
				writeFrame(conn, castMessage{Source: m.Dest, Dest: m.Source, Namespace: m.Namespace, Payload: string(b)})
			}
			apps := []any{}
			if launched {
				apps = append(apps, map[string]any{"appId": mediaApp, "transportId": "web-1", "sessionId": "s-1"})
			}
			media := func() map[string]any {
				if sid == 0 {
					return map[string]any{"type": "MEDIA_STATUS", "status": []any{}}
				}
				return map[string]any{"type": "MEDIA_STATUS", "status": []any{map[string]any{"mediaSessionId": sid, "playerState": state, "currentTime": at, "idleReason": "FINISHED", "media": map[string]any{"duration": 180.0, "contentId": "u"}}}}
			}
			switch p["type"] {
			case "GET_STATUS":
				if m.Namespace == nsReceiver {
					reply(map[string]any{"type": "RECEIVER_STATUS", "status": map[string]any{"applications": apps, "volume": map[string]any{"level": 0.4}}})
				} else {
					reply(media())
				}
			case "LAUNCH":
				launched = true
				apps = append(apps, map[string]any{"appId": mediaApp, "transportId": "web-1", "sessionId": "s-1"})
				reply(map[string]any{"type": "RECEIVER_STATUS", "status": map[string]any{"applications": apps}})
			case "LOAD":
				sid, state, at = 7, "PLAYING", p["currentTime"].(float64)
				reply(media())
			case "PAUSE":
				state = "PAUSED"
				reply(media())
			case "SEEK":
				at = p["currentTime"].(float64)
				reply(media())
			case "SET_VOLUME":
				reply(map[string]any{"type": "RECEIVER_STATUS", "status": map[string]any{"applications": apps}})
			}
		}
	}()
	return got
}

func TestChromecastLaunchesLoadsAndReads(t *testing.T) {
	ours, theirs := net.Pipe()
	got := fakeCast(t, theirs)
	r := newChromecast("x")
	r.dial = func(context.Context, string) (io.ReadWriteCloser, error) { return ours, nil }
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Load(ctx, Media{URL: "http://nas/stream/a.flac", Type: "audio/flac", Title: "歌", Artist: "人"}, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Load("LAUNCH"); !ok {
		t.Fatal("the media app must be started")
	}
	load, _ := got.Load("LOAD")
	if media := load.(map[string]any)["media"].(map[string]any); media["contentId"] != "http://nas/stream/a.flac" || media["contentType"] != "audio/flac" {
		t.Fatalf("load = %v", load)
	}
	st, err := r.Status(ctx)
	if err != nil || st.State != Playing || st.Position != 30*time.Second || st.Duration != 180*time.Second || st.Volume != 40 {
		t.Fatalf("status = %+v %v", st, err)
	}
	if err := r.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := r.Status(ctx); st.State != Paused {
		t.Fatalf("after pause: %+v", st)
	}
}

// fakeRenderer plays by a clock the test moves.
type fakeRenderer struct {
	mu     sync.Mutex
	loaded []Media
	state  string
	pos    time.Duration
	fail   map[string]bool // titles that cannot be played
	down   bool
	url    string
}

func (f *fakeRenderer) Load(_ context.Context, m Media, start time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loaded = append(f.loaded, m)
	f.state, f.pos, f.url = Playing, start, m.URL
	if f.fail[m.Title] {
		f.state = Failed
	}
	return nil
}
func (f *fakeRenderer) set(s string) error          { f.mu.Lock(); f.state = s; f.mu.Unlock(); return nil }
func (f *fakeRenderer) Play(context.Context) error  { return f.set(Playing) }
func (f *fakeRenderer) Pause(context.Context) error { return f.set(Paused) }
func (f *fakeRenderer) Stop(context.Context) error  { return f.set(Stopped) }
func (f *fakeRenderer) Seek(_ context.Context, to time.Duration) error {
	f.mu.Lock()
	f.pos = to
	f.mu.Unlock()
	return nil
}
func (f *fakeRenderer) SetVolume(context.Context, int) error { return nil }
func (f *fakeRenderer) Status(context.Context) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return Status{}, io.ErrUnexpectedEOF
	}
	return Status{State: f.state, Position: f.pos, Volume: 20, URL: f.url}, nil
}
func (f *fakeRenderer) Close() {}
func (f *fakeRenderer) titles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, m := range f.loaded {
		out = append(out, m.Title)
	}
	return out
}

func fast(t *testing.T) {
	old := [3]time.Duration{tickEvery, startFor, commandFor}
	tickEvery, startFor, commandFor = 20*time.Millisecond, 400*time.Millisecond, 2*time.Second
	t.Cleanup(func() { tickEvery, startFor, commandFor = old[0], old[1], old[2] })
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("waited for %s", what)
}

func TestPlayerActsAsATV(t *testing.T) {
	fast(t)
	remote := tv.NewRemote()
	f := &fakeRenderer{state: Stopped, fail: map[string]bool{"坏": true}}
	prepare := func(_ context.Context, ref tv.Ref) (Media, error) {
		return Media{URL: "u/" + ref.Title, Title: ref.Title}, nil
	}
	p := newPlayer("dlna-x", "音响", f, remote, prepare, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.run(ctx)
	waitFor(t, "online", func() bool { _, on := remote.State("dlna-x", false); return on })

	submit := func(c tv.Command) {
		t.Helper()
		r, err := remote.Submit("dlna-x", c)
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, c.Action+" done", func() bool { return remote.Status("dlna-x", r.ID).State == "done" })
	}
	items := []tv.Ref{{Online: "kuwo:1", Title: "一"}, {Online: "kuwo:2", Title: "坏"}, {Online: "kuwo:3", Title: "三"}}
	submit(tv.Command{Action: "load", Items: items, Index: 0, Position: 5000})
	pb, _ := remote.State("dlna-x", true)
	if pb.Phase != "playing" || pb.Index != 0 || pb.Count != 3 || len(pb.Queue) != 3 || pb.Position != 5000 {
		t.Fatalf("after load: %+v", pb)
	}
	// The song plays out: the next one cannot be played and is skipped.
	f.set(Stopped)
	waitFor(t, "skips to 三", func() bool { pb, _ := remote.State("dlna-x", false); return pb.Index == 2 && pb.Phase == "playing" })
	if got := strings.Join(f.titles(), ","); got != "一,坏,三" {
		t.Fatalf("loaded %s", got)
	}
	// A pause is not the end of a song.
	submit(tv.Command{Action: "pause"})
	time.Sleep(100 * time.Millisecond)
	if pb, _ := remote.State("dlna-x", false); pb.Phase != "paused" || pb.Index != 2 {
		t.Fatalf("after pause: %+v", pb)
	}
	submit(tv.Command{Action: "insert", Items: []tv.Ref{{Online: "kuwo:4", Title: "四"}}, QueueMode: "next"})
	submit(tv.Command{Action: "next"})
	if pb, _ := remote.State("dlna-x", true); pb.Current.Title != "四" || pb.Count != 4 {
		t.Fatalf("after insert next: %+v", pb)
	}
	// A device that stops answering goes offline, and comes back by itself.
	f.mu.Lock()
	f.down = true
	f.mu.Unlock()
	// Every screen shows it offline at once, not when its last report grows old.
	waitFor(t, "offline", func() bool { _, on := remote.State("dlna-x", false); return !on })
	f.mu.Lock()
	f.down = false
	f.mu.Unlock()
	waitFor(t, "back", func() bool { _, on := remote.State("dlna-x", false); return on })

	// Another app puts something on: ours shows paused where it was, and is not touched.
	submit(tv.Command{Action: "play"})
	submit(tv.Command{Action: "seek", Position: 7000})
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	f.url, f.pos, f.state = "other/radio", 99*time.Second, Playing
	f.mu.Unlock()
	waitFor(t, "held for the other app", func() bool {
		pb, _ := remote.State("dlna-x", false)
		return pb.Phase == "paused" && pb.Position == 7000
	})
	submit(tv.Command{Action: "pause"})
	if f.mu.Lock(); f.state != Playing || f.url != "other/radio" {
		t.Fatalf("the other app was paused: %s %s", f.state, f.url)
	}
	f.mu.Unlock()
	// Play takes the device back, at our place.
	submit(tv.Command{Action: "play"})
	f.mu.Lock()
	last := f.loaded[len(f.loaded)-1]
	at := f.pos
	f.mu.Unlock()
	if last.Title != "四" || at != 7*time.Second {
		t.Fatalf("taken back with %s at %v", last.Title, at)
	}
	waitFor(t, "ours again", func() bool { pb, _ := remote.State("dlna-x", false); return pb.Phase == "playing" })
}

func TestManagerKeepsDevicesTurnedOn(t *testing.T) {
	fast(t)
	settings := map[string]string{}
	var mu sync.Mutex
	f := &fakeRenderer{state: Stopped}
	m := &Manager{Remote: tv.NewRemote(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Setting: func(_ context.Context, k string) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			return settings[k], nil
		},
		SetSetting: func(_ context.Context, k, v string) error { mu.Lock(); defer mu.Unlock(); settings[k] = v; return nil },
		Finders: []Finder{func(context.Context) ([]Device, error) {
			return []Device{{ID: "dlna-a", Name: "书房", Kind: DLNA, Location: "http://x"}}, nil
		}},
		Open: func(Device) Renderer { return f },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	waitFor(t, "found", func() bool { return len(m.Listing().Devices) == 1 })
	if err := m.Set("dlna-b", true); err != ErrNoDevice {
		t.Fatalf("unknown device: %v", err)
	}
	if err := m.Set("dlna-a", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "online", func() bool { l := m.Listing(); return l.Devices[0].On && l.Devices[0].Online })
	mu.Lock()
	kept := settings[settingOn]
	mu.Unlock()
	if !strings.Contains(kept, `"location":"http://x"`) {
		t.Fatalf("saved %s", kept)
	}
	if err := m.Set("dlna-a", false); err != nil {
		t.Fatal(err)
	}
	if len(m.On()) != 0 {
		t.Fatal("still on")
	}
}

func TestTXTNamesAreUnescaped(t *testing.T) {
	for in, want := range map[string]string{
		`fn=\229\141\167\229\174\164 TV`: "fn=卧室 TV",
		`fn=Living\ Room`:                "fn=Living Room",
		`fn=a\"b\\c`:                     `fn=a"b\c`,
		"fn=plain":                       "fn=plain",
		`fn=end\`:                        `fn=end\`,
	} {
		if got := unescapeTXT(in); got != want {
			t.Errorf("unescapeTXT(%q) = %q, want %q", in, got, want)
		}
	}
}
