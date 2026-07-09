package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadFileMergesYAML checks YAML fields map onto Config.
// TestLoadFileMergesYAML 检查 YAML 字段是否正确合并进 Config。
func TestLoadFileMergesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
db: /tmp/test.db
daemon_api_addr: 127.0.0.1:9000
process_traffic: auto
exclude_self: false
log_level: info
retention: 48h
top_n: 10
bits: true
web_addr: 127.0.0.1:9090
daemon_interval: 2s
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	if err := LoadFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Fatalf("db=%q", cfg.DBPath)
	}
	if cfg.DaemonAPIAddr != "127.0.0.1:9000" {
		t.Fatalf("api=%q", cfg.DaemonAPIAddr)
	}
	if cfg.ProcessTraffic != "auto" || cfg.ExcludeSelf {
		t.Fatalf("traffic/exclude=%q/%v", cfg.ProcessTraffic, cfg.ExcludeSelf)
	}
	if cfg.LogLevel != "info" || !cfg.UseBits || cfg.TopN != 10 {
		t.Fatalf("log/bits/top=%q/%v/%d", cfg.LogLevel, cfg.UseBits, cfg.TopN)
	}
	if cfg.Retention != 48*time.Hour || cfg.DaemonInterval != 2*time.Second {
		t.Fatalf("retention/interval=%v/%v", cfg.Retention, cfg.DaemonInterval)
	}
	if cfg.WebAddr != "127.0.0.1:9090" {
		t.Fatalf("web=%q", cfg.WebAddr)
	}
}

func TestLoadFileMissingPathNoop(t *testing.T) {
	cfg := Default()
	if err := LoadFile("", &cfg); err != nil {
		t.Fatal(err)
	}
}

func TestPeekConfigPath(t *testing.T) {
	if got := PeekConfigPath([]string{"bytepulse", "--config", "/a.yaml", "daemon"}); got != "/a.yaml" {
		t.Fatalf("got %q", got)
	}
	if got := PeekConfigPath([]string{"--config=/b.yaml"}); got != "/b.yaml" {
		t.Fatalf("got %q", got)
	}
}

func TestDaemonStartHint(t *testing.T) {
	cfg := Default()
	if h := DaemonStartHint(cfg); h != "bytepulse daemon" {
		t.Fatalf("hint=%q", h)
	}
	cfg.DaemonAPIAddr = "127.0.0.1:1"
	if h := DaemonStartHint(cfg); h == "bytepulse daemon" {
		t.Fatalf("expected custom hint, got %q", h)
	}
}
