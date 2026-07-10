package collector

import (
	"testing"
	"time"

	"bytepulse/internal/storage"
)

type fakeInterfaceStore struct {
	cleanupCalls int
}

func (f *fakeInterfaceStore) InsertSamples([]storage.Sample) error { return nil }

func (f *fakeInterfaceStore) Cleanup(time.Time, time.Duration) error {
	f.cleanupCalls++
	return nil
}

func TestInterfaceCleanupCadence(t *testing.T) {
	store := &fakeInterfaceStore{}
	c := New(store, Options{Retention: 24 * time.Hour})
	t0 := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	for _, now := range []time.Time{t0, t0.Add(time.Second), t0.Add(59 * time.Minute)} {
		if err := c.cleanupIfDue(now); err != nil {
			t.Fatal(err)
		}
	}
	if store.cleanupCalls != 1 {
		t.Fatalf("cleanup calls=%d want=1", store.cleanupCalls)
	}
	if err := c.cleanupIfDue(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if store.cleanupCalls != 2 {
		t.Fatalf("cleanup calls=%d want=2", store.cleanupCalls)
	}
}

func TestProcessCleanupCadence(t *testing.T) {
	store := &fakeProcessStore{}
	c := NewProcessConnectionCollector(store, &fakeProcessSampler{}, nil, ProcessConnectionOptions{Retention: 24 * time.Hour})
	t0 := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	for _, now := range []time.Time{t0, t0.Add(time.Second), t0.Add(59 * time.Minute)} {
		if err := c.cleanupIfDue(now); err != nil {
			t.Fatal(err)
		}
	}
	if store.cleanupCalls != 1 {
		t.Fatalf("cleanup calls=%d want=1", store.cleanupCalls)
	}
	if err := c.cleanupIfDue(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if store.cleanupCalls != 2 {
		t.Fatalf("cleanup calls=%d want=2", store.cleanupCalls)
	}
}
