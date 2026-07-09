//go:build darwin

// macOS process connection sampler using gopsutil + ps.
// 基于 gopsutil + ps 的 macOS 进程连接采样器。
package proc

import (
	"os/exec"
	"strconv"
	"strings"
	"time"

	"bytepulse/internal/logx"

	gopsnet "github.com/shirou/gopsutil/v4/net"
)

// darwinSampler is the macOS ConnectionSampler implementation.
// darwinSampler 是 macOS 的 ConnectionSampler 实现。
type darwinSampler struct{}

// identity caches process name/path/key lookups per PID within one Sample().
// identity 在一次 Sample() 内按 PID 缓存进程名/路径/key 查询结果。
type identity struct {
	name string
	path string
	key  string
}

// NewSampler returns the macOS sampler (build-tagged to darwin only).
// NewSampler 返回 macOS 采样器（构建标签仅 darwin）。
func NewSampler() ConnectionSampler {
	return darwinSampler{}
}

// Sample enumerates sockets, attributes them to PIDs, and deduplicates.
// Sample 枚举套接字、归属到 PID，并去重。
func (darwinSampler) Sample() ([]Connection, error) {
	// Shared SeenAt for all rows in this sample batch.
	// 本批样本所有行共用的 SeenAt。
	now := time.Now()
	// "all" requests TCP+UDP (and related) connection stats with PIDs.
	// "all" 请求带 PID 的 TCP+UDP（及相关）连接统计。
	stats, err := gopsnet.Connections("all")
	if err != nil {
		logx.Debug("darwin Connections(all) failed", "component", "proc", "err", err)
		return nil, err
	}

	// seen dedupes identical 5-tuples (+ status) within this sample.
	// seen 在本样本内对相同五元组（+ 状态）去重。
	seen := map[string]bool{}
	// identities avoids repeated `ps` calls for the same PID.
	// identities 避免对同一 PID 重复调用 `ps`。
	identities := map[int]identity{}
	out := make([]Connection, 0, len(stats))
	skippedPID := 0
	for _, stat := range stats {
		// Skip kernel/unowned sockets without a PID.
		// 跳过无 PID 的内核/无主套接字。
		if stat.Pid <= 0 {
			skippedPID++
			continue
		}
		pid := int(stat.Pid)
		// Resolve process identity once per PID.
		// 每个 PID 只解析一次进程身份。
		id, ok := identities[pid]
		if !ok {
			name, path, key := lookupProcessIdentity(pid)
			id = identity{name: name, path: path, key: key}
			identities[pid] = id
		}
		conn := Connection{
			PID:         pid,
			ProcessName: id.name,
			ProcessPath: id.path,
			ProcessKey:  id.key,
			// Map SOCK_STREAM/DGRAM to tcp/udp labels.
			// 将 SOCK_STREAM/DGRAM 映射为 tcp/udp 标签。
			Protocol:   protocolName(stat.Type, stat.Status),
			LocalAddr:  stat.Laddr.IP,
			LocalPort:  stat.Laddr.Port,
			RemoteAddr: stat.Raddr.IP,
			RemotePort: stat.Raddr.Port,
			Status:     stat.Status,
			SeenAt:     now,
		}
		// Drop duplicates (OS APIs sometimes report the same socket twice).
		// 丢弃重复项（OS API 有时会重复报告同一套接字）。
		dkey := dedupeKey(conn)
		if seen[dkey] {
			continue
		}
		seen[dkey] = true
		out = append(out, conn)
	}
	logx.Debug("darwin connection sample",
		"component", "proc",
		"raw_stats", len(stats),
		"skipped_no_pid", skippedPID,
		"unique_conns", len(out),
		"unique_pids", len(identities),
	)
	return out, nil
}

// lookupProcessIdentity runs `ps` for the executable path and create time.
// lookupProcessIdentity 用 `ps` 查询可执行路径与创建时间。
func lookupProcessIdentity(pid int) (string, string, string) {
	// `comm=` prints the command path/name for the PID.
	// `comm=` 打印该 PID 的命令路径/名称。
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		// Process may have exited between connection list and lookup.
		// 进程可能在连接列表与查询之间已退出。
		return processIdentity(pid, "", 0)
	}
	path := strings.TrimSpace(string(out))
	createTimeMs := lookupCreateTime(pid)
	return processIdentity(pid, path, createTimeMs)
}

// lookupCreateTime parses `ps -o lstart=` into unix milliseconds.
// lookupCreateTime 将 `ps -o lstart=` 解析为 Unix 毫秒。
func lookupCreateTime(pid int) int64 {
	// lstart is a human-readable start time; used to disambiguate PID reuse.
	// lstart 是人类可读启动时间；用于区分 PID 复用。
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return 0
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return 0
	}
	// macOS lstart format example: "Mon Jul  7 15:04:05 2026".
	// macOS lstart 格式示例："Mon Jul  7 15:04:05 2026"。
	start, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", text, time.Local)
	if err != nil {
		return 0
	}
	return start.UnixMilli()
}
