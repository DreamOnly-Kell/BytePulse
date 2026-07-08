package processstate

import (
	"sort"
	"sync"
	"time"

	"bytepulse/internal/proc"
)

type ProcessConnectionDetail struct {
	PID         int       `json:"pid"`
	ProcessName string    `json:"process_name"`
	ProcessPath string    `json:"process_path"`
	ProcessKey  string    `json:"process_key"`
	Protocol    string    `json:"protocol"`
	LocalAddr   string    `json:"local_addr"`
	LocalPort   uint32    `json:"local_port"`
	RemoteAddr  string    `json:"remote_addr"`
	RemotePort  uint32    `json:"remote_port"`
	Status      string    `json:"status"`
	SeenAt      time.Time `json:"seen_at"`
}

type ProcessConnectionSummary struct {
	PID             int       `json:"pid"`
	ProcessName     string    `json:"process_name"`
	ProcessPath     string    `json:"process_path"`
	ProcessKey      string    `json:"process_key"`
	ConnectionCount int       `json:"connection_count"`
	LastSeen        time.Time `json:"last_seen"`
}

type ProcessConnectionMinute struct {
	MinuteStart        time.Time `json:"minute_start"`
	PID                int       `json:"pid"`
	ProcessName        string    `json:"process_name"`
	ProcessPath        string    `json:"process_path"`
	ProcessKey         string    `json:"process_key"`
	MaxConnectionCount int       `json:"max_connection_count"`
	SampleCount        int       `json:"sample_count"`
	LastSeen           time.Time `json:"last_seen"`
}

type State struct {
	mu                sync.RWMutex
	latestSummaries   []ProcessConnectionSummary
	latestConnections map[string][]ProcessConnectionDetail
	minuteBuckets     map[string]ProcessConnectionMinute
}

func New() *State {
	return &State{
		latestConnections: map[string][]ProcessConnectionDetail{},
		minuteBuckets:     map[string]ProcessConnectionMinute{},
	}
}

func (s *State) Update(conns []proc.Connection, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	byProcess := map[string][]ProcessConnectionDetail{}
	for _, conn := range conns {
		detail := ProcessConnectionDetail{
			PID:         conn.PID,
			ProcessName: conn.ProcessName,
			ProcessPath: conn.ProcessPath,
			ProcessKey:  conn.ProcessKey,
			Protocol:    conn.Protocol,
			LocalAddr:   conn.LocalAddr,
			LocalPort:   conn.LocalPort,
			RemoteAddr:  conn.RemoteAddr,
			RemotePort:  conn.RemotePort,
			Status:      conn.Status,
			SeenAt:      conn.SeenAt,
		}
		byProcess[conn.ProcessKey] = append(byProcess[conn.ProcessKey], detail)
	}

	s.latestConnections = byProcess
	s.latestSummaries = make([]ProcessConnectionSummary, 0, len(byProcess))
	minuteStart := now.Truncate(time.Minute)
	for key, details := range byProcess {
		first := details[0]
		lastSeen := first.SeenAt
		for _, detail := range details[1:] {
			if detail.SeenAt.After(lastSeen) {
				lastSeen = detail.SeenAt
			}
		}
		summary := ProcessConnectionSummary{
			PID:             first.PID,
			ProcessName:     first.ProcessName,
			ProcessPath:     first.ProcessPath,
			ProcessKey:      key,
			ConnectionCount: len(details),
			LastSeen:        lastSeen,
		}
		s.latestSummaries = append(s.latestSummaries, summary)

		bucketKey := bucketKey(minuteStart, key)
		bucket := s.minuteBuckets[bucketKey]
		if bucket.ProcessKey == "" {
			bucket = ProcessConnectionMinute{
				MinuteStart: minuteStart,
				PID:         first.PID,
				ProcessName: first.ProcessName,
				ProcessPath: first.ProcessPath,
				ProcessKey:  key,
			}
		}
		if len(details) > bucket.MaxConnectionCount {
			bucket.MaxConnectionCount = len(details)
		}
		bucket.SampleCount++
		if lastSeen.After(bucket.LastSeen) {
			bucket.LastSeen = lastSeen
		}
		s.minuteBuckets[bucketKey] = bucket
	}
	sortSummaries(s.latestSummaries)
}

func (s *State) LatestSummaries(limit int) []ProcessConnectionSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := append([]ProcessConnectionSummary(nil), s.latestSummaries...)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (s *State) LatestConnections(processKey string) []ProcessConnectionDetail {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return append([]ProcessConnectionDetail(nil), s.latestConnections[processKey]...)
}

func (s *State) FlushCompleted(before time.Time) []ProcessConnectionMinute {
	return s.flush(before.Truncate(time.Minute), false)
}

func (s *State) FlushBefore(before time.Time) []ProcessConnectionMinute {
	return s.flush(before.Truncate(time.Minute), true)
}

func (s *State) flush(cutoff time.Time, includeCutoff bool) []ProcessConnectionMinute {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := []ProcessConnectionMinute{}
	for key, bucket := range s.minuteBuckets {
		flush := bucket.MinuteStart.Before(cutoff)
		if includeCutoff && bucket.MinuteStart.Equal(cutoff.Add(-time.Minute)) {
			flush = true
		}
		if !flush {
			continue
		}
		out = append(out, bucket)
		delete(s.minuteBuckets, key)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MinuteStart.Equal(out[j].MinuteStart) {
			return out[i].ProcessKey < out[j].ProcessKey
		}
		return out[i].MinuteStart.Before(out[j].MinuteStart)
	})
	return out
}

func sortSummaries(items []ProcessConnectionSummary) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].ConnectionCount != items[j].ConnectionCount {
			return items[i].ConnectionCount > items[j].ConnectionCount
		}
		if items[i].ProcessName != items[j].ProcessName {
			return items[i].ProcessName < items[j].ProcessName
		}
		return items[i].PID < items[j].PID
	})
}

func bucketKey(minuteStart time.Time, processKey string) string {
	return minuteStart.Format(time.RFC3339) + "|" + processKey
}
