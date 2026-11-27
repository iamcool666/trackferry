package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"trackferry/internal/live"
	"trackferry/internal/store"
	"trackferry/internal/takeover"
	"trackferry/internal/tv"
)

func (s *Server) speakerPlayback(id string, runID int64) (live.Flow, store.TakeoverRun, bool) {
	flows := s.Live.Flows()
	for i := len(flows) - 1; i >= 0; i-- {
		f := flows[i]
		run, ok := f.Run.(store.TakeoverRun)
		if f.DeviceID != id || f.Done || !ok || run.Song == nil {
			continue
		}
		if runID != 0 && run.ID != runID {
			break
		}
		return f, run, true
	}
	return live.Flow{}, store.TakeoverRun{}, false
}

// speakerClockFresh bounds how old the runner's clock may be: it is read every few seconds.
const speakerClockFresh = 15 * time.Second

func (s *Server) speakerSnapshot(w http.ResponseWriter, r *http.Request) {
	d, ok := s.target(w, r)
	if !ok {
		return
	}
	f, run, ok := s.speakerPlayback(d.DeviceID, 0)
	if !ok || f.Player == nil || (run.Song.TrackID == 0 && run.Song.Online == "") || (f.Player.Phase != "paused" && (!f.Player.Ours || f.Player.Phase != "playing")) {
		writeError(w, 409, "no_song", "小爱当前没有可接续的歌")
		return
	}
	current := takeover.Item{ID: run.Song.TrackID}
	if current.ID == 0 {
		current = takeover.Item{Online: run.Song.Online, Title: run.Song.Title, Artist: run.Song.Artist}
	}
	queued, index := s.Dispatcher.Snapshot(d.DeviceID, current)
	if len(queued) > tv.MaxQueue {
		writeError(w, 400, "large_queue", "队列超过电视支持的长度")
		return
	}
	items := make([]tv.Ref, 0, len(queued))
	for _, it := range queued {
		items = append(items, tv.Ref{ID: it.ID, Online: it.Online, Title: it.Title, Artist: it.Artist})
	}
	// Elapsed is the runner's own clock at AtMS, not a guess: it runs on from there while the
	// song plays. A clock that has not been read for long belongs to a runner that is stuck, and
	// a position taken from it would be wrong; the page asks again instead.
	pos := f.Player.Elapsed
	if f.Player.Phase == "playing" && !f.Player.Held {
		since := time.Now().UnixMilli() - f.Player.AtMS
		if since > speakerClockFresh.Milliseconds() {
			writeError(w, 409, "stale_clock", "小爱的进度暂时读不到，稍后再试")
			return
		}
		pos += max(0, since)
	}
	if run.Song.DurationMS > 0 {
		pos = min(pos, max(0, run.Song.DurationMS-100))
	}
	// tracks: the same queue with what this page needs to play it itself.
	writeJSON(w, 200, map[string]any{"run_id": run.ID, "items": items, "tracks": s.tvMetadata(items), "index": index, "position_ms": max(0, pos), "mode": s.Dispatcher.PlayMode(r.Context(), d.DeviceID)})
}

func (s *Server) speakerPauseRun(w http.ResponseWriter, r *http.Request) {
	d, ok := s.target(w, r)
	if !ok {
		return
	}
	var req struct {
		RunID int64 `json:"run_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	if err := s.Engine.PauseRun(ctx, d.DeviceID, d.Hardware, req.RunID); errors.Is(err, takeover.ErrSeekMoved) {
		writeError(w, 409, "moved", "小爱已经换歌，保留当前播放")
		return
	} else if err != nil {
		writeError(w, 502, "not_done", "小爱没有确认暂停")
		return
	}
	w.WriteHeader(204)
}

func (s *Server) speakerReceive(w http.ResponseWriter, r *http.Request) {
	d, ok := s.target(w, r)
	if !ok {
		return
	}
	var req struct {
		TrackIDs []int64  `json:"track_ids"` // library songs only, as before
		Items    []tv.Ref `json:"items"`
		Index    int      `json:"index"`
		Position int64    `json:"position_ms"`
	}
	if !tvJSON(w, r, &req) {
		return
	}
	items := make([]takeover.Item, 0, len(req.Items)+len(req.TrackIDs))
	for _, ref := range req.Items {
		items = append(items, takeover.Item{ID: ref.ID, Online: ref.Online, Title: ref.Title, Artist: ref.Artist})
	}
	if len(req.Items) == 0 {
		for _, id := range req.TrackIDs {
			items = append(items, takeover.Item{ID: id})
		}
	}
	if req.Index < 0 || req.Index >= len(items) || len(items) > tv.MaxQueue || req.Position < 0 || req.Position > 24*60*60*1000 {
		writeError(w, 400, "bad_queue", "队列或进度无效")
		return
	}
	for _, it := range items {
		if it.ID > 0 {
			if _, ok := s.Library.Track(it.ID); !ok {
				writeError(w, 404, "no_track", "队列中有曲目已离开曲库")
				return
			}
		} else if it.Online == "" || len(it.Online) > 512 || len(it.Title) > 300 || len(it.Artist) > 300 {
			writeError(w, 400, "bad_queue", "队列里有一首歌说不清是哪首")
			return
		}
	}
	run, err := s.Dispatcher.Receive(r.Context(), d, items, req.Index, time.Duration(req.Position)*time.Millisecond)
	if err != nil {
		writeError(w, 409, "cannot_handoff", err.Error())
		return
	}
	confirmed := false
	defer func() {
		if !confirmed {
			s.Engine.CancelRun(d.DeviceID, run.ID)
		}
	}()
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	tick := time.NewTicker(230 * time.Millisecond)
	defer tick.Stop()
	for {
		if !s.Engine.IsRun(d.DeviceID, run.ID) {
			writeError(w, 409, "unconfirmed", "音箱未确认本次接力，源设备继续播放")
			return
		}
		f, cur, ok := s.speakerPlayback(d.DeviceID, run.ID)
		if ok && f.Player != nil && f.Player.Phase == "playing" && f.Player.Ours {
			for _, step := range cur.Steps {
				if step.Step == takeover.StepConfirmed && step.OK && ctx.Err() == nil {
					confirmed = true
					writeJSON(w, 200, map[string]int64{"run_id": run.ID})
					return
				}
			}
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			writeError(w, 504, "unconfirmed", "音箱没有确认播放，源设备继续播放")
			return
		}
	}
}
