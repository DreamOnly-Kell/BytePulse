package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"bytepulse/internal/config"
	"bytepulse/internal/i18n"
	"bytepulse/internal/processstate"
	"bytepulse/internal/storage"
)

type fakeTrafficStore struct {
	latest    storage.Sample
	latestErr error
}

func (f fakeTrafficStore) LatestAggregateSample(string) (storage.Sample, error) {
	return f.latest, f.latestErr
}

func (f fakeTrafficStore) Summary(time.Time, time.Time, string) (storage.SummaryResult, error) {
	return storage.SummaryResult{}, nil
}

func (f fakeTrafficStore) RecentSeries(time.Time, time.Time, string) ([]storage.Sample, error) {
	return []storage.Sample{}, nil
}

type fakeProcessClient struct {
	items     []processstate.ProcessConnectionSummary
	healthErr error
	itemsErr  error
}

func (f fakeProcessClient) Health(context.Context) error { return f.healthErr }

func (f fakeProcessClient) Processes(context.Context, int) ([]processstate.ProcessConnectionSummary, error) {
	return f.items, f.itemsErr
}

func TestViewWaitsWhenDaemonDown(t *testing.T) {
	i18n.SetLang("en")
	m := model{
		cfg:       config.Default(),
		daemonOK:  false,
		showProc:  true,
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
	m := model{cfg: config.Default(), daemonOK: false, showProc: true, waitTicks: 1}
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

func TestIndependentRefreshUpdatesProcessesWhenSQLiteHasNoSamples(t *testing.T) {
	m := model{
		store:         fakeTrafficStore{latestErr: storage.ErrNotFound},
		cfg:           config.Default(),
		processClient: fakeProcessClient{items: []processstate.ProcessConnectionSummary{{PID: 1, ProcessName: "curl"}}},
	}

	m.refresh()

	if !m.daemonOK || len(m.procs) != 1 || m.procs[0].ProcessName != "curl" {
		t.Fatalf("process refresh failed: daemonOK=%v procs=%+v err=%v", m.daemonOK, m.procs, m.processErr)
	}
	if !errors.Is(m.err, storage.ErrNotFound) {
		t.Fatalf("traffic err=%v want ErrNotFound", m.err)
	}
}

func TestIndependentRefreshKeepsTrafficWhenDaemonAPIFails(t *testing.T) {
	latest := storage.Sample{Timestamp: time.Now(), RXSpeedBps: 123}
	m := model{
		store:         fakeTrafficStore{latest: latest},
		cfg:           config.Default(),
		processClient: fakeProcessClient{healthErr: errors.New("connection refused")},
	}

	m.refresh()

	if m.daemonOK || m.processErr == nil {
		t.Fatalf("process state not failed: daemonOK=%v err=%v", m.daemonOK, m.processErr)
	}
	if !m.loaded || m.latest.RXSpeedBps != 123 || m.err != nil {
		t.Fatalf("traffic state lost: loaded=%v latest=%+v err=%v", m.loaded, m.latest, m.err)
	}
	if view := m.View(); strings.Contains(view, "Background collector is not running") {
		t.Fatalf("traffic view was replaced by daemon wait screen: %q", view)
	}
}
