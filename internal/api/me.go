package api

import (
	"net/http"
	"strings"
	"unicode/utf8"
)

// deviceName is a first name for a browser that signs in, from what it says it is. The
// family renames it in settings: 客厅 iPad, 妈妈的手机.
func deviceName(ua string) string {
	switch {
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "Android") && strings.Contains(ua, "Mobile"):
		return "安卓手机"
	case strings.Contains(ua, "Android"):
		return "安卓平板"
	case strings.Contains(ua, "Macintosh"):
		return "Mac"
	case strings.Contains(ua, "Windows"):
		return "Windows 电脑"
	case strings.Contains(ua, "Linux"), strings.Contains(ua, "CrOS"):
		return "电脑"
	}
	return "浏览器"
}

// who is what the browser asking is called; "" for a bearer token or the television.
// A session from before names existed is named now.
func (s *Server) who(r *http.Request) string {
	ck, err := r.Cookie(sessionCookie)
	if err != nil || ck.Value == "" {
		return ""
	}
	name, err := s.Store.SessionName(r.Context(), ck.Value)
	if err != nil || name != "" {
		return name
	}
	name = s.Store.FreeName(r.Context(), deviceName(r.UserAgent()))
	if s.Store.RenameSession(r.Context(), ck.Value, name) != nil {
		return ""
	}
	return name
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"name": s.who(r)})
}

// meRename names this browser; other browsers signed in with the same password keep theirs.
func (s *Server) meRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.Join(strings.Fields(req.Name), " ")
	ck, err := r.Cookie(sessionCookie)
	switch {
	case err != nil || ck.Value == "":
		writeError(w, http.StatusBadRequest, "no_session", "只有登录的浏览器能起名字")
		return
	case name == "" || utf8.RuneCountInString(name) > 20:
		writeError(w, http.StatusBadRequest, "bad_name", "名字要 1 到 20 个字")
		return
	}
	if err := s.Store.RenameSession(r.Context(), ck.Value, name); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}
