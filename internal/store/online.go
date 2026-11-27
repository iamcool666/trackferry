package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// maxOnlineSongs bounds the online songs remembered; the ones seen longest ago go first.
const maxOnlineSongs = 50_000

// RememberOnline keeps online songs (key → what identifies it, as JSON) as just seen.
func (s *Store) RememberOnline(ctx context.Context, songs map[string]string) error {
	if len(songs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	for k, v := range songs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO online_songs(key, song, seen_ms) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET song = excluded.song, seen_ms = excluded.seen_ms`, k, v, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM online_songs WHERE key IN (
		SELECT key FROM online_songs ORDER BY seen_ms DESC LIMIT -1 OFFSET ?)`, maxOnlineSongs); err != nil {
		return err
	}
	return tx.Commit()
}

// RecallOnline is a remembered online song, as JSON; false when it is not known.
func (s *Store) RecallOnline(ctx context.Context, key string) (string, bool, error) {
	var song string
	err := s.db.QueryRowContext(ctx, `SELECT song FROM online_songs WHERE key = ?`, key).Scan(&song)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE online_songs SET seen_ms = ? WHERE key = ?`, time.Now().UnixMilli(), key)
	return song, true, nil
}
