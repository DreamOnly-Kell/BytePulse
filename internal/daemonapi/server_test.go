package daemonapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bytepulse/internal/config"
	"bytepulse/internal/proc"
	"bytepulse/internal/processstate"
	"bytepulse/internal/storage"
)

type fakeTopStore struct {
	items []storage.ProcessConnectionSummary
}

func (f fakeTopStore) TopProcessConnectionMinutes(time.Time, time.Time, int) ([]storage.ProcessConnectionSummary, error) {
	return f.items, nil
}

func TestProcessesEndpointReadsRealtimeState(t *testing.T) {
	state := processstate.New()
	now := time.Date(2026, 7, 8, 10, 15, 1, 0, time.UTC)
	state.Update([]proc.Connection{{PID: 1, ProcessName: "curl", ProcessKey: "1:0", SeenAt: now}}, now)
	server := NewServer(state, fakeTopStore{}, config.Config{TopN: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/processes", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var got []processstate.ProcessConnectionSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ProcessKey != "1:0" {
		t.Fatalf("got=%v", got)
	}
}

func TestProcessConnectionsEndpointRequiresProcessKey(t *testing.T) {
	server := NewServer(processstate.New(), fakeTopStore{}, config.Config{TopN: 30})
	req := httptest.NewRequest(http.MethodGet, "/api/processes/connections", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestProcessesTopEndpointReadsStorageRollups(t *testing.T) {
	server := NewServer(processstate.New(), fakeTopStore{
		items: []storage.ProcessConnectionSummary{{PID: 1, ProcessName: "curl", ProcessKey: "1:0", ConnectionCount: 3}},
	}, config.Config{TopN: 30})
	req := httptest.NewRequest(http.MethodGet, "/api/processes/top?range=24h", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var got []storage.ProcessConnectionSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ConnectionCount != 3 {
		t.Fatalf("got=%v", got)
	}
}

func TestInvalidRangeReturnsBadRequest(t *testing.T) {
	server := NewServer(processstate.New(), fakeTopStore{}, config.Config{TopN: 30})
	req := httptest.NewRequest(http.MethodGet, "/api/processes/top?range=nope", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}
