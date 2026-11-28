package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"trackferry/internal/fill"
	"trackferry/internal/netcover"
	"trackferry/internal/online"
)

// netCover serves a platform's cover picture, kept on disk after the first ask.
func (s *Server) netCover(w http.ResponseWriter, r *http.Request, id string) {
	if s.NetCovers == nil {
		writeError(w, http.StatusNotFound, "no_cover", "没有这张封面")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	mime, b, err := s.NetCovers.Get(ctx, id)
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, netcover.ErrNoCover) {
			code = http.StatusNotFound
		}
		writeError(w, code, "no_cover", "没有这张封面")
		return
	}
	w.Header().Set("Content-Type", mime)
	// The ID is the picture's address: it never names another picture.
	w.Header().Set("Cache-Control", "private, max-age=2592000, immutable")
	w.Write(b)
}

// A miss is asked for again after this short wait: a platform can fail one song today and
// answer for it tomorrow. Lyrics found are kept.
const onlineLyricsAgain = 10 * time.Minute

// onlineLyrics gets an online song's lyrics: ?key=. Kept once found, misses for a while.
func (s *Server) onlineLyrics(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	source, _, ok := strings.Cut(key, ":")
	if !ok || len(key) > 300 {
		writeError(w, http.StatusBadRequest, "bad_request", "要 key")
		return
	}
	if text, at, ok, err := s.Store.OnlineLyrics(r.Context(), key); err == nil && ok && (text != "" || time.Since(at) < onlineLyricsAgain) {
		writeJSON(w, http.StatusOK, map[string]any{"lyrics": text, "from": source})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	text, err := s.Router.Online.Lyrics(ctx, key)
	switch {
	case err == nil, errors.Is(err, online.ErrNoLyrics):
		_ = s.Store.KeepOnlineLyrics(context.WithoutCancel(ctx), key, text)
	case errors.Is(err, online.ErrGone):
		writeError(w, http.StatusNotFound, "gone", err.Error())
		return
	default:
		writeError(w, http.StatusBadGateway, "no_lyrics", "歌词暂时读不到")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lyrics": text, "from": source})
}

func (s *Server) fillStatus(ctx context.Context) *fill.Status {
	if s.Fill == nil {
		return nil
	}
	st := s.Fill.Status(ctx)
	return &st
}

func (s *Server) fillUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if s.Fill == nil {
		writeError(w, http.StatusNotFound, "no_fill", "这个服务器不补封面歌词")
		return
	}
	if err := s.Fill.SetOn(r.Context(), req.On); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fill": s.fillStatus(r.Context())})
}
