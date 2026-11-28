package update

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxy(t *testing.T) {
	asked := ""
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Host
		io.WriteString(w, `[{"tag_name":"v1.2.0","html_url":"https://github.com/x"}]`)
	}))
	defer proxy.Close()
	// The environment's proxy is not ours.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	file := filepath.Join(t.TempDir(), "private", "update_proxy")
	mk := func() *Checker {
		c := New("madrays/trackferry", "1.0.0", true, slog.New(slog.DiscardHandler))
		c.API = "http://github.invalid"
		if err := c.LoadProxy(file); err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := mk()
	if st := c.Check(context.Background()); st.Error == "" {
		t.Fatal("reached a host that does not exist")
	}
	for _, bad := range []string{"192.168.1.2:7890", "ftp://a:1", "http://", "http://a/path", "file:///etc"} {
		if c.SetProxy(bad) == nil {
			t.Error(bad)
		}
	}
	addr := strings.Replace(proxy.URL, "http://", "http://me:secret@", 1)
	if err := c.SetProxy(addr); err != nil {
		t.Fatal(err)
	}
	st := c.Check(context.Background())
	if asked != "github.invalid" || !st.Newer || strings.Contains(st.Proxy, "secret") {
		t.Fatal(asked, st)
	}
	if err := c.SetProxy(st.Proxy); err != nil { // saved unchanged from the page
		t.Fatal(err)
	}
	c = mk()
	if u := c.proxy.Load(); u == nil || u.String() != addr {
		t.Fatal("the password was lost")
	}
	if i, _ := os.Stat(file); i.Mode().Perm() != 0o600 {
		t.Fatal(i.Mode())
	}
	if err := c.SetProxy(""); err != nil || mk().proxy.Load() != nil {
		t.Fatal("not removed", err)
	}
}
