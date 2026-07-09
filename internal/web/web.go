// Package web serves the local HTTP dashboard and JSON APIs.
// web 包提供本机 HTTP 看板与 JSON API。
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bytepulse/internal/config"
	"bytepulse/internal/daemonclient"
	"bytepulse/internal/i18n"
	"bytepulse/internal/logx"
	"bytepulse/internal/processstate"
	"bytepulse/internal/storage"
)

// processClient is the subset of daemonclient used for realtime processes.
// processClient 是用于实时进程的 daemonclient 子集。
type processClient interface {
	Processes(context.Context, int) ([]processstate.ProcessConnectionSummary, error)
}

// Server holds SQLite access, config, routes, and the process API client.
// Server 持有 SQLite 访问、配置、路由与进程 API 客户端。
type Server struct {
	// store backs interface traffic endpoints. / store 支撑网卡流量相关端点。
	store *storage.Store
	// cfg supplies interface filter, bits mode, TopN, daemon API addr.
	// cfg 提供网卡过滤、bits 模式、TopN、daemon API 地址。
	cfg config.Config
	// mux is the HTTP router. / mux 是 HTTP 路由器。
	mux *http.ServeMux
	// processClient proxies /api/processes to the daemon.
	// processClient 将 /api/processes 代理到 daemon。
	processClient processClient
}

// New constructs the web server and registers routes.
// New 构造 Web 服务器并注册路由。
func New(store *storage.Store, cfg config.Config) *Server {
	s := &Server{
		store: store,
		cfg:   cfg,
		mux:   http.NewServeMux(),
		// Default process client points at the running daemon API.
		// 默认进程客户端指向正在运行的 daemon API。
		processClient: daemonclient.New(cfg.DaemonAPIAddr),
	}
	s.routes()
	return s
}

// ListenAndServe binds addr and serves until error/shutdown.
// ListenAndServe 绑定 addr 并服务直到错误/关闭。
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.mux)
}

// routes wires dashboard HTML and all JSON API paths.
// routes 挂接看板 HTML 与全部 JSON API 路径。
func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)                         // SPA page / 单页看板
	s.mux.HandleFunc("/api/realtime", s.handleRealtime)          // latest speed / 最新速率
	s.mux.HandleFunc("/api/summary", s.handleSummary)            // one range total / 单范围总量
	s.mux.HandleFunc("/api/ranges", s.handleRanges)              // all preset ranges / 全部预设范围
	s.mux.HandleFunc("/api/hourly", s.handleHourly)              // hourly buckets / 小时桶
	s.mux.HandleFunc("/api/daily", s.handleDaily)                // daily buckets / 日桶
	s.mux.HandleFunc("/api/series", s.handleSeries)              // chart series / 图表序列
	s.mux.HandleFunc("/api/processes", s.handleProcesses)        // realtime processes / 实时进程
	s.mux.HandleFunc("/api/processes/top", s.handleProcessesTop) // historical ranks / 历史排行
}

// handleIndex serves the embedded SPA; only exact "/" is valid.
// handleIndex 提供内嵌 SPA；仅精确路径 "/" 有效。
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Reject paths like /favicon.ico so they are not the dashboard HTML.
	// 拒绝 /favicon.ico 等路径，避免误返回看板 HTML。
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Inject runtime constants and UI language strings into the embedded dashboard.
	// 将运行时常量与界面语言文案注入内嵌看板。
	html := indexHTML
	replacements := map[string]string{
		"__USE_BITS__":         strconv.FormatBool(s.cfg.UseBits),
		"__DAEMON_API__":       s.cfg.DaemonAPIAddr,
		"__DAEMON_HINT__":      config.DaemonStartHint(s.cfg),
		"__L_DOWNLOAD__":       i18n.T("web.download"),
		"__L_UPLOAD__":         i18n.T("web.upload"),
		"__L_TOTAL__":          i18n.T("web.total"),
		"__L_WAITING__":        i18n.T("web.waiting_samples"),
		"__L_RANGE__":          i18n.T("web.range"),
		"__L_AVG_TOTAL__":      i18n.T("web.avg_total"),
		"__L_PROCESSES__":      i18n.T("web.processes"),
		"__L_PID__":            i18n.T("web.pid"),
		"__L_NAME__":           i18n.T("web.name"),
		"__L_PATH__":           i18n.T("web.path"),
		"__L_CONNS__":          i18n.T("web.connections"),
		"__L_RX__":             i18n.T("web.rx_s"),
		"__L_TX__":             i18n.T("web.tx_s"),
		"__L_LAST__":           i18n.T("web.last_seen"),
		"__L_PROC_WAITING__":   i18n.T("web.proc_waiting"),
		"__L_PROC_WAIT_BODY__": i18n.T("web.proc_wait_body"),
		"__L_PROC_EMPTY__":     i18n.T("web.proc_empty"),
		"__L_UPDATED_WAIT__":   i18n.T("web.updated_wait"),
		"__L_COPY__":           i18n.T("web.copy"),
		"__L_COPIED__":         i18n.T("web.copied"),
		"__L_COPY_PATH__":      i18n.T("web.copy_path"),
	}
	for k, v := range replacements {
		html = strings.ReplaceAll(html, k, v)
	}
	_, _ = w.Write([]byte(html))
}

