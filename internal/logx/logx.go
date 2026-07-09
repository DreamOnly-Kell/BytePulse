// Package logx is a thin slog wrapper for BytePulse diagnostics.
// logx 包是 BytePulse 诊断用的轻量 slog 封装。
//
// User-facing tables (status/report/processes) stay on stdout via fmt.
// Diagnostic logs go to stderr or an optional file so TUI/watch are not polluted.
// 用户结果表继续用 stdout；诊断日志走 stderr 或可选文件，避免干扰 TUI/watch。
package logx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Options configures the global logger.
// Options 配置全局日志器。
type Options struct {
	// Level is debug|info|warn|error (default error).
	// Level 为 debug|info|warn|error（默认 error）。
	Level string
	// Format is text|json (default text).
	// Format 为 text|json（默认 text）。
	Format string
	// File is an optional log file path; empty means stderr only.
	// File 为可选日志文件路径；空表示仅 stderr。
	File string
}

var (
	// mu protects logger and closer.
	// mu 保护 logger 与 closer。
	mu sync.Mutex
	// logger is the process-wide slog instance (discard until Init).
	// logger 为进程级 slog（Init 前为 discard）。
	logger *slog.Logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	// closer closes the log file if File was set.
	// closer 在设置了 File 时用于关闭日志文件。
	closer io.Closer

	// rateMu / rateLast implement WarnEvery throttling.
	// rateMu / rateLast 实现 WarnEvery 限频。
	rateMu   sync.Mutex
	rateLast = map[string]time.Time{}
)

// Init installs the global logger. Safe to call once at process start.
// Init 安装全局日志器；进程启动时调用一次即可。
func Init(opts Options) error {
	level, err := parseLevel(opts.Level)
	if err != nil {
		return err
	}
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return fmt.Errorf("unsupported log format %q; use text or json", opts.Format)
	}

	var w io.Writer = os.Stderr
	var c io.Closer
	if path := strings.TrimSpace(opts.File); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create log dir: %w", err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		w = f
		c = f
	}

	handlerOpts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(w, handlerOpts)
	} else {
		handler = slog.NewTextHandler(w, handlerOpts)
	}

	mu.Lock()
	if closer != nil {
		_ = closer.Close()
	}
	closer = c
	logger = slog.New(handler)
	mu.Unlock()

	// Confirm configuration at info+ (silent at default error level).
	// 在 info 及以上确认配置（默认 error 级别下不输出）。
	Info("logger initialized",
		"component", "logx",
		"level", strings.ToLower(strings.TrimSpace(opts.Level)),
		"format", format,
		"file", strings.TrimSpace(opts.File),
	)
	return nil
}

// Close releases the log file if one was opened.
// Close 关闭已打开的日志文件。
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if closer == nil {
		return nil
	}
	err := closer.Close()
	closer = nil
	return err
}

// L returns the global logger.
// L 返回全局日志器。
func L() *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	return logger
}

// With returns a child logger with attributes (e.g. component).
// With 返回带属性的子日志器（例如 component）。
func With(args ...any) *slog.Logger {
	return L().With(args...)
}

// Debug logs at debug level (per-tick sample sizes, API row counts, etc.).
// Debug 输出 debug 级别（每拍采样规模、API 行数等）。
func Debug(msg string, args ...any) { L().Debug(msg, args...) }

// Info logs at info level (lifecycle: start/stop, config loaded, flush).
// Info 输出 info 级别（生命周期：启停、配置加载、落库等）。
func Info(msg string, args ...any) { L().Info(msg, args...) }

// Warn logs at warn level (recoverable issues).
// Warn 输出 warn 级别（可恢复问题）。
func Warn(msg string, args ...any) { L().Warn(msg, args...) }

// Error logs at error level (failures that usually abort a path).
// Error 输出 error 级别（通常导致某条路径失败）。
func Error(msg string, args ...any) { L().Error(msg, args...) }

// WarnEvery logs a warning at most once per key within interval (rate limit).
// WarnEvery 对同一 key 在 interval 内最多打一条 warn（限频）。
func WarnEvery(interval time.Duration, key, msg string, args ...any) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	now := time.Now()
	rateMu.Lock()
	last, ok := rateLast[key]
	if ok && now.Sub(last) < interval {
		rateMu.Unlock()
		return
	}
	rateLast[key] = now
	rateMu.Unlock()
	Warn(msg, args...)
}

// Enabled reports whether level is enabled on the global logger.
// Enabled 报告全局日志器是否启用某级别。
func Enabled(level slog.Level) bool {
	return L().Enabled(context.Background(), level)
}

// parseLevel maps user strings to slog levels; empty defaults to error.
// parseLevel 将用户字符串映射为 slog 级别；空则默认为 error。
func parseLevel(text string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "error":
		return slog.LevelError, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q; use debug, info, warn, or error", text)
	}
}
