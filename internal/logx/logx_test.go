package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitLevelFiltersInfoWhenErrorDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := Init(Options{Level: "error", Format: "text", File: path}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })

	Info("should not appear")
	Error("must appear", "k", 1)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "should not appear") {
		t.Fatalf("info leaked at error level: %s", text)
	}
	if !strings.Contains(text, "must appear") {
		t.Fatalf("error missing: %s", text)
	}
}

func TestInitDebugIncludesInfo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := Init(Options{Level: "debug", Format: "text", File: path}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })

	Debug("dbg")
	Info("inf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "dbg") || !strings.Contains(text, "inf") {
		t.Fatalf("expected debug+info lines: %s", text)
	}
}

func TestWarnEveryRateLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := Init(Options{Level: "warn", Format: "text", File: path}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })

	WarnEvery(time.Hour, "k1", "once")
	WarnEvery(time.Hour, "k1", "once")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if c := strings.Count(string(data), "once"); c != 1 {
		t.Fatalf("count=%d, want 1", c)
	}
}

func TestParseLevelRejectsUnknown(t *testing.T) {
	if err := Init(Options{Level: "verbose"}); err == nil {
		t.Fatal("expected error for unknown level")
	}
}
