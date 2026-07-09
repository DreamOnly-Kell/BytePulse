package tui

import (
	"errors"
	"strings"
	"testing"

	"bytepulse/internal/config"
	"bytepulse/internal/i18n"
)

func TestViewWaitsWhenDaemonDown(t *testing.T) {
	i18n.SetLang("en")
	m := model{
		cfg:       config.Default(),
		daemonOK:  false,
		waitTicks: 3,
	}

	view := m.View()
	if !strings.Contains(view, "Background collector is not running") {
		t.Fatalf("view=%q, want wait-for-daemon message", view)
	}
	if !strings.Contains(view, "bytepulse daemon") {
		t.Fatalf("view=%q, want start hint", view)
	}
	if !strings.Contains(view, "Waiting for daemon... (3s)") {
		t.Fatalf("view=%q, want wait counter", view)
	}
}

func TestViewWaitsWhenDaemonDownZH(t *testing.T) {
	i18n.SetLang("zh")
	defer i18n.SetLang("en")
	m := model{cfg: config.Default(), daemonOK: false, waitTicks: 1}
	view := m.View()
	if !strings.Contains(view, "后台采集未运行") {
		t.Fatalf("view=%q, want zh wait message", view)
	}
}

func TestProcessViewShowsAPIErrorWhenDaemonWasOK(t *testing.T) {
	i18n.SetLang("en")
	m := model{
		cfg:        config.Default(),
		showProc:   true,
		daemonOK:   true,
		processErr: errors.New("connection refused"),
	}

	view := m.View()
	if !strings.Contains(view, "Daemon API error") {
		t.Fatalf("view=%q, want mid-session API error", view)
	}
}