// handleRealtime returns the latest combined interface sample as JSON.
// handleRealtime 以 JSON 返回最新合并网卡样本。
func (s *Server) handleRealtime(w http.ResponseWriter, r *http.Request) {
	iface := s.interfaceName(r)
	sample, err := s.store.LatestAggregateSample(iface)
	if err != nil {
		// Empty DB → 404 so the UI can show "waiting for samples".
		// 空库 → 404，UI 可显示 waiting for samples。
		logx.Debug("web realtime empty", "component", "web", "interface", iface, "err", err)
		writeError(w, http.StatusNotFound, err)
		return
	}
	logx.Debug("web realtime", "component", "web", "interface", sample.Interface, "rx_bps", sample.RXSpeedBps, "tx_bps", sample.TXSpeedBps)
	writeJSON(w, sample)
}

// handleSummary returns RX/TX totals for ?range= (default 24h).
// handleSummary 返回 ?range=（默认 24h）的 RX/TX 总量。
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
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
	summary, err := s.store.Summary(now.Add(-d), now, s.interfaceName(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, summary)
}

// handleRanges returns summaries for every built-in range label.
// handleRanges 返回每个内置范围标签的汇总。
func (s *Server) handleRanges(w http.ResponseWriter, r *http.Request) {
	labels := []string{"1h", "2h", "3h", "5h", "10h", "12h", "24h", "2d", "3d", "7d", "15d"}
	now := time.Now()
	items := make([]rangeResponse, 0, len(labels))
	for _, label := range labels {
		// ParseRange cannot fail for these hardcoded labels.
		// 这些硬编码标签不会 Parse 失败。
		d, _ := config.ParseRange(label)
		summary, err := s.store.Summary(now.Add(-d), now, s.interfaceName(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		items = append(items, rangeResponse{Range: label, Summary: summary})
	}
	writeJSON(w, items)
}

// handleHourly returns hourly buckets for the last 24 hours.
// handleHourly 返回最近 24 小时的小时桶。
func (s *Server) handleHourly(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	buckets, err := s.store.Hourly(now.Add(-24*time.Hour), now, s.interfaceName(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, buckets)
}

// handleDaily returns daily buckets for the last 15 days.
// handleDaily 返回最近 15 天的日桶。
func (s *Server) handleDaily(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	buckets, err := s.store.Daily(now.Add(-15*24*time.Hour), now, s.interfaceName(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, buckets)
}

// handleSeries returns the last hour of per-timestamp aggregates for charting.
// handleSeries 返回最近一小时按时间戳聚合的序列用于画图。
func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	series, err := s.store.RecentSeries(now.Add(-time.Hour), now, s.interfaceName(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, series)
}

// handleProcesses proxies realtime process rows from the daemon API.
// handleProcesses 将实时进程行从 daemon API 代理过来。
func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	limit, err := s.parseLimit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.processClient.Processes(r.Context(), limit)
	if err != nil {
		// 503 when daemon is not running or unreachable.
		// daemon 未运行或不可达时返回 503。
		logx.WarnEvery(30*time.Second, "web.processes.daemon", "web processes proxy failed", "component", "web", "err", err, "api", s.cfg.DaemonAPIAddr)
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("daemon API unavailable: %w", err))
		return
	}
	logx.Debug("web processes proxy ok", "component", "web", "rows", len(items), "limit", limit)
	writeJSON(w, items)
}

// handleProcessesTop returns historical process ranks from SQLite.
// handleProcessesTop 从 SQLite 返回历史进程排行。
func (s *Server) handleProcessesTop(w http.ResponseWriter, r *http.Request) {
	limit, err := s.parseLimit(r)
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
	fetchLimit := limit
	if s.cfg.ExcludeSelf && limit > 0 {
		fetchLimit = limit + 5
	}
	items, err := s.store.TopProcessConnectionMinutes(now.Add(-d), now, fetchLimit)
	if err != nil {
		logx.Error("web processes/top failed", "component", "web", "range", rangeText, "err", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Drop historical bytepulse rows when exclude-self is enabled.
	// 开启 exclude-self 时去掉历史中的 bytepulse 行。
	before := len(items)
	items = storage.FilterSelfSummaries(items, s.cfg.ExcludeSelf)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	logx.Debug("web processes/top", "component", "web", "range", rangeText, "raw", before, "rows", len(items))
	writeJSON(w, items)
}

// parseLimit reads ?limit= or falls back to cfg.TopN (default 30).
// parseLimit 读取 ?limit= 或回退 cfg.TopN（默认 30）。
func (s *Server) parseLimit(r *http.Request) (int, error) {
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

// interfaceName prefers ?interface= over the server-wide cfg.Interface.
// interfaceName 优先 ?interface=，否则用服务器级 cfg.Interface。
func (s *Server) interfaceName(r *http.Request) string {
	if v := r.URL.Query().Get("interface"); v != "" {
		return v
	}
	return s.cfg.Interface
}

// rangeResponse is one entry in /api/ranges.
// rangeResponse 是 /api/ranges 中的一条。
type rangeResponse struct {
	Range   string                `json:"range"`
	Summary storage.SummaryResult `json:"summary"`
}

// writeJSON encodes value as pretty-printed JSON.
// writeJSON 将 value 编码为美化 JSON。
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	// Indent for browser readability. / 缩进便于浏览器阅读。
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

// writeError returns a JSON error body with the given status.
// writeError 以给定状态返回 JSON 错误体。
func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// indexHTML is the embedded single-page dashboard (HTML/CSS/JS).
// indexHTML 是内嵌单页看板（HTML/CSS/JS）。
// Placeholder __USE_BITS__ is replaced at serve time with true/false.
// 占位符 __USE_BITS__ 在响应时替换为 true/false。
const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>BytePulse</title>
  <style>
    :root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; background: #f6f7f9; color: #171a1f; }
    header { padding: 18px 24px; border-bottom: 1px solid #d9dee7; background: #ffffff; display: flex; align-items: center; justify-content: space-between; }
    h1 { margin: 0; font-size: 20px; letter-spacing: 0; }
    main { max-width: 1120px; margin: 0 auto; padding: 24px; }
    .grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
    .panel { background: #ffffff; border: 1px solid #d9dee7; border-radius: 8px; padding: 16px; }
    .label { color: #697386; font-size: 13px; }
    .value { font-size: 28px; font-weight: 700; margin-top: 6px; overflow-wrap: anywhere; }
    .chart { margin-top: 12px; height: 320px; }
    canvas { width: 100%; height: 100%; display: block; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; table-layout: fixed; }
    th, td { padding: 9px 8px; border-bottom: 1px solid #e5e9f0; text-align: right; vertical-align: middle; }
    th:first-child, td:first-child { text-align: left; }
    th { color: #697386; font-weight: 600; }
    /* Process table column widths / 进程表列宽 */
    #processes-table th:nth-child(1), #processes-table td:nth-child(1) { width: 64px; }
    #processes-table th:nth-child(2), #processes-table td:nth-child(2) { width: 120px; text-align: left; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    #processes-table th:nth-child(3), #processes-table td:nth-child(3) { width: auto; text-align: left; }
    #processes-table th:nth-child(4), #processes-table td:nth-child(4) { width: 72px; }
    #processes-table th:nth-child(5), #processes-table td:nth-child(5) { width: 88px; }
    #processes-table th:nth-child(6), #processes-table td:nth-child(6) { width: 88px; }
    #processes-table th:nth-child(7), #processes-table td:nth-child(7) { width: 88px; }
    /* Path cell: truncate + ellipsis copy control / 路径截断与省略号复制控件 */
    .path-cell { text-align: left; max-width: 0; }
    .path-line { display: flex; align-items: center; gap: 2px; min-width: 0; width: 100%; }
    .path-text {
      overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; flex: 1 1 auto;
      cursor: default;
    }
    .path-more {
      flex: 0 0 auto; border: 1px solid transparent; background: transparent; color: #697386;
      font: inherit; font-weight: 600; padding: 1px 6px; border-radius: 4px; cursor: pointer;
      line-height: 1.4; min-width: 1.5em;
    }
    .path-more:hover, .path-more:focus-visible {
      border-color: #c5cdd8; background: #eef2ff; color: #2563eb;
    }
    .path-more.is-copy { min-width: 3.2em; }
    .path-more.is-copied { color: #0f766e; border-color: #99f6e4; background: #ecfdf5; }
    .path-line:not(.is-long) .path-more { display: none; }
    @media (max-width: 760px) { .grid { grid-template-columns: 1fr; } main { padding: 14px; } .value { font-size: 24px; } }
  </style>
</head>
<body>
  <header>
    <h1>BytePulse</h1>
    <div class="label" id="updated">__L_WAITING__</div>
  </header>
  <main>
    <section class="grid">
      <div class="panel"><div class="label">__L_DOWNLOAD__</div><div class="value" id="down">--</div></div>
      <div class="panel"><div class="label">__L_UPLOAD__</div><div class="value" id="up">--</div></div>
      <div class="panel"><div class="label">__L_TOTAL__</div><div class="value" id="total">--</div></div>
    </section>
    <section class="panel chart">
      <canvas id="chart" width="1000" height="320"></canvas>
    </section>
    <section class="panel" style="margin-top:12px">
      <table>
        <thead><tr><th>__L_RANGE__</th><th>__L_DOWNLOAD__</th><th>__L_UPLOAD__</th><th>__L_TOTAL__</th><th>__L_AVG_TOTAL__</th></tr></thead>
        <tbody id="ranges"></tbody>
      </table>
    </section>
    <section class="panel" style="margin-top:12px">
      <div class="label" id="process-status">__L_PROCESSES__</div>
      <table id="processes-table">
        <thead><tr><th>__L_PID__</th><th>__L_NAME__</th><th>__L_PATH__</th><th>__L_CONNS__</th><th>__L_RX__</th><th>__L_TX__</th><th>__L_LAST__</th></tr></thead>
        <tbody id="processes"></tbody>
      </table>
    </section>
  </main>
  <script>
    const useBits = __USE_BITS__;
    const daemonAPI = "__DAEMON_API__";
    const daemonHint = "__DAEMON_HINT__";
    const L_PROCESSES = "__L_PROCESSES__";
    const L_PROC_WAITING = "__L_PROC_WAITING__";
    const L_PROC_WAIT_BODY = "__L_PROC_WAIT_BODY__";
    const L_PROC_EMPTY = "__L_PROC_EMPTY__";
    const L_UPDATED_WAIT = "__L_UPDATED_WAIT__";
    const L_DOWNLOAD = "__L_DOWNLOAD__";
    const L_UPLOAD = "__L_UPLOAD__";
    const L_COPY = "__L_COPY__";
    const L_COPIED = "__L_COPIED__";
    const L_COPY_PATH = "__L_COPY_PATH__";
    const fmtBytes = (v) => fmt(v, ["B", "KB", "MB", "GB", "TB"]);
    const fmtRate = (v) => useBits ? fmt(v * 8, ["b/s", "Kb/s", "Mb/s", "Gb/s", "Tb/s"]) : fmt(v, ["B/s", "KB/s", "MB/s", "GB/s", "TB/s"]);
    let lastProcessKey = "";
    function fmt(v, labels) {
      let i = 0;
      while (v >= 1024 && i < labels.length - 1) { v /= 1024; i++; }
      return (i === 0 ? v.toFixed(0) : v >= 100 ? v.toFixed(0) : v >= 10 ? v.toFixed(1) : v.toFixed(2)) + " " + labels[i];
    }
    function escapeHtml(s) {
      return String(s)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#39;");
    }
    function escapeAttr(s) {
      return escapeHtml(s).replace(/\u0060/g, "&#96;");
    }
    // Path cell: CSS ellipsis + title tip; "…" becomes Copy on hover.
    // 路径单元格：CSS 省略 + title 提示；悬停「…」变为复制。
    function renderPathCell(path) {
      const full = path || "";
      const safe = escapeHtml(full);
      const attr = escapeAttr(full);
      return '<td class="path-cell"><span class="path-line" data-full="' + attr + '">' +
        '<span class="path-text" title="' + attr + '">' + safe + '</span>' +
        '<button type="button" class="path-more" data-path="' + attr + '" title="' + escapeAttr(L_COPY_PATH) + '" aria-label="' + escapeAttr(L_COPY_PATH) + '">…</button>' +
        '</span></td>';
    }
    function markLongPaths(root) {
      root.querySelectorAll(".path-line").forEach(line => {
        const text = line.querySelector(".path-text");
        if (!text) return;
        // Show "…" control only when text is visually truncated.
        // 仅当文字被视觉截断时显示「…」控件。
        if (text.scrollWidth > text.clientWidth + 1) {
          line.classList.add("is-long");
        } else {
          line.classList.remove("is-long");
        }
      });
    }
    function bindPathInteractions(root) {
      root.querySelectorAll(".path-more").forEach(btn => {
        btn.addEventListener("mouseenter", () => {
          if (!btn.classList.contains("is-copied")) {
            btn.classList.add("is-copy");
            btn.textContent = L_COPY;
          }
        });
        btn.addEventListener("mouseleave", () => {
          btn.classList.remove("is-copy");
          if (!btn.classList.contains("is-copied")) {
            btn.textContent = "…";
          } else {
            // After leave, restore ellipsis unless we reset soon.
            // 离开后恢复省略号（若刚复制会短暂显示已复制）。
            setTimeout(() => {
              if (!btn.matches(":hover")) {
                btn.classList.remove("is-copied");
                btn.textContent = "…";
              }
            }, 800);
          }
        });
        btn.addEventListener("click", async (ev) => {
          ev.preventDefault();
          ev.stopPropagation();
          const path = btn.getAttribute("data-path") || "";
          try {
            if (navigator.clipboard && navigator.clipboard.writeText) {
              await navigator.clipboard.writeText(path);
            } else {
              const ta = document.createElement("textarea");
              ta.value = path;
              ta.style.position = "fixed";
              ta.style.left = "-9999px";
              document.body.appendChild(ta);
              ta.select();
              document.execCommand("copy");
              document.body.removeChild(ta);
            }
            btn.classList.add("is-copied", "is-copy");
            btn.textContent = L_COPIED;
          } catch (e) {
            btn.textContent = "…";
          }
        });
      });
    }
    function drawChart(series) {
      const canvas = document.getElementById("chart");
      const rect = canvas.getBoundingClientRect();
      const ratio = window.devicePixelRatio || 1;
      canvas.width = Math.max(600, Math.floor(rect.width * ratio));
      canvas.height = Math.max(260, Math.floor(rect.height * ratio));
      const ctx = canvas.getContext("2d");
      ctx.scale(ratio, ratio);
      const w = canvas.width / ratio;
      const h = canvas.height / ratio;
      ctx.clearRect(0, 0, w, h);
      ctx.fillStyle = "#ffffff";
      ctx.fillRect(0, 0, w, h);
      const pad = { left: 56, right: 16, top: 18, bottom: 32 };
      const plotW = w - pad.left - pad.right;
      const plotH = h - pad.top - pad.bottom;
      const down = series.map(s => s.rx_speed_bps || 0);
      const up = series.map(s => s.tx_speed_bps || 0);
      const peak = Math.max(1, ...down, ...up);

      ctx.strokeStyle = "#e5e9f0";
      ctx.lineWidth = 1;
      ctx.fillStyle = "#697386";
      ctx.font = "12px system-ui, sans-serif";
      for (let i = 0; i <= 4; i++) {
        const y = pad.top + plotH * i / 4;
        ctx.beginPath();
        ctx.moveTo(pad.left, y);
        ctx.lineTo(w - pad.right, y);
        ctx.stroke();
        ctx.fillText(fmtRate(peak * (1 - i / 4)), 8, y + 4);
      }

      const draw = (values, color) => {
        ctx.strokeStyle = color;
        ctx.lineWidth = 2;
        ctx.beginPath();
        values.forEach((v, i) => {
          const x = pad.left + (values.length <= 1 ? 0 : plotW * i / (values.length - 1));
          const y = pad.top + plotH - (v / peak) * plotH;
          if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
        });
        ctx.stroke();
      };
      draw(down, "#2563eb");
      draw(up, "#0f766e");

      ctx.fillStyle = "#2563eb";
      ctx.fillRect(pad.left, h - 18, 10, 10);
      ctx.fillStyle = "#171a1f";
      ctx.fillText(L_DOWNLOAD, pad.left + 16, h - 9);
      ctx.fillStyle = "#0f766e";
      ctx.fillRect(pad.left + 110, h - 18, 10, 10);
      ctx.fillStyle = "#171a1f";
      ctx.fillText(L_UPLOAD, pad.left + 126, h - 9);
    }
    function fillTpl(s, map) {
      return s.replace(/\{(\w+)\}/g, (_, k) => (map[k] != null ? map[k] : ""));
    }
    async function load() {
      try {
        const realtime = await fetch("/api/realtime").then(r => r.json());
        document.getElementById("down").textContent = fmtRate(realtime.rx_speed_bps || 0);
        document.getElementById("up").textContent = fmtRate(realtime.tx_speed_bps || 0);
        document.getElementById("total").textContent = fmtRate((realtime.rx_speed_bps || 0) + (realtime.tx_speed_bps || 0));
        document.getElementById("updated").textContent = new Date(realtime.timestamp).toLocaleString();

        const series = await fetch("/api/series").then(r => r.json());
        drawChart(series);

        const ranges = await fetch("/api/ranges").then(r => r.json());
        document.getElementById("ranges").innerHTML = ranges.map(row => {
          const s = row.summary;
          const total = (s.rx_bytes || 0) + (s.tx_bytes || 0);
          const avg = total / Math.max(1, s.duration_sec || 1);
          return "<tr><td>" + row.range + "</td><td>" + fmtBytes(s.rx_bytes || 0) + "</td><td>" + fmtBytes(s.tx_bytes || 0) + "</td><td>" + fmtBytes(total) + "</td><td>" + fmtRate(avg) + "</td></tr>";
        }).join("");

        const processResp = await fetch("/api/processes");
        if (!processResp.ok) {
          lastProcessKey = "";
          document.getElementById("process-status").textContent = fillTpl(L_PROC_WAITING, {api: daemonAPI});
          document.getElementById("processes").innerHTML =
            "<tr><td colspan=\"7\">" + fillTpl(L_PROC_WAIT_BODY, {cmd: "<code>" + daemonHint + "</code>"}) + "</td></tr>";
        } else {
          const processes = await processResp.json();
          // Skip DOM rebuild when payload unchanged so hover/copy is not interrupted.
          // 数据未变时跳过 DOM 重建，避免打断悬停与复制。
          const key = JSON.stringify(processes);
          if (key !== lastProcessKey) {
            lastProcessKey = key;
            document.getElementById("process-status").textContent = L_PROCESSES;
            const tbody = document.getElementById("processes");
            tbody.innerHTML = processes.length === 0
              ? "<tr><td colspan=\"7\">" + L_PROC_EMPTY + "</td></tr>"
              : processes.map(p => {
                  const hasTraffic = !!p.traffic_available;
                  const path = p.process_path || p.process_name || "";
                  const name = escapeHtml(p.process_name || "unknown");
                  return "<tr><td>" + (p.pid || 0) + "</td><td title=\"" + name + "\">" + name + "</td>" +
                    renderPathCell(path) +
                    "<td>" + (p.connection_count || 0) + "</td><td>" + (hasTraffic ? fmtRate(p.rx_bps || 0) : "--") + "</td><td>" + (hasTraffic ? fmtRate(p.tx_bps || 0) : "--") + "</td><td>" + new Date(p.last_seen).toLocaleTimeString() + "</td></tr>";
                }).join("");
            markLongPaths(tbody);
            bindPathInteractions(tbody);
          } else {
            markLongPaths(document.getElementById("processes"));
          }
        }
      } catch (err) {
        document.getElementById("updated").textContent = fillTpl(L_UPDATED_WAIT, {cmd: daemonHint});
      }
    }
    load();
    setInterval(load, 1000);
    window.addEventListener("resize", () => {
      markLongPaths(document.getElementById("processes"));
      load();
    });
  </script>
</body>
</html>`
