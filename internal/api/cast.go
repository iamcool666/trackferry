package api

import (
	"errors"
	"net/http"

	"trackferry/internal/cast"
)

// castList lists the DLNA and Chromecast devices found, and which ones are on.
func (s *Server) castList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.castListing())
}

// castListing says up front when devices could not fetch songs at all.
func (s *Server) castListing() cast.Listing {
	l := s.Cast.Listing()
	if s.Stream != nil && s.Stream.Ready() != nil {
		l.Error = noBaseURL
	}
	return l
}

const noBaseURL = "还没设置 TRACKFERRY_URL（设备取歌用的局域网地址），打开的设备取不到歌"

// castScan searches the network again.
func (s *Server) castScan(w http.ResponseWriter, _ *http.Request) {
	s.Cast.Rescan()
	writeJSON(w, http.StatusAccepted, s.castListing())
}

// castSet turns a device on, so it is an output, or off: PUT {on}.
func (s *Server) castSet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On bool `json:"on"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	if !cast.Is(id) || len(id) > 80 {
		writeError(w, http.StatusBadRequest, "bad_device", "设备无效")
		return
	}
	if err := s.Cast.Set(id, in.On); err != nil {
		code := http.StatusConflict
		if errors.Is(err, cast.ErrNoDevice) {
			code = http.StatusNotFound
		}
		writeError(w, code, "cast_device", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.castListing())
}
