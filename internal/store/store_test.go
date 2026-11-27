package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenMigratesAndPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "trackferry.db")

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.RecordBoot(ctx, "v1", "abc", time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(ctx, path) // migrations must be idempotent
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.RecordBoot(ctx, "v2", "def", time.Now()); err != nil {
		t.Fatal(err)
	}
	n, err := s.BootCount(ctx)
	if err != nil || n != 2 {
		t.Fatalf("boot count = %d, %v; want 2", n, err)
	}
	got, err := s.FirstBoot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != first.ID || !got.StartedAt.Equal(first.StartedAt) || got.Commit != "abc" {
		t.Fatalf("first boot = %+v, want %+v", got, first)
	}
	v, err := s.SchemaVersion(ctx)
	if err != nil || v != 12 {
		t.Fatalf("schema version = %d, %v", v, err)
	}
}

func TestWALEnabled(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q", mode)
	}
}

func TestMigrationsWellFormed(t *testing.T) {
	migs, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range migs {
		if m.version != i+1 {
			t.Fatalf("migration %s: version %d, want contiguous %d", m.name, m.version, i+1)
		}
	}
}

func TestSyncDevicesKeepsModeAndMarksMissing(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	res, err := s.SyncDevices(ctx, []DiscoveredDevice{{DeviceID: "a", Name: "书房"}, {DeviceID: "b", Name: "客厅"}}, now)
	if err != nil || res.Added != 2 {
		t.Fatalf("first sync %+v %v", res, err)
	}
	if _, err := s.SetDeviceMode(ctx, "a", ModeObserve); err != nil {
		t.Fatal(err)
	}
	res, err = s.SyncDevices(ctx, []DiscoveredDevice{{DeviceID: "a", Name: "书房音箱"}}, now.Add(time.Hour))
	if err != nil || res.Updated != 1 || res.Missing != 1 {
		t.Fatalf("second sync %+v %v", res, err)
	}
	devs, err := s.Devices(ctx)
	if err != nil || len(devs) != 2 {
		t.Fatalf("devices %v %v", devs, err)
	}
	if devs[0].DeviceID != "a" || devs[0].Mode != ModeObserve || devs[0].Name != "书房音箱" || !devs[0].InAccount {
		t.Fatalf("a = %+v", devs[0])
	}
	if devs[1].DeviceID != "b" || devs[1].InAccount || devs[1].Mode != ModeOff {
		t.Fatalf("b = %+v", devs[1])
	}
	if _, err := s.SetDeviceMode(ctx, "zzz", ModeOff); err != ErrNotFound {
		t.Fatalf("missing device: %v", err)
	}
}

func TestCommandsDedupAndNote(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	if _, err := s.SyncDevices(ctx, []DiscoveredDevice{{DeviceID: "d1", Name: "LX06", Alias: "书房"}}, now); err != nil {
		t.Fatal(err)
	}
	c := Command{DeviceID: "d1", RequestKey: "r1", Query: "播放晴天", AnswerTypes: []string{"TTS", "AUDIO"}, UpstreamMS: 1, PrevPollMS: 2, SeenMS: now.UnixMilli(),
		IntentKind: "song", IntentTitle: "晴天", Stage: StageObserved}
	if ok, err := s.AddCommand(ctx, &c); !ok || err != nil || c.ID == 0 {
		t.Fatalf("add: %v %v", ok, err)
	}
	dup := c
	if ok, err := s.AddCommand(ctx, &dup); ok || err != nil {
		t.Fatalf("duplicate stored: %v %v", ok, err)
	}
	got, err := s.Commands(ctx, "", 0, 10)
	if err != nil || len(got) != 1 || got[0].DeviceName != "书房" || len(got[0].AnswerTypes) != 2 {
		t.Fatalf("list: %+v %v", got, err)
	}
	n, err := s.SetNativeNote(ctx, c.ID, "playing")
	if err != nil || n.NativeNote != "playing" {
		t.Fatalf("note: %+v %v", n, err)
	}
	if _, err := s.SetNativeNote(ctx, 999, "spoke"); err != ErrNotFound {
		t.Fatalf("missing: %v", err)
	}
	if n, err := s.PruneCommands(ctx, now.Add(time.Hour)); n != 1 || err != nil {
		t.Fatalf("prune: %d %v", n, err)
	}
}

func TestTakeoverRunsAndPendingVolume(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	s.SyncDevices(ctx, []DiscoveredDevice{{DeviceID: "d1", Name: "LX06", Alias: "书房"}}, now)
	r := TakeoverRun{DeviceID: "d1", Kind: "test", Source: "提示音", StartedMS: now.UnixMilli()}
	if err := s.StartRun(ctx, &r); err != nil || r.ID == 0 {
		t.Fatal(r.ID, err)
	}
	r.Steps = []Step{{Step: "stop", AtMS: 120, OK: true}}
	r.Outcome, r.FinishedMS = OutcomeConfirmed, now.UnixMilli()+3000
	if err := s.SaveRun(ctx, &r); err != nil {
		t.Fatal(err)
	}
	r2 := TakeoverRun{DeviceID: "d1", Kind: "test", Source: "x", StartedMS: now.UnixMilli()}
	s.StartRun(ctx, &r2)
	if err := s.FailRunningRuns(ctx, now); err != nil {
		t.Fatal(err)
	}
	runs, err := s.Runs(ctx, 10)
	if err != nil || len(runs) != 2 {
		t.Fatal(runs, err)
	}
	if runs[0].Outcome != OutcomeFailed || runs[1].Outcome != OutcomeConfirmed || runs[1].DeviceName != "书房" || len(runs[1].Steps) != 1 {
		t.Fatalf("%+v", runs)
	}

	s.SetPendingVolume(ctx, "d1", 40, now)
	s.SetPendingVolume(ctx, "d1", 0, now) // must not overwrite the real volume
	if v, ok, err := s.PendingVolume(ctx, "d1"); v != 40 || !ok || err != nil {
		t.Fatal(v, ok, err)
	}
	s.ClearPendingVolume(ctx, "d1")
	if all, _ := s.PendingVolumes(ctx); len(all) != 0 {
		t.Fatal(all)
	}
}

func TestReadsDoNotWaitForAWrite(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tx, _ := s.db.BeginTx(ctx, nil)
	defer tx.Rollback()
	tx.ExecContext(ctx, "INSERT INTO settings(key, value) VALUES ('a', 'b')")
	c, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := s.Devices(c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.rd.ExecContext(c, "DELETE FROM settings"); err == nil {
		t.Fatal("the reading side can write")
	}
}

func TestPruneKeepsTheFirstStartAndTheNewest(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for range 520 {
		s.RecordBoot(ctx, "v", "c", time.Now())
	}
	for range 60 {
		s.StartScan(ctx, time.Now())
	}
	if n, err := s.Prune(ctx, time.Now()); err != nil || n != 29 {
		t.Fatal(n, err)
	}
	if b, err := s.FirstBoot(ctx); err != nil || b.ID != 1 {
		t.Fatal(b, err)
	}
	if n, _ := s.BootCount(ctx); n != 501 {
		t.Fatal(n)
	}
}
