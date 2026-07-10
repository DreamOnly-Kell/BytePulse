package proc

import "testing"

func TestFilterSelfConnectionsRemovesOwnPIDAndBytepulseName(t *testing.T) {
	conns := []Connection{
		{PID: 100, ProcessName: "curl", ProcessPath: "/usr/bin/curl"},
		{PID: 42, ProcessName: "bytepulse", ProcessPath: "/tmp/bytepulse"},
		{PID: 99, ProcessName: "helper", ProcessPath: "/opt/bytepulse"}, // path basename is bytepulse
		{PID: 7, ProcessName: "Safari", ProcessPath: "/Applications/Safari.app/Contents/MacOS/Safari"},
		{PID: 42, ProcessName: "other", ProcessPath: "/usr/bin/other"}, // same PID as self
	}

	got := FilterSelfConnections(conns, true, 42)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2 (curl + Safari); got %#v", len(got), got)
	}
	if got[0].PID != 100 || got[1].PID != 7 {
		t.Fatalf("remaining=%v, want curl then Safari", got)
	}
}

func TestFilterSelfConnectionsDisabledKeepsAll(t *testing.T) {
	conns := []Connection{
		{PID: 42, ProcessName: "bytepulse", ProcessPath: "/tmp/bytepulse"},
		{PID: 1, ProcessName: "curl", ProcessPath: "/usr/bin/curl"},
	}
	got := FilterSelfConnections(conns, false, 42)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2 when exclude disabled", len(got))
	}
}

func TestIsSelfProcessMatchesNameCaseInsensitive(t *testing.T) {
	if !IsSelfProcess(1, "BytePulse", "", 0) {
		t.Fatal("expected name match")
	}
	if !IsSelfProcess(1, "unknown", "/usr/local/bin/BytePulse", 0) {
		t.Fatal("expected path basename match")
	}
	if !IsSelfProcess(1, "unknown", `C:\tools\bytepulse.exe`, 0) {
		t.Fatal("expected windows exe basename match")
	}
	if IsSelfProcess(1, "curl", "/usr/bin/curl", 0) {
		t.Fatal("curl should not match self")
	}
	if !IsSelfProcess(99, "curl", "/usr/bin/curl", 99) {
		t.Fatal("expected self PID match")
	}
}
