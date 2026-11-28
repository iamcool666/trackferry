package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type world struct {
	t    *testing.T
	data string
	key  ed25519.PrivateKey
	body string // the binary on offer
	sig  string
	file string // its name in the release
}

func newWorld(t *testing.T) *world {
	pub, key, _ := ed25519.GenerateKey(nil)
	oldKey, oldSite := publicKey, githubSite
	publicKey = base64.StdEncoding.EncodeToString(pub)
	w := &world{t: t, data: t.TempDir(), key: key, body: "new program", file: "trackferry-linux-" + runtime.GOARCH}
	w.sig = w.sign("1.2.0", w.body)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		name := "/madrays/trackferry/releases/download/v1.2.0/" + w.file
		switch r.URL.Path {
		case name:
			io.WriteString(rw, w.body)
		case name + ".sig":
			io.WriteString(rw, w.sig+"\n")
		default:
			http.NotFound(rw, r)
		}
	}))
	githubSite = srv.URL
	t.Cleanup(func() { publicKey, githubSite = oldKey, oldSite; srv.Close() })
	t.Setenv(launchedEnv, "")
	os.WriteFile(filepath.Join(w.data, "trackferry.db"), []byte("old database"), 0o600)
	return w
}

func (w *world) sign(version, body string) string {
	sum := sha256.Sum256([]byte(body))
	return base64.StdEncoding.EncodeToString(ed25519.Sign(w.key, Message(version, runtime.GOARCH, hex.EncodeToString(sum[:]))))
}

// install runs a whole installation and returns its error text.
func (w *world) install() string {
	done := make(chan struct{})
	c := New("madrays/trackferry", "1.0.0", true, slog.New(slog.DiscardHandler))
	c.Installer = &Installer{Data: w.data, Restart: func() { close(done) },
		Backup: func(_ context.Context, p string) error { return os.WriteFile(p, []byte("backup"), 0o600) },
		Try:    func(context.Context, string) (string, error) { return "trackferry 1.2.0 (abc)\n", nil }}
	if err := c.Install(context.Background()); !errors.Is(err, ErrNothing) {
		w.t.Fatal("installed without a newer version")
	}
	c.remember(&release{Tag: "v1.2.0", URL: "https://github.com/x"}, nil)
	if err := c.Install(context.Background()); err != nil {
		w.t.Fatal(err)
	}
	for range 400 {
		select {
		case <-done:
			return ""
		case <-time.After(10 * time.Millisecond):
		}
		if p := c.Status().Install; p.Error != "" {
			return p.Error
		}
	}
	w.t.Fatal("no end")
	return ""
}

func (w *world) record() Record { r, _ := readRecord(Dir(w.data)); return r }
func (w *world) db() string {
	b, _ := os.ReadFile(filepath.Join(w.data, "trackferry.db"))
	return string(b)
}

// launch returns the binary that was started, "" for the image's own.
func (w *world) launch(version string, serving bool) (string, string) {
	started := ""
	note := Launch(w.data, version, "stable", serving, func(p string, env []string) error { started = p; return nil })
	return started, note
}

func TestInstallAndLaunch(t *testing.T) {
	w := newWorld(t)
	if e := w.install(); e != "" {
		t.Fatal(e)
	}
	if p, _ := w.launch("1.0.0", false); p != "" {
		t.Fatal("a short command ran an unproven version")
	}
	p, note := w.launch("1.0.0", true)
	if filepath.Base(p) != "trackferry-1.2.0" || note != "" {
		t.Fatal(p, note)
	}
	t.Setenv(launchedEnv, "/trackferry")
	if p, _ := w.launch("1.2.0", true); p != "" {
		t.Fatal("launched itself")
	}
	if err := Confirm(w.data, "1.2.0", "stable"); err != nil || !w.record().Confirmed {
		t.Fatal(err)
	}
	t.Setenv(launchedEnv, "")
	for range 5 {
		if p, _ := w.launch("1.0.0", true); p == "" {
			t.Fatal("a proven version stays")
		}
	}
	if p, _ := w.launch("1.0.0", false); p == "" {
		t.Fatal("short commands use a proven version")
	}
	if p, _ := w.launch("1.2.0", true); p != "" {
		t.Fatal("the image has caught up")
	}
	if _, err := os.Stat(filepath.Join(Dir(w.data), "installed.json")); err == nil {
		t.Fatal("not cleaned up")
	}
}

