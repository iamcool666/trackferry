// Package fill finds online what the library's songs lack: album covers in the background,
// lyrics when a song without any is played. Matches are strict (same title, singer, about the
// same length, and for covers the same album), the platforms are asked one at a time and
// paced, and whatever was looked for is not looked for again for a long while.
package fill

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"trackferry/internal/library"
	"trackferry/internal/names"
	"trackferry/internal/online"
	"trackferry/internal/store"
)

const (
	settingOn   = "library.fill" // "off" turns it off
	coverAgain  = 30 * 24 * time.Hour
	lyricsAgain = 7 * 24 * time.Hour
	between     = 2 * time.Second // between albums, on top of each platform's own pace
	every       = 6 * time.Hour
)

// Filler does the finding.
type Filler struct {
	Library *library.Library
	Store   *store.Store
	Online  *online.Service
	Log     *slog.Logger

	poke chan struct{}
	mu   sync.Mutex
	st   Status
}

// Status is how far the covers are.
type Status struct {
	On      bool  `json:"on"`
	Running bool  `json:"running"`
	Left    int   `json:"left"`  // albums and songs still to look for in this pass
	Asked   int   `json:"asked"` // looked for in this pass
	Found   int   `json:"found"` // songs given a cover in this pass
	LastMS  int64 `json:"last_ms,omitempty"`
	Missing int   `json:"missing"` // songs with no cover at all
}

// New makes a Filler.
func New(l *library.Library, st *store.Store, o *online.Service, log *slog.Logger) *Filler {
	return &Filler{Library: l, Store: st, Online: o, Log: log, poke: make(chan struct{}, 1)}
}

// On says whether finding is on. It is on unless turned off.
func (f *Filler) On(ctx context.Context) bool {
	v, _ := f.Store.Setting(ctx, settingOn)
	return v != "off"
}

// SetOn turns finding on or off; on starts a pass.
func (f *Filler) SetOn(ctx context.Context, on bool) error {
	v := "on"
	if !on {
		v = "off"
	}
	if err := f.Store.SetSetting(ctx, settingOn, v); err != nil {
		return err
	}
	if on {
		f.Poke()
	}
	return nil
}

// Poke starts a pass soon, as after a scan.
func (f *Filler) Poke() {
	select {
	case f.poke <- struct{}{}:
	default:
	}
}

// Status reports the covers.
func (f *Filler) Status(ctx context.Context) Status {
	f.mu.Lock()
	s := f.st
	f.mu.Unlock()
	s.On = f.On(ctx)
	for _, g := range f.Library.Coverless() {
		s.Missing += len(g)
	}
	return s
}

// Run looks for covers now and then until ctx ends.
func (f *Filler) Run(ctx context.Context) {
	t := time.NewTimer(2 * time.Minute) // let the library load and scan first
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-f.poke:
		}
		if f.On(ctx) {
			f.pass(ctx)
		}
		t.Reset(every)
	}
}

func (f *Filler) set(fn func(*Status)) {
	f.mu.Lock()
	fn(&f.st)
	f.mu.Unlock()
}

func (f *Filler) pass(ctx context.Context) {
	tried, err := f.Store.CoversTried(ctx)
	if err != nil {
		return
	}
	var todo [][]store.TrackRow
	for _, g := range f.Library.Coverless() {
		if at, ok := tried[g[0].ID]; ok && time.Since(at) < coverAgain {
			continue
		}
		todo = append(todo, g)
	}
	f.set(func(s *Status) { *s = Status{Running: true, Left: len(todo)} })
	defer f.set(func(s *Status) { s.Running, s.LastMS = false, time.Now().UnixMilli() })
	if len(todo) == 0 {
		return
	}
	found, since := 0, time.Now()
	reload := func() {
		if found > 0 {
			_ = f.Library.Load(ctx)
		}
		found, since = 0, time.Now()
	}
	defer reload()
	for _, g := range todo {
		if ctx.Err() != nil || !f.On(ctx) {
			return
		}
		cover := f.cover(ctx, g[0])
		if ctx.Err() != nil {
			return
		}
		for _, t := range g {
			if err := f.Store.CoverTried(ctx, t.ID, cover); err != nil {
				return
			}
		}
		f.set(func(s *Status) {
			s.Left--
			s.Asked++
			if cover != "" {
				s.Found += len(g)
			}
		})
		if cover != "" {
			found += len(g)
		}
		// New covers show in the library a few minutes apart, not one reload per album.
		if found >= 200 || found > 0 && time.Since(since) > 3*time.Minute {
			reload()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(between):
		}
	}
}

// cover is a platform's picture for the song's album, or "".
func (f *Filler) cover(ctx context.Context, t store.TrackRow) string {
	album := names.Norm(t.Album)
	c, ok := f.Online.Lookup(ctx, t.Title, t.Artist, t.DurationMS, nil, func(c online.Candidate) bool {
		return c.Cover != "" && (album == "" || names.Norm(c.Album) == album)
	})
	if !ok {
		return ""
	}
	return c.Cover
}

// Lyrics finds lyrics for a library song that has none of its own: kept ones, or asked for
// now. from is the platform. "" when there are none.
func (f *Filler) Lyrics(ctx context.Context, t store.TrackRow) (text, from string) {
	kept, err := f.Store.TrackFill(ctx, t.ID)
	if err != nil {
		return "", ""
	}
	if kept.Lyrics != "" || !kept.LyricsTried.IsZero() && time.Since(kept.LyricsTried) < lyricsAgain || !f.On(ctx) {
		return kept.Lyrics, kept.LyricsFrom
	}
	// Platform by platform: one that has the song may still have no lyrics for it.
	asked := map[string]bool{}
	for range 4 {
		c, ok := f.Online.Lookup(ctx, t.Title, t.Artist, t.DurationMS, func(s string) bool { return asked[s] }, func(online.Candidate) bool { return true })
		if !ok {
			break
		}
		asked[c.Source] = true
		if text, _ = f.Online.LyricsOf(ctx, c); text != "" {
			from = c.Source
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() != nil && text == "" {
		return "", "" // ran out of time: asked again next play, not kept as a miss
	}
	_ = f.Store.LyricsTried(context.WithoutCancel(ctx), t.ID, text, from)
	return text, from
}
