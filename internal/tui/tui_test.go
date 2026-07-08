package tui

import (
	"errors"
	"strings"
	"testing"

	"bytepulse/internal/config"
)

func TestProcessViewShowsDaemonUnavailable(t *testing.T) {
	m := model{
		cfg:        config.Default(),
		showProc:   true,
		processErr: errors.New("connection refused"),
	}

	view := m.View()
	if !strings.Contains(view, "Daemon API unavailable") {
		t.Fatalf("view=%q, want daemon unavailable message", view)
	}
}
