package collector

import (
	"context"
	"errors"
	"time"

	"bytepulse/internal/proc"
	"bytepulse/internal/processstate"
	"bytepulse/internal/storage"
)

type ProcessConnectionStore interface {
	UpsertProcessConnectionMinutes([]storage.ProcessConnectionMinute) error
	CleanupProcessConnectionMinutes(now time.Time, retention time.Duration) error
}

type ProcessConnectionOptions struct {
	Interval  time.Duration
	Retention time.Duration
}

type ProcessConnectionCollector struct {
	store   ProcessConnectionStore
	sampler proc.ConnectionSampler
	state   *processstate.State
	opts    ProcessConnectionOptions
}

var errProcessConnectionUnsupported = errors.New("process connection sampling unsupported")

func NewProcessConnectionCollector(
	store ProcessConnectionStore,
	sampler proc.ConnectionSampler,
	state *processstate.State,
	opts ProcessConnectionOptions,
) *ProcessConnectionCollector {
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
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

func (c *ProcessConnectionCollector) Run(ctx context.Context) error {
	if err := c.sampleOnce(time.Now()); err != nil {
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
			return flushProcessMinutes(c.store, c.state, time.Now(), c.opts.Retention)
		case now := <-ticker.C:
			if err := c.sampleOnce(now); err != nil {
				if errors.Is(err, errProcessConnectionUnsupported) {
					return nil
				}
				return err
			}
		}
	}
}

func (c *ProcessConnectionCollector) sampleOnce(now time.Time) error {
	conns, err := c.sampler.Sample()
	if errors.Is(err, proc.ErrNotSupported) {
		return errProcessConnectionUnsupported
	}
	if err != nil {
		return nil
	}
	c.state.Update(conns, now)
	return flushProcessMinutes(c.store, c.state, now, c.opts.Retention)
}

func flushProcessMinutes(store ProcessConnectionStore, state *processstate.State, now time.Time, retention time.Duration) error {
	minutes := state.FlushCompleted(now)
	if len(minutes) == 0 {
		return nil
	}
	items := make([]storage.ProcessConnectionMinute, 0, len(minutes))
	for _, minute := range minutes {
		items = append(items, processMinuteToStorage(minute))
	}
	if err := store.UpsertProcessConnectionMinutes(items); err != nil {
		return err
	}
	return store.CleanupProcessConnectionMinutes(now, retention)
}

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
