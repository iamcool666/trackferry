// Package update asks GitHub whether a newer TrackFerry has been released and installs it when
// the owner says so. It sends nothing about this installation.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Status is what the settings page shows.
type Status struct {
	Enabled   bool   `json:"enabled"`
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Newer     bool   `json:"newer"`
	URL       string `json:"url,omitempty"`
	Notes     string `json:"notes,omitempty"`
	Published string `json:"published,omitempty"`
	CheckedMS int64  `json:"checked_ms,omitempty"`
	Error     string `json:"error,omitempty"`

	Proxy string `json:"proxy,omitempty"` // password hidden

	Installable bool      `json:"installable"`       // the page may offer to install it
	Install     *Progress `json:"install,omitempty"` // the installation going on
	FailedLast  string    `json:"failed,omitempty"`  // a version that was installed but did not run
	Image       string    `json:"image,omitempty"`   // "ffmpeg": the image with ffmpeg, updated only by pulling
}

// Checker remembers the last answer.
type Checker struct {
	Repo    string // owner/name
	API     string // https://api.github.com
	Enabled bool
	Client  *http.Client
	Log     *slog.Logger
	// Installer is nil where updates cannot be installed.
	Installer *Installer
	Image     string // TRACKFERRY_IMAGE of the image that runs us

	mu     sync.Mutex
	st     Status
	etag   string
	asking chan struct{}

	installing bool
	tv         *tvApps // nil: no television app is handed out
	proxy      atomic.Pointer[url.URL]
	proxyPath  string
}

// New returns a checker for the running version.
func New(repo, version string, enabled bool, log *slog.Logger) *Checker {
	c := &Checker{Repo: repo, API: githubAPI, Enabled: enabled, Log: log,
		Client: &http.Client{Timeout: 15 * time.Second}, asking: make(chan struct{}, 1),
		st: Status{Enabled: enabled, Current: version}}
	c.Client.Transport = c.transport()
	return c
}

// Status returns the last known answer.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.st
	st.Image = c.Image
	if _, ok := parse(st.Current); ok && c.Installer != nil {
		st.Installable = true
		if r, ok := readRecord(Dir(c.Installer.Data)); ok {
			st.FailedLast = r.FailedVersion
		}
	}
	return st
}

// Run looks a minute after the start and twice a day from then on.
func (c *Checker) Run(ctx context.Context) {
	if !c.Enabled {
		return
	}
	for wait := time.Minute; ; wait = 12 * time.Hour {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		was := c.Status().Latest
		if st := c.Check(ctx); st.Newer && st.Latest != was {
			c.Log.Info("更新　有新版本", "现在", st.Current, "最新", st.Latest, "说明", st.URL)
		}
	}
}

var errNone = errors.New("还没有发布过版本")

// Check asks now. Asking again within a minute returns the last answer.
func (c *Checker) Check(ctx context.Context) Status {
	select {
	case c.asking <- struct{}{}:
	case <-ctx.Done():
		return c.Status()
	}
	defer func() { <-c.asking }()
	if st := c.Status(); time.Since(time.UnixMilli(st.CheckedMS)) < time.Minute {
		return st
	}
	rel, err := c.latest(ctx)
	c.remember(rel, err)
	return c.Status()
}

func (c *Checker) remember(rel *release, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.st.CheckedMS = time.Now().UnixMilli()
	c.st.Error = ""
	switch {
	case err != nil:
		c.st.Error = err.Error()
		if errors.Is(err, errNone) {
			c.st.Newer = false
			c.st.Latest, c.st.URL, c.st.Notes, c.st.Published = "", "", "", ""
		}
	case rel != nil:
		c.st.Latest, c.st.URL, c.st.Notes, c.st.Published = strings.TrimPrefix(rel.Tag, "v"), rel.URL, rel.Body, rel.Published
		c.st.Newer = Newer(c.st.Latest, c.st.Current)
	}
}

type release struct {
	Tag       string `json:"tag_name"`
	URL       string `json:"html_url"`
	Body      string `json:"body"`
	Published string `json:"published_at"`
}

// latest returns nil when nothing changed since the last answer.
func (c *Checker) latest(ctx context.Context) (*release, error) {
	// /releases/latest excludes prereleases. This fork publishes signed dev builds
	// as prereleases, so inspect the release list and select the highest semver tag.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.API+"/repos/"+c.Repo+"/releases?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "trackferry")
	if c.etag != "" {
		req.Header.Set("If-None-Match", c.etag)
	}
	res, err := c.Client.Do(req)
	if err != nil {
		return nil, c.unreachable()
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotModified:
		return nil, nil
	case http.StatusNotFound:
		return nil, errNone
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, errors.New("GitHub 限制了访问次数，稍后再试")
	default:
		return nil, fmt.Errorf("GitHub 返回 %d", res.StatusCode)
	}
	var releases []release
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&releases); err != nil {
		return nil, errors.New("GitHub 的回答看不懂")
	}
	var latest *release
	for i := range releases {
		r := &releases[i]
		if _, ok := parse(r.Tag); !ok || !strings.HasPrefix(r.URL, "https://github.com/") {
			continue
		}
		if latest == nil || Newer(strings.TrimPrefix(r.Tag, "v"), strings.TrimPrefix(latest.Tag, "v")) {
			latest = r
		}
	}
	if latest == nil {
		return nil, errNone
	}
	if n := []rune(latest.Body); len(n) > 4000 {
		latest.Body = string(n[:4000])
	}
	c.etag = res.Header.Get("ETag")
	return latest, nil

// Newer reports whether release a comes after b. A build without a version is never told to update.
func Newer(a, b string) bool {
	x, ok := parse(a)
	y, ok2 := parse(b)
	if !ok || !ok2 {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

func parse(s string) (v [3]int, ok bool) {
	p := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(p) != 3 {
		return v, false
	}
	for i, x := range p {
		n, err := strconv.Atoi(x)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}
