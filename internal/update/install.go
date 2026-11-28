package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// An update is a signed binary in <data>/update. The image stays as it is: at every start its
// binary hands over to the installed one, as long as that is newer, carries a valid signature
// and has been able to run.

// Variables, so that a test build can point elsewhere.
var (
	githubAPI  = "https://api.github.com"
	githubSite = "https://github.com"
	// publicKey verifies releases. The private half never leaves the maintainer's machine.
	publicKey = "3OboEDZ9oWm53Mb6GNkvz2l8UZRA8KTghLAlMjkMVVE="
)

const (
	launchedEnv = "TRACKFERRY_LAUNCHED_BY" // path of the image's binary
	maxBinary   = 200 << 20
	maxTries    = 3
	// Settle is how long a new version must run before it is trusted.
	Settle = 20 * time.Second
)

// Message is what a release signature covers: a signature fits one version on one platform.
// arch is a Linux architecture (amd64), or a whole platform with its slash (android/arm64-v8a).
func Message(version, arch, sha256hex string) []byte {
	platform := arch
	if !strings.Contains(arch, "/") {
		platform = "linux/" + arch
	}
	return fmt.Appendf(nil, "trackferry\n%s\n%s\n%s\n", version, platform, sha256hex)
}

// KeyMatches reports whether pub is the key this build trusts.
func KeyMatches(pub ed25519.PublicKey) bool {
	return base64.StdEncoding.EncodeToString(pub) == publicKey
}

func verify(version, arch, sum, sig string) error {
	key, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("这个程序不带更新公钥")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig))
	if err != nil || !ed25519.Verify(key, Message(version, arch, sum), raw) {
		return errors.New("新版本的签名不对，没有安装")
	}
	return nil
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Record is <data>/update/installed.json.
type Record struct {
	Version   string `json:"version"`
	Channel   string `json:"channel,omitempty"`
	Signature string `json:"signature"`
	Confirmed bool   `json:"confirmed"`
	Tries     int    `json:"tries"`
	// FailedVersion is the last version that did not run; an empty Version means the image's own.
	FailedVersion string `json:"failed_version,omitempty"`
	// Previous is the installed version that ran before this one, kept until this one is confirmed.
	Previous *Record `json:"previous,omitempty"`
}

// Dir is the update folder of a data directory.
func Dir(data string) string { return filepath.Join(data, "update") }

func (r Record) binary(dir string) string { return filepath.Join(dir, "trackferry-"+r.Version) }
func (r Record) backup(dir string) string { return filepath.Join(dir, "before-"+r.Version+".db") }

func readRecord(dir string) (r Record, ok bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "installed.json"))
	if err != nil || json.Unmarshal(raw, &r) != nil {
		return r, false
	}
	if r.Version == "" {
		return r, r.FailedVersion != ""
	}
	if p := r.Previous; p != nil {
		if v, ok := parse(p.Version); !ok || p.Version != format(v) {
			r.Previous = nil
		}
	}
	v, ok := parse(r.Version)
	return r, ok && r.Version == format(v)
}

func (r Record) write(dir string) error {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "installed.json.new")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "installed.json"))
}

func format(v [3]int) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// Launch hands this process over to the installed version, if there is a usable one; then it
// does not return. serving is false for short commands, which never try an unproven version.
// It must run before the database is opened. note says why an installed version is not used.
func Launch(data, version, channel string, serving bool, run func(path string, env []string) error) (note string) {
	dir := Dir(data)
	if os.Getenv(launchedEnv) != "" {
		return ""
	}
	r, ok := readRecord(dir)
	if !ok || r.Version == "" {
		return ""
	}
	recordChannel := r.Channel
	if recordChannel == "" { // records made before channel separation are stable
		recordChannel = "stable"
	}
	if channel == "" {
		channel = "stable"
	}
	if recordChannel != channel {
		return "" // never run a dev binary in stable, or vice versa
	}
	if !Newer(r.Version, version) {
		if serving { // the image has caught up
			_ = os.Remove(filepath.Join(dir, "installed.json"))
			_ = os.Remove(r.binary(dir))
		}
		return ""
	}
	if !serving && !r.Confirmed {
		return ""
	}
	if !r.Confirmed {
		if r.Tries >= maxTries {
			note = giveUp(data, r, "启动了几次都没有成功")
			return join(note, Launch(data, version, channel, serving, run))
		}
		r.Tries++
		if err := r.write(dir); err != nil {
			return err.Error()
		}
	}
	sum, err := fileSum(r.binary(dir))
	if err == nil {
		err = verify(r.Version, runtime.GOARCH, sum, r.Signature)
	}
	if err == nil {
		self, e := os.Executable()
		if err = e; err == nil {
			err = run(r.binary(dir), append(os.Environ(), launchedEnv+"="+self))
		}
	}
	if err == nil || !serving {
		return ""
	}
	note = giveUp(data, r, err.Error())
	return join(note, Launch(data, version, channel, serving, run))
}

