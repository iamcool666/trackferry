package online

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"trackferry/internal/names"
)

// NetCover starts the ID of a platform's cover: the address, base64url. Only addresses on the
// platforms' own image hosts are ever fetched, so an ID cannot point the server anywhere else.
const NetCover = "net-"

var coverHosts = []string{".kuwo.cn", ".kugou.com", ".migu.cn", ".126.net"}

func allowedArt(u string) bool {
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.User != nil || p.Port() != "" {
		return false
	}
	h := "." + strings.ToLower(p.Hostname())
	for _, s := range coverHosts {
		if strings.HasSuffix(h, s) {
			return true
		}
	}
	return false
}

// CoverID names a platform's cover address; "" when it is not one.
func CoverID(u string) string {
	if u == "" || len(u) > 400 || !allowedArt(u) {
		return ""
	}
	return NetCover + base64.RawURLEncoding.EncodeToString([]byte(u))
}

// CoverURL is the address a CoverID names, checked again.
func CoverURL(id string) (string, bool) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, NetCover))
	if !strings.HasPrefix(id, NetCover) || err != nil || !allowedArt(string(b)) {
		return "", false
	}
	return string(b), true
}

// ErrNoLyrics means the platform has no lyrics for the song.
var ErrNoLyrics = errors.New("没有歌词")

// Lyricist is a platform that has lyrics for its songs.
type Lyricist interface {
	Lyrics(ctx context.Context, c Candidate) (string, error)
}

