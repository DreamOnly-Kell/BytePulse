package proctraffic

import (
	"runtime"
	"testing"
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
