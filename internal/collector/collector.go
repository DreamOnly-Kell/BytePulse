package collector

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"bytepulse/internal/storage"

	gopsnet "github.com/shirou/gopsutil/v4/net"
)

type InterfaceCounter struct {
	Name       string
	RXBytes    uint64
	TXBytes    uint64
	IsLoopback bool
}

type Options struct {
	Interval  time.Duration
	Interface string
	Retention time.Duration
}

type Store interface {
	InsertSamples([]storage.Sample) error
	Cleanup(now time.Time, retention time.Duration) error
}

type Collector struct {
	store Store
	opts  Options
}

func New(store Store, opts Options) *Collector {
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	if opts.Retention <= 0 {
		opts.Retention = 30 * 24 * time.Hour
	}
	return &Collector{store: store, opts: opts}
}

func (c *Collector) Run(ctx context.Context) error {
	prev, err := ReadCounters(c.opts.Interface)
	if err != nil {
		return err
	}
	prevAt := time.Now()

	ticker := time.NewTicker(c.opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			current, err := ReadCounters(c.opts.Interface)
			if err != nil {
				return err
			}

			samples := diff(prev, current, prevAt, now)
			if len(samples) > 0 {
				if err := c.store.InsertSamples(samples); err != nil {
					return err
				}
				if err := c.store.Cleanup(now, c.opts.Retention); err != nil {
					return err
				}
			}

			prev = current
			prevAt = now
		}
	}
}

func ReadCounters(interfaceName string) ([]InterfaceCounter, error) {
	return readCounters(interfaceName, true)
}

func ReadAllCounters() ([]InterfaceCounter, error) {
	return readCounters("", false)
}

func readCounters(interfaceName string, excludeLoopbackWhenAll bool) ([]InterfaceCounter, error) {
	counters, err := gopsnet.IOCounters(true)
	if err != nil {
		return nil, err
	}

	loopbacks := loopbackNames()
	out := make([]InterfaceCounter, 0, len(counters))
	for _, c := range counters {
		if interfaceName != "" && c.Name != interfaceName {
			continue
		}
		isLoopback := loopbacks[c.Name] || strings.HasPrefix(c.Name, "lo")
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

	if interfaceName != "" && len(out) == 0 {
		return nil, fmt.Errorf("interface %q not found", interfaceName)
	}
	return out, nil
}

func diff(prev, current []InterfaceCounter, prevAt, now time.Time) []storage.Sample {
	seconds := now.Sub(prevAt).Seconds()
	if seconds <= 0 {
		return nil
	}

	byName := make(map[string]InterfaceCounter, len(prev))
	for _, p := range prev {
		byName[p.Name] = p
	}

	samples := make([]storage.Sample, 0, len(current))
	for _, c := range current {
		p, ok := byName[c.Name]
		if !ok {
			continue
		}
		if c.RXBytes < p.RXBytes || c.TXBytes < p.TXBytes {
			continue
		}
		rxDelta := c.RXBytes - p.RXBytes
		txDelta := c.TXBytes - p.TXBytes
		samples = append(samples, storage.Sample{
			Timestamp:   now,
			Interface:   c.Name,
			RXBytes:     rxDelta,
			TXBytes:     txDelta,
			RXSpeedBps:  float64(rxDelta) / seconds,
			TXSpeedBps:  float64(txDelta) / seconds,
			IntervalSec: seconds,
		})
	}
	return samples
}

func loopbackNames() map[string]bool {
	result := map[string]bool{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return result
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			result[iface.Name] = true
		}
	}
	return result
}
