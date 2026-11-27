package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// AdminCredential is the stored admin password hash.
type AdminCredential struct {
	Hash       []byte
	Salt       []byte
	Iterations int
}

// AdminCredential returns the admin password hash, or ErrNotFound before setup.
func (s *Store) AdminCredential(ctx context.Context) (AdminCredential, error) {
	var c AdminCredential
	err := s.rd.QueryRowContext(ctx, "SELECT password_hash, salt, iterations FROM admin WHERE id = 1").
		Scan(&c.Hash, &c.Salt, &c.Iterations)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// SetAdminCredential stores the admin password hash. When replace is false it fails if one
// already exists, so two concurrent first-run setups cannot both win.
func (s *Store) SetAdminCredential(ctx context.Context, c AdminCredential, replace bool) error {
	now := ts(time.Now())
	q := "INSERT INTO admin(id, password_hash, salt, iterations, created_at, updated_at) VALUES (1, ?, ?, ?, ?, ?)"
	if replace {
		q += " ON CONFLICT(id) DO UPDATE SET password_hash = excluded.password_hash, salt = excluded.salt, iterations = excluded.iterations, updated_at = excluded.updated_at"
	}
	_, err := s.db.ExecContext(ctx, q, c.Hash, c.Salt, c.Iterations, now, now)
	return err
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// CreateSession stores a session for token until expires. name is what the browser is called;
// when another live session already goes by it, a number is added: "iPhone 2".
func (s *Store) CreateSession(ctx context.Context, token string, expires time.Time, name string) error {
	name = s.FreeName(ctx, name)
	_, err := s.db.ExecContext(ctx, "INSERT INTO sessions(token_hash, created_at, expires_at, name) VALUES (?, ?, ?, ?)",
		hashToken(token), ts(time.Now()), ts(expires), name)
	return err
}

// FreeName is name, or name with a number when a signed-in browser already goes by it.
func (s *Store) FreeName(ctx context.Context, name string) string {
	if name == "" {
		return ""
	}
	taken := map[string]bool{}
	rows, err := s.rd.QueryContext(ctx, "SELECT name FROM sessions WHERE expires_at >= ?", ts(time.Now()))
	if err != nil {
		return name
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			taken[n] = true
		}
	}
	out := name
	for i := 2; taken[out]; i++ {
		out = fmt.Sprintf("%s %d", name, i)
	}
	return out
}

// SessionName is what the browser holding token is called; "" when it is not signed in.
func (s *Store) SessionName(ctx context.Context, token string) (string, error) {
	var name string
	err := s.rd.QueryRowContext(ctx, "SELECT name FROM sessions WHERE token_hash = ?", hashToken(token)).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// RenameSession changes what the browser holding token is called.
func (s *Store) RenameSession(ctx context.Context, token, name string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sessions SET name = ? WHERE token_hash = ?", name, hashToken(token))
	return err
}

// SessionValid reports whether token names an unexpired session.
func (s *Store) SessionValid(ctx context.Context, token string, now time.Time) (bool, error) {
	var exp string
	err := s.rd.QueryRowContext(ctx, "SELECT expires_at FROM sessions WHERE token_hash = ?", hashToken(token)).Scan(&exp)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	t, err := time.Parse(time.RFC3339Nano, exp)
	return err == nil && now.Before(t), err
}

// DeleteSession removes one session.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", hashToken(token))
	return err
}

// DeleteAllSessions signs every browser out, e.g. after a password change.
func (s *Store) DeleteAllSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions")
	return err
}

// PruneSessions removes expired sessions.
func (s *Store) PruneSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at < ?", ts(now))
	return err
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// ResetAdmin removes the admin password and every session, returning the install to its
// first-run state. Speakers and the Xiaomi link are untouched.
func (s *Store) ResetAdmin(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM admin"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions"); err != nil {
		return err
	}
	return tx.Commit()
}
