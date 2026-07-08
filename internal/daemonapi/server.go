package daemonapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"bytepulse/internal/config"
	"bytepulse/internal/processstate"
	"bytepulse/internal/storage"
)

type Store interface {
	TopProcessConnectionMinutes(start, end time.Time, limit int) ([]storage.ProcessConnectionSummary, error)
}

type Server struct {
	state *processstate.State
	store Store
	cfg   config.Config
	mux   *http.ServeMux
}

func NewServer(state *processstate.State, store Store, cfg config.Config) *Server {
	s := &Server{
		state: state,
		store: store,
		cfg:   cfg,
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/processes", s.handleProcesses)
	s.mux.HandleFunc("/api/processes/connections", s.handleProcessConnections)
	s.mux.HandleFunc("/api/processes/top", s.handleProcessesTop)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	limit, err := s.limit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, s.state.LatestSummaries(limit))
}

func (s *Server) handleProcessConnections(w http.ResponseWriter, r *http.Request) {
	processKey := r.URL.Query().Get("process_key")
	if processKey == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("process_key is required"))
		return
	}
	writeJSON(w, s.state.LatestConnections(processKey))
}

func (s *Server) handleProcessesTop(w http.ResponseWriter, r *http.Request) {
	limit, err := s.limit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rangeText := r.URL.Query().Get("range")
	if rangeText == "" {
		rangeText = "24h"
	}
	d, err := config.ParseRange(rangeText)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	now := time.Now()
	items, err := s.store.TopProcessConnectionMinutes(now.Add(-d), now, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, items)
}

func (s *Server) limit(r *http.Request) (int, error) {
	limit := s.cfg.TopN
	if limit <= 0 {
		limit = 30
	}
	text := r.URL.Query().Get("limit")
	if text == "" {
		return limit, nil
	}
	parsed, err := strconv.Atoi(text)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("limit must be a positive integer")
	}
	return parsed, nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
