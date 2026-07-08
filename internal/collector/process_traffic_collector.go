package collector

import (
	"context"

	"bytepulse/internal/processstate"
	"bytepulse/internal/proctraffic"
)

type ProcessTrafficCollector struct {
	attributor proctraffic.Attributor
	state      *processstate.State
}

func NewProcessTrafficCollector(attributor proctraffic.Attributor, state *processstate.State) *ProcessTrafficCollector {
	return &ProcessTrafficCollector{attributor: attributor, state: state}
}

func (c *ProcessTrafficCollector) Run(ctx context.Context) error {
	if c == nil || c.attributor == nil || c.state == nil {
		return nil
	}
	_ = c.attributor.Run(ctx, func(samples []proctraffic.Sample) {
		c.state.UpdateTraffic(trafficSamplesToState(samples))
	})
	return nil
}

func trafficSamplesToState(samples []proctraffic.Sample) []processstate.ProcessTrafficSample {
	out := make([]processstate.ProcessTrafficSample, 0, len(samples))
	for _, sample := range samples {
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
