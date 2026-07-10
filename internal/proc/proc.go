// Package proc defines process connection sampling types and helpers.
// proc 包定义进程连接采样类型与辅助函数。
package proc

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotSupported is returned on platforms without a sampler implementation.
// ErrNotSupported 在无采样实现的平台上返回。
var ErrNotSupported = errors.New("process connection sampling is not supported on this platform")

// Connection is one network socket attributed to a process.
// Connection 是归属到某进程的一条网络套接字。
type Connection struct {
	// PID is the operating-system process id.
	// PID 是操作系统进程 ID。
	PID int
	// ProcessName is the short executable name (basename).
	// ProcessName 是短可执行名（basename）。
	ProcessName string
	// ProcessPath is the full path when available.
	// ProcessPath 是可用时的完整路径。
	ProcessPath string
	// ProcessKey uniquely tags a process instance (pid + create time).
	// ProcessKey 唯一标记进程实例（pid + 创建时间）。
	ProcessKey string
	// Protocol is typically "tcp" or "udp".
	// Protocol 通常为 "tcp" 或 "udp"。
	Protocol string
	// LocalAddr / LocalPort are the local endpoint.
	// LocalAddr / LocalPort 为本地端点。
	LocalAddr string
	LocalPort uint32
	// RemoteAddr / RemotePort are the peer endpoint (may be empty for listeners).
	// RemoteAddr / RemotePort 为对端端点（监听套接字可能为空）。
	RemoteAddr string
	RemotePort uint32
	// Status is the TCP state string when provided by the OS (e.g. ESTABLISHED).
	// Status 是操作系统提供的 TCP 状态字符串（如 ESTABLISHED）。
	Status string
	// SeenAt is when this sample was taken.
	// SeenAt 是本次采样时间。
	SeenAt time.Time
}

// ConnectionSampler abstracts platform-specific connection enumeration.
// ConnectionSampler 抽象平台相关的连接枚举。
type ConnectionSampler interface {
	// Sample returns the current set of process-attributed sockets.
	// Sample 返回当前带进程归属的套接字集合。
	Sample() ([]Connection, error)
}

// processKey builds the stable identity string "pid:createTimeMs".
// processKey 构建稳定身份字符串 "pid:createTimeMs"。
func processKey(pid int, createTimeMs int64) string {
	return fmt.Sprintf("%d:%d", pid, createTimeMs)
}

// processIdentity derives name, path, and process_key from a path + create time.
// processIdentity 从路径与创建时间推导 name、path、process_key。
func processIdentity(pid int, path string, createTimeMs int64) (string, string, string) {
	// Normalize whitespace from tools like `ps`.
	// 规范化来自 `ps` 等工具的空白。
	path = strings.TrimSpace(path)
	// Default name when path cannot be resolved.
	// 无法解析路径时的默认名称。
	name := "unknown"
	if path != "" {
		// Short name is the last path component.
		// 短名为路径最后一段。
		name = filepath.Base(path)
		// Guard against odd Base() results on empty/separator paths.
		// 防止空路径/分隔符路径下 Base() 的怪异结果。
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = path
		}
	}
	return name, path, processKey(pid, createTimeMs)
}

// dedupeKey collapses duplicate socket rows from the OS into one.
// dedupeKey 将操作系统返回的重复套接字行折叠为一条。
func dedupeKey(c Connection) string {
	return fmt.Sprintf("%d|%s|%s|%d|%s|%d|%s",
		c.PID,
		c.Protocol,
		c.LocalAddr,
		c.LocalPort,
		c.RemoteAddr,
		c.RemotePort,
		c.Status,
	)
}
