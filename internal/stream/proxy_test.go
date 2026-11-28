package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func proxyGet(t *testing.T, s *Server, open Opener, method, rng string) *httptest.ResponseRecorder {
	t.Helper()
	h, err := s.Proxy(open, "x", time.Minute, "audio/mpeg")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(h.URL)
	r := httptest.NewRequest(method, u.Path, nil)
	if rng != "" {
		r.Header.Set("Range", rng)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestProxyForwardsRange(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "secret=1")
		http.ServeContent(w, r, "a.mp3", time.Time{}, strings.NewReader("0123456789"))
	}))
	defer up.Close()
	open := func(ctx context.Context, rng string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", up.URL, nil)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		return http.DefaultClient.Do(req)
	}
	s := New("http://10.0.0.2:1", t.TempDir())
	w := proxyGet(t, s, open, "GET", "bytes=2-5")
	if w.Code != 206 || w.Body.String() != "2345" || w.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("%d %q %v", w.Code, w.Body, w.Header())
	}
	if w.Header().Get("Content-Type") != "audio/mpeg" || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("headers %v", w.Header())
	}
	if w = proxyGet(t, s, open, "HEAD", ""); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "10" {
		t.Fatalf("head %d %v", w.Code, w.Header())
	}
}

func TestProxySourceFailureHidesAddress(t *testing.T) {
	w := proxyGet(t, New("http://10.0.0.2:1", t.TempDir()), func(context.Context, string) (*http.Response, error) {
		return nil, errors.New("dial https://cdn.example/secret")
	}, "GET", "")
	b, _ := io.ReadAll(w.Result().Body)
	if w.Code != http.StatusBadGateway || strings.Contains(string(b), "secret") {
		t.Fatalf("%d %s", w.Code, b)
	}
}

// counting counts how often an opener went out, for the tests that want the probes answered
// without the source hearing of them.
type counting struct {
	mu    sync.Mutex
	opens int
}

func (c *counting) open(up string) Opener {
	return func(ctx context.Context, rng string) (*http.Response, error) {
		c.mu.Lock()
		c.opens++
		c.mu.Unlock()
		req, _ := http.NewRequestWithContext(ctx, "GET", up, nil)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		return http.DefaultClient.Do(req)
	}
}

func (c *counting) took() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.opens
}

func TestProxyBuffersWholeSongForProbes(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 3<<20)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.flac", time.Time{}, bytes.NewReader(body))
	}))
	defer up.Close()
	opens := &counting{}
	open := opens.open(up.URL)
	s := New("http://10.0.0.2:1", t.TempDir())
	h, err := s.Proxy(open, "x", time.Minute, "audio/mpeg")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()
	u, _ := url.Parse(h.URL)
	// The speaker takes a bite and hangs up, as speakers do while they sniff the file.
	req, _ := http.NewRequest("GET", srv.URL+u.Path, nil)
	req.Header.Set("Range", "bytes=0-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	bite := make([]byte, 100)
	if _, err = io.ReadFull(resp.Body, bite); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// What it left is drained into memory, and the probes that follow never reach the source.
	for i := 0; i < 500 && h.it.whole() == nil; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if h.it.whole() == nil {
		t.Fatal("the song never made it into memory")
	}
	req, _ = http.NewRequest("GET", srv.URL+u.Path, nil)
	req.Header.Set("Range", "bytes=1000000-1000009")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent || w.Body.String() != string(body[1000000:1000010]) {
		t.Fatalf("%d %q", w.Code, w.Body.String())
	}
	if n := opens.took(); n != 1 {
		t.Fatal("the probe went back to the source:", n)
	}
}

func TestProxyLeavesUnknownLengthAlone(t *testing.T) {
	var mu sync.Mutex
	opens := 0
	open := func(ctx context.Context, rng string) (*http.Response, error) {
		_ = rng // this source does not seek
		mu.Lock()
		opens++
		mu.Unlock()
		return &http.Response{
			StatusCode:    200,
			Header:        http.Header{"Content-Type": []string{"audio/mpeg"}},
			ContentLength: -1,
			Body:          io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 1<<20))),
		}, nil
	}
	s := New("http://10.0.0.2:1", t.TempDir())
	h, err := s.Proxy(open, "x", time.Minute, "audio/mpeg")
	if err != nil {
		t.Fatal(err)
	}
	proxyGetHandle(t, s, h, "")
	if h.it.whole() != nil {
		t.Fatal("buffered a stream of no length")
	}
	proxyGetHandle(t, s, h, "bytes=500-")
	mu.Lock()
	defer mu.Unlock()
	if opens != 2 {
		t.Fatal("the second pull did not go to the source:", opens)
	}
}

func proxyGetHandle(t *testing.T, s *Server, h *Handle, rng string) *httptest.ResponseRecorder {
	t.Helper()
	u, _ := url.Parse(h.URL)
	r := httptest.NewRequest("GET", u.Path, nil)
	if rng != "" {
		r.Header.Set("Range", rng)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestItemSourceFromMemory(t *testing.T) {
	it := &item{open: func(context.Context, string) (*http.Response, error) {
		t.Fatal("the source was asked")
		return nil, errors.New("no")
	}}
	it.keepWhole([]byte("0123456789"))
	r, total, err := it.source(context.Background(), 3, -1)
	if err != nil || total != 10 {
		t.Fatal(err, total)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if string(got) != "3456789" {
		t.Fatalf("%q", got)
	}
}
