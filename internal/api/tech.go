package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"trackferry/internal/audio"
	"trackferry/internal/library"
	"trackferry/internal/online"
	"trackferry/internal/store"
)

// techView is what a song really plays as, said the same way on every screen.
type techView struct {
	online.Tech
	Tier  string `json:"tier"`  // hires | lossless | high | standard | ""
	Label string `json:"label"` // "FLAC 24bit 96kHz", "MP3 320k"
	Known bool   `json:"known"` // read from the audio; false: only the format is known
}

var lossless = map[string]bool{"flac": true, "wav": true, "alac": true}

func viewOf(t online.Tech, known bool) techView {
	v := techView{Tech: t, Known: known}
	f := strings.ToUpper(t.Format)
	switch {
	case lossless[t.Format]:
		v.Tier = "lossless"
		if t.Bits > 16 || t.SampleRate > 48000 {
			v.Tier = "hires"
		}
		v.Label = f
		if t.Bits > 0 {
			v.Label += fmt.Sprintf(" %dbit", t.Bits)
		}
		if t.SampleRate > 0 {
			v.Label += " " + strings.TrimSuffix(strconv.FormatFloat(float64(t.SampleRate)/1000, 'f', 1, 64), ".0") + "kHz"
		}
	case t.Kbps > 0:
		v.Tier = "standard"
		if t.Kbps >= 256 {
			v.Tier = "high"
		}
		v.Label = fmt.Sprintf("%s %dk", f, t.Kbps)
	default:
		v.Label = f
	}
	return v
}

// tech says what a song plays as: ?track=<id> for the library, ?online=<key> for an online song.
func (s *Server) tech(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if id, err := strconv.ParseInt(q.Get("track"), 10, 64); err == nil && id > 0 {
		t, ok := s.Library.Track(id)
		if !ok {
			writeError(w, http.StatusNotFound, "no_track", "曲库里没有这首歌了")
			return
		}
		writeJSON(w, http.StatusOK, s.trackTech(t))
		return
	}
	key := q.Get("online")
	if key == "" || len(key) > 512 {
		writeError(w, http.StatusBadRequest, "bad_request", "要 track 或 online")
		return
	}
	if t, ok := s.Router.Online.TechOf(key); ok {
		writeJSON(w, http.StatusOK, viewOf(t, true))
		return
	}
	if source, id, ok := strings.Cut(key, ":"); ok && s.Cache != nil {
		if t, ok := s.Cache.Tech(source, id); ok {
			writeJSON(w, http.StatusOK, viewOf(t, true))
			return
		}
	}
	// Every platform serves MP3 here; its bit rate is read once the song is played.
	writeJSON(w, http.StatusOK, viewOf(online.Tech{Format: "mp3"}, false))
}

func (s *Server) trackTech(t store.TrackRow) techView {
	tt := online.Tech{Format: t.Format, SampleRate: t.SampleRate, Bits: t.Bits}
	if t.GUID == "" && !strings.HasPrefix(t.Path, library.RemotePrefix) {
		// The folder's own file: read its frames for the real bit rate.
		ext := strings.ToLower(filepath.Ext(t.Path))
		if !filepath.IsLocal(filepath.FromSlash(t.Path)) {
			return viewOf(tt, false)
		}
		if m, err := audio.ReadFile(filepath.Join(s.Config.MusicDir, filepath.FromSlash(t.Path)), ext); err == nil {
			tt.Kbps = m.Kbps
			if m.SampleRate > 0 {
				tt.SampleRate = m.SampleRate
			}
		}
	}
	if tt.Kbps == 0 && t.Size > 0 && t.DurationMS > 0 {
		tt.Kbps = int(t.Size * 8 / t.DurationMS) // the file's size over its length
	}
	if lossless[t.Format] {
		return viewOf(tt, t.Bits > 0 && t.SampleRate > 0)
	}
	return viewOf(tt, tt.Kbps > 0)
}

func onlineTech(kbps, rate int) online.Tech {
	return online.Tech{Format: "mp3", Kbps: kbps, SampleRate: rate, Bits: 16}
}
