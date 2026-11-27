package api

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"trackferry/internal/names"
)

// version is one way to play the song on screen: a file of the library or a platform's copy.
type version struct {
	TrackID    int64    `json:"track_id,omitempty"`
	Online     string   `json:"online,omitempty"` // key to play it by
	Source     string   `json:"source"`           // library | kuwo | kugou | migu | netease
	Title      string   `json:"title"`
	Artist     string   `json:"artist"`
	Album      string   `json:"album,omitempty"`
	Cover      string   `json:"cover,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	Tech       techView `json:"tech"`
	Error      string   `json:"error,omitempty"` // the platform has it but would not play it
}

// Versions are probed for real, so a list is kept a while: opening the panel again does not
// ask every platform again.
var (
	versionsMu   sync.Mutex
	versionsKept = map[string]keptVersions{}
)

type keptVersions struct {
	at   time.Time
	list []version
}

const versionsFor = 10 * time.Minute

// versions lists the copies of a song there are: ?title=&artist=.
func (s *Server) versions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	title, artist := q.Get("title"), q.Get("artist")
	if names.Norm(title) == "" || len(title) > 300 || len(artist) > 300 {
		writeError(w, http.StatusBadRequest, "bad_request", "要歌名")
		return
	}
	key := names.Norm(title) + "\x00" + names.Norm(artist)
	versionsMu.Lock()
	if k, ok := versionsKept[key]; ok && time.Since(k.at) < versionsFor && q.Get("fresh") == "" {
		versionsMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"versions": k.list})
		return
	}
	versionsMu.Unlock()

	var out []version
	plain := names.Norm(title)
	for _, m := range s.Library.Song(title, artist, title).Matches {
		if names.Norm(m.Title) != plain || artist != "" && !names.ArtistMatch(m.Artist, artist) {
			continue
		}
		out = append(out, version{TrackID: m.ID, Source: "library", Title: m.Title, Artist: m.Artist, Album: m.Album, Cover: m.Cover,
			DurationMS: m.DurationMS, Tech: s.trackTech(m.TrackRow)})
	}

	// One copy per platform, the closest to the song, each actually fetched to read what it is.
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	found, _ := s.Router.Online.Search(ctx, title+" "+artist)
	picked := map[string]bool{}
	var online []version
	for _, c := range found {
		if picked[c.Source] || c.Restricted || c.Key == "" || names.Norm(c.Title) != plain || artist != "" && !names.ArtistMatch(c.Artist, artist) {
			continue
		}
		picked[c.Source] = true
		online = append(online, version{Online: c.Key, Source: c.Source, Title: c.Title, Artist: c.Artist, Album: c.Album, Cover: c.Cover, DurationMS: c.DurationMS})
	}
	var wg sync.WaitGroup
	for i := range online {
		wg.Add(1)
		go func(v *version) {
			defer wg.Done()
			f, err := s.Router.Online.Take(ctx, v.Online)
			if err != nil {
				v.Error = err.Error()
				return
			}
			v.Tech = viewOf(onlineTech(f.Stream.Kbps, f.Stream.SampleRate), f.Stream.Kbps > 0)
			if d := f.Stream.Duration.Milliseconds(); d > 0 {
				v.DurationMS = d
			}
		}(&online[i])
	}
	wg.Wait()
	out = append(out, online...)
	rank := map[string]int{"hires": 0, "lossless": 1, "high": 2, "standard": 3, "": 4}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Error == "") != (out[j].Error == "") {
			return out[i].Error == ""
		}
		if rank[out[i].Tech.Tier] != rank[out[j].Tech.Tier] {
			return rank[out[i].Tech.Tier] < rank[out[j].Tech.Tier]
		}
		return out[i].Tech.Kbps > out[j].Tech.Kbps
	})
	if out == nil {
		out = []version{}
	}
	versionsMu.Lock()
	for k, v := range versionsKept {
		if time.Since(v.at) > versionsFor {
			delete(versionsKept, k)
		}
	}
	versionsKept[key] = keptVersions{time.Now(), out}
	versionsMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"versions": out})
}
