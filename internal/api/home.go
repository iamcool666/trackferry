package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"trackferry/internal/home"
)

// Settings of the endpoint for home automation.
const (
	haEnabled = "ha.enabled"
	haToken   = "ha.token_hash"
)

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func (s *Server) homeView(r *http.Request) (map[string]any, error) {
	on, err := s.Store.Setting(r.Context(), haEnabled)
	if err != nil {
		return nil, err
	}
	tok, err := s.Store.Setting(r.Context(), haToken)
	if err != nil {
		return nil, err
	}
	return map[string]any{"enabled": on == "1", "has_token": tok != "", "connected": s.Home.Listeners()}, nil
}

func (s *Server) homeStatus(w http.ResponseWriter, r *http.Request) {
	v, err := s.homeView(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) homeSwitch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	v := "0"
	if in.Enabled {
		v = "1"
	}
	if err := s.Store.SetSetting(r.Context(), haEnabled, v); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if !in.Enabled {
		s.Home.Drop()
	}
	s.Log.Info("智能家居　改了开关", "启用", in.Enabled)
	s.homeStatus(w, r)
}

// homeToken makes a new token. It is shown this once; only its hash is kept.
func (s *Server) homeToken(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 32)
	rand.Read(b)
	tok := "tfha_" + hex.EncodeToString(b)
	if err := s.Store.SetSetting(r.Context(), haToken, hashToken(tok)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.Home.Drop()
	s.Log.Info("智能家居　换了令牌")
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}

// haAllowed checks the endpoint's own token: on says whether the endpoint exists at all.
func (s *Server) haAllowed(r *http.Request) (on, ok bool) {
	v, err := s.Store.Setting(r.Context(), haEnabled)
	if err != nil || v != "1" {
		return false, false
	}
	want, err := s.Store.Setting(r.Context(), haToken)
	got, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if err != nil || want == "" || !bearer {
		return true, false
	}
	return true, subtle.ConstantTimeCompare([]byte(hashToken(got)), []byte(want)) == 1
}

// haAuth guards the endpoint. Sessions and the admin token do not open it, and its token opens
// nothing else.
func (s *Server) haAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		on, ok := s.haAllowed(r)
		switch {
		case !on:
			writeError(w, http.StatusNotFound, "not_found", "没有这个接口")
		case !ok:
			w.Header().Set("WWW-Authenticate", `Bearer realm="trackferry-ha"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "令牌不对")
		default:
			next(w, r)
		}
	})
}

func (s *Server) haState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Home.Snapshot(r.Context()))
}

// haEvents streams the speakers: everything first, then what changes.
func (s *Server) haEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	rc.SetReadDeadline(time.Time{})
	ch, stop := s.Home.Subscribe(r.Context())
	defer stop()
	// The token may have changed while the first state was read.
	if _, ok := s.haAllowed(r); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "令牌不对")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	beat := time.NewTicker(20 * time.Second)
	defer beat.Stop()
	for {
		var msg []byte
		select {
		case <-r.Context().Done():
			return
		case <-beat.C:
			msg = []byte(": \n\n")
		case m, ok := <-ch:
			if !ok {
				return
			}
			msg = m
		}
		rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := w.Write(msg); err != nil || rc.Flush() != nil {
			return
		}
	}
}

func (s *Server) haLyrics(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	t, ok := s.Library.Track(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no_track", "曲库里没有这首歌了")
		return
	}
	text, err := s.trackLyrics(r.Context(), t)
	if err != nil {
		writeError(w, http.StatusBadGateway, "lyrics", "歌词暂时读不到")
		return
	}
	synced, lines := home.Lyrics(text)
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": home.SchemaVersion, "track_id": id, "synced": synced, "offset_ms": s.offset(r, id), "lines": lines})
}
