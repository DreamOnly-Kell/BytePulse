//go:build windows

// Windows per-process traffic via TCP connection ESTATS (iphlpapi).
// 通过 TCP 连接 ESTATS（iphlpapi）实现 Windows 每进程流量。
package proctraffic

import (
	"context"
	"fmt"
	"time"
	"unsafe"

	"bytepulse/internal/logx"

	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows"
)

const (
	afINET  = 2
	afINET6 = 23

	// TCP_TABLE_OWNER_PID_ALL from iprtrmib.h
	tcpTableOwnerPIDAll = 5

	// TcpConnectionEstatsData from tcpestats.h
	tcpConnectionEstatsData = 1

	errInsufficientBuffer = 122
)

var (
	modIphlpapi                    = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable        = modIphlpapi.NewProc("GetExtendedTcpTable")
	procGetPerTcpConnectionEStats  = modIphlpapi.NewProc("GetPerTcpConnectionEStats")
	procSetPerTcpConnectionEStats  = modIphlpapi.NewProc("SetPerTcpConnectionEStats")
	procGetPerTcp6ConnectionEStats = modIphlpapi.NewProc("GetPerTcp6ConnectionEStats")
	procSetPerTcp6ConnectionEStats = modIphlpapi.NewProc("SetPerTcp6ConnectionEStats")
)

// mibTCPRow matches MIB_TCPROW (IPv4) used by ESTATS APIs.
// mibTCPRow 对应 ESTATS API 使用的 MIB_TCPROW（IPv4）。
type mibTCPRow struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
}

// mibTCPRowOwnerPID is one row from TCP_TABLE_OWNER_PID_*.
// mibTCPRowOwnerPID 是 TCP_TABLE_OWNER_PID_* 中的一行。
type mibTCPRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPid  uint32
}

// mibTCP6RowOwnerPID is one IPv6 owner-PID TCP row (table layout).
// Note: field order differs from MIB_TCP6ROW used by ESTATS APIs.
// mibTCP6RowOwnerPID 是 IPv6 带 PID 的 TCP 表行布局。
// 注意：字段顺序与 ESTATS 使用的 MIB_TCP6ROW 不同。
type mibTCP6RowOwnerPID struct {
	LocalAddr     [16]byte
	LocalScopeId  uint32
	LocalPort     uint32
	RemoteAddr    [16]byte
	RemoteScopeId uint32
	RemotePort    uint32
	State         uint32
	OwningPid     uint32
}

// mibTCP6Row matches MIB_TCP6ROW for GetPerTcp6ConnectionEStats.
// mibTCP6Row 对应 GetPerTcp6ConnectionEStats 的 MIB_TCP6ROW。
type mibTCP6Row struct {
	State         uint32
	LocalAddr     [16]byte
	LocalScopeId  uint32
	LocalPort     uint32
	RemoteAddr    [16]byte
	RemoteScopeId uint32
	RemotePort    uint32
}

// tcpEstatsDataRW enables data-stats collection (TCP_ESTATS_DATA_RW_v0 = BOOLEAN).
// tcpEstatsDataRW 启用数据统计采集（TCP_ESTATS_DATA_RW_v0 即 BOOLEAN）。
type tcpEstatsDataRW struct {
	EnableCollection byte
}

// tcpEstatsDataROD is read-only data stats (TCP_ESTATS_DATA_ROD_v0).
// tcpEstatsDataROD 为只读数据统计（TCP_ESTATS_DATA_ROD_v0）。
type tcpEstatsDataROD struct {
	DataBytesOut    uint64
	DataBytesIn     uint64
	DataSegmentsOut uint64
	DataSegmentsIn  uint64
	DataDupAcksIn   uint64
	SoftErrorsRcvd  uint64
	SoftErrorsSent  uint64
}

// estatsAttributor polls TCP ESTATS and aggregates bytes by PID.
// estatsAttributor 轮询 TCP ESTATS 并按 PID 聚合字节。
type estatsAttributor struct {
	interval time.Duration
}

// NewEstatsAttributor builds the Windows TCP-ESTATS traffic backend.
// NewEstatsAttributor 构建 Windows TCP-ESTATS 流量后端。
func NewEstatsAttributor() Attributor {
	return &estatsAttributor{interval: time.Second}
}

