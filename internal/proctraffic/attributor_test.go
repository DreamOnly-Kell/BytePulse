package proctraffic

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNettopArgsUseProcessCSVDeltaMode(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("nettop is macOS-only")
	}
	got := nettopArgs()
	want := []string{"-P", "-L", "0", "-x", "-n", "-d", "-s", "1"}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestScanNettopGroupsEpochsDiscardsFirstAndUsesElapsedTime(t *testing.T) {
	header := "time,,interface,state,bytes_in,bytes_out"
	input := strings.NewReader(strings.Join([]string{
		header,
		"17:27:12.000000,apsd.138,,,3872,11776",
		header,
		"17:27:13.000000,apsd.138,,,200,100",
		"17:27:13.000000,curl.2468,,,80,40",
		header,
	}, "\n") + "\n")
	t0 := time.Unix(100, 0)
	times := []time.Time{t0, t0.Add(time.Second), t0.Add(3 * time.Second)}
	clockIndex := 0
	clock := func() time.Time {
		value := times[clockIndex]
		clockIndex++
		return value
	}

	var batches [][]Sample
	err := scanNettopCSVWithClock(context.Background(), input, func(samples []Sample) {
		batches = append(batches, samples)
	}, clock)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(batches) != 1 {
		t.Fatalf("batches=%d want=1: %+v", len(batches), batches)
	}
	if len(batches[0]) != 2 {
		t.Fatalf("samples=%d want=2: %+v", len(batches[0]), batches[0])
	}
	if batches[0][0].PID != 138 || batches[0][0].RXBytes != 200 || batches[0][0].RXBps != 100 {
		t.Fatalf("apsd sample=%+v", batches[0][0])
	}
	if batches[0][1].PID != 2468 || batches[0][1].TXBytes != 40 || batches[0][1].TXBps != 20 {
		t.Fatalf("curl sample=%+v", batches[0][1])
	}
}

func TestScanNettopEmptyFirstEpochDoesNotDiscardSecondEpoch(t *testing.T) {
	header := "time,,interface,state,bytes_in,bytes_out"
	input := strings.NewReader(strings.Join([]string{
		header,
		header,
		"17:27:13.000000,curl.2468,,,80,40",
		header,
	}, "\n") + "\n")
	t0 := time.Unix(100, 0)
	times := []time.Time{t0, t0.Add(time.Second), t0.Add(2 * time.Second)}
	i := 0
	var batches [][]Sample
	err := scanNettopCSVWithClock(context.Background(), input, func(samples []Sample) {
		batches = append(batches, samples)
	}, func() time.Time {
		value := times[i]
		i++
		return value
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(batches) != 1 || len(batches[0]) != 1 || batches[0][0].PID != 2468 {
		t.Fatalf("batches=%+v", batches)
	}
}
