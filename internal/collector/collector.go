// Package collector samples OS network counters and process activity.
// collector 包采样操作系统网卡计数器与进程活动。
package collector

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"bytepulse/internal/logx"
	"bytepulse/internal/storage"

	gopsnet "github.com/shirou/gopsutil/v4/net"
)

// InterfaceCounter is a point-in-time OS counter snapshot for one NIC.
// InterfaceCounter 是某块网卡某一时刻的操作系统计数快照。
type InterfaceCounter struct {
	// Name is the OS interface name (e.g. en0, eth0).
	// Name 是操作系统网卡名（如 en0、eth0）。
	Name string
	// RXBytes is the cumulative received-byte counter from the OS.
	// RXBytes 是操作系统累计接收字节计数。
	RXBytes uint64
	// TXBytes is the cumulative transmitted-byte counter from the OS.
	// TXBytes 是操作系统累计发送字节计数。
	TXBytes uint64
	// IsLoopback marks lo / loopback devices.
	// IsLoopback 标记 lo / 回环设备。
	IsLoopback bool
}

// Options configures the interface sampling loop.
// Options 配置网卡采样循环。
type Options struct {
	// Interval between counter reads.
	// 两次读取计数器的间隔。
	Interval time.Duration
	// Interface limits sampling to one NIC; empty = all non-loopback.
	// Interface 将采样限制为单网卡；空 = 所有非回环。
	Interface string
	// Retention is passed to Cleanup after each successful insert.
	// Retention 在每次成功插入后传给 Cleanup。
	Retention time.Duration
}

// Store is the persistence surface the collector depends on.
// Store 是采集器依赖的持久化接口。
type Store interface {
	// InsertSamples writes one interval of per-interface deltas.
	// InsertSamples 写入一个间隔内的每网卡增量。
	InsertSamples([]storage.Sample) error
	// Cleanup deletes samples older than retention.
	// Cleanup 删除超过保留期的样本。
	Cleanup(now time.Time, retention time.Duration) error
}

// Collector diffs interface counters on a ticker and stores samples.
// Collector 在定时器上对网卡计数做差分并落库。
type Collector struct {
	store Store
	opts  Options
}

// New builds a Collector with sane defaults for interval/retention.
// New 构造 Collector，并为间隔/保留期填入合理默认值。
func New(store Store, opts Options) *Collector {
	// Default sample once per second.
	// 默认每秒采样一次。
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	// Default keep about 30 days of samples.
	// 默认保留约 30 天样本。
	if opts.Retention <= 0 {
		opts.Retention = 30 * 24 * time.Hour
	}
	return &Collector{store: store, opts: opts}
}

// Run blocks until ctx is cancelled, sampling each Interval.
// Run 阻塞直到 ctx 取消，每个 Interval 采样一次。
func (c *Collector) Run(ctx context.Context) error {
	// Take a baseline snapshot; the first tick computes deltas from this.
	// 先取基线快照；第一拍相对此基线计算增量。
	prev, err := ReadCounters(c.opts.Interface)
	if err != nil {
		logx.Error("read interface counters failed at start", "component", "collector", "err", err, "interface", c.opts.Interface)
		return err
	}
	logx.Info("interface collector running", "component", "collector", "interval", c.opts.Interval.String(), "interface", c.opts.Interface)
	// Timestamp of the baseline (or last successful sample).
	// 基线（或上次成功样本）的时间戳。
	prevAt := time.Now()

	// Fire on each sampling interval.
	// 每个采样间隔触发一次。
	ticker := time.NewTicker(c.opts.Interval)
	defer ticker.Stop()

	for {
		select {
		// Cooperative shutdown.
		// 协作式关闭。
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			// Read current cumulative counters.
			// 读取当前累计计数。
			current, err := ReadCounters(c.opts.Interface)
			if err != nil {
				logx.Error("read interface counters failed", "component", "collector", "err", err)
				return err
			}

			// Convert counter pairs into per-interval Sample rows.
			// 将计数对转换为区间 Sample 行。
			samples := diff(prev, current, prevAt, now)
			if len(samples) > 0 {
				// Persist this interval.
				// 持久化本区间。
				if err := c.store.InsertSamples(samples); err != nil {
					logx.Error("insert samples failed", "component", "collector", "err", err, "n", len(samples))
					return err
				}
				// Drop rows older than retention (runs every successful tick).
				// 删除超过保留期的行（每次成功采样都会跑）。
				if err := c.store.Cleanup(now, c.opts.Retention); err != nil {
					logx.Error("cleanup samples failed", "component", "collector", "err", err)
					return err
				}
				var rx, tx uint64
				for _, s := range samples {
					rx += s.RXBytes
					tx += s.TXBytes
				}
				logx.Debug("interface samples stored",
					"component", "collector",
					"ifaces", len(samples),
					"rx_bytes", rx,
					"tx_bytes", tx,
					"interval_sec", now.Sub(prevAt).Seconds(),
				)
			} else {
				logx.Debug("no interface samples this tick", "component", "collector", "ifaces_seen", len(current))
			}

			// Advance baseline for the next tick.
			// 推进基线供下一拍使用。
			prev = current
			prevAt = now
		}
	}
}

