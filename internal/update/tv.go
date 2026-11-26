package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The television app is released with the server: each Release carries
// trackferry-tv-<version>-<abi>.apk and its signature. A television asks its own server, which
// fetches the app of the version it runs, checks the signature and only then hands it over.
// The television's Android checks once more that the app is signed like the one installed.

// TVABIs are the processors the television app is built for.
var TVABIs = []string{"arm64-v8a", "armeabi-v7a"}

const maxTVApp = 80 << 20

// TVApp is what a television is told about the app of the server's version.
type TVApp struct {
	Version string `json:"version"`         // the server's, and so the app's
	ABI     string `json:"abi"`             // the build it gets
	Phase   string `json:"phase"`           // "" | download | ready | failed
	Done    int64  `json:"done,omitempty"`  // bytes so far
	Total   int64  `json:"total,omitempty"` // bytes in all, when GitHub says
	Size    int64  `json:"size,omitempty"`  // of the ready file
	SHA256  string `json:"sha256,omitempty"`
	Error   string `json:"error,omitempty"`
}

type tvApps struct {
	mu   sync.Mutex
	dir  string
	busy map[string]*TVApp // abi: the download going on, or how the last one ended
	seen map[string]string // path: size and time of the file last found sound, and its sum
}

// SetTVDir says where the television apps are kept. Without it they are not offered.
func (c *Checker) SetTVDir(dir string) {
	c.tv = &tvApps{dir: dir, busy: map[string]*TVApp{}, seen: map[string]string{}}
}

func tvABI(abi string) bool {
	for _, a := range TVABIs {
		if a == abi {
			return true
		}
	}
	return false
}

func (t *tvApps) file(version, abi string) string {
	return filepath.Join(t.dir, "trackferry-tv-"+version+"-"+abi+".apk")
}

// errNoTV means this server cannot hand out a television app.
var errNoTV = errors.New("这个服务器版本不能给电视在线更新")

// TVApp says whether the app of the server's version is ready for abi. A file on disk is checked
// against its signature every time, so one that was damaged or swapped is fetched again.
func (c *Checker) TVApp(abi string) (TVApp, error) {
	version, ok := c.tvVersion()
	if !ok || c.tv == nil {
		return TVApp{}, errNoTV
	}
	if !tvABI(abi) {
		return TVApp{}, errors.New("不认识这台电视的处理器")
	}
	t := c.tv
	t.mu.Lock()
	defer t.mu.Unlock()
	if b := t.busy[abi]; b != nil && b.Version == version && b.Phase == "download" {
		return *b, nil
	}
	st := TVApp{Version: version, ABI: abi}
	if size, sum, err := t.verified(version, abi); err == nil {
		st.Phase, st.Size, st.SHA256 = "ready", size, sum
		return st, nil
	}
	if b := t.busy[abi]; b != nil && b.Version == version && b.Phase == "failed" {
		return *b, nil // the file that was ready is gone or bad: it is fetched again when asked
	}
	return st, nil
}

// TVFile is the checked file to send, if it is ready.
func (c *Checker) TVFile(abi string) (path string, st TVApp, err error) {
	st, err = c.TVApp(abi)
	if err != nil {
		return "", st, err
	}
	if st.Phase != "ready" {
		return "", st, errors.New("新版电视应用还没有准备好")
	}
	return c.tv.file(st.Version, abi), st, nil
}

func (c *Checker) tvVersion() (string, bool) {
	c.mu.Lock()
	v := c.st.Current
	c.mu.Unlock()
	p, ok := parse(v)
	return format(p), ok && v == format(p)
}

// PrepareTV fetches the app for abi in the background, unless it is there or on its way.
func (c *Checker) PrepareTV(ctx context.Context, abi string) (TVApp, error) {
	st, err := c.TVApp(abi)
	if err != nil || st.Phase == "ready" || st.Phase == "download" {
		return st, err
	}
	t := c.tv
	t.mu.Lock()
	if b := t.busy[abi]; b != nil && b.Version == st.Version && b.Phase == "download" {
		t.mu.Unlock()
		return *b, nil
	}
	st.Phase, st.Error, st.Done, st.Total = "download", "", 0, 0
	t.busy[abi] = &st
	t.mu.Unlock()
	go func() {
		err := c.fetchTV(context.WithoutCancel(ctx), st.Version, abi)
		t.mu.Lock()
		defer t.mu.Unlock()
		b := *t.busy[abi]
		if err != nil {
			b.Phase, b.Error = "failed", err.Error()
			if c.Log != nil {
				c.Log.Warn("更新　电视应用没有取到", "版本", st.Version, "处理器", abi, "原因", err)
			}
		} else {
			b.Phase = "ready"
		}
		t.busy[abi] = &b
	}()
	return st, nil
}

func (c *Checker) fetchTV(ctx context.Context, version, abi string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	t := c.tv
	if err := os.MkdirAll(t.dir, 0o700); err != nil {
		return err
	}
	name := "trackferry-tv-" + version + "-" + abi + ".apk"
	base := githubSite + "/" + c.Repo + "/releases/download/v" + version + "/" + name
	var sig bytes.Buffer
	if err := c.fetch(ctx, base+".sig", &sig, 1<<10, nil); err != nil {
		return tvMissing(err)
	}
	tmp := t.file(version, abi) + ".download"
	defer os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = c.fetch(ctx, base, io.MultiWriter(f, h), maxTVApp, func(done, total int64) {
		t.mu.Lock()
		if b := t.busy[abi]; b != nil {
			b.Done, b.Total = done, total
		}
		t.mu.Unlock()
	})
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return tvMissing(err)
	}
	signature := strings.TrimSpace(sig.String())
	if err := verify(version, "android/"+abi, hex.EncodeToString(h.Sum(nil)), signature); err != nil {
		return err
	}
	if err := os.WriteFile(t.file(version, abi)+".sig", []byte(signature+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, t.file(version, abi)); err != nil {
		return err
	}
	// Apps of other versions are no use any more.
	old, _ := filepath.Glob(filepath.Join(t.dir, "trackferry-tv-*"))
	for _, o := range old {
		if !strings.HasPrefix(filepath.Base(o), "trackferry-tv-"+version+"-") {
			_ = os.Remove(o)
		}
	}
	return nil
}

func tvMissing(err error) error {
	if err != nil && strings.Contains(err.Error(), "docker compose") {
		return errors.New("这个版本的 Release 里没有电视应用")
	}
	return err
}

// verified checks the kept file against its kept signature. Call with t.mu held.
func (t *tvApps) verified(version, abi string) (int64, string, error) {
	path := t.file(version, abi)
	sig, err := os.ReadFile(path + ".sig")
	if err != nil {
		return 0, "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, "", err
	}
	stamp := fmt.Sprint(info.Size(), info.ModTime().UnixNano(), strings.TrimSpace(string(sig)))
	if v, ok := t.seen[path]; ok && strings.HasPrefix(v, stamp+" ") {
		return info.Size(), strings.TrimPrefix(v, stamp+" "), nil
	}
	sum, err := fileSum(path)
	if err != nil {
		return 0, "", err
	}
	if err := verify(version, "android/"+abi, sum, string(sig)); err != nil {
		delete(t.seen, path)
		_ = os.Remove(path)
		_ = os.Remove(path + ".sig")
		return 0, "", err
	}
	t.seen[path] = stamp + " " + sum
	return info.Size(), sum, nil
}