func getText(ctx context.Context, c *http.Client, u string, header map[string]string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, x := range header {
		req.Header.Set(k, x)
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", ErrLimited
	}
	if resp.StatusCode != http.StatusOK {
		return "", httpError(resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(b), err
}

// Lyrics implements Lyricist.
func (k *Kuwo) Lyrics(ctx context.Context, c Candidate) (string, error) {
	var r struct {
		Data struct {
			List []struct {
				Line string `json:"lineLyric"`
				Time text   `json:"time"`
			} `json:"lrclist"`
		} `json:"data"`
	}
	if err := getJSON(ctx, k.Client, "http://kuwo.cn/openapi/v1/www/lyric/getlyric?musicId="+url.QueryEscape(c.ID), nil, &r); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, l := range r.Data.List {
		sec, err := strconv.ParseFloat(string(l.Time), 64)
		if err != nil || sec < 0 {
			continue
		}
		ms := int64(sec * 1000)
		fmt.Fprintf(&b, "[%02d:%02d.%02d]%s\n", ms/60000, ms/1000%60, ms%1000/10, unesc(text(l.Line)))
	}
	if b.Len() == 0 {
		return "", ErrNoLyrics
	}
	return b.String(), nil
}

// Lyrics implements Lyricist: the lyrics matched to the file, then downloaded.
func (k *Kugou) Lyrics(ctx context.Context, c Candidate) (string, error) {
	var found struct {
		Candidates []struct {
			ID        text   `json:"id"`
			AccessKey string `json:"accesskey"`
		} `json:"candidates"`
	}
	v := url.Values{"ver": {"1"}, "man": {"yes"}, "client": {"mobi"}, "keyword": {""}, "hash": {c.ID}}
	if c.Duration > 0 {
		v.Set("duration", strconv.FormatInt(c.Duration.Milliseconds(), 10))
	}
	if err := getJSON(ctx, k.Client, "http://krcs.kugou.com/search?"+v.Encode(), nil, &found); err != nil {
		return "", err
	}
	if len(found.Candidates) == 0 || found.Candidates[0].ID == "" {
		return "", ErrNoLyrics
	}
	f := found.Candidates[0]
	var got struct {
		Content string `json:"content"`
	}
	d := url.Values{"ver": {"1"}, "client": {"pc"}, "id": {string(f.ID)}, "accesskey": {f.AccessKey}, "fmt": {"lrc"}, "charset": {"utf8"}}
	if err := getJSON(ctx, k.Client, "http://lyrics.kugou.com/download?"+d.Encode(), nil, &got); err != nil {
		return "", err
	}
	b, err := base64.StdEncoding.DecodeString(got.Content)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return "", ErrNoLyrics
	}
	return strings.TrimPrefix(string(b), "\ufeff"), nil
}

// Lyrics implements Lyricist: the address came with the search.
func (m *Migu) Lyrics(ctx context.Context, c Candidate) (string, error) {
	if c.lyric == "" || !allowedArt(c.lyric) {
		return "", ErrNoLyrics
	}
	s, err := getText(ctx, m.Client, c.lyric, miguHeader)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(s) == "" || strings.HasPrefix(strings.TrimSpace(s), "<") {
		return "", ErrNoLyrics
	}
	return s, nil
}

// Lyrics implements Lyricist.
func (n *Netease) Lyrics(ctx context.Context, c Candidate) (string, error) {
	var r struct {
		Lrc struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
	}
	if err := getJSON(ctx, n.Client, "https://music.163.com/api/song/lyric?lv=1&kv=1&tv=-1&id="+url.QueryEscape(c.ID), map[string]string{"Referer": "https://music.163.com/"}, &r); err != nil {
		return "", err
	}
	if strings.TrimSpace(r.Lrc.Lyric) == "" {
		return "", ErrNoLyrics
	}
	return r.Lrc.Lyric, nil
}

// pacer spaces the requests made to one platform for lyrics and the library's covers, so
// a large library never hammers one: requests wait for their turn.
type pacer struct {
	mu   sync.Mutex
	next time.Time
}

const paceGap = 700 * time.Millisecond

func (s *Service) wait(ctx context.Context, name string) error {
	s.paceMu.Lock()
	if s.pace == nil {
		s.pace = map[string]*pacer{}
	}
	p := s.pace[name]
	if p == nil {
		p = &pacer{}
		s.pace[name] = p
	}
	s.paceMu.Unlock()
	p.mu.Lock()
	at := time.Now()
	if p.next.After(at) {
		at = p.next
	}
	p.next = at.Add(paceGap)
	p.mu.Unlock()
	t := time.NewTimer(time.Until(at))
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) source(name string) (Source, bool) {
	for _, e := range s.srcs {
		if e.src.Name() != name {
			continue
		}
		s.mu.Lock()
		ok := e.enabled && !time.Now().Before(e.open)
		s.mu.Unlock()
		return e.src, ok
	}
	return nil, false
}

// Lyrics gets the lyrics of an online song a search showed. ErrNoLyrics: the platform has none.
func (s *Service) Lyrics(ctx context.Context, key string) (string, error) {
	c, ok := s.candidate(key)
	if !ok {
		return "", ErrGone
	}
	return s.LyricsOf(ctx, c)
}

// LyricsOf gets a candidate's lyrics from its platform, in turn with the platform's other asks.
func (s *Service) LyricsOf(ctx context.Context, c Candidate) (string, error) {
	src, ok := s.source(c.Source)
	l, can := src.(Lyricist)
	if !ok || !can {
		return "", ErrNoLyrics
	}
	if err := s.wait(ctx, c.Source); err != nil {
		return "", err
	}
	return l.Lyrics(ctx, c)
}

// Lookup finds a library song on the platforms, one at a time and paced, for its cover or
// lyrics: the first that has the same title and singer, about as long, and what want asks for.
// Nothing is remembered or counted against a platform's health: it is background work.
func (s *Service) Lookup(ctx context.Context, title, artist string, durationMS int64, skip func(source string) bool, want func(Candidate) bool) (Candidate, bool) {
	if names.Norm(title) == "" || names.Norm(artist) == "" {
		return Candidate{}, false
	}
	for _, e := range s.srcs {
		if _, ok := s.source(e.src.Name()); !ok || skip != nil && skip(e.src.Name()) {
			continue
		}
		if err := s.wait(ctx, e.src.Name()); err != nil {
			return Candidate{}, false
		}
		c, done := context.WithTimeout(ctx, 5*time.Second)
		found, err := e.src.Search(c, title+" "+artist)
		done()
		if err != nil {
			continue
		}
		for _, f := range found {
			if names.Norm(f.Title) != names.Norm(title) || !names.ArtistMatch(f.Artist, artist) {
				continue
			}
			if durationMS > 0 && f.DurationMS > 0 && abs(durationMS-f.DurationMS) > 4000 {
				continue
			}
			if names.VersionPenalty(f.Title, title) > 0 || !want(f) {
				continue
			}
			f.Key = f.Source + ":" + f.ID
			return f, true
		}
	}
	return Candidate{}, false
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
