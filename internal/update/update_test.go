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

func TestCheckIncludesPrereleases(t *testing.T) {
	calls := 0
	body := `[
		{"tag_name":"v1.2.0","html_url":"https://github.com/iamcool666/trackferry/releases/tag/v1.2.0","body":"dev build","prerelease":true},
		{"tag_name":"v1.1.9","html_url":"https://github.com/iamcool666/trackferry/releases/tag/v1.1.9","body":"stable","prerelease":false}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/iamcool666/trackferry/releases" || r.URL.Query().Get("per_page") != "100" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		io.WriteString(w, body)
	}))
	defer srv.Close()

	c := New("iamcool666/trackferry", "1.0.0", true, slog.New(slog.DiscardHandler))
	c.API = srv.URL
	st := c.Check(context.Background())
	if !st.Newer || st.Latest != "1.2.0" || st.Notes != "dev build" || st.Error != "" {
		t.Fatalf("%+v", st)
	}
	c.Check(context.Background())
	if calls != 1 {
		t.Fatalf("asked %d times", calls)
	}
}

func TestCheckNoReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := New("iamcool666/trackferry", "1.0.0", true, slog.New(slog.DiscardHandler))
	c.API = srv.URL
	if st := c.Check(context.Background()); st.Error != errNone.Error() || st.Newer {
		t.Fatalf("%+v", st)
	}
}
