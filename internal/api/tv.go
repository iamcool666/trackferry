package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"trackferry/internal/store"
	"trackferry/internal/tv"
)

const tvAPI = "/api/tv/v1"

// TVHandler is served on the pinned TLS port. It never mounts the admin application.
func (s *Server) TVHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+tvAPI+"/info", s.tvInfo)
	mux.HandleFunc("POST "+tvAPI+"/pair", s.tvPairStart)
	mux.HandleFunc("POST "+tvAPI+"/pair/status", s.tvPairStatus)
	mux.HandleFunc("POST "+tvAPI+"/pair/cancel", s.tvPairCancel)
	read := func(path string, handler http.HandlerFunc) { mux.Handle("GET "+tvAPI+path, s.tvAuth(handler)) }
	read("/shelf", s.tvShelf)
	read("/search", s.tvSearch)
	read("/online", s.librarySearchOnline)
	read("/pending", s.tvPending)
	mux.Handle("POST "+tvAPI+"/sync", s.tvAuth(http.HandlerFunc(s.tvSync)))
	mux.Handle("POST "+tvAPI+"/resolve", s.tvAuth(http.HandlerFunc(s.tvResolve)))
	read("/playlists", s.libraryPlaylists)
	read("/tracks/{id}/favorite", s.isFavorite)
	mux.Handle("PUT "+tvAPI+"/tracks/{id}/favorite", s.tvAuth(http.HandlerFunc(s.favorite)))
	mux.Handle("POST "+tvAPI+"/playlists/{guid}/tracks", s.tvAuth(http.HandlerFunc(s.libraryPlaylistAdd)))
	read("/queue", s.tvQueue)
	read("/lyrics/{id}", s.tvLyrics)
	mux.Handle("PUT "+tvAPI+"/lyrics/{id}/offset", s.tvAuth(http.HandlerFunc(s.lyricsOffset)))
	read("/app", s.tvAppStatus)
	mux.Handle("POST "+tvAPI+"/app", s.tvAuth(http.HandlerFunc(s.tvAppPrepare)))
	read("/app/apk", s.tvAppFile)
	read("/cover/{id}", s.tvCover)
	read("/tech", s.tech)
	read("/versions", s.versions)
	read("/online-lyrics", s.onlineLyrics)
	read("/audio", s.tvAudio)
	return securityHeaders(mux)
}

func (s *Server) tvInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"name": s.TV.Name, "protocol": 1, "id": s.TV.Pin, "version": s.Build.Version})
}

