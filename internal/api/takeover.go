package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"trackferry/internal/live"
	"trackferry/internal/store"
	"trackferry/internal/stream"
	"trackferry/internal/takeover"
)

// takeoverTest interrupts Xiao AI on one speaker and plays a test sound through the same
// loop a real takeover uses. It works while the
// takeover mode itself is still locked; it only needs the speaker to be enabled.
func (s *Server) takeoverTest(w http.ResponseWriter, r *http.Request) {
	if s.Engine == nil || s.Stream == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "这个版本没有接管功能")
		return
	}
	var req struct {
		Source  string `json:"source"` // tone | library | track
		TrackID int64  `json:"track_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Source != "tone" && req.Source != "library" && req.Source != "track" {
		writeError(w, http.StatusBadRequest, "bad_source", "source 只能是 tone、library 或 track")
		return
	}
	d, ok := s.target(w, r)
	if !ok {
		return
	}
	var h *stream.Handle
	var err error
	var song *store.Song
	kind := "test"
	length := time.Duration(0) // only the tone is stopped by the clock
	if req.Source == "tone" {
		if h, err = s.Stream.Tone(); err == nil {
			length = h.Duration
		}
	} else if req.Source == "track" {
		t, ok := store.TrackRow{}, false
		if s.Library != nil {
			t, ok = s.Library.Track(req.TrackID)
		}
		if !ok {
			writeError(w, http.StatusNotFound, "no_track", "曲库里没有这首歌了")
			return
		}
		kind = "push"
		song = &store.Song{TrackID: t.ID, Title: t.Title, Artist: t.Artist, Album: t.Album, Cover: t.Cover, DurationMS: t.DurationMS}
		h, err = s.Stream.Song(t.Path, strings.Trim(t.Artist+" - "+t.Title, " -"), time.Duration(t.DurationMS)*time.Millisecond)
	} else if s.Library != nil && s.Library.Size() > 0 {
		m, _ := s.Library.Random()
		h, err = s.Stream.Song(m.Path, strings.Trim(m.Artist+" - "+m.Title, " -"), time.Duration(m.DurationMS)*time.Millisecond)
	} else {
		var rel string
		if rel, err = s.Stream.RandomFile(); err == nil {
			h, err = s.Stream.File(rel, "")
		}
	}
	if err != nil {
		writeError(w, http.StatusConflict, "no_audio", "准备测试音频失败："+err.Error())
		return
	}
	if song != nil && s.Played != nil {
		s.Played(d.DeviceID, song.Title, song.Artist, song.TrackID)
	}
	tr := s.Live.Begin(live.Flow{Kind: kind, DeviceID: d.DeviceID, Device: firstNonEmpty(d.Alias, d.Name), Said: h.Label})
	takeover.Follow(h, tr)
	var ended func(takeover.End)
	if song != nil && s.Dispatcher != nil {
		ended = s.Dispatcher.After(d.DeviceID, d.Hardware, song.TrackID, nil, true)
	}
	run, err := s.Engine.Start(r.Context(), takeover.Request{DeviceID: d.DeviceID, Hardware: d.Hardware, Kind: kind, Trace: tr, Song: song, Ended: ended,
		Track: takeover.Track{URL: h.URL, Label: h.Label, Fetched: h.Fetched(), Restarted: h.Restarted(), Duration: length,
			Length: h.Duration, Lost: h.Lost, Resume: takeover.Resumer(h, tr), Seekable: h.Seekable()}})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.Log.Info("接管　开始测试", "音箱", d.Name, "音频", req.Source, "记录", run.ID)
	run.DeviceName = firstNonEmpty(d.Alias, d.Name)
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) takeoverRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.Store.Runs(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs, "stream_ready": s.Stream != nil && s.Stream.Ready() == nil})
}

func firstNonEmpty(v ...string) string {
	for _, x := range v {
		if x != "" {
			return x
		}
	}
	return ""
}
