//go:build windows

// Windows process connection sampler using gopsutil (iphlpapi under the hood).
// 基于 gopsutil 的 Windows 进程连接采样器（底层为 iphlpapi 等）。
package proc

import (
	"time"

	"bytepulse/internal/logx"

	gopsnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// windowsSampler is the Windows ConnectionSampler implementation.
// windowsSampler 是 Windows 的 ConnectionSampler 实现。
type windowsSampler struct{}

// identity caches process name/path/key lookups per PID within one Sample().
// identity 在一次 Sample() 内按 PID 缓存进程名/路径/key。
type identity struct {
	name string
	path string
	key  string
}

// NewSampler returns the Windows sampler (build-tagged to windows only).
// NewSampler 返回 Windows 采样器（构建标签仅 windows）。
func NewSampler() ConnectionSampler {
	return windowsSampler{}
}

// Sample enumerates sockets via Connections("all"), same approach as macOS.
// Sample 通过 Connections("all") 枚举套接字，与 macOS 做法一致。
func (windowsSampler) Sample() ([]Connection, error) {
	now := time.Now()
	// Same entry point as darwin: gopsutil merges TCP/UDP tables for us.
	// 与 darwin 相同入口：gopsutil 负责合并 TCP/UDP 等表。
	stats, err := gopsnet.Connections("all")
	if err != nil {
		logx.Debug("windows Connections(all) failed", "component", "proc", "err", err)
		return nil, err
	}

	seen := map[string]bool{}
	identities := map[int]identity{}
	out := make([]Connection, 0, len(stats))
	skippedPID := 0
	for _, stat := range stats {
		if stat.Pid <= 0 {
			skippedPID++
			continue
		}
		pid := int(stat.Pid)
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
			Protocol:    protocolName(stat.Type, stat.Status),
			LocalAddr:   stat.Laddr.IP,
			LocalPort:   stat.Laddr.Port,
			RemoteAddr:  stat.Raddr.IP,
			RemotePort:  stat.Raddr.Port,
			Status:      stat.Status,
			SeenAt:      now,
		}
		dkey := dedupeKey(conn)
		if seen[dkey] {
			continue
		}
		seen[dkey] = true
		out = append(out, conn)
	}
	logx.Debug("windows connection sample",
		"component", "proc",
		"raw_stats", len(stats),
		"skipped_no_pid", skippedPID,
		"unique_conns", len(out),
		"unique_pids", len(identities),
	)
	return out, nil
}

// lookupProcessIdentity resolves path and create time via gopsutil/process.
// Failures degrade to unknown / process_key pid:0 (no admin required).
// lookupProcessIdentity 用 gopsutil/process 解析路径与创建时间。
// 失败时降级为 unknown / process_key 为 pid:0（不要求管理员）。
func lookupProcessIdentity(pid int) (string, string, string) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return processIdentity(pid, "", 0)
	}
	path, err := p.Exe()
	if err != nil {
		// Fall back to short name when full path is denied.
		// 完整路径被拒绝时回退短名。
		if name, nameErr := p.Name(); nameErr == nil && name != "" {
			path = name
		} else {
			return processIdentity(pid, "", 0)
		}
	}
	createTimeMs := int64(0)
	if ms, err := p.CreateTime(); err == nil && ms > 0 {
		createTimeMs = ms
	}
	return processIdentity(pid, path, createTimeMs)
}
