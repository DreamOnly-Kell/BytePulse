package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"bytepulse/internal/proc"
	"bytepulse/internal/processstate"
	"bytepulse/internal/storage"
)

type fakeProcessSampler struct {
	results [][]proc.Connection
	errs    []error
	calls   int
}

func (f *fakeProcessSampler) Sample() ([]proc.Connection, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i < len(f.results) {
		return f.results[i], nil
	}
	return nil, nil
}

type fakeProcessStore struct {
	minutes []storage.ProcessConnectionMinute
	err     error
}

func (f *fakeProcessStore) UpsertProcessConnectionMinutes(items []storage.ProcessConnectionMinute) error {
	if f.err != nil {
		return f.err
	}
	f.minutes = append(f.minutes, items...)
	return nil
}

func (f *fakeProcessStore) CleanupProcessConnectionMinutes(time.Time, time.Duration) error {
	return nil
}

func TestProcessConnectionCollectorUpdatesRealtimeStateBeforeFlush(t *testing.T) {
	state := processstate.New()
	sampler := &fakeProcessSampler{
		results: [][]proc.Connection{{
			{PID: 1, ProcessName: "curl", ProcessKey: "1:0", SeenAt: time.Now()},
		}},
	}
	store := &fakeProcessStore{}
	collector := NewProcessConnectionCollector(store, sampler, state, ProcessConnectionOptions{Interval: time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()

	deadline := time.After(3 * time.Second)
	for {
		if len(state.LatestSummaries(10)) > 0 {
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("run: %v", err)
			}
			return
		}
		select {
		case <-deadline:
			cancel()
			t.Fatalf("state was not updated")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestProcessConnectionCollectorFlushesCompletedMinute(t *testing.T) {
	state := processstate.New()
	store := &fakeProcessStore{}
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)
	state.Update([]proc.Connection{{PID: 1, ProcessName: "curl", ProcessKey: "1:0", SeenAt: now}}, now)

	err := flushProcessMinutes(store, state, now.Add(time.Minute), time.Hour)
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	if len(store.minutes) != 1 {
		t.Fatalf("flushed=%d, want 1", len(store.minutes))
	}
}

func TestProcessConnectionCollectorExcludesSelfWhenEnabled(t *testing.T) {
	state := processstate.New()
	now := time.Now()
	sampler := &fakeProcessSampler{
		results: [][]proc.Connection{{
			{PID: 42, ProcessName: "bytepulse", ProcessKey: "42:0", SeenAt: now},
			{PID: 7, ProcessName: "curl", ProcessKey: "7:0", SeenAt: now},
		}},
	}
	store := &fakeProcessStore{}
	c := NewProcessConnectionCollector(store, sampler, state, ProcessConnectionOptions{
		Interval:    time.Hour,
		ExcludeSelf: true,
		SelfPID:     42,
	})
	if err := c.sampleOnce(now); err != nil {
		t.Fatalf("sampleOnce: %v", err)
	}
	got := state.LatestSummaries(10)
	if len(got) != 1 || got[0].ProcessName != "curl" {
		t.Fatalf("summaries=%v, want only curl", got)
	}
}

func TestProcessConnectionCollectorUnsupportedSamplerExitsNil(t *testing.T) {
	state := processstate.New()
	sampler := &fakeProcessSampler{errs: []error{proc.ErrNotSupported}}
	store := &fakeProcessStore{}
	collector := NewProcessConnectionCollector(store, sampler, state, ProcessConnectionOptions{Interval: time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := collector.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(store.minutes) != 0 {
		t.Fatalf("store received minutes: %v", store.minutes)
	}
}

func TestProcessConnectionCollectorContinuesAfterTransientSampleError(t *testing.T) {
	state := processstate.New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)
	sampler := &fakeProcessSampler{
		errs: []error{errors.New("temporary")},
		results: [][]proc.Connection{
			nil,
			{{PID: 1, ProcessName: "curl", ProcessKey: "1:0", SeenAt: now}},
		},
	}
	store := &fakeProcessStore{}
	collector := NewProcessConnectionCollector(store, sampler, state, ProcessConnectionOptions{Interval: time.Millisecond})

	if err := collector.sampleOnce(now); err != nil {
		t.Fatalf("first sample: %v", err)
	}
	if len(state.LatestSummaries(10)) != 0 {
		t.Fatalf("state updated after transient error")
	}
	if err := collector.sampleOnce(now.Add(time.Second)); err != nil {
		t.Fatalf("second sample: %v", err)
	}
	if len(state.LatestSummaries(10)) != 1 {
		t.Fatalf("state was not updated after transient error")
	}
}

func TestProcessConnectionCollectorReturnsStoreError(t *testing.T) {
	state := processstate.New()
	storeErr := errors.New("store down")
	store := &fakeProcessStore{err: storeErr}
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)
	state.Update([]proc.Connection{{PID: 1, ProcessName: "curl", ProcessKey: "1:0", SeenAt: now}}, now)

	err := flushProcessMinutes(store, state, now.Add(time.Minute), time.Hour)
	if !errors.Is(err, storeErr) {
		t.Fatalf("err=%v, want %v", err, storeErr)
	}
}
