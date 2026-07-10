// Package config holds runtime configuration and range parsing helpers.
// config 包保存运行时配置以及时间范围解析辅助函数。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is the shared CLI / daemon / UI configuration.
// Config 是 CLI、daemon、UI 共用的配置结构。
type Config struct {
	// DBPath is the SQLite database file path.
	// DBPath 为 SQLite 数据库文件路径。
	DBPath string
	// PIDPath is the daemon PID file path.
	// PIDPath 为 daemon PID 文件路径。
	PIDPath string
	// Interface filters queries/collection to one NIC; empty means all non-loopback.
	// Interface 将查询/采集限定到某块网卡；空表示所有非回环网卡。
	Interface string
	// UseBits switches rate display from B/s to bits/s.
	// UseBits 将速率显示从 B/s 切换为 bits/s。
	UseBits bool
	// Retention is how long samples and process minutes are kept.
	// Retention 表示 samples 与进程分钟数据的保留时长。
	Retention time.Duration
	// TopN is the default row limit for process lists.
	// TopN 是进程列表的默认行数上限。
	TopN int
	// ProcessInterval is how often process connections are sampled.
	// ProcessInterval 是进程连接采样间隔。
	ProcessInterval time.Duration
	// DaemonAPIAddr is the local HTTP address for realtime process APIs.
	// DaemonAPIAddr 是进程实时 API 的本机 HTTP 地址。
	DaemonAPIAddr string
	// ProcessTraffic selects per-process traffic mode: off, auto, nettop, estats.
	// ProcessTraffic 选择每进程流量模式：off、auto、nettop、estats。
	ProcessTraffic string
	// ExcludeSelf hides the bytepulse process itself from process views (default true).
	// ExcludeSelf 为 true 时从进程视图中隐藏 bytepulse 自身（默认开启）。
	ExcludeSelf bool
	// LogLevel is debug|info|warn|error (default error).
	// LogLevel 为 debug|info|warn|error（默认 error）。
	LogLevel string
	// LogFormat is text|json (default text).
	// LogFormat 为 text|json（默认 text）。
	LogFormat string
	// LogFile is optional log path; empty writes to stderr.
	// LogFile 为可选日志路径；空则写 stderr。
	LogFile string
	// DaemonInterval is the interface sampling interval for `daemon` (default 1s).
	// DaemonInterval 是 `daemon` 网卡采样间隔（默认 1s）。
	DaemonInterval time.Duration
	// WebAddr is the default listen address for `web`.
	// WebAddr 是 `web` 的默认监听地址。
	WebAddr string
	// ConfigPath is the config file path that was loaded (empty if none).
	// ConfigPath 是已加载的配置文件路径（未加载则为空）。
	ConfigPath string
	// Lang is UI language: en or zh (logs stay English; help is bilingual in code).
	// Lang 为界面语言：en 或 zh（日志保持英文；help 在代码里中英双语写死）。
	Lang string
}

// Default returns sensible paths under ~/.bytepulse and safe feature defaults.
// Default 返回 ~/.bytepulse 下的合理路径以及安全的功能默认值。
func Default() Config {
	// Prefer the user home directory for state files.
	// 优先使用用户主目录存放状态文件。
	home, err := os.UserHomeDir()
	// Fall back to the current directory if home is unavailable.
	// 若无法获取主目录则回退到当前目录。
	if err != nil || home == "" {
		home = "."
	}
	// Keep DB/PID under a dedicated application state directory.
	// 将 DB/PID 放在专用应用状态目录下。
	stateDir := filepath.Join(home, ".bytepulse")
	return Config{
		// Default SQLite path.
		// 默认 SQLite 路径。
		DBPath: filepath.Join(stateDir, "bytepulse.db"),
		// Default PID file path for daemon lifecycle commands.
		// daemon 生命周期命令使用的默认 PID 路径。
		PIDPath: filepath.Join(stateDir, "bytepulse.pid"),
		// Keep about 30 days of history by default.
		// 默认保留约 30 天历史。
		Retention: 30 * 24 * time.Hour,
		// Show top 30 processes by default.
		// 默认展示前 30 个进程。
		TopN: 30,
		// Sample process connections every second (matches interface polling).
		// 每秒采样进程连接（与网卡轮询一致）。
		ProcessInterval: time.Second,
		// Bind daemon API to localhost only by default.
		// 默认仅将 daemon API 绑定到本机回环。
		DaemonAPIAddr: "127.0.0.1:8988",
		// Disable optional nettop attribution unless explicitly enabled.
		// 默认关闭可选的 nettop 流量归因，需显式开启。
		ProcessTraffic: "off",
		// Hide self from process lists by default.
		// 默认在进程列表中隐藏自身。
		ExcludeSelf: true,
		// Quiet by default; raise to info/debug when diagnosing.
		// 默认安静；排错时提高到 info/debug。
		LogLevel:  "error",
		LogFormat: "text",
		// Default daemon interface sample interval.
		// 默认 daemon 网卡采样间隔。
		DaemonInterval: time.Second,
		// Default web listen address.
		// 默认 web 监听地址。
		WebAddr: "127.0.0.1:8989",
		// Default UI language English.
		// 默认界面语言为英文。
		Lang: "en",
	}
}

// InterfaceLabel returns a human-readable label for reports.
// InterfaceLabel 返回用于报告展示的可读网卡标签。
func InterfaceLabel(name string) string {
	// Empty/whitespace means the collector aggregates all non-loopback NICs.
	// 空/空白表示采集器聚合所有非回环网卡。
	if strings.TrimSpace(name) == "" {
		return "all non-loopback interfaces"
	}
	// Otherwise show the concrete interface name.
	// 否则直接展示具体网卡名。
	return name
}

// ParseRange converts short range tokens (1h, 24h, 7d, ...) into a Duration.
// ParseRange 将短范围标记（1h、24h、7d 等）转换为 Duration。
func ParseRange(text string) (time.Duration, error) {
	// Normalize case and surrounding spaces before matching.
	// 匹配前统一大小写并去掉首尾空白。
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "1h":
		return time.Hour, nil
	case "2h":
		return 2 * time.Hour, nil
	case "3h":
		return 3 * time.Hour, nil
	case "5h":
		return 5 * time.Hour, nil
	case "10h":
		return 10 * time.Hour, nil
	case "12h":
		return 12 * time.Hour, nil
	// Accept both 24h and 1d as one day.
	// 同时接受 24h 与 1d 表示一天。
	case "24h", "1d":
		return 24 * time.Hour, nil
	case "2d":
		return 48 * time.Hour, nil
	case "3d":
		return 72 * time.Hour, nil
	// Accept both 7d and 1w as one week.
	// 同时接受 7d 与 1w 表示一周。
	case "7d", "1w":
		return 7 * 24 * time.Hour, nil
	case "15d":
		return 15 * 24 * time.Hour, nil
	default:
		// Unknown tokens fail fast with the allowed set in the error message.
		// 未知标记快速失败，并在错误信息中给出允许集合。
		return 0, fmt.Errorf("unsupported range %q; use one of 1h,2h,3h,5h,10h,12h,24h,2d,3d,7d,15d", text)
	}
}
