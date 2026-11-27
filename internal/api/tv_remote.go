package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"trackferry/internal/store"
	"trackferry/internal/tv"
)

type tvDeviceKey struct{}

// Snapshots carry track IDs; larger catalogs still fit beneath this separate body limit.
func tvJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, 400, "bad_json", "电视播放数据无效或过大")
		return false
	}
	return true
}

func (s *Server) tvSync(w http.ResponseWriter, r *http.Request) {
	id, _ := r.Context().Value(tvDeviceKey{}).(string)
	var h tv.Heartbeat
	if !tvJSON(w, r, &h) {
		return
	}
	p := h.Playback
	if len(h.Epoch) == 0 || len(h.Epoch) > 80 || h.Sequence < 1 || len(p.Revision) > 80 || len(p.Entry) > 150 ||
		p.Position < 0 || p.Duration < 0 || p.Count < 0 || p.Count > tv.MaxQueue || p.Index < 0 || (p.Count > 0 && p.Index >= p.Count) ||
		(p.Queue != nil && len(p.Queue) != p.Count) || !slices.Contains([]string{"idle", "paused", "buffering", "playing", "ended", "error"}, p.Phase) ||
		!validTVRefs(p.Queue) || !validTVRefs([]tv.Ref{p.Current}) ||
		(h.Result != nil && (len(h.Result.ID) > 30 || len(h.Result.Message) > 400 || !slices.Contains([]string{"done", "failed"}, h.Result.State))) {
		writeError(w, 400, "bad_report", "电视播放状态无效")
		return
	}
	update := s.Remote.Sync(id, h)
	if update.ResultNew {
		s.Log.Info("电视　操作回报", "指令", h.Result.ID, "结果", h.Result.State, "播放", p.Phase, "队列长度", p.Count)
	}
	writeJSON(w, 200, update)
}

func validTVRefs(refs []tv.Ref) bool {
	for _, x := range refs {
		if x.ID < 0 || len(x.Online) > 200 || len(x.Title) > 400 || len(x.Artist) > 400 {
			return false
		}
	}
	return true
}

type tvOutput struct {
	store.TVDevice
	Kind     string      `json:"kind,omitempty"` // dlna | chromecast; "" for a TrackFerry TV
	Online   bool        `json:"online"`
	Playback tv.Playback `json:"playback"`
}

func (s *Server) tvOutputs(w http.ResponseWriter, r *http.Request) {
	var devs []store.TVDevice
	var prompt *tv.PairPrompt
	if s.TV != nil {
		var err error
		if devs, err = s.Store.TVs(r.Context()); err != nil {
			tvError(w, err)
			return
		}
		prompt = s.TV.Prompt()
	}
	out := make([]tvOutput, 0, len(devs))
	for _, d := range devs {
		p, online := s.Remote.State(d.ID, false)
		out = append(out, tvOutput{TVDevice: d, Online: online, Playback: p})
	}
	if s.Cast != nil {
		for _, d := range s.Cast.On() {
			p, online := s.Remote.State(d.ID, false)
			out = append(out, tvOutput{TVDevice: store.TVDevice{ID: d.ID, Name: d.Name}, Kind: d.Kind, Online: online, Playback: p})
		}
	}
	writeJSON(w, 200, map[string]any{"devices": out, "now_ms": time.Now().UnixMilli(), "pairing": prompt})
}

func (s *Server) tvSnapshot(w http.ResponseWriter, r *http.Request) {
	p, ok := s.Remote.State(r.PathValue("id"), true)
	if !ok {
		writeError(w, 409, "offline", tv.ErrOffline.Error())
		return
	}
	rows := s.tvMetadata(p.Queue)
	writeJSON(w, 200, map[string]any{"playback": p, "items": rows})
}

func (s *Server) tvMetadata(refs []tv.Ref) []tvTrack {
	rows := make([]tvTrack, 0, len(refs))
	for _, ref := range refs {
		if t, ok := s.Library.Track(ref.ID); ok && ref.ID > 0 {
			rows = append(rows, tvTrackFrom(t))
			continue
		}
		rows = append(rows, tvTrack{ID: ref.ID, Online: ref.Online, Title: ref.Title, Artist: ref.Artist})
	}
	return rows
}

