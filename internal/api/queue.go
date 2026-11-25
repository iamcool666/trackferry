package api

import (
	"errors"
	"net/http"
	"strconv"

	"trackferry/internal/takeover"
)

// queued is a song that comes on a speaker, as a page shows it.
type queued struct {
	takeover.Pick
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Cover      string `json:"cover,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

func (s *Server) named(q queued) queued {
	if s.Library == nil || q.ID == 0 {
		return q
	}
	if t, ok := s.Library.Track(q.ID); ok {
		q.Title, q.Artist, q.Cover, q.DurationMS = t.Title, t.Artist, t.Cover, t.DurationMS
	} else {
		q.Title = "曲库里没有这首歌了"
	}
	return q
}

const queuePage = 60

// queue lists what comes on a speaker.
func (s *Server) queue(w http.ResponseWriter, r *http.Request) {
	out := struct {
		On     bool     `json:"on"` // a song of ours is
		List   string   `json:"list"`
		Total  int      `json:"total"`
		Aside  bool     `json:"aside"`
		Mode   string   `json:"mode"`
		Wishes []queued `json:"wishes"`
		Songs  []queued `json:"songs"`
		Left   int      `json:"left"` // songs after these
	}{Wishes: []queued{}, Songs: []queued{}}
	dev := r.PathValue("id")
	l, on := s.Dispatcher.Listing(r.Context(), dev)
	out.On, out.List, out.Total, out.Aside, out.Mode = on, l.List, l.Total, l.Aside, s.Dispatcher.PlayMode(r.Context(), dev)
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))
	from = min(max(0, from), len(l.Songs))
	to := min(from+queuePage, len(l.Songs))
	for _, x := range l.Songs[from:to] {
		out.Songs = append(out.Songs, s.named(queued{Pick: takeover.Pick{Slot: x}}))
	}
	for _, x := range l.Wishes {
		out.Wishes = append(out.Wishes, s.named(queued{Pick: takeover.Pick{Wish: x.ID, Slot: takeover.Slot{ID: x.Track}}, Title: x.Title, Artist: x.Artist}))
	}
	out.Left = len(l.Songs) - to
	writeJSON(w, http.StatusOK, out)
}

// queueChange plays one of the songs that come, or takes it off the list.
func (s *Server) queueChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"` // play | remove
		Now    bool   `json:"now"`    // remove: the song that is on
		takeover.Pick
	}
	if !readJSON(w, r, &req) {
		return
	}
	var err error
	dev := r.PathValue("id")
	switch {
	case req.Action == "play":
		d, ok := s.target(w, r)
		if !ok {
			return
		}
		err = s.Dispatcher.Jump(r.Context(), d.DeviceID, req.Pick)
	case req.Action == "remove" && req.Now:
		d, ok := s.target(w, r)
		if !ok {
			return
		}
		err = s.Dispatcher.RemoveCurrent(r.Context(), d.DeviceID)
	case req.Action == "remove":
		err = s.Dispatcher.Remove(dev, req.Pick)
	default:
		writeError(w, http.StatusBadRequest, "bad_action", "action 只能是 play 或 remove")
		return
	}
	switch {
	case errors.Is(err, takeover.ErrNoQueue), errors.Is(err, takeover.ErrMoved), errors.Is(err, takeover.ErrNoSong):
		writeError(w, http.StatusConflict, "moved", err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, "not_done", err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
