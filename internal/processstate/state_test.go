package processstate

import (
	"testing"
	"time"

	"bytepulse/internal/proc"
)

func TestUpdateReplacesLatestSummariesEverySample(t *testing.T) {
	state := New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)

	state.Update([]proc.Connection{
		{PID: 1, ProcessName: "A", ProcessKey: "1:1", SeenAt: now},
		{PID: 2, ProcessName: "B", ProcessKey: "2:1", SeenAt: now},
	}, now)
	state.Update([]proc.Connection{
		{PID: 2, ProcessName: "B", ProcessKey: "2:1", SeenAt: now.Add(time.Second)},
	}, now.Add(time.Second))

	got := state.LatestSummaries(10)
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	if got[0].ProcessKey != "2:1" {
		t.Fatalf("process key=%q, want 2:1", got[0].ProcessKey)
	}
}

func TestLatestConnectionsAreProcessScoped(t *testing.T) {
	state := New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)

	state.Update([]proc.Connection{
		{PID: 1, ProcessName: "A", ProcessKey: "1:1", Protocol: "tcp", LocalPort: 1, SeenAt: now},
		{PID: 2, ProcessName: "B", ProcessKey: "2:1", Protocol: "tcp", LocalPort: 2, SeenAt: now},
	}, now)

	got := state.LatestConnections("2:1")
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	if got[0].LocalPort != 2 {
		t.Fatalf("local port=%d, want 2", got[0].LocalPort)
	}
}

func TestUpdateCarriesProcessPath(t *testing.T) {
	state := New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)

	state.Update([]proc.Connection{
		{PID: 1, ProcessName: "curl", ProcessPath: "/usr/bin/curl", ProcessKey: "1:1", SeenAt: now},
	}, now)

	summaries := state.LatestSummaries(10)
	if len(summaries) != 1 {
		t.Fatalf("len=%d, want 1", len(summaries))
	}
	if summaries[0].ProcessPath != "/usr/bin/curl" {
		t.Fatalf("summary path=%q, want /usr/bin/curl", summaries[0].ProcessPath)
	}
	details := state.LatestConnections("1:1")
	if len(details) != 1 {
		t.Fatalf("details len=%d, want 1", len(details))
	}
	if details[0].ProcessPath != "/usr/bin/curl" {
		t.Fatalf("detail path=%q, want /usr/bin/curl", details[0].ProcessPath)
	}
}

func TestUpdateTrafficMergesRatesIntoLatestSummaries(t *testing.T) {
	state := New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)
	state.Update([]proc.Connection{
		{PID: 1, ProcessName: "curl", ProcessPath: "/usr/bin/curl", ProcessKey: "1:1", SeenAt: now},
	}, now)

	state.UpdateTraffic([]ProcessTrafficSample{
		{PID: 1, RXBytes: 3000, TXBytes: 1000, RXBps: 3000, TXBps: 1000, SeenAt: now, Source: "nettop"},
	})

	got := state.LatestSummaries(10)
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	if got[0].RXBps != 3000 || got[0].TXBps != 1000 {
		t.Fatalf("rates rx=%v tx=%v, want 3000/1000", got[0].RXBps, got[0].TXBps)
	}
	if !got[0].TrafficAvailable {
		t.Fatalf("traffic should be available")
	}
	if got[0].TrafficSource != "nettop" {
		t.Fatalf("source=%q, want nettop", got[0].TrafficSource)
	}
}

func TestUpdatePreservesLatestTrafficAcrossConnectionRefresh(t *testing.T) {
	state := New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)
	state.UpdateTraffic([]ProcessTrafficSample{
		{PID: 1, RXBytes: 3000, TXBytes: 1000, RXBps: 3000, TXBps: 1000, SeenAt: now, Source: "nettop"},
	})

	state.Update([]proc.Connection{
		{PID: 1, ProcessName: "curl", ProcessPath: "/usr/bin/curl", ProcessKey: "1:1", SeenAt: now.Add(time.Second)},
	}, now.Add(time.Second))

	got := state.LatestSummaries(10)
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	if !got[0].TrafficAvailable {
		t.Fatalf("traffic should remain available after connection refresh")
	}
	if got[0].RXBps != 3000 || got[0].TXBps != 1000 {
		t.Fatalf("rates rx=%v tx=%v, want 3000/1000", got[0].RXBps, got[0].TXBps)
	}
}

func TestUpdateTrafficDoesNotOverwriteConnectionPath(t *testing.T) {
	state := New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)
	state.Update([]proc.Connection{
		{PID: 1, ProcessName: "curl", ProcessPath: "/usr/bin/curl", ProcessKey: "1:1", SeenAt: now},
	}, now)

	state.UpdateTraffic([]ProcessTrafficSample{
		{PID: 1, ProcessName: "curl", ProcessPath: "curl", RXBps: 1, TXBps: 2, SeenAt: now, Source: "nettop"},
	})

	got := state.LatestSummaries(10)
	if got[0].ProcessPath != "/usr/bin/curl" {
		t.Fatalf("path=%q, want original full path", got[0].ProcessPath)
	}
}

func TestMinuteBucketAccumulatesSampleCountAndMaxConnections(t *testing.T) {
	state := New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)

	state.Update([]proc.Connection{
		{PID: 1, ProcessName: "A", ProcessKey: "1:1", SeenAt: now},
	}, now)
	state.Update([]proc.Connection{
		{PID: 1, ProcessName: "A", ProcessKey: "1:1", LocalPort: 1, SeenAt: now.Add(time.Second)},
		{PID: 1, ProcessName: "A", ProcessKey: "1:1", LocalPort: 2, SeenAt: now.Add(time.Second)},
	}, now.Add(time.Second))

	flushed := state.FlushBefore(now.Add(time.Minute))
	if len(flushed) != 1 {
		t.Fatalf("len=%d, want 1", len(flushed))
	}
	if flushed[0].SampleCount != 2 {
		t.Fatalf("sample count=%d, want 2", flushed[0].SampleCount)
	}
	if flushed[0].MaxConnectionCount != 2 {
		t.Fatalf("max connections=%d, want 2", flushed[0].MaxConnectionCount)
	}
}

func TestFlushCompletedKeepsCurrentMinuteInMemory(t *testing.T) {
	state := New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)

	state.Update([]proc.Connection{{PID: 1, ProcessName: "A", ProcessKey: "1:1", SeenAt: now}}, now)

	if got := state.FlushCompleted(now); len(got) != 0 {
		t.Fatalf("flushed current minute: %v", got)
	}
	if got := state.FlushBefore(now.Add(time.Minute)); len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
}

func TestLatestSummariesLimitAndSortOrder(t *testing.T) {
	state := New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)

	state.Update([]proc.Connection{
		{PID: 3, ProcessName: "C", ProcessKey: "3:1", LocalPort: 1, SeenAt: now},
		{PID: 1, ProcessName: "A", ProcessKey: "1:1", LocalPort: 1, SeenAt: now},
		{PID: 2, ProcessName: "B", ProcessKey: "2:1", LocalPort: 1, SeenAt: now},
		{PID: 2, ProcessName: "B", ProcessKey: "2:1", LocalPort: 2, SeenAt: now},
	}, now)

	got := state.LatestSummaries(2)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if got[0].ProcessKey != "2:1" || got[1].ProcessKey != "1:1" {
		t.Fatalf("order=%v", got)
	}
}
