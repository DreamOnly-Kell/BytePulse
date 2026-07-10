package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"bytepulse/internal/proc"
	"bytepulse/internal/processstate"
	"bytepulse/internal/proctraffic"
)

type fakeTrafficAttributor struct {
	samples []proctraffic.Sample
	err     error
}

func (f fakeTrafficAttributor) Run(ctx context.Context, onSample func([]proctraffic.Sample)) error {
	if f.err != nil {
		return f.err
	}
	onSample(f.samples)
	<-ctx.Done()
	return nil
}

func TestProcessTrafficCollectorUpdatesState(t *testing.T) {
	state := processstate.New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)
	state.Update([]proc.Connection{{PID: 1, ProcessName: "curl", ProcessKey: "1:1", SeenAt: now}}, now)

	collector := NewProcessTrafficCollector(fakeTrafficAttributor{
		samples: []proctraffic.Sample{{PID: 1, RXBps: 12, TXBps: 34, SeenAt: now, Source: "nettop"}},
	}, state)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()

	deadline := time.After(3 * time.Second)
	for {
		summaries := state.LatestSummaries(10)
		if len(summaries) == 1 && summaries[0].TrafficAvailable {
			status := state.TrafficBackendStatus()
			if status.State != processstate.TrafficBackendHealthy || status.LastSampleAt.IsZero() {
				t.Fatalf("status=%+v", status)
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("run: %v", err)
			}
			return
		}
		select {
		case <-deadline:
			cancel()
			t.Fatalf("traffic was not applied")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestProcessTrafficCollectorUnsupportedReturnsNil(t *testing.T) {
	collector := NewProcessTrafficCollector(fakeTrafficAttributor{err: proctraffic.ErrNotSupported}, processstate.New())
	if err := collector.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestProcessTrafficCollectorOtherErrorReturnsNil(t *testing.T) {
	state := processstate.New()
	now := time.Now()
	state.Update([]proc.Connection{{PID: 1, ProcessName: "curl", ProcessKey: "1:1", SeenAt: now}}, now)
	state.UpdateTraffic([]processstate.ProcessTrafficSample{{PID: 1, RXBps: 10, SeenAt: now, Source: "nettop"}})
	collector := NewProcessTrafficCollector(fakeTrafficAttributor{err: errors.New("nettop failed")}, state)
	if err := collector.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	status := state.TrafficBackendStatus()
	if status.State != processstate.TrafficBackendDegraded || status.LastError != "nettop failed" {
		t.Fatalf("status=%+v", status)
	}
	got := state.LatestSummaries(1)
	if len(got) != 1 || got[0].TrafficAvailable {
		t.Fatalf("stale traffic was not cleared: %+v", got)
	}
}