// NewAttributor selects a Windows traffic backend from mode.
// NewAttributor 按 mode 选择 Windows 流量后端。
// Supported: auto, on, estats. nettop is macOS-only.
// 支持：auto、on、estats。nettop 仅 macOS。
func NewAttributor(mode string) (Attributor, error) {
	switch mode {
	case "auto", "on", "estats":
		return NewEstatsAttributor(), nil
	case "nettop":
		return nil, fmt.Errorf("process traffic mode %q is only supported on macOS; use auto or estats on Windows", mode)
	case "off", "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported process traffic mode %q; use off, auto, or estats", mode)
	}
}

// NewNettopAttributor is kept for API symmetry; on Windows use NewAttributor("estats").
// NewNettopAttributor 为 API 对称保留；Windows 上请用 NewAttributor("estats")。
func NewNettopAttributor() Attributor {
	return NewEstatsAttributor()
}

// Run samples about once per second until ctx is cancelled.
// Run 约每秒采样一次，直到 ctx 取消。
func (a *estatsAttributor) Run(ctx context.Context, onSample func([]Sample)) error {
	if a.interval <= 0 {
		a.interval = time.Second
	}
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	var prev map[int]pidByteCounters
	var prevAt time.Time

	logx.Info("estats attributor starting", "component", "proctraffic", "interval", a.interval.String())
	// Prime baseline without emitting rates.
	// 先取基线，不产出速率。
	if counters, meta, err := snapshotPIDCounters(); err == nil {
		prev = counters
		prevAt = time.Now()
		logx.Info("estats baseline ready", "component", "proctraffic", "pids", len(counters))
		_ = meta
	} else {
		logx.Warn("estats baseline failed", "component", "proctraffic", "err", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			curr, meta, err := snapshotPIDCounters()
			if err != nil {
				logx.WarnEvery(30*time.Second, "estats.snapshot", "estats snapshot failed", "component", "proctraffic", "err", err)
				continue
			}
			if prev != nil && !prevAt.IsZero() {
				dt := now.Sub(prevAt).Seconds()
				samples := computeRateSamples(prev, curr, dt, now, "estats", meta)
				logx.Debug("estats rates computed", "component", "proctraffic", "pids", len(curr), "samples", len(samples))
				if len(samples) > 0 && onSample != nil {
					onSample(samples)
				}
			}
			prev = curr
			prevAt = now
		}
	}
}

// snapshotPIDCounters sums TCP ESTATS data bytes per owning PID (IPv4+IPv6).
// snapshotPIDCounters 按拥有 PID 汇总 TCP ESTATS 数据字节（IPv4+IPv6）。
func snapshotPIDCounters() (map[int]pidByteCounters, map[int]processMeta, error) {
	out := map[int]pidByteCounters{}
	meta := map[int]processMeta{}

	if err := accumulateIPv4(out); err != nil {
		logx.Debug("estats ipv4 accumulate failed", "component", "proctraffic", "err", err)
		// Still try IPv6 if IPv4 table fails.
		// IPv4 表失败时仍尝试 IPv6。
	}
	if err := accumulateIPv6(out); err != nil {
		logx.Debug("estats ipv6 accumulate failed", "component", "proctraffic", "err", err)
	}

	if len(out) == 0 {
		// Distinguish empty host vs total failure: return empty map, nil error.
		// 区分空主机与全失败：返回空 map、nil error。
		logx.Debug("estats snapshot empty", "component", "proctraffic")
		return out, meta, nil
	}

	for pid := range out {
		meta[pid] = lookupMeta(pid)
	}
	logx.Debug("estats snapshot", "component", "proctraffic", "pids_with_bytes", len(out))
	return out, meta, nil
}

