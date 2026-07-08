package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	DBPath          string
	PIDPath         string
	Interface       string
	UseBits         bool
	Retention       time.Duration
	TopN            int
	ProcessInterval time.Duration
	DaemonAPIAddr   string
}

func Default() Config {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	stateDir := filepath.Join(home, ".bytepulse")
	return Config{
		DBPath:          filepath.Join(stateDir, "bytepulse.db"),
		PIDPath:         filepath.Join(stateDir, "bytepulse.pid"),
		Retention:       30 * 24 * time.Hour,
		TopN:            30,
		ProcessInterval: time.Second,
		DaemonAPIAddr:   "127.0.0.1:8988",
	}
}

func InterfaceLabel(name string) string {
	if strings.TrimSpace(name) == "" {
		return "all non-loopback interfaces"
	}
	return name
}

func ParseRange(text string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "1h":
		return time.Hour, nil
	case "2h":
		return 2 * time.Hour, nil
	case "3h":
		return 3 * time.Hour, nil
	case "5h":
		return 5 * time.Hour, nil
	case "10h":
		return 10 * time.Hour, nil
	case "12h":
		return 12 * time.Hour, nil
	case "24h", "1d":
		return 24 * time.Hour, nil
	case "2d":
		return 48 * time.Hour, nil
	case "3d":
		return 72 * time.Hour, nil
	case "7d", "1w":
		return 7 * 24 * time.Hour, nil
	case "15d":
		return 15 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported range %q; use one of 1h,2h,3h,5h,10h,12h,24h,2d,3d,7d,15d", text)
	}
}
