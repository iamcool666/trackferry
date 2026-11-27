package online

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"trackferry/internal/names"
)

// Kugou is 酷狗音乐.
type Kugou struct {
	Client             *http.Client
	SearchURL, PlayURL string // endpoint overrides for tests
}

// Name implements Source.
func (*Kugou) Name() string { return "kugou" }

// Search implements Source.
func (k *Kugou) Search(ctx context.Context, q string) ([]Candidate, error) {
	base := k.SearchURL
	if base == "" {
		base = "http://mobilecdn.kugou.com/api/v3/search/song"
	}
	v := url.Values{"format": {"json"}, "page": {"1"}, "pagesize": {"15"}, "showtype": {"1"}, "keyword": {q}}
	var r struct {
		Data struct {
			Info []struct {
				Hash      string `json:"hash"`
				Name      text   `json:"songname"`
				Artist    text   `json:"singername"`
				Duration  text   `json:"duration"`
				Privilege text   `json:"privilege"` // 0 = uploaded, 8 = licensed, 10 = to be paid for
				AlbumN    text   `json:"album_name"`
				Trans     struct {
					Cover string `json:"union_cover"` // with {size}
				} `json:"trans_param"`
			} `json:"info"`
		} `json:"data"`
	}
	if err := getJSON(ctx, k.Client, base+"?"+v.Encode(), nil, &r); err != nil {
		return nil, err
	}
	var out []Candidate
	var uploads []int
	for _, s := range r.Data.Info {
		if s.Hash == "" {
			continue
		}
		if s.Privilege.int() == 0 {
			uploads = append(uploads, len(out))
		}
		d := seconds(s.Duration)
		out = append(out, Candidate{Source: "kugou", ID: s.Hash, Title: unesc(s.Name), Artist: unesc(s.Artist), Duration: d, DurationMS: d.Milliseconds(),
			Restricted: s.Privilege.int() >= 10, Cover: CoverID(strings.ReplaceAll(s.Trans.Cover, "{size}", "480")), Album: unesc(s.AlbumN)})
	}
	// Anyone may upload under a famous name. Where the real recording is for members, such an
	// upload is something else: a clip, another speed, another singer.
	for _, i := range uploads {
		for _, c := range out {
			if c.Restricted && c.Note == "" && names.Norm(c.Title) == names.Norm(out[i].Title) && names.SameArtist(c.Artist, out[i].Artist) {
				out[i].Restricted, out[i].Note = true, "网友上传"
				break
			}
		}
	}
	return out, nil
}

// Resolve implements Source.
func (k *Kugou) Resolve(ctx context.Context, c Candidate) (string, error) {
	base := k.PlayURL
	if base == "" {
		base = "http://m.kugou.com/app/i/getSongInfo.php"
	}
	v := url.Values{"cmd": {"playInfo"}, "hash": {c.ID}}
	var r struct {
		URL       string `json:"url"`
		Error     string `json:"error"`
		Privilege text   `json:"privilege"`
		Seconds   text   `json:"timeLength"`
	}
	if err := getJSON(ctx, k.Client, base+"?"+v.Encode(), map[string]string{"Referer": "http://m.kugou.com/"}, &r); err != nil {
		return "", err
	}
	if r.URL == "" {
		if r.Error != "" || r.Privilege.int() >= 10 {
			return "", ErrRestricted
		}
		return "", ErrNoAudio
	}
	if d := seconds(r.Seconds); d > 0 && c.Duration > 0 && float64(d) < minFullShare*float64(c.Duration) {
		return "", ErrTrial
	}
	return r.URL, nil
}
