package routing

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"trackferry/internal/feiniu"
	"trackferry/internal/intent"
	"trackferry/internal/library"
	"trackferry/internal/names"
	"trackferry/internal/online"
)

// Lists reads the lists of 飞牛音乐 (feiniu.Manager).
type Lists interface {
	Linked() bool
	Favorites(ctx context.Context) ([]feiniu.Song, error)
	Playlists(ctx context.Context) ([]feiniu.Playlist, error)
	PlaylistSongs(ctx context.Context, guid string) ([]feiniu.Song, error)
	PutInPlaylist(ctx context.Context, playlist, guid string, on bool) (bool, error)
}

// listWait bounds the time a list may take; Xiao AI talks meanwhile.
const listWait = 8 * time.Second

// Queue is the songs to play one after another, the chosen one first.
func (r *Route) Queue() []int64 { return r.queue }

// OnlineQueue is the online part appended to a singer's local queue.
func (r *Route) OnlineQueue() []QueueSong { return slices.Clone(r.onlineQueue) }

// Order is the same list as it stands, not shuffled.
func (r *Route) Order() []int64 { return r.order }

// playlist finds a playlist by what was said. The error is for people.
func (r *Router) playlist(ctx context.Context, name string) (feiniu.Playlist, error) {
	all, err := r.Lists.Playlists(ctx)
	if err != nil {
		return feiniu.Playlist{}, fmt.Errorf("没能读到飞牛音乐的歌单：%w", err)
	}
	if len(all) == 0 {
		return feiniu.Playlist{}, errors.New("飞牛音乐里还没有歌单")
	}
	if name == "" {
		if len(all) == 1 {
			return all[0], nil
		}
		return feiniu.Playlist{}, errors.New("没说是哪个歌单，有" + listNames(all))
	}
	want := names.Norm(name)
	for _, same := range []func(string) bool{
		func(s string) bool { return s == want || s == want+"歌单" },
		func(s string) bool { return s != "" && (strings.Contains(s, want) || strings.Contains(want, s)) },
		func(s string) bool { return nearly(s, want) },
	} {
		var hit []feiniu.Playlist
		for _, p := range all {
			if want != "" && same(names.Norm(p.Name)) {
				hit = append(hit, p)
			}
		}
		if len(hit) == 1 {
			return hit[0], nil
		}
		if len(hit) > 1 {
			return feiniu.Playlist{}, errors.New("有几个歌单都像“" + name + "”：" + listNames(hit))
		}
	}
	return feiniu.Playlist{}, errors.New("飞牛音乐里没有叫“" + name + "”的歌单，有" + listNames(all))
}

// nearly reports names of three or more characters with one misheard.
func nearly(a, b string) bool {
	x, y := []rune(a), []rune(b)
	if len(x) != len(y) || len(x) < 3 {
		return false
	}
	n := 0
	for i := range x {
		if x[i] != y[i] {
			n++
		}
	}
	return n == 1
}

func listNames(ps []feiniu.Playlist) string {
	var s []string
	for _, p := range ps[:min(5, len(ps))] {
		s = append(s, p.Name)
	}
	out := strings.Join(s, "、")
	if len(ps) > 5 {
		out += fmt.Sprintf(" 等 %d 个", len(ps))
	}
	return out
}

// list draws a list of 飞牛音乐 in random order.
func (r *Router) list(ctx context.Context, rt *Route, in intent.Intent) {
	start := time.Now()
	l := Layer{Name: "library", State: online.StateMiss, Candidates: []Candidate{}}
	defer func() { l.MS = time.Since(start).Milliseconds(); rt.Layers = append(rt.Layers, l) }()
	if r.Lists == nil || !r.Lists.Linked() {
		rt.Reason = "没有连接飞牛音乐"
		return
	}
	ctx, cancel := context.WithTimeout(ctx, listWait)
	defer cancel()
	name := "我的收藏"
	var ids []int64
	total := 0
	collect := func(songs []feiniu.Song) {
		total = len(songs)
		for _, s := range songs {
			if t, ok := r.Library.ByPath(library.RemotePrefix + s.GUID); ok {
				ids = append(ids, t.ID)
			}
		}
	}
	if in.Control == intent.ListFavorites {
		songs, err := r.Lists.Favorites(ctx)
		collect(songs)
		if err != nil {
			ids = r.Library.FavoriteIDs() // as of the last sync
			if total = len(ids); total == 0 {
				l.State, rt.Reason = online.StateError, "没能读到飞牛音乐的收藏："+err.Error()
				return
			}
		}
	} else {
		p, err := r.playlist(ctx, in.Title)
		if err != nil {
			l.State, rt.Reason = online.StateError, err.Error()
			return
		}
		name = "歌单“" + p.Name + "”"
		songs, err := r.Lists.PlaylistSongs(ctx, p.GUID)
		if err != nil {
			l.State, rt.Reason = online.StateError, "没能读到"+name+"："+err.Error()
			return
		}
		collect(songs)
	}
	switch {
	case total == 0:
		rt.Reason = name + "里没有歌"
		return
	case len(ids) == 0:
		rt.Reason = name + "里的歌还没有同步到曲库"
		return
	}
	rt.order = append([]int64{}, ids...)
	rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	for i, id := range ids[:min(5, len(ids))] {
		m, ok := r.Track(id)
		if !ok {
			continue
		}
		rt.take(m)
		rt.List, rt.queue = name, append(append([]int64{}, ids[i:]...), ids[:i]...)
		l.State, l.Detail = online.StateHit, fmt.Sprintf("%s，%d 首连着放", name, len(ids))
		if n := total - len(ids); n > 0 {
			l.Detail += fmt.Sprintf("（另有 %d 首放不了或还没同步）", n)
		}
		l.Candidates = show([]library.Match{m})
		rt.Because("选它", "%s", l.Detail)
		return
	}
	l.State, l.Detail = online.StateError, "文件暂时读不到"
	rt.Reason = name + "里的歌暂时读不到"
}

