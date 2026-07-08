package storage

import (
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestUpsertProcessConnectionMinutesMergesSameMinuteAndProcess(t *testing.T) {
	store := newTestStore(t)
	minute := time.Date(2026, 7, 8, 10, 15, 0, 0, time.UTC)

	err := store.UpsertProcessConnectionMinutes([]ProcessConnectionMinute{
		{
			MinuteStart:        minute,
			PID:                100,
			ProcessName:        "Safari",
			ProcessPath:        "/Applications/Safari.app/Contents/MacOS/Safari",
			ProcessKey:         "100:1",
			MaxConnectionCount: 3,
			SampleCount:        10,
			LastSeen:           minute.Add(10 * time.Second),
		},
		{
			MinuteStart:        minute,
			PID:                100,
			ProcessName:        "Safari",
			ProcessPath:        "/Applications/Safari.app/Contents/MacOS/Safari",
			ProcessKey:         "100:1",
			MaxConnectionCount: 5,
			SampleCount:        7,
			LastSeen:           minute.Add(40 * time.Second),
		},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.TopProcessConnectionMinutes(minute.Add(-time.Minute), minute.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	if got[0].ConnectionCount != 17 {
		t.Fatalf("connection count=%d, want merged sample count 17", got[0].ConnectionCount)
	}
	if got[0].ProcessPath != "/Applications/Safari.app/Contents/MacOS/Safari" {
		t.Fatalf("path=%q, want Safari path", got[0].ProcessPath)
	}
	if !got[0].LastSeen.Equal(minute.Add(40 * time.Second).Local()) {
		t.Fatalf("last seen=%s, want %s", got[0].LastSeen, minute.Add(40*time.Second).Local())
	}
}

func TestTopProcessConnectionMinutesRanksBySampleCountThenMaxConnections(t *testing.T) {
	store := newTestStore(t)
	minute := time.Date(2026, 7, 8, 10, 15, 0, 0, time.UTC)

	err := store.UpsertProcessConnectionMinutes([]ProcessConnectionMinute{
		{MinuteStart: minute, PID: 1, ProcessName: "B", ProcessKey: "1:1", MaxConnectionCount: 20, SampleCount: 5, LastSeen: minute},
		{MinuteStart: minute, PID: 2, ProcessName: "A", ProcessKey: "2:1", MaxConnectionCount: 2, SampleCount: 8, LastSeen: minute},
		{MinuteStart: minute, PID: 3, ProcessName: "C", ProcessKey: "3:1", MaxConnectionCount: 9, SampleCount: 5, LastSeen: minute},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.TopProcessConnectionMinutes(minute, minute.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	keys := []string{got[0].ProcessKey, got[1].ProcessKey, got[2].ProcessKey}
	want := []string{"2:1", "1:1", "3:1"}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("order=%v, want %v", keys, want)
		}
	}
}

func TestCleanupProcessConnectionMinutesRemovesExpiredRows(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 7, 8, 10, 15, 0, 0, time.UTC)

	err := store.UpsertProcessConnectionMinutes([]ProcessConnectionMinute{
		{MinuteStart: now.Add(-2 * time.Hour), PID: 1, ProcessName: "old", ProcessKey: "1:1", MaxConnectionCount: 1, SampleCount: 1, LastSeen: now.Add(-2 * time.Hour)},
		{MinuteStart: now.Add(-30 * time.Minute), PID: 2, ProcessName: "new", ProcessKey: "2:1", MaxConnectionCount: 1, SampleCount: 1, LastSeen: now.Add(-30 * time.Minute)},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.CleanupProcessConnectionMinutes(now, time.Hour); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	got, err := store.TopProcessConnectionMinutes(now.Add(-3*time.Hour), now, 10)
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	if len(got) != 1 || got[0].ProcessKey != "2:1" {
		t.Fatalf("remaining=%v, want only 2:1", got)
	}
}

func TestTopProcessConnectionMinutesReturnsEmptySliceWithoutRows(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 7, 8, 10, 15, 0, 0, time.UTC)

	got, err := store.TopProcessConnectionMinutes(now.Add(-time.Hour), now, 10)
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	if got == nil {
		t.Fatalf("got nil, want empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("len=%d, want 0", len(got))
	}
}
