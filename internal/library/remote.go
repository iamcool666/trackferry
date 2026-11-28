package library

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"trackferry/internal/feiniu"
	"trackferry/internal/store"
)

// Remote is a library that lists its own songs (feiniu.Manager).
type Remote interface {
	Linked() bool
	Songs(ctx context.Context, page, size int) ([]feiniu.Song, int, error)
}

// RemotePrefix marks the Path of a song that 飞牛音乐 holds.
const RemotePrefix = "fn:"

const pageSize = 200

// sync mirrors 飞牛音乐's index. Nothing is removed unless every page was read.
func (l *Library) sync(ctx context.Context, id int64, rep *Report, known map[string]store.TrackRow) error {
	l.mu.Lock()
	rep.Source = "feiniu"
	l.mu.Unlock()
	roots := map[string]int{}
	got, total := 0, 0
	seen := make(map[string]bool, len(known))
	for page := 1; ; page++ {
		songs, n, err := l.Remote.Songs(ctx, page, pageSize)
		if err != nil {
			return err
		}
		var changed []store.TrackRow
		l.mu.Lock()
		total, rep.Total = n, n
		for _, s := range songs {
			got++
			l.seen++
			t, ok := remoteRow(s)
			if pathFormat := strings.TrimPrefix(strings.ToLower(path.Ext(s.Audio.Path)), "."); pathFormat != "" && pathFormat != t.Format && ok && l.log != nil {
				l.log.Warn("飞牛曲库　音频格式字段不一致",
					"歌曲", s.Title,
					"Path扩展名", pathFormat,
					"Format", s.Audio.Format,
					"Container", s.Audio.Container,
					"Codec", s.Audio.Codec,
					"TrackFerry识别格式", t.Format,
					"采样率", s.Audio.SampleRate,
					"位深", s.Audio.Bits)
			}
			if !ok {
				rep.Skipped++
				continue
			}
			i, ok := roots[t.Root]
			if !ok {
				i = len(rep.Roots)
				roots[t.Root] = i
				rep.Roots = append(rep.Roots, RootReport{Name: t.Root, Samples: []string{}})
			}
			rep.Roots[i].Tracks++
			old := known[t.Path]
			t.ID = old.ID
			if t.Cover == "" && strings.HasPrefix(old.Cover, NetCover) {
				t.Cover = old.Cover // found online; 飞牛 has none
			}
			seen[t.Path] = true
			if old.ID == 0 || old != t {
				changed = append(changed, t)
				rep.Added++
			}
		}
		l.mu.Unlock()
		if len(changed) > 0 {
			if err := l.st.SaveTracks(ctx, id, changed, nil); err != nil {
				return err
			}
		}
		if len(songs) < pageSize || got >= total {
			break
		}
	}
	if got < total {
		return fmt.Errorf("飞牛音乐说有 %d 首，只读到 %d 首", total, got)
	}
	// LX playlists are separate from /track/list. Read them during the same sync,
	// but keep their counts separate from the playable local-library index.
	if l.OnlinePlaylistStats != nil {
		playlists, tracks, statsErr := l.OnlinePlaylistStats(ctx)
		l.mu.Lock()
		rep.OnlinePlaylists, rep.OnlineTracks = playlists, tracks
		if statsErr != nil {
			rep.OnlineError = statsErr.Error()
		}
		l.mu.Unlock()
	}
	var n int64
	var gone []int64
	for p, t := range known {
		if !seen[p] {
			gone = append(gone, t.ID)
		}
	}
	for part := range slices.Chunk(gone, 500) {
		if err := l.st.DropTracks(ctx, part); err != nil {
			return err
		}
		n += int64(len(part))
	}
	l.mu.Lock()
	rep.Removed = n
	same := rep.Added == 0 && n == 0
	l.mu.Unlock()
	if same {
		return nil // the index in memory is this one
	}
	return l.Load(ctx)
}

// remoteRow converts a song; ok is false for what speakers cannot be given on its own.
func remoteRow(s feiniu.Song) (store.TrackRow, bool) {
	format := formats[strings.ToLower(path.Ext(s.Audio.Path))]
	if format == "" {
		format = formats["."+strings.ToLower(s.Audio.Format)]
	}
	if format == "" {
		format = formats["."+strings.ToLower(s.Audio.Container)]
	}
	if format == "" || s.Cue || s.GUID == "" || strings.TrimSpace(s.Title) == "" {
		return store.TrackRow{}, false
	}
	if strings.Contains(strings.ToLower(s.Audio.Codec), "dts") {
		return store.TrackRow{}, false
	}
	var as []string
	for _, a := range s.Artists {
		if a.Name != "" {
			as = append(as, a.Name)
		}
	}
	t := store.TrackRow{Path: RemotePrefix + s.GUID, GUID: s.GUID, Root: libraryOf(s.Audio.Path), Size: s.Audio.Size, Title: s.Title,
		Artist: strings.Join(as, "/"), Cover: s.CoverID, DurationMS: int64(s.Duration), Format: format,
		SampleRate: s.Audio.SampleRate, Bits: s.Audio.Bits, Tagged: true, Favorite: s.Favorite, AddedMS: s.Created * 1000}
	if strings.EqualFold(s.Audio.Codec, "alac") {
		t.Format = "alac"
	}
	if s.Album != nil {
		t.Album, t.AlbumID = s.Album.Name, s.Album.GUID
		if t.Cover == "" {
			t.Cover = s.Album.CoverID
		}
	}
	return t, true
}

// libraryOf names the shared folder a song lies in: "/vol1/1000/音乐/…" is 音乐.
func libraryOf(p string) string {
	f := strings.Split(strings.Trim(p, "/"), "/")
	if len(f) > 3 && strings.HasPrefix(f[0], "vol") {
		return f[2]
	}
	return "飞牛音乐"
}
