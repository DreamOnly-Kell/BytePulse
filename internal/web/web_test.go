package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"bytepulse/internal/config"
	"bytepulse/internal/processstate"
	"bytepulse/internal/storage"
)

type failingProcessClient struct{}

func (failingProcessClient) Processes(context.Context, int) ([]processstate.ProcessConnectionSummary, error) {
	return nil, errors.New("daemon down")
}

func TestProcessesFacadeReturnsServiceUnavailableWhenDaemonUnavailable(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	server := New(store, config.Default())
	server.processClient = failingProcessClient{}
	req := httptest.NewRequest(http.MethodGet, "/api/processes", nil)
	rec := httptest.NewRecorder()

	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", rec.Code)
	}
}
