package store

import (
	"context"
	"database/sql"
	"errors"
)

type TVDevice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PairedAt string `json:"paired_at"`
}

func (s *Store) AddTV(ctx context.Context, id, model, pairedAt string, hash []byte) (TVDevice, error) {
	d := TVDevice{ID: id, PairedAt: pairedAt}
	// At least one of the first N+1 default names is free among N devices.
	err := s.db.QueryRowContext(ctx, `
		WITH RECURSIVE numbers(n) AS (
			SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n <= (SELECT count(*) FROM tv_devices)
		), candidates AS (
			SELECT n, CASE WHEN n = 1 THEN '电视' ELSE '电视 ' || n END AS name FROM numbers
		)
		INSERT INTO tv_devices(id, name, model, token_hash, paired_at)
		SELECT ?, name, ?, ?, ? FROM candidates
		WHERE NOT EXISTS (SELECT 1 FROM tv_devices WHERE tv_devices.name = candidates.name)
		ORDER BY n LIMIT 1 RETURNING name`, id, model, hash, pairedAt).Scan(&d.Name)
	return d, err
}

func (s *Store) RenameTV(ctx context.Context, id, name string) (TVDevice, error) {
	d := TVDevice{ID: id, Name: name}
	err := s.db.QueryRowContext(ctx, `UPDATE tv_devices SET name = ? WHERE id = ? RETURNING paired_at`, name, id).Scan(&d.PairedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

func (s *Store) TVs(ctx context.Context) ([]TVDevice, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT id, name, paired_at FROM tv_devices ORDER BY paired_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := []TVDevice{}
	for rows.Next() {
		var d TVDevice
		if err := rows.Scan(&d.ID, &d.Name, &d.PairedAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (s *Store) TVAuthorized(ctx context.Context, hash []byte) (bool, error) {
	var yes bool
	err := s.rd.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tv_devices WHERE token_hash = ?)`, hash).Scan(&yes)
	return yes, err
}

func (s *Store) RevokeTV(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tv_devices WHERE id = ?`, id)
	return err
}

func (s *Store) TVByToken(ctx context.Context, hash []byte) (string, error) {
	var id string
	err := s.rd.QueryRowContext(ctx, `SELECT id FROM tv_devices WHERE token_hash = ?`, hash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
