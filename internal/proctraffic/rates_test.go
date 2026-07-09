package proctraffic

import (
	"testing"
	"time"
)

func TestComputeRateSamplesUsesDeltas(t *testing.T) {
	prev := map[int]pidByteCounters{
		1: {RX: 1000, TX: 200},
		2: {RX: 50, TX: 10},
	}
	curr := map[int]pidByteCounters{
		1: {RX: 3000, TX: 700},  // +2000 / +500
		2: {RX: 50, TX: 10},     // idle
		3: {RX: 100, TX: 100},   // first seen → skip
	}
	meta := map[int]processMeta{
		1: {name: "chrome", path: `C:\chrome.exe`},
		2: {name: "svchost", path: `C:\svchost.exe`},
	}
	now := time.Unix(100, 0)
	got := computeRateSamples(prev, curr, 1.0, now, "estats", meta)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2 (pid 3 first-seen skipped)", len(got))
	}
	byPID := map[int]Sample{}
	for _, s := range got {
		byPID[s.PID] = s
	}
	if byPID[1].RXBps != 2000 || byPID[1].TXBps != 500 {
		t.Fatalf("pid1 rates=%v/%v, want 2000/500", byPID[1].RXBps, byPID[1].TXBps)
	}
	if byPID[1].ProcessName != "chrome" || byPID[1].Source != "estats" {
		t.Fatalf("pid1 meta=%+v", byPID[1])
	}
	if byPID[2].RXBps != 0 || byPID[2].TXBps != 0 {
		t.Fatalf("pid2 idle rates=%v/%v", byPID[2].RXBps, byPID[2].TXBps)
	}
}

func TestComputeRateSamplesHandlesCounterReset(t *testing.T) {
	prev := map[int]pidByteCounters{1: {RX: 5000, TX: 5000}}
	curr := map[int]pidByteCounters{1: {RX: 10, TX: 20}} // reset
	got := computeRateSamples(prev, curr, 1, time.Now(), "estats", nil)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].RXBps != 0 || got[0].TXBps != 0 {
		t.Fatalf("reset should yield 0 rates, got %v/%v", got[0].RXBps, got[0].TXBps)
	}
}
