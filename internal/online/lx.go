package online

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"trackferry/internal/names"
)

// Lx is 洛雪音乐 (LX Music) served by a self-hosted server, spoken to over its Subsonic
// endpoint. One server fronts every platform it knows (酷我, 酷狗, QQ, 网易云, 咪咕, 汽水) and
// resolves through the sources its owner added, so songs the platforms keep for members can
// still come back in full. Trial clips are refused like anywhere else.
type Lx struct {
	BaseURL  string // the Subsonic endpoint, e.g. http://192.168.1.10:9527/rest
	User     string
	Password string
	Client   *http.Client
}

// Name implements Source.
func (*Lx) Name() string { return "lx" }

// HTTP implements HTTPClient: the server is usually on the same network, at a private address,
// and redirects to a platform's own address when its cache is cold.
func (l *Lx) HTTP() *http.Client { return l.Client }

// lxMaxBody bounds one answer: the server may send lyrics and covers along with songs.
const lxMaxBody = 8 << 20

// lxPlatforms are the platforms the server can search, best first, each with its own prefix in a
// Subsonic
// query. Asking one platform at a time is what makes the first answer arrive sooner: one search
// over all of them only comes back once every platform has answered.
var lxPlatforms = []string{"kw", "kg", "tx", "wy", "mg", "qs"}

// lxNames are what people call them.
var lxNames = map[string]string{
	"kw": "酷我", "kg": "酷狗", "tx": "QQ音乐", "wy": "网易云", "mg": "咪咕", "qs": "汽水",
}

// LxName is what a platform is called in the page.
func LxName(p string) string {
	if n, ok := lxNames[p]; ok {
		return n
	}
	return p
}

// lxPlatformOf reads the platform out of an id: "kw_123" is 酷我.
func lxPlatformOf(id string) string {
	p, _, _ := strings.Cut(id, "_")
	if _, ok := lxNames[p]; ok {
		return p
	}
	return ""
}

// auth adds the Subsonic credentials as a salted token, never the password. The stream address
// is handed to the speaker, written to logs and kept with a takeover run, so what it carries is
// what anyone reading those can use; a token costs nothing and can be neither replayed nor read
// back into the password.
func (l *Lx) auth(v url.Values) url.Values {
	if v == nil {
		v = url.Values{}
	}
	salt := l.newSalt()
	sum := md5.Sum([]byte(l.Password + salt))
	v.Set("u", l.User)
	v.Set("s", salt)
	v.Set("t", hex.EncodeToString(sum[:]))
	v.Set("v", "1.16.1")
	v.Set("c", "trackferry")
	v.Set("f", "json")
	return v
}

// newSalt is a new salt for one request. Each address is its own, so an address that leaks is
// worth nothing a moment later.
func (l *Lx) newSalt() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A salt that never varies is still better than sending the password.
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (l *Lx) get(ctx context.Context, method string, v url.Values, out any) error {
	q := l.auth(v).Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(l.BaseURL, "/rest")+"/rest/"+method+"?"+q, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := l.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return httpError(resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, lxMaxBody))
	if err != nil {
		return err
	}
	// Subsonic wraps every answer in "subsonic-response".
	var r struct {
		Response json.RawMessage `json:"subsonic-response"`
	}
	if err := json.Unmarshal(b, &r); err != nil || len(r.Response) == 0 {
		return errors.New("返回的内容读不懂")
	}
	if err := json.Unmarshal(r.Response, out); err != nil {
		return errors.New("返回的内容读不懂")
	}
	return nil
}


