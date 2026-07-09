package i18n

// en holds English UI strings (not used for logx messages).
// en 为英文界面文案（logx 日志不使用本表）。
var en = map[string]string{
	// TUI — wait for daemon / TUI 等待后台
	"tui.wait.title":           "BytePulse",
	"tui.wait.headline":        "Background collector is not running.",
	"tui.wait.need_api":        "Realtime process data requires the daemon API:",
	"tui.wait.start_other":     "In another terminal, start:",
	"tui.wait.waiting":         "Waiting for daemon... ({seconds}s)",
	"tui.wait.footer":          "This screen refreshes every second. q: quit",
	"tui.main.footer":          "tab: switch view | q: quit",
	"tui.main.no_samples":      "No interface samples yet: {err}",
	"tui.main.wait_samples":    "Waiting for the collector to write samples...",
	"tui.main.loading":         "Loading...",
	"tui.main.last60":          "Last 60 seconds",
	"tui.main.windows":         "Traffic windows",
	"tui.proc.title":           "BytePulse [Processes]",
	"tui.proc.api_error":       "Daemon API error. Is the collector still running?",
	"tui.proc.start":           "Start: {cmd}",
	"tui.proc.waiting_data":    "Waiting for process connection data...",
	"tui.proc.header":          "PID      NAME             CONNS  RX/s        TX/s        LAST     PATH",
	"tui.rate.download":        "Download",
	"tui.rate.upload":          "Upload",
	"tui.rate.total":           "Total",
	"tui.rate.updated":         "Updated",

	// CLI user-facing / CLI 用户可见
	"cli.daemon_down": "daemon is not running (API {api})\nstart the background collector first, then retry:\n  {cmd}",
	"cli.daemon_err":  "daemon API error: {err}\nensure the collector is running:\n  {cmd}",
	"cli.web_no_daemon": "warning: daemon is not running (API {api}); process view will wait until it is up.\n  start: {cmd}\n",
	"cli.daemon_already_running": "another bytepulse daemon is already running (pid {pid}, API {api})\nOnly one collector is allowed. Stop it first, then start again:\n  {stop}\n  {start}",
	"cli.daemon_already_running_nopid": "another bytepulse daemon is already running (API {api})\nOnly one collector is allowed. Stop it first, then start again:\n  {stop}\n  {start}",


	// Web labels (injected into HTML) / Web 标签
	"web.download":         "Download",
	"web.upload":           "Upload",
	"web.total":            "Total",
	"web.waiting_samples":  "waiting for samples",
	"web.range":            "Range",
	"web.avg_total":        "Avg total",
	"web.processes":        "Processes",
	"web.pid":              "PID",
	"web.name":             "Name",
	"web.path":             "Path",
	"web.connections":      "Connections",
	"web.rx_s":             "RX/s",
	"web.tx_s":             "TX/s",
	"web.last_seen":        "Last Seen",
	"web.proc_waiting":     "Processes — waiting for daemon ({api})",
	"web.proc_wait_body":   "Background collector is not running. Start it in a terminal: {cmd} — this table will fill automatically once the daemon is up.",
	"web.proc_empty":       "No process connection samples yet",
	"web.updated_wait":     "waiting for samples — start: {cmd}",
	"web.copy":             "Copy",
	"web.copied":           "Copied",
	"web.copy_path":        "Copy path",
}