func TestInstallRefuses(t *testing.T) {
	w := newWorld(t)
	w.body = "tampered"
	if e := w.install(); !strings.Contains(e, "签名") {
		t.Fatal(e)
	}
	w.body = "new program"
	w.sig = w.sign("1.1.0", w.body) // a genuine signature of another version
	if e := w.install(); !strings.Contains(e, "签名") {
		t.Fatal(e)
	}
	if p, _ := w.launch("1.0.0", true); p != "" {
		t.Fatal(p)
	}
}

func TestLaunchRefusesChangedBinary(t *testing.T) {
	w := newWorld(t)
	w.install()
	os.WriteFile(filepath.Join(Dir(w.data), "trackferry-1.2.0"), []byte("changed"), 0o700)
	if p, note := w.launch("1.0.0", true); p != "" || note == "" || w.record().FailedVersion != "1.2.0" {
		t.Fatal(p, note)
	}
}

func TestLaunchIgnoresPathInRecord(t *testing.T) {
	w := newWorld(t)
	os.MkdirAll(Dir(w.data), 0o700)
	Record{Version: "../../9.9.9"}.write(Dir(w.data))
	if p, _ := w.launch("1.0.0", true); p != "" {
		t.Fatal(p)
	}
}

func TestLaunchGivesUp(t *testing.T) {
	w := newWorld(t)
	w.install()
	for range maxTries {
		if p, _ := w.launch("1.0.0", true); p == "" {
			t.Fatal("gave up too early")
		}
	}
	os.WriteFile(filepath.Join(w.data, "trackferry.db"), []byte("migrated"), 0o600)
	os.WriteFile(filepath.Join(w.data, "trackferry.db-wal"), []byte("x"), 0o600)
	if p, note := w.launch("1.0.0", true); p != "" || note == "" {
		t.Fatal(p, note)
	}
	if _, err := os.Stat(filepath.Join(w.data, "trackferry.db-wal")); err == nil || w.db() != "backup" {
		t.Fatal("database not restored:", w.db())
	}
	if p, note := w.launch("1.0.0", true); p != "" || note != "" {
		t.Fatal("tried again", p, note)
	}
	if e := w.install(); e != "" || w.record().FailedVersion != "" {
		t.Fatal("cannot be installed again", e)
	}
}

func TestLaunchGoesBackToPreviousInstalled(t *testing.T) {
	w := newWorld(t)
	dir := Dir(w.data)
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "trackferry-1.1.0"), []byte("good"), 0o700)
	Record{Version: "1.1.0", Signature: w.sign("1.1.0", "good"), Confirmed: true}.write(dir)
	if e := w.install(); e != "" {
		t.Fatal(e)
	}
	for range maxTries {
		w.launch("1.0.0", true)
	}
	w.launch("1.0.0", true)
	if p, note := w.launch("1.0.0", true); filepath.Base(p) != "trackferry-1.1.0" || note != "" {
		t.Fatal(p, note)
	}
	if r := w.record(); r.FailedVersion != "1.2.0" || !r.Confirmed || w.db() != "backup" {
		t.Fatal(r, w.db())
	}
}

func TestAssetNames(t *testing.T) {
	for _, c := range []struct{ arch, want string }{{"amd64", "trackferry-1.9.0-linux-amd64"}, {"arm64", "trackferry-1.9.0-linux-arm64"}, {"arm", "trackferry-1.9.0-linux-armv7"}} {
		got := ServerAssets("1.9.0", c.arch)
		if got[0] != c.want || got[1] != "trackferry-linux-"+c.arch {
			t.Errorf("%s: %v", c.arch, got)
		}
	}
	if got := TVAssets("1.9.0", "armeabi-v7a"); got[0] != "trackferry-tv-1.9.0-android-armeabi-v7a.apk" || got[1] != "trackferry-tv-1.9.0-armeabi-v7a.apk" {
		t.Error(got)
	}
}

// A release with only the new names installs as well as one with only the old.
func TestInstallFindsTheNewName(t *testing.T) {
	w := newWorld(t)
	w.file = ServerAssets("1.2.0", runtime.GOARCH)[0]
	if got := w.install(); got != "" {
		t.Fatal(got)
	}
}

func TestLaunchDoesNotCrossUpdateChannels(t *testing.T) {
	w := newWorld(t)
	w.install("0.0.2", true)
	started := ""
	Launch(w.data, "1.12.8", "stable", true, func(p string, env []string) error { started = p; return nil })
	if started != "" {
		t.Fatalf("stable launch used dev update: %s", started)
	}
}