// Search implements Source. Every platform is asked at once, so none holds the others up, but
// the answers are kept in a fixed order of preference rather than whoever replies first: the
// platforms are asked best-first, and a fast platform with a cover or a live cut no longer wins
// over the original another platform has.
func (l *Lx) Search(ctx context.Context, q string) ([]Candidate, error) {
	q = strings.Join(strings.Fields(q), " ")
	answers := make([][]Candidate, len(lxPlatforms))
	errs := make([]error, len(lxPlatforms))
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for i, p := range lxPlatforms {
		wg.Go(func() {
			answers[i], errs[i] = l.searchOne(cctx, p+":"+q)
		})
	}
	wg.Wait()
	var out []Candidate
	var firstErr error
	for i := range lxPlatforms {
		if errs[i] != nil {
			if firstErr == nil {
				firstErr = errs[i]
			}
			continue
		}
		out = append(out, answers[i]...)
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// searchOne asks the server for one platform's songs.
func (l *Lx) searchOne(ctx context.Context, query string) ([]Candidate, error) {
	var r struct {
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
		Songs struct {
			Song []struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Artist   string `json:"artist"`
				Duration int    `json:"duration"` // seconds
				IsLocal  bool   `json:"isLocal"`
				Suffix   string `json:"suffix"`
			} `json:"song"`
		} `json:"searchResult3"`
	}
	v := url.Values{"query": {query}, "songCount": {"15"}, "songOffset": {"0"}, "artistCount": {"0"}, "albumCount": {"0"}}
	if err := l.get(ctx, "search3", v, &r); err != nil {
		return nil, err
	}
	if r.Status != "ok" {
		if r.Error.Message != "" {
			return nil, lxError(r.Error.Message)
		}
		return nil, errors.New("搜索失败")
	}
	var out []Candidate
	for _, s := range r.Songs.Song {
		if s.ID == "" || s.IsLocal {
			continue // the library layer plays those, and plays them from disk
		}
		d := time.Duration(s.Duration) * time.Second
		out = append(out, Candidate{Source: "lx", ID: s.ID, Title: s.Title, Artist: s.Artist,
			Duration: d, DurationMS: d.Milliseconds(), Note: lxNote(s.ID, s.Suffix)})
	}
	return out, nil
}

// lxNote is what a song of the server came from, for the page: "酷我 · flac".
func lxNote(id, suffix string) string {
	p := LxName(lxPlatformOf(id))
	if suffix != "" {
		if p != "" {
			return p + " · " + strings.ToUpper(suffix)
		}
		return strings.ToUpper(suffix)
	}
	return p
}

// ArtistSongs lists songs by one singer, for a singer's list that the library cannot fill.
func (l *Lx) ArtistSongs(ctx context.Context, artist string, limit int) ([]Candidate, error) {
	var r struct {
		Status string `json:"status"`
		Songs  struct {
			Song []struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Artist   string `json:"artist"`
				Duration int    `json:"duration"`
				Suffix   string `json:"suffix"`
				IsLocal  bool   `json:"isLocal"`
			} `json:"song"`
		} `json:"searchResult3"`
	}
	v := url.Values{"query": {strings.Join(strings.Fields(artist), " ")},
		"songCount": {fmt.Sprint(max(limit, 20))}, "songOffset": {"0"}, "artistCount": {"0"}, "albumCount": {"0"}}
	if err := l.get(ctx, "search3", v, &r); err != nil {
		return nil, err
	}
	if r.Status != "ok" {
		return nil, errors.New("没有找到这个歌手")
	}
	seen := map[string]bool{}
	var out []Candidate
	for _, s := range r.Songs.Song {
		if s.ID == "" || s.IsLocal || seen[names.Norm(s.Title)] {
			continue
		}
		// The search answers with other singers too; only songs this singer is on count.
		if !names.ArtistMatch(s.Artist, artist) {
			continue
		}
		d := time.Duration(s.Duration) * time.Second
		seen[names.Norm(s.Title)] = true
		// Every song here comes from the server's own resolvers, which is a way of playing what
		// the platform does not hand out freely. Member keeps it out of the cache: the file is
		// played and forgotten rather than kept on disk (see cache.keep).
		out = append(out, Candidate{Source: "lx", ID: s.ID, Title: s.Title, Artist: s.Artist,
			Duration: d, DurationMS: d.Milliseconds(), Member: true, Note: lxNote(s.ID, s.Suffix)})
	}
	return out[:min(limit, len(out))], nil
}


// Playlist reads an LXServer Subsonic playlist. TrackFerry prefixes these IDs with
// "online:playlist:" so they cannot be confused with 飞牛音乐's native playlist GUIDs.
type Playlist struct {
	Name  string
	Songs []Candidate
}

func lxPlaylistID(id string) (string, error) {
	if !strings.HasPrefix(id, "public:lx:") {
		return "", errors.New("不支持的洛雪在线列表 ID")
	}
	parts := strings.SplitN(strings.TrimPrefix(id, "public:lx:"), ":", 3)
	if len(parts) != 3 || parts[2] == "" {
		return "", errors.New("不支持的洛雪在线列表 ID")
	}
	if _, ok := lxNames[parts[0]]; !ok {
		return "", errors.New("不支持的洛雪在线列表来源")
	}
	switch parts[1] {
	case "playlist":
		// LXServer shared playlists use pl_<source>_<id>.
		return "pl_" + parts[0] + "_" + parts[2], nil
	case "chart":
		// LXServer leaderboards use lb_<source>_<bangid>.
		return "lb_" + parts[0] + "_" + parts[2], nil
	default:
		return "", errors.New("不支持的洛雪在线列表类型")
	}
}

func (l *Lx) Playlist(ctx context.Context, id string) (Playlist, error) {
	subsonicID, err := lxPlaylistID(id)
	if err != nil {
		return Playlist{}, err
	}
	var r struct {
		Status string `json:"status"`
		Error struct { Message string `json:"message"` } `json:"error"`
		Playlist struct {
			ID string `json:"id"`
			Name string `json:"name"`
			SongCount int `json:"songCount"`
			Entry []struct {
				ID string `json:"id"`
				Title string `json:"title"`
				Artist string `json:"artist"`
				Album string `json:"album"`
				Duration int `json:"duration"`
				Cover string `json:"coverArt"`
				Suffix string `json:"suffix"`
			} `json:"entry"`
		} `json:"playlist"`
	}
	if err := l.get(ctx, "getPlaylist", url.Values{"id": {subsonicID}}, &r); err != nil {
		return Playlist{}, err
	}
	if r.Status != "ok" {
		if r.Error.Message != "" { return Playlist{}, lxError(r.Error.Message) }
		return Playlist{}, errors.New("洛雪没有返回这个歌单")
	}
	if r.Playlist.ID == "" || r.Playlist.Name == "" {
		return Playlist{}, errors.New("洛雪返回的歌单信息不完整")
	}
	out := Playlist{Name: r.Playlist.Name, Songs: []Candidate{}}
	for _, s := range r.Playlist.Entry {
		if s.ID == "" || lxPlatformOf(s.ID) == "" { continue }
		d := time.Duration(s.Duration) * time.Second
		out.Songs = append(out.Songs, Candidate{Source: "lx", ID: s.ID, Title: s.Title, Artist: s.Artist,
			Album: s.Album, Duration: d, DurationMS: d.Milliseconds(), Cover: s.Cover, Note: lxNote(s.ID, s.Suffix)})
	}
	return out, nil
}

