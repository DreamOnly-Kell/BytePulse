package proc

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

var ErrNotSupported = errors.New("process connection sampling is not supported on this platform")

type Connection struct {
	PID         int
	ProcessName string
	ProcessPath string
	ProcessKey  string
	Protocol    string
	LocalAddr   string
	LocalPort   uint32
	RemoteAddr  string
	RemotePort  uint32
	Status      string
	SeenAt      time.Time
}

type ConnectionSampler interface {
	Sample() ([]Connection, error)
}

func processKey(pid int, createTimeMs int64) string {
	return fmt.Sprintf("%d:%d", pid, createTimeMs)
}

func processIdentity(pid int, path string, createTimeMs int64) (string, string, string) {
	path = strings.TrimSpace(path)
	name := "unknown"
	if path != "" {
		name = filepath.Base(path)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = path
		}
	}
	return name, path, processKey(pid, createTimeMs)
}

func dedupeKey(c Connection) string {
	return fmt.Sprintf("%d|%s|%s|%d|%s|%d|%s",
		c.PID,
		c.Protocol,
		c.LocalAddr,
		c.LocalPort,
		c.RemoteAddr,
		c.RemotePort,
		c.Status,
	)
}