func join(a, b string) string {
	if b == "" {
		return a
	}
	return a + "。" + b
}

// giveUp goes back to the version before: an installed one, or the image's. An update that never ran properly may have changed
// the database, so the copy from before it returns.
func giveUp(data string, r Record, why string) string {
	dir := Dir(data)
	if !r.Confirmed && r.Tries > 0 {
		if err := restore(r.backup(dir), filepath.Join(data, "trackferry.db")); err != nil {
			why += "；数据库没能还原：" + err.Error()
		}
	}
	next := Record{}
	if r.Previous != nil {
		next = *r.Previous
	}
	next.FailedVersion, next.Previous = r.Version, nil
	if err := next.write(dir); err != nil {
		// Without the record the image's version runs.
		_ = os.Remove(filepath.Join(dir, "installed.json"))
		why += "；" + err.Error()
	}
	_ = os.Remove(r.binary(dir))
	return fmt.Sprintf("%s 没有启用，继续使用原来的版本：%s", r.Version, why)
}

func restore(from, to string) error {
	raw, err := os.Open(from)
	if err != nil {
		return err
	}
	defer raw.Close()
	tmp := to + ".restore"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, raw); err == nil {
		err = out.Sync()
	}
	if e := out.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	// A log of the newer database must not be replayed into the older one.
	for _, s := range []string{"-wal", "-shm"} {
		if err := os.Remove(to + s); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Rename(tmp, to)
}

// Confirm marks the running installed version as good.
func Confirm(data, version, channel string) error {
	dir := Dir(data)
	if os.Getenv(launchedEnv) == "" {
		return nil
	}
	r, ok := readRecord(dir)
	if !ok || r.Version != version || r.Confirmed {
		return nil
	}
	recordChannel := r.Channel
	if recordChannel == "" {
		recordChannel = "stable"
	}
	if channel == "" {
		channel = "stable"
	}
	if recordChannel != channel {
		return nil
	}
	old := r.Previous
	r.Confirmed, r.Tries, r.Previous = true, 0, nil
	if err := r.write(dir); err != nil {
		return err
	}
	if old != nil && old.Version != r.Version {
		_ = os.Remove(old.binary(dir))
	}
	return nil
}

// Relaunch starts the image's binary again, which picks the version to run.
func Relaunch(run func(path string, env []string) error) error {
	path := os.Getenv(launchedEnv)
	if path == "" {
		var err error
		if path, err = os.Executable(); err != nil {
			return err
		}
	}
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, launchedEnv+"=") {
			env = append(env, e)
		}
	}
	return run(path, env)
}

// Progress of an installation.
type Progress struct {
	Phase string `json:"phase"` // download | check | restart
	Done  int64  `json:"done,omitempty"`
	Total int64  `json:"total,omitempty"`
	Error string `json:"error,omitempty"`
}

// Installer says where and how an update is put in place.
type Installer struct {
	Data    string
	Channel string
	Backup  func(ctx context.Context, path string) error
	Restart func()
	// Try runs a downloaded binary and returns what it says about itself.
	Try func(ctx context.Context, path string) (string, error)
}

func tryVersion(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	return string(out), err
}

// ErrNothing means there is no newer version to install, or one is being installed already.
var ErrNothing = errors.New("没有可以安装的新版本，或者正在安装")

// Install downloads the newest release in the background and then restarts into it. Status
// follows its progress.
func (c *Checker) Install(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := parse(c.st.Latest)
	releaseTag := c.releaseTag
	if c.Installer == nil || !ok || !c.st.Newer || c.installing || releaseTag == "" {
		return ErrNothing
	}
	c.installing = true
	c.st.Install = &Progress{Phase: "download"}
	go func() {
		err := c.install(ctx, format(v), releaseTag)
		if err != nil {
			c.Log.Warn("更新　没有安装", "版本", format(v), "原因", err)
			c.progress(func(p *Progress) { p.Error = err.Error() })
			c.mu.Lock()
			c.installing = false
			c.mu.Unlock()
			return
		}
		c.Log.Info("更新　已安装，正在重启", "版本", format(v))
		c.progress(func(p *Progress) { p.Phase = "restart" })
		time.Sleep(1500 * time.Millisecond) // lets the page see it
		c.Installer.Restart()
	}()
	return nil
}

