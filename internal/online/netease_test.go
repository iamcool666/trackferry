package online

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNeteasePlaysWhatTheAccountMay(t *testing.T) {
	stage := 801
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		in := strings.Contains(r.Header.Get("Cookie"), "MUSIC_U=secret")
		r.ParseForm()
		switch r.URL.Path {
		case "/api/cloudsearch/pc":
			pl := "0"
			if in {
				pl = "320000"
			}
			w.Write([]byte(`{"code":200,"result":{"songs":[{"id":1,"name":"甲","dt":200000,"fee":1,"ar":[{"name":"人"}],"privilege":{"pl":` + pl + `,"st":0}},
				{"id":2,"name":"乙","dt":200000,"fee":8,"ar":[{"name":"人"}],"privilege":{"pl":320000,"st":0}}]}}`))
		case "/api/song/enhance/player/url/v1":
			switch r.Form.Get("ids") {
			case "[1]":
				w.Write([]byte(`{"code":200,"data":[{"url":"http://m/1.mp3","code":200,"freeTrialInfo":{"start":0,"end":30}}]}`))
			case "[2]":
				w.Write([]byte(`{"code":200,"data":[{"url":"http://m/2.mp3","code":200,"freeTrialInfo":null}]}`))
			default:
				w.Write([]byte(`{"code":200,"data":[{"url":null,"code":-110}]}`))
			}
		case "/api/login/qrcode/unikey":
			w.Write([]byte(`{"code":200,"unikey":"k"}`))
		case "/api/login/qrcode/client/login":
			if stage == 803 {
				http.SetCookie(w, &http.Cookie{Name: "MUSIC_U", Value: "secret"})
				w.Write([]byte(`{"code":803}`))
				return
			}
			w.Write([]byte(`{"code":801}`))
		case "/api/nuser/account/get":
			if in && stage == 803 {
				w.Write([]byte(`{"code":200,"account":{},"profile":{"nickname":"某人","vipType":11}}`))
				return
			}
			w.Write([]byte(`{"code":200,"account":null,"profile":null}`))
		default:
			w.Write([]byte(`{"code":200}`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "private", "netease.json")
	n := &Netease{Client: srv.Client(), Base: srv.URL, File: file}

	got, err := n.Search(ctx, "甲")
	if err != nil || len(got) != 2 || !got[0].Restricted || got[1].Restricted || got[1].Member {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := n.Resolve(ctx, Candidate{ID: "1"}); !errors.Is(err, ErrTrial) {
		t.Fatal(err)
	}
	if u, err := n.Resolve(ctx, Candidate{ID: "2"}); err != nil || u != "http://m/2.mp3" {
		t.Fatal(u, err)
	}
	if _, err := n.Resolve(ctx, Candidate{ID: "3"}); !errors.Is(err, ErrRestricted) {
		t.Fatal(err)
	}
	if _, err := n.QR(ctx); !errors.Is(err, ErrNoQR) {
		t.Fatal(err)
	}
	if u, err := n.StartQR(ctx); err != nil || !strings.HasSuffix(u, "codekey=k") {
		t.Fatal(u, err)
	}
	if s, _ := n.QR(ctx); s != QRWaiting {
		t.Fatal(s)
	}
	stage = 803
	if s, err := n.QR(ctx); s != QRDone || err != nil {
		t.Fatal(s, err)
	}
	if a := n.Account(); !a.Linked || a.Name != "某人" || !a.VIP || a.Expired {
		t.Fatalf("%+v", a)
	}
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatal(err)
	}
	if got, _ := n.Search(ctx, "甲"); got[0].Restricted || !got[0].Member {
		t.Fatalf("%+v", got[0])
	}
	// A browser's session instead of the code: only a live one is taken.
	b := &Netease{Client: srv.Client(), Base: srv.URL, File: filepath.Join(t.TempDir(), "n.json")}
	live := "secret" + strings.Repeat("a", 40)
	for _, bad := range []string{"", "x", "__csrf=1", "MUSIC_U=short", strings.Repeat("b", 40), live + "\r\nX: y"} {
		if err := b.SignIn(ctx, bad); err == nil || b.Account().Linked {
			t.Fatalf("%q taken", bad)
		}
	}
	for _, good := range []string{" " + live + "\n", "NMTID=1; MUSIC_U=" + live + "; __csrf=abc"} {
		if err := b.SignIn(ctx, good); err != nil || b.Account().Name != "某人" {
			t.Fatal(err)
		}
	}
	// Read back; then the session ends elsewhere: free songs only, and the owner is told.
	m := &Netease{Client: srv.Client(), Base: srv.URL, File: file}
	if err := m.Load(); err != nil || !m.Account().Linked {
		t.Fatal(err)
	}
	stage = 0
	if m.Check(ctx); !m.Account().Expired {
		t.Fatal("not noticed")
	}
	if got, _ := m.Search(ctx, "甲"); !got[0].Restricted {
		t.Fatal("still used")
	}
	if err := m.Logout(ctx); err != nil || m.Account().Linked {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("still there")
	}
}

func TestNeteaseSignedInAsksAsTheDesktopClientAndThenAsAStranger(t *testing.T) {
	frame := make([]byte, 417) // MPEG1 layer 3, 128 kbit/s, 44.1 kHz
	copy(frame, []byte{0xff, 0xfb, 0x90, 0})
	mp3 := bytes.Repeat(frame, 7000)
	var signed, old, stranger string
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer := ""
		switch {
		case r.URL.Path == "/good":
			http.ServeContent(w, r, "a.mp3", time.Time{}, bytes.NewReader(mp3))
			return
		case r.URL.Path == "/bad":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			return
		case r.Header.Get("Cookie") == "":
			answer = stranger
		case !strings.Contains(r.Header.Get("Cookie"), "os=pc"):
			t.Error("asked as a browser")
		case strings.HasSuffix(r.URL.Path, "/v1"):
			answer = signed
		default:
			answer = old
		}
		asked = append(asked, r.URL.Path)
		w.Write([]byte(`{"code":200,"data":[` + strings.ReplaceAll(answer, "BASE", "http://"+r.Host) + `]}`))
	}))
	defer srv.Close()
	n := &Netease{Client: srv.Client(), Base: srv.URL, ses: neteaseSession{Cookie: "MUSIC_U=x"}}
	s := New(slog.New(slog.NewTextHandler(io.Discard, nil)), srv.Client(), n)
	s.SetQuality(MP3128) // one quality: the calls counted are the fallbacks between clients
	const good, bad = `{"url":"BASE/good","code":200,"type":"mp3"}`, `{"url":"BASE/bad","code":200,"type":"mp3"}`
	const m4a, none = `{"url":"BASE/good","code":200,"type":"m4a"}`, `{"url":null,"code":404}`
	const denied, trial = `{"url":null,"code":-110}`, `{"url":"BASE/good","code":200,"freeTrialInfo":{"end":30}}`
	for name, c := range map[string]struct {
		signed, old, stranger string
		calls                 int
		err                   error
	}{
		"signed in": {good, bad, bad, 1, nil},
		"older":     {m4a, good, bad, 2, nil},
		"refused":   {bad, good, good, 2, nil}, // the CDN's 403
		"denied":    {denied, bad, good, 2, nil},
		"empty":     {"", none, good, 3, nil},
		"nowhere":   {m4a, none, trial, 3, ErrNoAudio},
		"no right":  {denied, good, denied, 2, ErrRestricted},
		"trial":     {trial, good, good, 1, ErrTrial},
		"dead":      {bad, bad, bad, 2, httpError(403)},
	} {
		asked, signed, old, stranger = nil, c.signed, c.old, c.stranger
		_, err := s.resolveOne(context.Background(), n, Candidate{ID: "1", Duration: 180 * time.Second})
		if !errors.Is(err, c.err) || len(asked) != c.calls {
			t.Errorf("%s: %v %v", name, err, asked)
		}
		if err != nil && strings.Contains(reason(err), "/good") {
			t.Error(reason(err))
		}
	}
}
