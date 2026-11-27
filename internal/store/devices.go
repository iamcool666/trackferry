package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Device modes.
const (
	ModeOff      = "off"
	ModeObserve  = "observe"
	ModeTakeover = "takeover"
)

// Device is a speaker known to TrackFerry.
type Device struct {
	DeviceID    string    `json:"device_id"`
	Name        string    `json:"name"`
	Alias       string    `json:"alias"`
	Model       string    `json:"model"`
	Hardware    string    `json:"hardware"`
	MiotDID     string    `json:"miot_did"`
	Presence    string    `json:"presence"`
	Mode        string    `json:"mode"`
	Hush        string    `json:"hush"` // "" (up to the model) | normal | strong | wait
	MP3         bool      `json:"mp3"`  // lossless songs reach it as MP3, when ffmpeg is there
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	InAccount   bool      `json:"in_account"`
}

// DiscoveredDevice is what an account device list reports.
type DiscoveredDevice struct {
	DeviceID string
	Name     string
	Alias    string
	Model    string
	Hardware string
	MiotDID  string
	Presence string
}

// SyncResult summarises a device-list sync.
type SyncResult struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Missing int `json:"missing"`
}

// SyncDevices records the account's current device list. New speakers start in mode "off";
// existing ones keep the mode the user chose. Speakers no longer in the account are kept,
// marked in_account = 0, so their history and settings survive.
func (s *Store) SyncDevices(ctx context.Context, found []DiscoveredDevice, now time.Time) (SyncResult, error) {
	var res SyncResult
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	at := ts(now)
	seen := map[string]bool{}
	for _, d := range found {
		if d.DeviceID == "" || seen[d.DeviceID] {
			continue
		}
		seen[d.DeviceID] = true
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE device_id = ?", d.DeviceID).Scan(&exists); err != nil {
			return res, err
		}
		if exists == 0 {
			res.Added++
		} else {
			res.Updated++
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO devices(device_id, name, alias, model, hardware, miot_did, presence, mode, first_seen_at, last_seen_at, in_account)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'off', ?, ?, 1)
			ON CONFLICT(device_id) DO UPDATE SET name = excluded.name, alias = excluded.alias, model = excluded.model,
				hardware = excluded.hardware, miot_did = excluded.miot_did, presence = excluded.presence,
				last_seen_at = excluded.last_seen_at, in_account = 1`,
			d.DeviceID, d.Name, d.Alias, d.Model, d.Hardware, d.MiotDID, d.Presence, at, at)
		if err != nil {
			return res, err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT device_id FROM devices WHERE in_account = 1")
	if err != nil {
		return res, err
	}
	var gone []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return res, err
		}
		if !seen[id] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		if _, err := tx.ExecContext(ctx, "UPDATE devices SET in_account = 0 WHERE device_id = ?", id); err != nil {
			return res, err
		}
	}
	res.Missing = len(gone)
	return res, tx.Commit()
}

// Devices lists all known speakers, those still in the account first.
func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT device_id, name, alias, model, hardware, miot_did, presence, mode, first_seen_at, last_seen_at, in_account, hush, mp3
		FROM devices ORDER BY in_account DESC, name, device_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetDeviceMode changes how TrackFerry treats one speaker.
func (s *Store) SetDeviceMode(ctx context.Context, id, mode string) (Device, error) {
	res, err := s.db.ExecContext(ctx, "UPDATE devices SET mode = ? WHERE device_id = ?", mode, id)
	if err != nil {
		return Device{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Device{}, ErrNotFound
	}
	row := s.rd.QueryRowContext(ctx, `SELECT device_id, name, alias, model, hardware, miot_did, presence, mode, first_seen_at, last_seen_at, in_account, hush, mp3
		FROM devices WHERE device_id = ?`, id)
	d, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// SetDeviceHush changes how a speaker is kept quiet while Xiao AI answers.
func (s *Store) SetDeviceHush(ctx context.Context, id, hush string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE devices SET hush = ? WHERE device_id = ?", hush, id)
	return err
}

// SetDeviceMP3 turns the lossless-to-MP3 conversion on or off for one speaker.
func (s *Store) SetDeviceMP3(ctx context.Context, id string, on bool) error {
	v := 0
	if on {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, "UPDATE devices SET mp3 = ? WHERE device_id = ?", v, id)
	return err
}

type scanner interface{ Scan(...any) error }

func scanDevice(r scanner) (Device, error) {
	var d Device
	var first, last string
	var in, mp3 int
	if err := r.Scan(&d.DeviceID, &d.Name, &d.Alias, &d.Model, &d.Hardware, &d.MiotDID, &d.Presence, &d.Mode, &first, &last, &in, &d.Hush, &mp3); err != nil {
		return d, err
	}
	d.InAccount, d.MP3 = in == 1, mp3 == 1
	d.FirstSeenAt, _ = time.Parse(time.RFC3339Nano, first)
	d.LastSeenAt, _ = time.Parse(time.RFC3339Nano, last)
	return d, nil
}