// album plays an album of the library; without a title the one that song is from.
func (r *Router) album(rt *Route, in intent.Intent, song int64) {
	start := time.Now()
	l := Layer{Name: "library", State: online.StateMiss, Candidates: []Candidate{}}
	defer func() { l.MS = time.Since(start).Milliseconds(); rt.Layers = append(rt.Layers, l) }()
	var a library.Album
	if in.Title == "" {
		t, ok := r.Library.Track(song)
		switch {
		case !ok:
			rt.Reason = "不知道说的是哪张专辑"
			return
		case t.AlbumID == "":
			rt.Reason = "《" + t.Title + "》没有专辑信息"
			return
		}
		a.ID = t.AlbumID
	} else {
		as := r.Library.FindAlbums(in.Title, in.Artist)
		if len(as) == 0 {
			rt.Reason = "曲库里没有专辑“" + strings.TrimSpace(in.Artist+" "+in.Title) + "”"
			return
		}
		a = as[0]
	}
	a, ts := r.Library.AlbumTracks(a.ID)
	var ids []int64
	first := -1
	var m library.Match
	for _, t := range ts {
		if first < 0 {
			var ok bool
			if m, ok = r.Track(t.ID); ok {
				first = len(ids)
			}
		}
		ids = append(ids, t.ID)
	}
	if first < 0 {
		l.State, l.Detail, rt.Reason = online.StateError, "文件暂时读不到", "专辑“"+a.Name+"”里的歌暂时读不到"
		return
	}
	rt.take(m)
	rt.List, rt.order = "专辑“"+a.Name+"”", ids
	rt.queue = append(append([]int64{}, ids[first:]...), ids[:first]...)
	l.State, l.Detail = online.StateHit, fmt.Sprintf("%s，%d 首连着放", rt.List, len(ids))
	l.Candidates = show([]library.Match{m})
	rt.Because("选它", "%s", l.Detail)
}

// PutIn adds a song of the library to a playlist picked by hand.
func (r *Router) PutIn(ctx context.Context, id int64, guid string) (changed bool, err error) {
	if r.Lists == nil || !r.Lists.Linked() {
		return false, errors.New("没有连接飞牛音乐")
	}
	t, ok := r.Library.Track(id)
	if !ok || t.GUID == "" {
		return false, library.ErrNotRemote
	}
	return r.Lists.PutInPlaylist(ctx, guid, t.GUID, true)
}

// Track is a song of the library, if its file can be read right now.
func (r *Router) Track(id int64) (library.Match, bool) {
	t, ok := r.Library.Track(id)
	m := library.Match{TrackRow: t}
	return m, ok && r.openable(m)
}

// Next is the route of a later song of a list.
func (r *Router) Next(id int64, list string) (*Route, bool) {
	m, ok := r.Track(id)
	if !ok {
		return nil, false
	}
	rt := &Route{Layers: []Layer{{Name: "library", State: online.StateHit, Detail: list, Candidates: show([]library.Match{m})}}}
	rt.take(m)
	rt.List = list
	return rt, true
}

// Put adds a song of the library to a playlist of 飞牛音乐, or takes it out. changed is false
// when it was that way already; list is the playlist's own name.
func (r *Router) Put(ctx context.Context, id int64, name string, on bool) (list string, changed bool, err error) {
	if r.Lists == nil || !r.Lists.Linked() {
		return "", false, errors.New("没有连接飞牛音乐")
	}
	t, ok := r.Library.Track(id)
	if !ok || t.GUID == "" {
		return "", false, library.ErrNotRemote
	}
	p, err := r.playlist(ctx, name)
	if err != nil {
		return "", false, err
	}
	changed, err = r.Lists.PutInPlaylist(ctx, p.GUID, t.GUID, on)
	return p.Name, changed, err
}

// shuffled puts ids in random order, first at the beginning.
func shuffled(ids []int64, first int64) []int64 {
	out := []int64{first}
	for _, id := range ids {
		if id != first {
			out = append(out, id)
		}
	}
	rand.Shuffle(len(out)-1, func(i, j int) { out[i+1], out[j+1] = out[j+1], out[i+1] })
	return out
}

// Collect lists the songs of a playlist of 飞牛音乐 that are in the library.
func (r *Router) Collect(ctx context.Context, guid string) (string, []int64, error) {
	if r.Lists == nil || !r.Lists.Linked() {
		return "", nil, errors.New("没有连接飞牛音乐")
	}
	all, err := r.Lists.Playlists(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("没能读到飞牛音乐的歌单：%w", err)
	}
	for _, p := range all {
		if p.GUID != guid {
			continue
		}
		songs, err := r.Lists.PlaylistSongs(ctx, guid)
		if err != nil {
			return "", nil, fmt.Errorf("没能读到歌单“%s”：%w", p.Name, err)
		}
		ids := []int64{}
		for _, s := range songs {
			if t, ok := r.Library.ByPath(library.RemotePrefix + s.GUID); ok {
				ids = append(ids, t.ID)
			}
		}
		return p.Name, ids, nil
	}
	return "", nil, errors.New("没有这个歌单了")
}

// FromOnline is the route of an online song picked by hand.
func FromOnline(f *online.Found) *Route {
	c := f.Candidate
	rt := &Route{Layers: []Layer{{Name: c.Source, State: online.StateHit, Candidates: []Candidate{}}},
		asked: askedKey(intent.Intent{Title: c.Title, Artist: c.Artist}, c.Title)}
	rt.takeOnline(f)
	return rt
}
