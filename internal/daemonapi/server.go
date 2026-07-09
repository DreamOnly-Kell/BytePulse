// Package daemonapi exposes the daemon-local HTTP API for process realtime data.
// daemonapi 包暴露 daemon 本机 HTTP API，提供进程实时数据。
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

// Store is the historical query surface used by /api/processes/top.
// Store 是 /api/processes/top 使用的历史查询接口。
type Store interface {
	TopProcessConnectionMinutes(start, end time.Time, limit int) ([]storage.ProcessConnectionSummary, error)
}

// Server is the daemon API HTTP handler set.
// Server 是 daemon API 的 HTTP handler 集合。
type Server struct {
	// state is the in-memory process connection/traffic snapshot.
	// state 是进程连接/流量的内存快照。
	state *processstate.State
	// store is SQLite (or mock) for historical process ranks.
	// store 是用于历史进程排行的 SQLite（或 mock）。
	store Store
	// cfg supplies default TopN and related settings.
	// cfg 提供默认 TopN 及相关设置。
	cfg config.Config
	// mux routes API paths.
	// mux 路由 API 路径。
	mux *http.ServeMux
}

// NewServer constructs the API server and registers routes.
// NewServer 构造 API 服务器并注册路由。
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

// Handler returns the root HTTP handler for ListenAndServe.
// Handler 返回供 ListenAndServe 使用的根 HTTP handler。
func (s *Server) Handler() http.Handler {
	return s.mux
}

// routes registers all daemon API endpoints.
// routes 注册全部 daemon API 端点。
func (s *Server) routes() {
	// Liveness check for clients before polling processes.
	// 客户端轮询进程前的存活检查。
	s.mux.HandleFunc("/api/health", s.handleHealth)
	// Realtime process list from memory.
	// 来自内存的实时进程列表。
	s.mux.HandleFunc("/api/processes", s.handleProcesses)
	// Realtime socket details for one process_key.
	// 某 process_key 的实时套接字明细。
	s.mux.HandleFunc("/api/processes/connections", s.handleProcessConnections)
	// Historical process ranking from SQLite minute rollups.
	// 来自 SQLite 分钟聚合的历史进程排行。
	s.mux.HandleFunc("/api/processes/top", s.handleProcessesTop)
}

// handleHealth returns {"ok":true} for GET /api/health.
// handleHealth 对 GET /api/health 返回 {"ok":true}。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleProcesses returns the latest in-memory process summaries.
// handleProcesses 返回最新内存进程摘要。
func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	limit, err := s.limit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// No method check: ServeMux only matches path; GET is assumed by clients.
	// 未校验 method：ServeMux 只匹配路径；客户端约定 GET。
	writeJSON(w, s.state.LatestSummaries(limit))
}

// handleProcessConnections returns socket details for ?process_key=.
// handleProcessConnections 返回 ?process_key= 的套接字明细。
func (s *Server) handleProcessConnections(w http.ResponseWriter, r *http.Request) {
	processKey := r.URL.Query().Get("process_key")
	if processKey == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("process_key is required"))
		return
	}
	writeJSON(w, s.state.LatestConnections(processKey))
}

// handleProcessesTop queries historical process ranks for a time range.
// handleProcessesTop 按时间范围查询历史进程排行。
func (s *Server) handleProcessesTop(w http.ResponseWriter, r *http.Request) {
	limit, err := s.limit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// Default range is last 24 hours when omitted.
	// 省略时默认最近 24 小时。
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
	fetchLimit := limit
	if s.cfg.ExcludeSelf && limit > 0 {
		fetchLimit = limit + 5
	}
	items, err := s.store.TopProcessConnectionMinutes(now.Add(-d), now, fetchLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Drop historical bytepulse rows when exclude-self is enabled.
	// 开启 exclude-self 时去掉历史中的 bytepulse 行。
	items = storage.FilterSelfSummaries(items, s.cfg.ExcludeSelf)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	writeJSON(w, items)
}

// limit parses ?limit= or falls back to cfg.TopN (minimum 30 if TopN invalid).
// limit 解析 ?limit=，否则回退 cfg.TopN（TopN 无效时至少 30）。
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

// writeJSON encodes value as application/json with 200 status.
// writeJSON 以 application/json 编码 value，状态 200。
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

// writeError encodes {"error":"..."} with the given HTTP status.
// writeError 以给定 HTTP 状态编码 {"error":"..."}。
func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
