package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Every browser signed in with the one family password is its own: named apart, renamed
// alone, signed out alone.
func TestBrowsersSharingAPasswordStayApart(t *testing.T) {
	s, _ := newServer(t)
	h := s.Handler()
	signIn := func(ua string) *http.Cookie {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
		r.Header.Set("User-Agent", ua)
		if err := s.startSession(w, r, sessionTTL); err != nil {
			t.Fatal(err)
		}
		return w.Result().Cookies()[0]
	}
	call := func(ck *http.Cookie, method, path, body string) (int, string) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(ck)
		r.Header.Set("X-Requested-With", "trackferry")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var out bytes.Buffer
		_ = json.Compact(&out, w.Body.Bytes())
		return w.Code, out.String()
	}
	iphone := "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)"
	a, b, c := signIn(iphone), signIn(iphone), signIn("Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X)")
	for ck, want := range map[*http.Cookie]string{a: "iPhone", b: "iPhone 2", c: "iPad"} {
		if code, body := call(ck, "GET", "/api/me", ""); code != 200 || body != `{"name":"`+want+`"}` {
			t.Fatalf("%d %s, want %s", code, body, want)
		}
	}
	if code, _ := call(a, "PUT", "/api/me", `{"name":"  妈妈的   手机 "}`); code != 200 {
		t.Fatal(code)
	}
	if _, body := call(a, "GET", "/api/me", ""); body != `{"name":"妈妈的 手机"}` {
		t.Fatal(body)
	}
	if _, body := call(b, "GET", "/api/me", ""); body != `{"name":"iPhone 2"}` {
		t.Fatal("renaming one browser renamed another:", body)
	}
	for _, bad := range []string{`{"name":"  "}`, `{"name":"` + strings.Repeat("长", 21) + `"}`} {
		if code, _ := call(b, "PUT", "/api/me", bad); code != 400 {
			t.Fatal(bad, code)
		}
	}
	if code, _ := call(a, "POST", "/api/logout", ""); code >= 300 {
		t.Fatal("logout", code)
	}
	if code, _ := call(b, "GET", "/api/me", ""); code != 200 {
		t.Fatal("one browser signing out signed out another:", code)
	}
	if ok, _ := s.Store.SessionValid(t.Context(), c.Value, time.Now()); !ok {
		t.Fatal("iPad was signed out")
	}
}

func TestTookOnlyFromAnotherBrowser(t *testing.T) {
	for _, c := range []struct{ me, had, want string }{
		{"客厅 iPad", "", ""}, {"客厅 iPad", "客厅 iPad", ""}, {"客厅 iPad", "主卧 iPhone", "主卧 iPhone"}, {"", "主卧 iPhone", "主卧 iPhone"},
	} {
		if got := took(c.me, c.had); got != c.want {
			t.Fatalf("%+v: %q", c, got)
		}
	}
	for ua, want := range map[string]string{
		"Mozilla/5.0 (Linux; Android 14; Pixel) Mobile Safari": "安卓手机",
		"Mozilla/5.0 (Linux; Android 14; Tab)":                 "安卓平板",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0)":         "Mac",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64)":            "Windows 电脑",
		"curl/8": "浏览器",
	} {
		if got := deviceName(ua); got != want {
			t.Fatalf("%s: %s", ua, got)
		}
	}
}