// ReadCounters reads counters for collection (excludes loopback when all).
// ReadCounters 读取用于采集的计数（“全部”时排除回环）。
func ReadCounters(interfaceName string) ([]InterfaceCounter, error) {
	return readCounters(interfaceName, true)
}

// ReadAllCounters includes loopback; used by the `interfaces` command.
// ReadAllCounters 包含回环；供 `interfaces` 命令使用。
func ReadAllCounters() ([]InterfaceCounter, error) {
	return readCounters("", false)
}

// readCounters is the shared gopsutil IOCounters reader with filtering.
// readCounters 是带过滤的共享 gopsutil IOCounters 读取逻辑。
func readCounters(interfaceName string, excludeLoopbackWhenAll bool) ([]InterfaceCounter, error) {
	// true => per-interface stats rather than a single aggregate row.
	// true 表示按网卡分别统计，而不是单行合计。
	counters, err := gopsnet.IOCounters(true)
	if err != nil {
		return nil, err
	}

	// Map of loopback interface names from net.Interfaces flags.
	// 根据 net.Interfaces 标志得到的回环网卡名集合。
	loopbacks := loopbackNames()
	out := make([]InterfaceCounter, 0, len(counters))
	for _, c := range counters {
		// Optional single-interface filter.
		// 可选的单网卡过滤。
		if interfaceName != "" && c.Name != interfaceName {
			continue
		}
		// Detect loopback by OS flag or common "lo*" name prefix.
		// 通过 OS 标志或常见 "lo*" 前缀识别回环。
		isLoopback := loopbacks[c.Name] || strings.HasPrefix(c.Name, "lo")
		// When aggregating "all", skip loopback to avoid inflating totals.
		// 聚合“全部”时跳过回环，避免总量虚高。
		if interfaceName == "" && excludeLoopbackWhenAll && isLoopback {
			continue
		}
		out = append(out, InterfaceCounter{
			Name:       c.Name,
			RXBytes:    c.BytesRecv,
			TXBytes:    c.BytesSent,
			IsLoopback: isLoopback,
		})
	}

	// Fail clearly if a named interface was not found.
	// 指定网卡不存在时明确失败。
	if interfaceName != "" && len(out) == 0 {
		return nil, fmt.Errorf("interface %q not found", interfaceName)
	}
	return out, nil
}

// diff computes per-interface byte deltas and speeds between two snapshots.
// diff 计算两次快照之间每网卡的字节增量与速率。
func diff(prev, current []InterfaceCounter, prevAt, now time.Time) []storage.Sample {
	// Elapsed seconds for speed = delta / seconds.
	// 用于 speed = delta / seconds 的经过秒数。
	seconds := now.Sub(prevAt).Seconds()
	// Guard against zero/negative intervals (clock skew or same tick).
	// 防止零/负间隔（时钟回拨或同 tick）。
	if seconds <= 0 {
		return nil
	}

	// Index previous counters by interface name for O(1) lookup.
	// 按网卡名索引上次计数，便于 O(1) 查找。
	byName := make(map[string]InterfaceCounter, len(prev))
	for _, p := range prev {
		byName[p.Name] = p
	}

	samples := make([]storage.Sample, 0, len(current))
	for _, c := range current {
		// New interfaces appearing mid-run have no previous baseline → skip once.
		// 运行中新出现的网卡没有上次基线 → 跳过这一次。
		p, ok := byName[c.Name]
		if !ok {
			continue
		}
		// Counter reset/wrap: skip rather than emit a huge negative-as-unsigned delta.
		// 计数重置/回绕：跳过，避免把巨大无符号差值当真流量。
		if c.RXBytes < p.RXBytes || c.TXBytes < p.TXBytes {
			continue
		}
		// Interval traffic is the counter difference.
		// 区间流量 = 计数差。
		rxDelta := c.RXBytes - p.RXBytes
		txDelta := c.TXBytes - p.TXBytes
		samples = append(samples, storage.Sample{
			Timestamp:   now,
			Interface:   c.Name,
			RXBytes:     rxDelta,
			TXBytes:     txDelta,
			// Speeds are average B/s over this interval.
			// 速率为本区间平均 B/s。
			RXSpeedBps:  float64(rxDelta) / seconds,
			TXSpeedBps:  float64(txDelta) / seconds,
			IntervalSec: seconds,
		})
	}
	return samples
}

// loopbackNames returns interface names that have the loopback flag set.
// loopbackNames 返回带有 loopback 标志的网卡名集合。
func loopbackNames() map[string]bool {
	result := map[string]bool{}
	// Query the Go net package for interface metadata.
	// 用 Go net 包查询网卡元数据。
	ifaces, err := net.Interfaces()
	if err != nil {
		// On failure, rely on name-prefix heuristics only.
		// 失败时仅依赖名称前缀启发式。
		return result
	}
	for _, iface := range ifaces {
		// FlagLoopback is the authoritative loopback marker.
		// FlagLoopback 是权威的回环标记。
		if iface.Flags&net.FlagLoopback != 0 {
			result[iface.Name] = true
		}
	}
	return result
}
