package proctraffic

import (
	"strings"
	"testing"
	"time"
)

func TestParseNettopCSVParsesPIDProcessAndBytes(t *testing.T) {
	input := strings.NewReader("time,pid,process,bytes_in,bytes_out\n10:00:00,123,/usr/bin/curl,2048,1024\n")

	got, err := ParseNettopCSV(input, time.Unix(100, 0))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	if got[0].PID != 123 {
		t.Fatalf("pid=%d, want 123", got[0].PID)
	}
	if got[0].ProcessName != "curl" {
		t.Fatalf("name=%q, want curl", got[0].ProcessName)
	}
	if got[0].ProcessPath != "/usr/bin/curl" {
		t.Fatalf("path=%q, want /usr/bin/curl", got[0].ProcessPath)
	}
	if got[0].RXBytes != 2048 || got[0].TXBytes != 1024 {
		t.Fatalf("bytes rx=%d tx=%d, want 2048/1024", got[0].RXBytes, got[0].TXBytes)
	}
	if got[0].Source != "nettop" {
		t.Fatalf("source=%q, want nettop", got[0].Source)
	}
}

func TestParseNettopCSVAcceptsNettopStyleColumnAliases(t *testing.T) {
	input := strings.NewReader("timestamp,pid,command,rx_bytes,tx_bytes\n10:00:00,55,Safari,300,700\n")

	got, err := ParseNettopCSV(input, time.Unix(100, 0))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got[0].ProcessName != "Safari" {
		t.Fatalf("name=%q, want Safari", got[0].ProcessName)
	}
	if got[0].ProcessPath != "Safari" {
		t.Fatalf("path=%q, want Safari", got[0].ProcessPath)
	}
	if got[0].RXBytes != 300 || got[0].TXBytes != 700 {
		t.Fatalf("bytes rx=%d tx=%d, want 300/700", got[0].RXBytes, got[0].TXBytes)
	}
}

func TestParseNettopCSVParsesMacOSProcessPIDColumn(t *testing.T) {
	input := strings.NewReader("time,,interface,state,bytes_in,bytes_out,rx_dupe\n17:27:12.465745,mihomo.31842,,,231,338,0\n")

	got, err := ParseNettopCSV(input, time.Unix(100, 0))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	if got[0].PID != 31842 {
		t.Fatalf("pid=%d, want 31842", got[0].PID)
	}
	if got[0].ProcessName != "mihomo" {
		t.Fatalf("name=%q, want mihomo", got[0].ProcessName)
	}
	if got[0].ProcessPath != "mihomo" {
		t.Fatalf("path=%q, want mihomo", got[0].ProcessPath)
	}
	if got[0].RXBytes != 231 || got[0].TXBytes != 338 {
		t.Fatalf("bytes rx=%d tx=%d, want 231/338", got[0].RXBytes, got[0].TXBytes)
	}
}

func TestParseNettopCSVReturnsEmptyWithoutRequiredColumns(t *testing.T) {
	input := strings.NewReader("time,process,bytes_in,bytes_out\n10:00:00,curl,2048,1024\n")

	got, err := ParseNettopCSV(input, time.Unix(100, 0))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len=%d, want 0", len(got))
	}
}
