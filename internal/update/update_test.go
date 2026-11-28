package update

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"1.0.1", "1.0.0", true}, {"v1.10.0", "1.9.9", true}, {"1.0.0", "1.0.0", false}, {"0.9.0", "1.0.0", false}, {"2.0.0", "dev", false}, {"2.0.0-rc1", "1.0.0", false}} {
		if Newer(c.a, c.b) != c.want {
			t.Errorf("Newer(%q, %q) != %v", c.a, c.b, c.want)
		}
	}
}

func TestCheck(t *testing.T) {
	calls, code := 0, 200
	body := `[{"tag_name":"v1.1.9","name":"TrackFerry 1.1.9","html_url":"https://github.com/madrays/trackferry/releases/tag/v1.1.9","body":"旧正式版","prerelease":false},{"tag_name":"dev","name":"TrackFerry Dev","html_url":"https://github.com/madrays/trackferry/releases/tag/dev","body":"新 dev 版。Version: 0.0.42.","prerelease":true}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/madrays/trackferry/releases" {
			t.Error(r.URL.Path)
		}
		w.WriteHeader(code)
		io.WriteString(w, body)
	}))
	defer srv.Close()
	mk := func() *Checker {
		c := New("madrays/trackferry", "1.0.0", true, slog.New(slog.DiscardHandler))
		c.API = srv.URL
		return c
	}
	c := mk()
	if st := c.Check(context.Background()); !st.Newer || st.Latest != "1.1.9" || st.URL != "https://github.com/madrays/trackferry/releases/tag/v1.1.9" || st.Error != "" {
		t.Fatalf("%+v", st)
	}
	c.Check(context.Background())
	if calls != 1 {
		t.Fatalf("asked %d times", calls)
	}
	body = `[{"tag_name":"v1.2.0","name":"TrackFerry 1.2.0","html_url":"javascript:alert(1)","prerelease":false}]`
	if st := mk().Check(context.Background()); st.Error == "" || st.URL != "" {
		t.Fatalf("%+v", st)
	}
	code = 404
	if st := mk().Check(context.Background()); st.Error != errNone.Error() || st.Newer {
		t.Fatalf("%+v", st)
	}
}

func TestDevChannelOnlyFollowsRollingDevRelease(t *testing.T) {
	body := `[{"tag_name":"v1.12.8","name":"TrackFerry 1.12.8","html_url":"https://github.com/madrays/trackferry/releases/tag/v1.12.8","prerelease":false},{"tag_name":"dev","name":"TrackFerry Dev","html_url":"https://github.com/madrays/trackferry/releases/tag/dev","body":"开发版。Version: 0.0.43.","prerelease":true}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
	defer srv.Close()
	c := New("madrays/trackferry", "0.0.42", true, slog.New(slog.DiscardHandler))
	c.API, c.Channel = srv.URL, "dev"
	st := c.Check(context.Background())
	if !st.Newer || st.Latest != "0.0.43" || st.URL != "https://github.com/madrays/trackferry/releases/tag/dev" {
		t.Fatalf("%+v", st)
	}
}