func accumulateIPv4(out map[int]pidByteCounters) error {
	rows, err := tcpOwnerPIDTable(afINET)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.OwningPid == 0 {
			continue
		}
		// Only established-ish connections usually have useful data stats.
		// 通常仅类 ESTABLISHED 连接有有效数据统计。
		if row.State != 5 { // MIB_TCP_STATE_ESTAB = 5
			continue
		}
		base := mibTCPRow{
			State:      row.State,
			LocalAddr:  row.LocalAddr,
			LocalPort:  row.LocalPort,
			RemoteAddr: row.RemoteAddr,
			RemotePort: row.RemotePort,
		}
		in, outb, ok := tcp4EstatsBytes(base)
		if !ok {
			continue
		}
		pid := int(row.OwningPid)
		c := out[pid]
		c.RX += in
		c.TX += outb
		out[pid] = c
	}
	return nil
}

func accumulateIPv6(out map[int]pidByteCounters) error {
	rows, err := tcp6OwnerPIDTable()
	if err != nil {
		return err
	}
	for i := range rows {
		row := &rows[i]
		if row.OwningPid == 0 || row.State != 5 {
			continue
		}
		in, outb, ok := tcp6EstatsBytes(row)
		if !ok {
			continue
		}
		pid := int(row.OwningPid)
		c := out[pid]
		c.RX += in
		c.TX += outb
		out[pid] = c
	}
	return nil
}

func tcpOwnerPIDTable(af uint32) ([]mibTCPRowOwnerPID, error) {
	var size uint32
	r0, _, _ := procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 1, uintptr(af), tcpTableOwnerPIDAll, 0)
	if r0 != errInsufficientBuffer && r0 != 0 {
		return nil, fmt.Errorf("GetExtendedTcpTable size: %w", windows.Errno(r0))
	}
	for retries := 0; retries < 3; retries++ {
		buf := make([]byte, size)
		r0, _, _ = procGetExtendedTcpTable.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&size)),
			1,
			uintptr(af),
			tcpTableOwnerPIDAll,
			0,
		)
		if r0 == errInsufficientBuffer {
			continue
		}
		if r0 != 0 {
			return nil, fmt.Errorf("GetExtendedTcpTable: %w", windows.Errno(r0))
		}
		if len(buf) < 4 {
			return nil, nil
		}
		n := *(*uint32)(unsafe.Pointer(&buf[0]))
		rowSize := unsafe.Sizeof(mibTCPRowOwnerPID{})
		need := 4 + uintptr(n)*rowSize
		if uintptr(len(buf)) < need {
			return nil, fmt.Errorf("tcp table buffer too small")
		}
		rows := make([]mibTCPRowOwnerPID, n)
		for i := uint32(0); i < n; i++ {
			off := 4 + uintptr(i)*rowSize
			rows[i] = *(*mibTCPRowOwnerPID)(unsafe.Pointer(&buf[off]))
		}
		return rows, nil
	}
	return nil, fmt.Errorf("GetExtendedTcpTable: buffer resize failed")
}

func tcp6OwnerPIDTable() ([]mibTCP6RowOwnerPID, error) {
	var size uint32
	r0, _, _ := procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 1, afINET6, tcpTableOwnerPIDAll, 0)
	if r0 != errInsufficientBuffer && r0 != 0 {
		return nil, fmt.Errorf("GetExtendedTcpTable(v6) size: %w", windows.Errno(r0))
	}
	for retries := 0; retries < 3; retries++ {
		buf := make([]byte, size)
		r0, _, _ = procGetExtendedTcpTable.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&size)),
			1,
			afINET6,
			tcpTableOwnerPIDAll,
			0,
		)
		if r0 == errInsufficientBuffer {
			continue
		}
		if r0 != 0 {
			return nil, fmt.Errorf("GetExtendedTcpTable(v6): %w", windows.Errno(r0))
		}
		if len(buf) < 4 {
			return nil, nil
		}
		n := *(*uint32)(unsafe.Pointer(&buf[0]))
		rowSize := unsafe.Sizeof(mibTCP6RowOwnerPID{})
		need := 4 + uintptr(n)*rowSize
		if uintptr(len(buf)) < need {
			return nil, fmt.Errorf("tcp6 table buffer too small")
		}
		rows := make([]mibTCP6RowOwnerPID, n)
		for i := uint32(0); i < n; i++ {
			off := 4 + uintptr(i)*rowSize
			rows[i] = *(*mibTCP6RowOwnerPID)(unsafe.Pointer(&buf[off]))
		}
		return rows, nil
	}
	return nil, fmt.Errorf("GetExtendedTcpTable(v6): buffer resize failed")
}

