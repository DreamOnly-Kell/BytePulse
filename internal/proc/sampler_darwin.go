//go:build darwin

package proc

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	gopsnet "github.com/shirou/gopsutil/v4/net"
)

type darwinSampler struct{}

type identity struct {
	name string
	path string
	key  string
}

func NewSampler() ConnectionSampler {
	return darwinSampler{}
}

func (darwinSampler) Sample() ([]Connection, error) {
	now := time.Now()
	stats, err := gopsnet.Connections("all")
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	identities := map[int]identity{}
	out := make([]Connection, 0, len(stats))
	for _, stat := range stats {
		if stat.Pid <= 0 {
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
	return out, nil
}

func lookupProcessIdentity(pid int) (string, string, string) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return processIdentity(pid, "", 0)
	}
	path := strings.TrimSpace(string(out))
	createTimeMs := lookupCreateTime(pid)
	return processIdentity(pid, path, createTimeMs)
}

func lookupCreateTime(pid int) int64 {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return 0
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return 0
	}
	start, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", text, time.Local)
	if err != nil {
		return 0
	}
	return start.UnixMilli()
}

func protocolName(socketType uint32, status string) string {
	switch socketType {
	case syscall.SOCK_STREAM:
		return "tcp"
	case syscall.SOCK_DGRAM:
		return "udp"
	default:
		if status != "" {
			return "tcp"
		}
		return "udp"
	}
}