func (c *Checker) progress(f func(*Progress)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := *c.st.Install
	f(&p)
	c.st.Install = &p
}

func (c *Checker) install(ctx context.Context, version, releaseTag string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	in := c.Installer
	dir := Dir(in.Data)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	base, sig, err := c.findAsset(ctx, version, releaseTag, ServerAssets(version, runtime.GOARCH))
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "download")
	defer os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = c.fetch(ctx, base, io.MultiWriter(f, h), maxBinary, func(done, total int64) {
		c.progress(func(p *Progress) { p.Done, p.Total = done, total })
	})
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	c.progress(func(p *Progress) { p.Phase = "check" })
	r := Record{Version: version, Channel: in.Channel, Signature: strings.TrimSpace(sig)}
	if err := verify(version, runtime.GOARCH, hex.EncodeToString(h.Sum(nil)), r.Signature); err != nil {
		return err
	}
	try := in.Try
	if try == nil {
		try = tryVersion
	}
	says, err := try(ctx, tmp)
	if err != nil {
		return fmt.Errorf("新版本在这台机器上运行不了：%w", err)
	}
	if !strings.HasPrefix(says, "trackferry "+version+" ") {
		return errors.New("下载到的不是这个版本")
	}
	// Only the last update's copy is kept.
	old, _ := filepath.Glob(filepath.Join(dir, "before-*.db"))
	for _, o := range old {
		_ = os.Remove(o)
	}
	if err := in.Backup(ctx, r.backup(dir)); err != nil {
		return fmt.Errorf("备份数据库失败：%w", err)
	}
	if prev, ok := readRecord(dir); ok && prev.Confirmed && prev.Version != "" && prev.Version != version {
		prev.Previous, prev.FailedVersion = nil, ""
		r.Previous = &prev
	}
	if err := os.Rename(tmp, r.binary(dir)); err != nil {
		return err
	}
	return r.write(dir)
}

// errNoAsset is a Release without the file asked for.
var errNoAsset = errors.New("这个版本不能在线安装，请用 docker compose 更新")

// Asset names are trackferry-<version>-<os>-<arch>; releases before 1.9.0 used the older
// names, which are still uploaded so servers of those versions keep updating.
// ServerAssets gives both for the architecture this program runs on, the new one first.
func ServerAssets(version, goarch string) []string {
	arch := goarch
	if arch == "arm" {
		arch = "armv7" // the only 32-bit ARM that is built
	}
	return []string{"trackferry-" + version + "-linux-" + arch, "trackferry-linux-" + goarch}
}

// TVAssets gives both names of the television app for abi, the new one first.
func TVAssets(version, abi string) []string {
	return []string{"trackferry-tv-" + version + "-android-" + abi + ".apk", "trackferry-tv-" + version + "-" + abi + ".apk"}
}

// findAsset fetches the signature of the first of names the release has; base is its download address.
func (c *Checker) findAsset(ctx context.Context, version, releaseTag string, names []string) (base, sig string, err error) {
	for _, name := range names {
		base = githubSite + "/" + c.Repo + "/releases/download/" + releaseTag + "/" + name
		var b bytes.Buffer
		if err = c.fetch(ctx, base+".sig", &b, 1<<10, nil); err == nil {
			return base, b.String(), nil
		}
		if !errors.Is(err, errNoAsset) {
			return "", "", err
		}
	}
	return "", "", err
}

func (c *Checker) fetch(ctx context.Context, url string, to io.Writer, limit int64, tell func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "trackferry")
	res, err := (&http.Client{Transport: c.Client.Transport}).Do(req) // ctx limits it
	if err != nil {
		return c.unreachable()
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return errNoAsset
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，GitHub 返回 %d", res.StatusCode)
	}
	w := &meter{w: to, total: res.ContentLength, tell: tell}
	n, err := io.Copy(w, io.LimitReader(res.Body, limit+1))
	switch {
	case err != nil:
		return errors.New("下载中断了，请再试一次")
	case n > limit:
		return errors.New("下载的文件大得不对")
	}
	return nil
}

type meter struct {
	w           io.Writer
	done, total int64
	told        time.Time
	tell        func(done, total int64)
}

func (m *meter) Write(b []byte) (int, error) {
	n, err := m.w.Write(b)
	m.done += int64(n)
	if m.tell != nil && time.Since(m.told) > 200*time.Millisecond {
		m.told = time.Now()
		m.tell(m.done, m.total)
	}
	return n, err
}
