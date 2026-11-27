package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const maxOnlineLyrics = 20_000

// OnlineLyrics is what was kept for an online song: lyrics, or "" for a miss, and when.
func (s *Store) OnlineLyrics(ctx context.Context, key string) (text string, at time.Time, ok bool, err error) {
	var ms int64
	err = s.rd.QueryRowContext(ctx, `SELECT lyrics, fetched_ms FROM online_lyrics WHERE key = ?`, key).Scan(&text, &ms)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	return text, time.UnixMilli(ms), err == nil, err
}

// KeepOnlineLyrics stores an online song's lyrics; "" keeps a miss.
func (s *Store) KeepOnlineLyrics(ctx context.Context, key, text string) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO online_lyrics(key, lyrics, fetched_ms) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET lyrics = excluded.lyrics, fetched_ms = excluded.fetched_ms`, key, text, time.Now().UnixMilli()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM online_lyrics WHERE key IN (
		SELECT key FROM online_lyrics ORDER BY fetched_ms DESC LIMIT -1 OFFSET ?)`, maxOnlineLyrics)
	return err
}

// TrackFill is what was looked for online for a library song.
type TrackFill struct {
	CoverTried  time.Time
	Lyrics      string
	LyricsFrom  string // the platform
	LyricsTried time.Time
}

// TrackFill reads a song's fill; the zero value when nothing was looked for.
func (s *Store) TrackFill(ctx context.Context, id int64) (TrackFill, error) {
	var f TrackFill
	var c, l int64
	err := s.rd.QueryRowContext(ctx, `SELECT cover_tried_ms, lyrics, lyrics_from, lyrics_tried_ms FROM track_fills WHERE track_id = ?`, id).Scan(&c, &f.Lyrics, &f.LyricsFrom, &l)
	if errors.Is(err, sql.ErrNoRows) {
		return f, nil
	}
	if c > 0 {
		f.CoverTried = time.UnixMilli(c)
	}
	if l > 0 {
		f.LyricsTried = time.UnixMilli(l)
	}
	return f, err
}

// CoversTried is when each song's cover was last looked for.
func (s *Store) CoversTried(ctx context.Context) (map[int64]time.Time, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT track_id, cover_tried_ms FROM track_fills WHERE cover_tried_ms > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]time.Time{}
	for rows.Next() {
		var id, ms int64
		if err := rows.Scan(&id, &ms); err != nil {
			return nil, err
		}
		out[id] = time.UnixMilli(ms)
	}
	return out, rows.Err()
}

// CoverTried notes a song's cover was looked for; cover, when found, is set on the song as
// well where it has none of its own.
func (s *Store) CoverTried(ctx context.Context, id int64, cover string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO track_fills(track_id, cover_tried_ms) VALUES (?, ?)
		ON CONFLICT(track_id) DO UPDATE SET cover_tried_ms = excluded.cover_tried_ms`, id, time.Now().UnixMilli()); err != nil {
		return err
	}
	if cover != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE tracks SET cover = ? WHERE id = ? AND cover = ''`, cover, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LyricsTried keeps lyrics found for a library song; "" keeps a miss.
func (s *Store) LyricsTried(ctx context.Context, id int64, text, from string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO track_fills(track_id, lyrics, lyrics_from, lyrics_tried_ms) VALUES (?, ?, ?, ?)
		ON CONFLICT(track_id) DO UPDATE SET lyrics = excluded.lyrics, lyrics_from = excluded.lyrics_from, lyrics_tried_ms = excluded.lyrics_tried_ms`,
		id, text, from, time.Now().UnixMilli())
	return err
}
