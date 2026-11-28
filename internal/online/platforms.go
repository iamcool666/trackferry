// 酷我 and 咪咕 endpoints: thanks to musicdl (https://github.com/CharlesPikachu/musicdl).

package online

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func getJSON(ctx context.Context, c *http.Client, u string, header map[string]string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, x := range header {
		req.Header.Set(k, x)
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrLimited
	}
	if resp.StatusCode != http.StatusOK {
		return httpError(resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// text accepts a JSON string or number.
type text string

func (t *text) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = text(s)
		return nil
	}
	*t = text(b)
	return nil
}

func (t text) int() int { n, _ := strconv.Atoi(string(t)); return n }

func seconds(t text) time.Duration { return time.Duration(t.int()) * time.Second }

// Kuwo is 酷我音乐.
type Kuwo struct {
	Client             *http.Client
	SearchURL, PlayURL string // endpoint overrides for tests
}

// Name implements Source.
func (*Kuwo) Name() string { return "kuwo" }

// Search implements Source.
func (k *Kuwo) Search(ctx context.Context, q string) ([]Candidate, error) {
	base := k.SearchURL
	if base == "" {
		base = "http://search.kuwo.cn/r.s"
	}
	v := url.Values{"client": {"kt"}, "all": {q}, "pn": {"0"}, "rn": {"15"}, "ver": {"kwplayer_ar_9.2.2.1"}, "vipver": {"1"},
		"show_copyright_off": {"1"}, "newver": {"1"}, "ft": {"music"}, "cluster": {"0"}, "strategy": {"2012"}, "encoding": {"utf8"},
		"rformat": {"json"}, "vermerge": {"1"}, "mobi": {"1"}, "issubtitle": {"1"}}
	var r struct {
		List []struct {
			ID       text `json:"DC_TARGETID"`
			Name     text `json:"SONGNAME"`
			Artist   text `json:"ARTIST"`
			Duration text `json:"DURATION"`
			AlbumN   text `json:"ALBUM"`
			Album    text `json:"web_albumpic_short"` // "120/s4s…/x.jpg"
			Pay      struct {
				Fee struct {
					Song  text `json:"song"`
					VIP   text `json:"vip"`
					Album text `json:"album"`
				} `json:"feeType"`
				NoPlay text `json:"cannotOnlinePlay"`
				Clip   text `json:"listen_fragment"` // only a clip for free
			} `json:"payInfo"`
		} `json:"abslist"`
	}
	if err := getJSON(ctx, k.Client, base+"?"+v.Encode(), nil, &r); err != nil {
		return nil, err
	}
	var out []Candidate
	for _, s := range r.List {
		if s.ID == "" {
			continue
		}
		f := s.Pay.Fee
		d := seconds(s.Duration)
		out = append(out, Candidate{Source: "kuwo", ID: string(s.ID), Title: unesc(s.Name), Artist: unesc(s.Artist), Duration: d, DurationMS: d.Milliseconds(),
			Restricted: f.Song == "1" || f.VIP == "1" || f.Album == "1" || s.Pay.NoPlay == "1" || s.Pay.Clip == "1", Cover: kuwoCover(string(s.Album)), Album: unesc(s.AlbumN)})
	}
	return out, nil
}

// kuwoCover is the 500 px album picture from the short path a search gives.
func kuwoCover(short string) string {
	_, rest, ok := strings.Cut(short, "/")
	if !ok || rest == "" {
		return ""
	}
	return CoverID("https://img1.kuwo.cn/star/albumcover/500/" + rest)
}

func unesc(t text) string {
	s := html.UnescapeString(html.UnescapeString(string(t)))
	return strings.TrimSpace(strings.ReplaceAll(s, " ", " "))
}

// Resolve implements Source.
func (k *Kuwo) Resolve(ctx context.Context, c Candidate) (string, error) {
	base := k.PlayURL
	if base == "" {
		base = "https://mobi.kuwo.cn/mobi.s"
	}
	v := url.Values{"f": {"web"}, "source": {"kwplayer_ar_9.2.2.1"}, "type": {"convert_url_with_sign"}, "rid": {c.ID}, "br": {map[Quality]string{HiRes: "2000kflac", MP3320: "320kmp3", MP3192: "192kmp3", MP3128: "128kmp3"}[QualityOf(ctx)]}}
	var r struct {
		Code int `json:"code"`
		Data struct {
			URL      string `json:"url"`
			RID      text   `json:"rid"`
			Duration text   `json:"duration"`
		} `json:"data"`
	}
	if err := getJSON(ctx, k.Client, base+"?"+v.Encode(), nil, &r); err != nil {
		return "", err
	}
	if r.Data.URL == "" {
		return "", ErrNoAudio
	}
	// Songs that are not free come back as an 11-second announcement with another ID.
	if r.Data.RID != "" && string(r.Data.RID) != c.ID {
		return "", ErrRestricted
	}
	if d := seconds(r.Data.Duration); d > 0 && c.Duration > 0 && float64(d) < minFullShare*float64(c.Duration) {
		return "", ErrTrial
	}
	return r.Data.URL, nil
}

// Migu is 咪咕音乐.
type Migu struct {
	Client             *http.Client
	SearchURL, PlayURL string // endpoint overrides for tests
}

// Name implements Source.
func (*Migu) Name() string { return "migu" }

var miguHeader = map[string]string{"channel": "014000D"}

// Search implements Source.
func (m *Migu) Search(ctx context.Context, q string) ([]Candidate, error) {
	base := m.SearchURL
	if base == "" {
		base = "https://app.u.nf.migu.cn/pc/resource/song/item/search/v1.0"
	}
	v := url.Values{"text": {q}, "pageNo": {"1"}, "pageSize": {"15"}}
	var r []struct {
		Content   text     `json:"contentId"`
		Copyright text     `json:"copyrightId"`
		Name      text     `json:"songName"`
		Duration  text     `json:"duration"`
		Tags      []string `json:"showTags"`
		AlbumN    text     `json:"album"`
		Img1      string   `json:"img1"`
		Img2      string   `json:"img2"`
		Img3      string   `json:"img3"`
		Lrc       string   `json:"lrcUrl"`
		Singers   []struct {
			Name string `json:"name"`
		} `json:"singerList"`
	}
	if err := getJSON(ctx, m.Client, base+"?"+v.Encode(), miguHeader, &r); err != nil {
		return nil, err
	}
	var out []Candidate
	for _, s := range r {
		if s.Content == "" {
			continue
		}
		var as []string
		for _, a := range s.Singers {
			as = append(as, a.Name)
		}
		paid := false
		for _, t := range s.Tags {
			if strings.Contains(strings.ToLower(t), "vip") || t == "pay" {
				paid = true
			}
		}
		d := seconds(s.Duration)
		out = append(out, Candidate{Source: "migu", ID: string(s.Content), ref: string(s.Copyright), Title: unesc(s.Name), Artist: strings.Join(as, "/"),
			Duration: d, DurationMS: d.Milliseconds(), Restricted: paid, Cover: CoverID(miguArt(s.Img3, s.Img2, s.Img1)), lyric: miguArt(s.Lrc), Album: unesc(s.AlbumN)})
	}
	return out, nil
}

// miguArt is the first address given, made whole: the search gives paths on its own host.
func miguArt(paths ...string) string {
	for _, p := range paths {
		switch {
		case p == "":
		case strings.HasPrefix(p, "//"):
			return "https:" + p
		case strings.HasPrefix(p, "/"):
			return "https://d.musicapp.migu.cn" + p
		default:
			return p
		}
	}
	return ""
}

// Resolve implements Source.
func (m *Migu) Resolve(ctx context.Context, c Candidate) (string, error) {
	base := m.PlayURL
	if base == "" {
		base = "https://app.c.nf.migu.cn/MIGUM3.0/strategy/pc/listen/v1.0"
	}
	v := url.Values{"scene": {""}, "netType": {"01"}, "resourceType": {"2"}, "copyrightId": {c.ref}, "contentId": {c.ID}, "toneFlag": {map[Quality]string{HiRes: "ZQ", MP3320: "HQ", MP3192: "HQ", MP3128: "PQ"}[QualityOf(ctx)]}}
	var r struct {
		Data struct {
			URL        string `json:"url"`
			CannotCode string `json:"cannotCode"`
		} `json:"data"`
	}
	if err := getJSON(ctx, m.Client, base+"?"+v.Encode(), miguHeader, &r); err != nil {
		return "", err
	}
	if r.Data.URL == "" {
		if r.Data.CannotCode != "" {
			return "", ErrRestricted
		}
		return "", ErrNoAudio
	}
	if strings.HasPrefix(r.Data.URL, "//") {
		return "https:" + r.Data.URL, nil
	}
	return r.Data.URL, nil
}
