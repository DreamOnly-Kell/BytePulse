// Process connection sampling loop (Phase 2A).
// 进程连接采样循环（Phase 2A）。
package collector

import (
	"context"
	"errors"
	"time"

	"bytepulse/internal/proc"
	"bytepulse/internal/processstate"
	"bytepulse/internal/storage"
)

// ProcessConnectionStore persists minute-level process connection rollups.
// ProcessConnectionStore 持久化分钟级进程连接聚合。
type ProcessConnectionStore interface {
	// UpsertProcessConnectionMinutes merges rollup rows by (minute, process_key).
	// UpsertProcessConnectionMinutes 按 (分钟, process_key) 合并聚合行。
	UpsertProcessConnectionMinutes([]storage.ProcessConnectionMinute) error
	// CleanupProcessConnectionMinutes deletes rollups older than retention.
	// CleanupProcessConnectionMinutes 删除超过保留期的聚合。
	CleanupProcessConnectionMinutes(now time.Time, retention time.Duration) error
}

// ProcessConnectionOptions configures process connection sampling.
// ProcessConnectionOptions 配置进程连接采样。
type ProcessConnectionOptions struct {
	// Interval between connection samples (typically 1s).
	// 连接采样间隔（通常 1s）。
	Interval time.Duration
	// Retention for process_connection_minutes rows.
	// process_connection_minutes 行的保留期。
	Retention time.Duration
}

// ProcessConnectionCollector samples sockets, updates memory state, flushes minutes.
// ProcessConnectionCollector 采样套接字、更新内存态、刷写分钟数据。
type ProcessConnectionCollector struct {
	store   ProcessConnectionStore
	sampler proc.ConnectionSampler
	state   *processstate.State
	opts    ProcessConnectionOptions
}

// errProcessConnectionUnsupported is the internal sentinel for platform stubs.
// errProcessConnectionUnsupported 是平台 stub 的内部哨兵错误。
var errProcessConnectionUnsupported = errors.New("process connection sampling unsupported")

// NewProcessConnectionCollector wires store, sampler, state, and defaults.
// NewProcessConnectionCollector 组装 store、sampler、state 并填默认值。
func NewProcessConnectionCollector(
	store ProcessConnectionStore,
	sampler proc.ConnectionSampler,
	state *processstate.State,
	opts ProcessConnectionOptions,
) *ProcessConnectionCollector {
	// Default to 1s sampling to match realtime UI cadence.
	// 默认 1s 采样，匹配实时 UI 节奏。
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	// Default retention matches interface samples (~30 days).
	// 默认保留期与网卡样本一致（约 30 天）。
	if opts.Retention <= 0 {
		opts.Retention = 30 * 24 * time.Hour
	}
	return &ProcessConnectionCollector{
		store:   store,
		sampler: sampler,
		state:   state,
		opts:    opts,
	}
}

// Run samples once immediately, then on each Interval until ctx ends.
// Run 立即采样一次，之后每个 Interval 采样，直到 ctx 结束。
func (c *ProcessConnectionCollector) Run(ctx context.Context) error {
	// Prime memory state before waiting for the first tick.
	// 在等待第一拍前先填充内存态。
	if err := c.sampleOnce(time.Now()); err != nil {
		// Unsupported platform: exit cleanly; interface collector keeps running.
		// 不支持的平台：干净退出；网卡采集器继续运行。
		if errors.Is(err, errProcessConnectionUnsupported) {
			return nil
		}
		return err
	}

	ticker := time.NewTicker(c.opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// On shutdown, flush any completed minute buckets still in memory.
			// 关闭时刷写内存中已完成的分钟桶。
			return flushProcessMinutes(c.store, c.state, time.Now(), c.opts.Retention)
		case now := <-ticker.C:
			if err := c.sampleOnce(now); err != nil {
				// Platform became unsupported mid-run (unlikely) → stop quietly.
				// 运行中变为不支持（少见）→ 安静停止。
				if errors.Is(err, errProcessConnectionUnsupported) {
					return nil
				}
				return err
			}
		}
	}
}

// sampleOnce takes one connection snapshot and may flush completed minutes.
// sampleOnce 取一次连接快照，并可能刷写已完成分钟。
func (c *ProcessConnectionCollector) sampleOnce(now time.Time) error {
	// Platform sampler returns ErrNotSupported on Linux/Windows stubs.
	// 平台采样器在 Linux/Windows stub 上返回 ErrNotSupported。
	conns, err := c.sampler.Sample()
	if errors.Is(err, proc.ErrNotSupported) {
		return errProcessConnectionUnsupported
	}
	// Other sampling errors are currently swallowed (state stays previous).
	// 其它采样错误当前被吞掉（内存态保持为上一次）。
	if err != nil {
		return nil
	}
	// Replace latest process/connection maps and update minute buckets.
	// 替换最新进程/连接映射并更新分钟桶。
	c.state.Update(conns, now)
	// Persist any minute buckets that are no longer the current minute.
	// 持久化已不再是“当前分钟”的分钟桶。
	return flushProcessMinutes(c.store, c.state, now, c.opts.Retention)
}

// flushProcessMinutes writes completed rollups and runs retention cleanup.
// flushProcessMinutes 写入已完成聚合并执行保留期清理。
func flushProcessMinutes(store ProcessConnectionStore, state *processstate.State, now time.Time, retention time.Duration) error {
	// Pull completed (previous) minutes out of memory.
	// 从内存取出已完成（过去）的分钟。
	minutes := state.FlushCompleted(now)
	if len(minutes) == 0 {
		return nil
	}
	// Convert processstate types into storage types.
	// 将 processstate 类型转换为 storage 类型。
	items := make([]storage.ProcessConnectionMinute, 0, len(minutes))
	for _, minute := range minutes {
		items = append(items, processMinuteToStorage(minute))
	}
	// Upsert merged stats for each (minute_start, process_key).
	// 按 (minute_start, process_key) upsert 合并统计。
	if err := store.UpsertProcessConnectionMinutes(items); err != nil {
		return err
	}
	// Delete expired historical process minutes.
	// 删除过期的历史进程分钟数据。
	return store.CleanupProcessConnectionMinutes(now, retention)
}

// processMinuteToStorage maps in-memory minute rollup to the DB model.
// processMinuteToStorage 将内存分钟聚合映射为数据库模型。
func processMinuteToStorage(m processstate.ProcessConnectionMinute) storage.ProcessConnectionMinute {
	return storage.ProcessConnectionMinute{
		MinuteStart:        m.MinuteStart,
		PID:                m.PID,
		ProcessName:        m.ProcessName,
		ProcessPath:        m.ProcessPath,
		ProcessKey:         m.ProcessKey,
		MaxConnectionCount: m.MaxConnectionCount,
		SampleCount:        m.SampleCount,
		LastSeen:           m.LastSeen,
	}
}