func tcp4EstatsBytes(row mibTCPRow) (rx, tx uint64, ok bool) {
	var rod tcpEstatsDataROD
	rodSize := uint32(unsafe.Sizeof(rod))
	r0, _, _ := procGetPerTcpConnectionEStats.Call(
		uintptr(unsafe.Pointer(&row)),
		tcpConnectionEstatsData,
		0, 0, 0,
		0, 0, 0,
		uintptr(unsafe.Pointer(&rod)),
		0,
		uintptr(rodSize),
	)
	if r0 != 0 {
		// Try enabling collection then read again.
		// 尝试启用采集后再读。
		rw := tcpEstatsDataRW{EnableCollection: 1}
		rwSize := uint32(unsafe.Sizeof(rw))
		procSetPerTcpConnectionEStats.Call(
			uintptr(unsafe.Pointer(&row)),
			tcpConnectionEstatsData,
			uintptr(unsafe.Pointer(&rw)),
			0,
			uintptr(rwSize),
			0, 0, 0,
			0, 0, 0,
		)
		r0, _, _ = procGetPerTcpConnectionEStats.Call(
			uintptr(unsafe.Pointer(&row)),
			tcpConnectionEstatsData,
			0, 0, 0,
			0, 0, 0,
			uintptr(unsafe.Pointer(&rod)),
			0,
			uintptr(rodSize),
		)
		if r0 != 0 {
			return 0, 0, false
		}
	}
	return rod.DataBytesIn, rod.DataBytesOut, true
}

func tcp6EstatsBytes(owner *mibTCP6RowOwnerPID) (rx, tx uint64, ok bool) {
	// Convert owner-PID table row into MIB_TCP6ROW layout for ESTATS.
	// 将带 PID 的表行转换为 ESTATS 所需的 MIB_TCP6ROW 布局。
	row := mibTCP6Row{
		State:         owner.State,
		LocalAddr:     owner.LocalAddr,
		LocalScopeId:  owner.LocalScopeId,
		LocalPort:     owner.LocalPort,
		RemoteAddr:    owner.RemoteAddr,
		RemoteScopeId: owner.RemoteScopeId,
		RemotePort:    owner.RemotePort,
	}
	var rod tcpEstatsDataROD
	rodSize := uint32(unsafe.Sizeof(rod))
	r0, _, _ := procGetPerTcp6ConnectionEStats.Call(
		uintptr(unsafe.Pointer(&row)),
		tcpConnectionEstatsData,
		0, 0, 0,
		0, 0, 0,
		uintptr(unsafe.Pointer(&rod)),
		0,
		uintptr(rodSize),
	)
	if r0 != 0 {
		rw := tcpEstatsDataRW{EnableCollection: 1}
		rwSize := uint32(unsafe.Sizeof(rw))
		procSetPerTcp6ConnectionEStats.Call(
			uintptr(unsafe.Pointer(&row)),
			tcpConnectionEstatsData,
			uintptr(unsafe.Pointer(&rw)),
			0,
			uintptr(rwSize),
			0, 0, 0,
			0, 0, 0,
		)
		r0, _, _ = procGetPerTcp6ConnectionEStats.Call(
			uintptr(unsafe.Pointer(&row)),
			tcpConnectionEstatsData,
			0, 0, 0,
			0, 0, 0,
			uintptr(unsafe.Pointer(&rod)),
			0,
			uintptr(rodSize),
		)
		if r0 != 0 {
			return 0, 0, false
		}
	}
	return rod.DataBytesIn, rod.DataBytesOut, true
}

func lookupMeta(pid int) processMeta {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return processMeta{name: "unknown"}
	}
	path, err := p.Exe()
	if err != nil || path == "" {
		if name, err := p.Name(); err == nil {
			return processMeta{name: name, path: name}
		}
		return processMeta{name: "unknown"}
	}
	name := path
	if base := baseName(path); base != "" {
		name = base
	}
	return processMeta{name: name, path: path}
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '\\' || path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
