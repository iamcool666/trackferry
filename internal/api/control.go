package api

import (
	"errors"
	"net/http"
	"time"

	"trackferry/internal/home"
	"trackferry/internal/store"
	"trackferry/internal/takeover"
	"trackferry/internal/xiaomi"
)

// control presses a speaker's buttons.
func (s *Server) control(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"` // play | pause | stop | next | previous | mode | volume | wake | seek
		Volume *int   `json:"volume"`
		Mode   string `json:"mode"` // loop | shuffle | one
		// Where the song goes to with seek, in ms.
		Position *int64 `json:"position"`
		RunID    *int64 `json:"run_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	switch req.Action {
	case takeover.ControlPlay, takeover.ControlPause, takeover.ControlStop, takeover.ControlWake, takeover.ControlNext, takeover.ControlPrevious:
	case takeover.ControlVolume:
		if req.Volume == nil || *req.Volume < 0 || *req.Volume > 100 {
			writeError(w, http.StatusBadRequest, "bad_volume", "音量要在 0 到 100 之间")
			return
		}
	case takeover.ControlMode:
		if req.Mode != takeover.ModeLoop && req.Mode != takeover.ModeShuffle && req.Mode != takeover.ModeOne {
			writeError(w, http.StatusBadRequest, "bad_mode", "mode 只能是 loop、shuffle 或 one")
			return
		}
	case takeover.ControlSeek:
		if req.Position == nil || *req.Position < 0 || *req.Position > 24*60*60*1000 {
			writeError(w, http.StatusBadRequest, "bad_position", "position 是要拖到的位置，单位毫秒")
			return
		}
		if req.RunID != nil && *req.RunID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_run", "run_id 必须是当前播放记录的 ID")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "bad_action", "action 只能是 play、pause、stop、next、previous、mode、volume、seek 或 wake")
		return
	}
	d, err := s.Store.Device(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no_device", "没有这台音箱")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if !d.InAccount || d.Mode == store.ModeOff {
		writeError(w, http.StatusConflict, "device_off", "这台音箱没有接入。先在音箱页把它设为「观察」或「接管」")
		return
	}
	switch req.Action {
	case takeover.ControlVolume:
		err = s.Engine.SetVolume(r.Context(), d.DeviceID, *req.Volume)
	case takeover.ControlSeek:
		var run []int64
		if req.RunID != nil {
			run = append(run, *req.RunID)
		}
		err = s.Engine.Seek(r.Context(), d.DeviceID, time.Duration(*req.Position)*time.Millisecond, run...)
		if errors.Is(err, takeover.ErrSeekMoved) {
			writeError(w, http.StatusConflict, "moved", err.Error())
			return
		}
		if errors.Is(err, takeover.ErrCannotSeek) || errors.Is(err, takeover.ErrNoSeek) {
			writeError(w, http.StatusConflict, "cannot_seek", err.Error())
			return
		}
	case takeover.ControlWake:
		if err = s.Xiaomi.Wake(r.Context(), d.Hardware, d.MiotDID); err == nil {
			s.Engine.Woke(d.DeviceID)
		}
	case takeover.ControlNext, takeover.ControlPrevious, takeover.ControlMode:
		if s.Dispatcher == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "这个版本不能切歌")
			return
		}
		switch req.Action {
		case takeover.ControlMode:
			err = s.Dispatcher.SetPlayMode(r.Context(), d.DeviceID, req.Mode)
		case takeover.ControlNext:
			err = s.Dispatcher.Skip(r.Context(), d.DeviceID, 1)
		default:
			err = s.Dispatcher.Skip(r.Context(), d.DeviceID, -1)
		}
		if errors.Is(err, takeover.ErrNoQueue) || errors.Is(err, takeover.ErrNoSong) {
			writeError(w, http.StatusConflict, "no_song", err.Error())
			return
		}
	default:
		if req.Action == takeover.ControlStop && s.Dispatcher != nil {
			s.Dispatcher.Halt(d.DeviceID)
		}
		err = s.Engine.Press(r.Context(), d.DeviceID, req.Action)
	}
	if err != nil {
		s.Log.Warn("音箱　没有照做", "音箱", firstNonEmpty(d.Alias, d.Name), "操作", req.Action, "原因", takeover.Describe(err))
		if errors.Is(err, xiaomi.ErrCannotWake) {
			writeError(w, http.StatusConflict, "cannot_wake", "这个型号不支持唤醒")
			return
		}
		writeError(w, http.StatusBadGateway, "not_done", takeover.Describe(err))
		return
	}
	s.Home.Look(d.DeviceID)
	w.WriteHeader(http.StatusNoContent)
}

// controlState reads what the buttons need to know: playing, paused or idle, and the volume.
func (s *Server) controlState(w http.ResponseWriter, r *http.Request) {
	d, err := s.Store.Device(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no_device", "没有这台音箱")
		return
	}
	if !d.InAccount || d.Mode == store.ModeOff {
		writeError(w, http.StatusConflict, "device_off", "这台音箱没有接入")
		return
	}
	p, err := s.Xiaomi.PlayStatus(r.Context(), d.DeviceID)
	if err != nil {
		s.Log.Warn("音箱　读不到状态", "音箱", firstNonEmpty(d.Alias, d.Name), "原因", takeover.Describe(err))
		writeError(w, http.StatusBadGateway, "no_status", takeover.Describe(err))
		return
	}
	out := struct {
		State  string          `json:"state"`
		Volume *int            `json:"volume"`
		Mode   string          `json:"mode"`
		Queue  *takeover.Queue `json:"queue"` // null: nothing to skip
	}{State: home.Idle}
	if s.Dispatcher != nil {
		out.Mode, out.Queue = s.Dispatcher.PlayMode(r.Context(), d.DeviceID), s.Dispatcher.Queue(d.DeviceID)
	}
	switch p.Status {
	case xiaomi.PlayPlaying:
		out.State = home.Playing
	case xiaomi.PlayPaused:
		out.State = home.Paused
	}
	if p.Volume >= 0 {
		out.Volume = &p.Volume
	}
	if v, ok, err := s.Store.PendingVolume(r.Context(), d.DeviceID); err == nil && ok {
		out.Volume = &v
	}
	writeJSON(w, http.StatusOK, out)
}