type tvCommandRequest struct {
	tv.Command
	Source   string  `json:"source"`
	SourceID string  `json:"source_id"`
	Name     string  `json:"name"`
	TrackID  int64   `json:"track_id"`
	Picks    []int64 `json:"track_ids"`
}

func (s *Server) tvCommand(w http.ResponseWriter, r *http.Request) {
	var req tvCommandRequest
	if !tvJSON(w, r, &req) {
		return
	}
	c := req.Command
	if len(c.Items) > tv.MaxQueue || !validTVRefs(c.Items) || c.Position < 0 || c.Position > 24*60*60*1000 ||
		len(c.Revision) > 80 || len(c.Entry) > 150 || !slices.Contains([]string{"", "loop", "shuffle", "one"}, c.Mode) ||
		!slices.Contains([]string{"play", "pause", "stop", "next", "previous", "seek", "jump", "mode", "remove", "load", "insert", "volume"}, c.Action) ||
		c.Action == "volume" && (c.Volume == nil || *c.Volume < 0 || *c.Volume > 100) {
		writeError(w, 400, "bad_command", "播放指令无效")
		return
	}
	if req.Source != "" {
		if c.Action != "load" || len(c.Items) != 0 {
			writeError(w, 400, "bad_command", "每次只发送一个队列")
			return
		}
		_, ids, ok := s.collect(w, r, req.Source, req.SourceID, req.Name, req.Picks)
		if !ok {
			return
		}
		if req.TrackID != 0 {
			c.Index = slices.Index(ids, req.TrackID)
		}
		if len(ids) > tv.MaxQueue {
			writeError(w, 400, "large_queue", "队列超过电视支持的长度")
			return
		}
		for _, id := range ids {
			c.Items = append(c.Items, tv.Ref{ID: id})
		}
	}
	if req.Source == "" && c.Action == "load" && len(c.Items) == 1 && c.Index == 0 { // a song alone goes on as anywhere else
		for _, id := range s.withRandom(c.Items[0].ID)[1:] {
			c.Items = append(c.Items, tv.Ref{ID: id})
		}
	}
	if c.Action == "load" || c.Action == "insert" {
		if len(c.Items) == 0 || c.Action == "load" && (c.Index < 0 || c.Index >= len(c.Items)) || c.Action == "insert" && (len(c.Items) != 1 || c.QueueMode != "next" && c.QueueMode != "last") {
			writeError(w, 409, "bad_queue", "没有可播放的队列，或选中的曲目已移走")
			return
		}
		for _, song := range c.Items {
			if song.ID > 0 {
				if _, ok := s.Library.Track(song.ID); !ok {
					writeError(w, 404, "no_track", "队列中有曲目已离开曲库")
					return
				}
			} else if !strings.Contains(song.Online, ":") {
				writeError(w, 400, "bad_track", "队列中有无法识别的曲目")
				return
			}
		}
	}
	id := r.PathValue("id")
	result, err := s.Remote.Submit(id, c)
	if errors.Is(err, tv.ErrOffline) || errors.Is(err, tv.ErrMoved) {
		writeError(w, 409, "tv_unavailable", err.Error())
		return
	}
	s.Log.Info("电视　接收操作", "指令", result.ID, "动作", c.Action, "曲目数", len(c.Items))
	writeJSON(w, 202, result)
}

func (s *Server) tvCommandStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Remote.Status(r.PathValue("id"), r.PathValue("command")))
}

func (s *Server) tvCommandCancel(w http.ResponseWriter, r *http.Request) {
	s.Remote.Cancel(r.PathValue("id"), r.PathValue("command"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) tvResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []tv.Ref `json:"items"`
	}
	if !tvJSON(w, r, &req) {
		return
	}
	if len(req.Items) > tv.MaxQueue || !validTVRefs(req.Items) {
		writeError(w, 400, "bad_queue", "队列无效")
		return
	}
	writeJSON(w, 200, map[string]any{"tracks": s.tvMetadata(req.Items)})
}

func (s *Server) tvPending(w http.ResponseWriter, r *http.Request) {
	id, _ := r.Context().Value(tvDeviceKey{}).(string)
	result := s.Remote.Status(id, r.URL.Query().Get("command"))
	writeJSON(w, 200, map[string]bool{"pending": result.State == "pending"})
}