// Resolve implements Source. The address points back at the server, which talks to its own
// sources when the speaker fetches it.
func (l *Lx) Resolve(ctx context.Context, c Candidate) (string, error) {
	return l.ResolveAt(ctx, c, "")
}

// ResolveAt requests one exact step in the selected quality ladder. FLAC is requested
// without a bitrate cap; MP3 tiers request their explicit bitrate ceiling.
func (l *Lx) ResolveAt(ctx context.Context, c Candidate, maxBitRate string) (string, error) {
	format := "mp3"
	if QualityOf(ctx) == HiRes {
		format = "flac"
	}
	v := l.auth(url.Values{"id": {c.ID}, "format": {format}})
	if maxBitRate != "" {
		v.Set("maxBitRate", maxBitRate)
	}
	return strings.TrimSuffix(l.BaseURL, "/rest") + "/rest/stream?" + v.Encode(), nil
}

// Unprobed says this source's addresses are not to be read: each platform hands out its own
// encoding, and the speaker fetching the stream is the only judge of it.
func (*Lx) Unprobed() bool { return true }

// Check asks the server who we are, so a wrong password is told apart from a wrong address.
func (l *Lx) Check(ctx context.Context) error {
	var r struct {
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := l.get(ctx, "ping", nil, &r); err != nil {
		return fmt.Errorf("连不上 %s：%w", l.BaseURL, err)
	}
	if r.Status != "ok" {
		return lxError(r.Error.Message)
	}
	// ping needs no login on every build: a search proves the account and the sources too.
	if _, err := l.Search(ctx, "晴天"); err != nil {
		return err
	}
	return nil
}

// lxError turns an answer from the server into something a person can act on.
func lxError(msg string) error {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "username or password"), strings.Contains(msg, "密码"), strings.Contains(msg, "账号"):
		return errors.New("洛雪不认这个账号或密码")
	}
	return errors.New(msg)
}

// LxHosts is the host a server address points at, so its client may reach it.
func LxHosts(base string) []string {
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	return []string{u.Hostname()}
}

// Cover fetches a song's picture from the server, which keeps one for every online song.
func (l *Lx) Cover(ctx context.Context, id, size string) (*http.Response, error) {
	v := l.auth(url.Values{"id": {id}, "size": {size}})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(l.BaseURL, "/rest")+"/rest/getCoverArt?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := l.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, httpError(resp.StatusCode)
	}
	return resp, nil
}

// Lyrics implements Lyricist: the words of a song a search showed, with time stamps.
func (l *Lx) Lyrics(ctx context.Context, c Candidate) (string, error) {
	return l.lyricsOf(ctx, c.ID)
}

// lyricsOf fetches a song's words by its platform id ("kw_12").
func (l *Lx) lyricsOf(ctx context.Context, id string) (string, error) {
	var r struct {
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
		Lyrics struct {
			Value string `json:"value"`
		} `json:"lyrics"`
	}
	if err := l.get(ctx, "getLyricsBySongId", url.Values{"id": {id}}, &r); err != nil {
		return "", err
	}
	if r.Status != "ok" || strings.TrimSpace(r.Lyrics.Value) == "" {
		return "", ErrNoLyrics
	}
	return strings.TrimSpace(r.Lyrics.Value), nil
}

// IsOnlineID reports whether a cover id names an online song ("kw_12") rather than a library one.
func IsOnlineID(id string) bool { return lxPlatformOf(id) != "" }


// PlatformOf is the platform code an online id belongs to ("kw" for "kw_12").
func PlatformOf(id string) string { return lxPlatformOf(id) }

// LxFile is where a server set in the page is kept: the address and the account, so it
// survives a restart.
type LxFile struct {
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// LoadLx reads a saved server; a missing file is not an error.
func LoadLx(path string) (LxFile, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LxFile{}, false
	}
	var f LxFile
	if json.Unmarshal(raw, &f) != nil || f.URL == "" {
		return LxFile{}, false
	}
	return f, true
}

// SaveLx writes the server for the next start.
func SaveLx(path string, f LxFile) error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, raw, 0o600); err == nil {
		err = os.Rename(tmp, path)
	}
	return err
}

// RemoveLx forgets a saved server.
func RemoveLx(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
