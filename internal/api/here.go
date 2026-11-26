package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"trackferry/internal/online"
	"trackferry/internal/routing"
	"trackferry/internal/stream"
)

// Songs heard in the browser that shows the page.

// heardFor is how long the address of an online song is used again. A browser asks for a song
// in several pieces.
const heardFor = 20 * time.Minute

type here struct {
	mu    sync.Mutex
	found map[string]*heard
}

type heard struct {
	once  sync.Once
	at    time.Time
	open  stream.Opener
	label string
	err   error
}

// take finds an online song, once for all the pieces of it.
func (h *here) take(key string, find func() (stream.Opener, string, error)) *heard {
	h.mu.Lock()
	for k, x := range h.found {
		if time.Since(x.at) > heardFor {
			delete(h.found, k)
		}
	}
	x := h.found[key]
	if x == nil {
		x = &heard{at: time.Now()}
		if h.found == nil {
			h.found = map[string]*heard{}
		}
		h.found[key] = x
	}
	h.mu.Unlock()
	x.once.Do(func() { x.open, x.label, x.err = find() })
	if x.err != nil {
		h.drop(key, x)
	}
	return x
}

func (h *here) drop(key string, x *heard) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.found[key] == x {
		delete(h.found, key)
	}
}

// hereAudio sends a song to the browser: ?track= one of the library, ?online= one a search has
// shown. index=1 gives a browser that cannot find its way in FLAC one that says where what is.
func (s *Server) hereAudio(w http.ResponseWriter, r *http.Request) {
	if key := r.URL.Query().Get("online"); key != "" {
		if len(key) > 200 || s.Dispatcher == nil || s.Dispatcher.Open == nil {
			writeError(w, http.StatusNotFound, "no_audio", "这首放不了")
			return
		}
		x := s.here.take(key, func() (stream.Opener, string, error) {
			// Others may be waiting for it too, so it does not end with this request.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
			defer cancel()
			f, err := s.Router.Online.Take(ctx, key)
			if errors.Is(err, online.ErrGone) {
				title, artist := r.URL.Query().Get("title"), r.URL.Query().Get("artist")
				q := strings.TrimSpace(title + " " + artist)
				if q != "" && len([]rune(q)) <= 200 {
					offered, _ := s.Router.Online.Search(ctx, q)
					for _, c := range offered {
						if c.Key == key {
							f, err = s.Router.Online.Take(ctx, key)
							break
						}
					}
				}
			}
			if err != nil {
				return nil, "", err
			}
			rt := routing.FromOnline(f)
			return s.Dispatcher.Open(rt), rt.Label(), nil
		})
		switch {
		case errors.Is(x.err, online.ErrGone):
			writeError(w, http.StatusNotFound, "gone", x.err.Error())
		case x.err != nil:
			writeError(w, http.StatusBadGateway, "no_audio", "这首放不了："+x.err.Error())
		default:
			s.Stream.ProxyHere(w, r, func(ctx context.Context, rng string) (*http.Response, error) {
				resp, err := x.open(ctx, rng)
				if err != nil && ctx.Err() == nil { // addresses do not last: the next request finds a new one
					s.here.drop(key, x)
				}
				return resp, err
			}, x.label)
		}
		return
	}
	id, _ := strconv.ParseInt(r.URL.Query().Get("track"), 10, 64)
	t, ok := s.Library.Track(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no_track", "曲库里没有这首歌了")
		return
	}
	if err := s.Stream.SongHere(w, r, t.Path, strings.Trim(t.Artist+" - "+t.Title, " -"), r.URL.Query().Get("index") == "1"); err != nil {
		writeError(w, http.StatusNotFound, "no_audio", "这首歌暂时读不到")
	}
}

// hereList lists the songs of something to play through in the browser.
func (s *Server) hereList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var list string
	var ids []int64
	if q.Get("source") == "random" { // what comes after a song that was put on alone
		list, ids = "接着随机放", s.Library.RandomIDs(30)
	} else {
		var ok bool
		if list, ids, ok = s.collect(w, r, q.Get("source"), q.Get("id"), q.Get("name"), nil); !ok {
			return
		}
	}
	type song struct {
		ID         int64  `json:"id"`
		Title      string `json:"title"`
		Artist     string `json:"artist"`
		Album      string `json:"album,omitempty"`
		Cover      string `json:"cover,omitempty"`
		DurationMS int64  `json:"duration_ms"`
	}
	out := make([]song, 0, len(ids))
	for _, id := range ids {
		if t, ok := s.Library.Track(id); ok {
			out = append(out, song{t.ID, t.Title, t.Artist, t.Album, t.Cover, t.DurationMS})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": list, "songs": out})
}
