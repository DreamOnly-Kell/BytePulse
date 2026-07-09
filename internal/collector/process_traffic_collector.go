// Optional per-process traffic attribution collector (Phase 2B / nettop).
// 可选的每进程流量归因采集器（Phase 2B / nettop）。
package collector

import (
	"context"

	"bytepulse/internal/processstate"
	"bytepulse/internal/proctraffic"
)

// ProcessTrafficCollector streams attributed rates into processstate.State.
// ProcessTrafficCollector 将归因速率流式写入 processstate.State。
type ProcessTrafficCollector struct {
	// attributor is the platform backend (nettop on macOS, stub elsewhere).
	// attributor 是平台后端（macOS 为 nettop，其它为 stub）。
	attributor proctraffic.Attributor
	// state receives RX/TX updates keyed by PID.
	// state 接收按 PID 索引的 RX/TX 更新。
	state *processstate.State
}

// NewProcessTrafficCollector constructs a collector around an attributor + state.
// NewProcessTrafficCollector 用 attributor + state 构造采集器。
func NewProcessTrafficCollector(attributor proctraffic.Attributor, state *processstate.State) *ProcessTrafficCollector {
	return &ProcessTrafficCollector{attributor: attributor, state: state}
}

// Run blocks while the attributor produces samples; returns when ctx ends.
// Run 在 attributor 产出样本期间阻塞；ctx 结束时返回。
func (c *ProcessTrafficCollector) Run(ctx context.Context) error {
	// Nil-safe no-op for incomplete wiring.
	// 对不完整装配做空安全 no-op。
	if c == nil || c.attributor == nil || c.state == nil {
		return nil
	}
	// Feed each batch of samples into shared process memory state.
	// 将每批样本写入共享进程内存态。
	// Note: attributor errors are currently discarded.
	// 注意：attributor 错误当前被丢弃。
	_ = c.attributor.Run(ctx, func(samples []proctraffic.Sample) {
		c.state.UpdateTraffic(trafficSamplesToState(samples))
	})
	return nil
}

// trafficSamplesToState converts proctraffic samples into processstate samples.
// trafficSamplesToState 将 proctraffic 样本转换为 processstate 样本。
func trafficSamplesToState(samples []proctraffic.Sample) []processstate.ProcessTrafficSample {
	out := make([]processstate.ProcessTrafficSample, 0, len(samples))
	for _, sample := range samples {
		// Field-by-field copy keeps packages decoupled.
		// 逐字段拷贝以保持包解耦。
		out = append(out, processstate.ProcessTrafficSample{
			PID:         sample.PID,
			ProcessName: sample.ProcessName,
			ProcessPath: sample.ProcessPath,
			RXBytes:     sample.RXBytes,
			TXBytes:     sample.TXBytes,
			RXBps:       sample.RXBps,
			TXBps:       sample.TXBps,
			SeenAt:      sample.SeenAt,
			Source:      sample.Source,
		})
	}
	return out
}