func (s *Server) tvPairStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		Commitment string `json:"commitment"`
		TokenHash  string `json:"token_hash"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	address, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		address = r.RemoteAddr
	}
	id, err := s.TV.Start(address, req.Name, req.Commitment, req.TokenHash)
	if err != nil {
		tvError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

type tvPairProof struct {
	ID   string `json:"id"`
	Seed string `json:"seed"`
}

func (s *Server) tvPairStatus(w http.ResponseWriter, r *http.Request) {
	var req tvPairProof
	if !readJSON(w, r, &req) {
		return
	}
	status, err := s.TV.Status(r.Context(), req.ID, req.Seed)
	if err != nil {
		tvError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) tvPairCancel(w http.ResponseWriter, r *http.Request) {
	var req tvPairProof
	if !readJSON(w, r, &req) {
		return
	}
	s.TV.Cancel(req.ID, req.Seed)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) tvApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	d, err := s.TV.Approve(r.Context(), req.Code)
	if err != nil {
		tvError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) tvDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.Store.TVs(r.Context())
	if err != nil {
		tvError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (s *Server) tvRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 20 || strings.ContainsFunc(name, unicode.IsControl) {
		writeError(w, http.StatusBadRequest, "bad_name", "电视名称请填写 1 到 20 个字符")
		return
	}
	d, err := s.Store.RenameTV(r.Context(), r.PathValue("id"), name)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no_tv", "这台电视已解除配对")
	} else if err != nil {
		tvError(w, err)
	} else {
		writeJSON(w, http.StatusOK, d)
	}
}

func (s *Server) tvRevoke(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.RevokeTV(r.Context(), r.PathValue("id")); err != nil {
		tvError(w, err)
		return
	}
	s.Remote.Revoke(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) tvAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		id, err := s.TV.Device(r.Context(), token)
		if err != nil {
			tvError(w, err)
			return
		}
		if id == "" {
			writeError(w, http.StatusUnauthorized, "tv_unauthorized", "请在手机上重新配对电视")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tvDeviceKey{}, id)))
	})
}

func tvError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tv.ErrExpired):
		writeError(w, http.StatusGone, "pair_expired", err.Error())
	case errors.Is(err, tv.ErrInvalid):
		writeError(w, http.StatusBadRequest, "bad_pair", err.Error())
	case errors.Is(err, tv.ErrBusy):
		writeError(w, http.StatusTooManyRequests, "pair_busy", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "tv_error", "电视服务暂不可用")
	}
}

type tvTrack struct {
	Online     string `json:"online,omitempty"`
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album,omitempty"`
	AlbumID    string `json:"album_id,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Cover      string `json:"cover,omitempty"`
}

func tvTrackFrom(t store.TrackRow) tvTrack {
	return tvTrack{ID: t.ID, Title: t.Title, Artist: t.Artist, Album: t.Album, AlbumID: t.AlbumID, DurationMS: t.DurationMS, Cover: t.Cover}
}

func tvTracks(rows []store.TrackRow) []tvTrack {
	out := make([]tvTrack, 0, len(rows))
	for _, t := range rows {
		out = append(out, tvTrackFrom(t))
	}
	return out
}

func (s *Server) tvShelf(w http.ResponseWriter, r *http.Request) {
	var batch uint64
	if value := r.URL.Query().Get("batch"); value != "" {
		var err error
		batch, err = strconv.ParseUint(value, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_batch", "推荐批次无效")
			return
		}
	}
	picks, albums, artists := s.Library.TVBrowse(time.Now(), batch)
	favorites := len(s.Library.FavoriteIDs())
	writeJSON(w, http.StatusOK, map[string]any{"picks": tvTracks(picks), "albums": albums, "artists": artists, "favorite_count": favorites})
}

func (s *Server) tvSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) > 100 {
		writeError(w, http.StatusBadRequest, "too_long", "搜索词太长了")
		return
	}
	matches := s.Library.Find(q, 50)
	tracks := make([]store.TrackRow, 0, len(matches))
	for _, m := range matches {
		tracks = append(tracks, m.TrackRow)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tracks": tvTracks(tracks)})
}

func (s *Server) tvQueue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var picks []int64
	for _, raw := range q["pick"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_track", "曲目无效")
			return
		}
		picks = append(picks, id)
	}
	var name string
	var ids []int64
	if q.Get("source") == "random" { // what comes after a song that was put on alone, as in the browser
		name, ids = "接着随机放", s.following(fullList)
	} else {
		var ok bool
		if name, ids, ok = s.collect(w, r, q.Get("source"), q.Get("id"), q.Get("name"), picks); !ok {
			return
		}
	}
	rows := make([]store.TrackRow, 0, len(ids))
	for _, id := range ids {
		if t, ok := s.Library.Track(id); ok {
			rows = append(rows, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "tracks": tvTracks(rows)})
}

func (s *Server) tvLyrics(w http.ResponseWriter, r *http.Request) {
	s.lyrics(w, r, false)
}

func (s *Server) tvCover(w http.ResponseWriter, r *http.Request) {
	s.cover(w, r)
}

func (s *Server) tvAudio(w http.ResponseWriter, r *http.Request) {
	s.hereAudio(w, r)
}

// tvAppStatus says which app goes with this server and whether it is ready for the television.
func (s *Server) tvAppStatus(w http.ResponseWriter, r *http.Request) {
	if s.Update == nil {
		writeError(w, http.StatusNotFound, "no_update", "这个服务器不提供电视应用更新")
		return
	}
	st, err := s.Update.TVApp(r.URL.Query().Get("abi"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no_update", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) tvAppPrepare(w http.ResponseWriter, r *http.Request) {
	if s.Update == nil {
		writeError(w, http.StatusNotFound, "no_update", "这个服务器不提供电视应用更新")
		return
	}
	st, err := s.Update.PrepareTV(r.Context(), r.URL.Query().Get("abi"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no_update", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, st)
}

// tvAppFile sends the checked app. The television checks the sum again before installing it.
func (s *Server) tvAppFile(w http.ResponseWriter, r *http.Request) {
	if s.Update == nil {
		writeError(w, http.StatusNotFound, "no_update", "这个服务器不提供电视应用更新")
		return
	}
	path, st, err := s.Update.TVFile(r.URL.Query().Get("abi"))
	if err != nil {
		writeError(w, http.StatusConflict, "not_ready", err.Error())
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusConflict, "not_ready", "新版电视应用还没有准备好")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("X-TrackFerry-SHA256", st.SHA256)
	w.Header().Set("X-TrackFerry-Version", st.Version)
	http.ServeContent(w, r, filepath.Base(path), time.Time{}, f)
}
