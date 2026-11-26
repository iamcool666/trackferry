package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func tvWorld(t *testing.T, body string, sign func(sum string) []byte) (*Checker, *atomic.Int32) {
	pub, key, _ := ed25519.GenerateKey(nil)
	oldKey, oldSite := publicKey, githubSite
	publicKey = base64.StdEncoding.EncodeToString(pub)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		name := "/madrays/trackferry/releases/download/v1.3.0/trackferry-tv-1.3.0-arm64-v8a.apk"
		switch r.URL.Path {
		case name:
			hits.Add(1)
			io.WriteString(rw, body)
		case name + ".sig":
			sum := sha256.Sum256([]byte(body))
			msg := sign(hex.EncodeToString(sum[:]))
			io.WriteString(rw, base64.StdEncoding.EncodeToString(ed25519.Sign(key, msg))+"\n")
		default:
			http.NotFound(rw, r)
		}
	}))
	githubSite = srv.URL
	t.Cleanup(func() { publicKey, githubSite = oldKey, oldSite; srv.Close() })
	c := New("madrays/trackferry", "1.3.0", true, slog.New(slog.DiscardHandler))
	c.SetTVDir(t.TempDir())
	return c, &hits
}

func waitTV(t *testing.T, c *Checker) TVApp {
	t.Helper()
	for range 400 {
		st, err := c.TVApp("arm64-v8a")
		if err != nil {
			t.Fatal(err)
		}
		if st.Phase != "download" {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("never finished")
	return TVApp{}
}

func TestTVAppFetchedVerifiedAndKept(t *testing.T) {
	c, hits := tvWorld(t, "apk bytes", func(sum string) []byte { return Message("1.3.0", "android/arm64-v8a", sum) })
	if st, _ := c.TVApp("arm64-v8a"); st.Phase != "" {
		t.Fatalf("ready before any download: %+v", st)
	}
	if _, err := c.PrepareTV(context.Background(), "arm64-v8a"); err != nil {
		t.Fatal(err)
	}
	st := waitTV(t, c)
	if st.Phase != "ready" || st.Size != int64(len("apk bytes")) || st.SHA256 == "" {
		t.Fatalf("not ready: %+v", st)
	}
	path, _, err := c.TVFile("arm64-v8a")
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "apk bytes" {
		t.Fatalf("kept %q", raw)
	}
	// Ready already: asking again downloads nothing.
	c.PrepareTV(context.Background(), "arm64-v8a")
	if hits.Load() != 1 {
		t.Fatalf("downloaded %d times", hits.Load())
	}
	// A damaged file is thrown away and fetched again: it heals itself.
	os.WriteFile(path, []byte("apk bytez"), 0o600)
	os.Chtimes(path, time.Now().Add(time.Hour), time.Now().Add(time.Hour))
	if st, _ := c.TVApp("arm64-v8a"); st.Phase == "ready" {
		t.Fatal("a damaged file passed")
	}
	c.PrepareTV(context.Background(), "arm64-v8a")
	if st := waitTV(t, c); st.Phase != "ready" || hits.Load() != 2 {
		t.Fatalf("did not heal: %+v, %d downloads", st, hits.Load())
	}
}

func TestTVAppRefusesTheServerSignature(t *testing.T) {
	// A signature for the server binary of the same version must not pass for the app.
	c, _ := tvWorld(t, "apk bytes", func(sum string) []byte { return Message("1.3.0", "arm64", sum) })
	c.PrepareTV(context.Background(), "arm64-v8a")
	st := waitTV(t, c)
	if st.Phase != "failed" || !strings.Contains(st.Error, "签名") {
		t.Fatalf("accepted: %+v", st)
	}
	if _, _, err := c.TVFile("arm64-v8a"); err == nil {
		t.Fatal("handed out an unsigned app")
	}
}

func TestTVAppNeedsAVersion(t *testing.T) {
	c := New("madrays/trackferry", "dev", true, slog.New(slog.DiscardHandler))
	c.SetTVDir(t.TempDir())
	if _, err := c.TVApp("arm64-v8a"); err == nil {
		t.Fatal("offered an app without a version")
	}
	c = New("madrays/trackferry", "1.3.0", true, slog.New(slog.DiscardHandler))
	c.SetTVDir(t.TempDir())
	if _, err := c.TVApp("x86"); err == nil {
		t.Fatal("offered an app for an unknown processor")
	}
}

func TestMessageKeepsLinux(t *testing.T) {
	if string(Message("1.0.0", "amd64", "ab")) != "trackferry\n1.0.0\nlinux/amd64\nab\n" {
		t.Fatal("the server's signatures changed")
	}
}
