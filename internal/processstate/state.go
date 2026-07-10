// Package processstate holds in-memory realtime process connection/traffic state.
// processstate 包保存进程连接/流量的内存实时态。
package processstate

import (
	"sort"
	"sync"
	"time"

	"bytepulse/internal/proc"
)

const trafficTTL = 3 * time.Second

type TrafficBackendState string

const (
	TrafficBackendDisabled TrafficBackendState = "disabled"
	TrafficBackendStarting TrafficBackendState = "starting"
	TrafficBackendHealthy  TrafficBackendState = "healthy"
	TrafficBackendDegraded TrafficBackendState = "degraded"
)

// TrafficBackendStatus describes availability of optional per-process attribution.
// TrafficBackendStatus 描述可选进程流量归因后端的可用状态。
type TrafficBackendStatus struct {
	State        TrafficBackendState `json:"state"`
	LastSampleAt time.Time           `json:"last_sample_at,omitempty"`
	LastError    string              `json:"last_error,omitempty"`
}

// ProcessConnectionDetail is one socket row shown in connection drill-down.
// ProcessConnectionDetail 是连接下钻视图中的一条套接字记录。
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

// ProcessConnectionSummary is one process row for top lists (with optional rates).
// ProcessConnectionSummary 是排行列表中的一行进程（含可选速率）。
type ProcessConnectionSummary struct {
	PID         int    `json:"pid"`
	ProcessName string `json:"process_name"`
	ProcessPath string `json:"process_path"`
	ProcessKey  string `json:"process_key"`
	// ConnectionCount is the number of sockets in the latest sample.
	// ConnectionCount 是最近一次采样中的套接字数量。
	ConnectionCount int `json:"connection_count"`
	// RX/TX fields are filled only when traffic attribution is available.
	// RX/TX 字段仅在流量归因可用时填充。
	RXBytes          uint64    `json:"rx_bytes"`
	TXBytes          uint64    `json:"tx_bytes"`
	RXBps            float64   `json:"rx_bps"`
	TXBps            float64   `json:"tx_bps"`
	TrafficSource    string    `json:"traffic_source"`
	TrafficAvailable bool      `json:"traffic_available"`
	LastSeen         time.Time `json:"last_seen"`
}

// ProcessTrafficSample is a per-PID rate snapshot from nettop (or similar).
// ProcessTrafficSample 是来自 nettop（或类似工具）的每 PID 速率快照。
type ProcessTrafficSample struct {
	PID         int       `json:"pid"`
	ProcessName string    `json:"process_name"`
	ProcessPath string    `json:"process_path"`
	RXBytes     uint64    `json:"rx_bytes"`
	TXBytes     uint64    `json:"tx_bytes"`
	RXBps       float64   `json:"rx_bps"`
	TXBps       float64   `json:"tx_bps"`
	SeenAt      time.Time `json:"seen_at"`
	// Source labels the backend, e.g. "nettop".
	// Source 标记后端来源，例如 "nettop"。
	Source string `json:"source"`
}

type trafficCacheEntry struct {
	Sample     ProcessTrafficSample
	ProcessKey string
}

// ProcessConnectionMinute is the in-memory minute rollup before SQLite flush.
// ProcessConnectionMinute 是刷入 SQLite 前的内存分钟聚合。
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

// State is the thread-safe daemon memory model for process views.
// State 是进程视图使用的线程安全 daemon 内存模型。
type State struct {
	// mu protects all maps/slices below.
	// mu 保护下方所有 map/slice。
	mu sync.RWMutex
	// latestSummaries is the sorted process top list from the last sample.
	// latestSummaries 是最近一次采样排序后的进程排行。
	latestSummaries []ProcessConnectionSummary
	// latestConnections maps process_key → socket details (memory only).
	// latestConnections 将 process_key 映射到套接字明细（仅内存）。
	latestConnections map[string][]ProcessConnectionDetail
	// latestTraffic caches last known rates by PID from the traffic collector.
	// latestTraffic 缓存流量采集器按 PID 的最近已知速率。
	latestTraffic map[int]trafficCacheEntry
	trafficStatus TrafficBackendStatus
	// minuteBuckets accumulates current/previous minute rollups pending flush.
	// minuteBuckets 累积待刷写的当前/过去分钟聚合。
	minuteBuckets map[string]ProcessConnectionMinute
}

// New creates an empty State with initialized maps.
// New 创建带有已初始化 map 的空 State。
func New() *State {
	return &State{
		latestConnections: map[string][]ProcessConnectionDetail{},
		latestTraffic:     map[int]trafficCacheEntry{},
		trafficStatus:     TrafficBackendStatus{State: TrafficBackendDisabled},
		minuteBuckets:     map[string]ProcessConnectionMinute{},
	}
}

// Update replaces latest connection state from a full sample and updates minutes.
// Update 用完整采样替换最新连接态，并更新分钟桶。
func (s *State) Update(conns []proc.Connection, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Group socket details by process_key.
	// 按 process_key 分组套接字明细。
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

	// Full replace (not merge) so closed sockets disappear next second.
	// 全量替换（非合并），使已关闭套接字在下一秒消失。
	s.latestConnections = byProcess
	s.latestSummaries = make([]ProcessConnectionSummary, 0, len(byProcess))
	currentByPID := make(map[int]string, len(byProcess))
	for key, details := range byProcess {
		if len(details) > 0 {
			currentByPID[details[0].PID] = key
		}
	}
	for pid, entry := range s.latestTraffic {
		processKey, exists := currentByPID[pid]
		if !exists || (entry.ProcessKey != "" && entry.ProcessKey != processKey) || !trafficFresh(entry.Sample, now) {
			delete(s.latestTraffic, pid)
		}
	}
	// Roll up into the clock minute containing `now`.
	// 汇总到包含 `now` 的时钟分钟。
	minuteStart := now.Truncate(time.Minute)
	for key, details := range byProcess {
		// All details for one key share PID/name/path; use the first as identity.
		// 同一 key 的明细共享 PID/名/路径；用第一条作身份。
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
		// Attach cached traffic rates if we have a sample for this PID.
		// 若该 PID 有缓存流量样本则挂上速率。
		if entry, ok := s.latestTraffic[summary.PID]; ok {
			if entry.ProcessKey == "" {
				entry.ProcessKey = summary.ProcessKey
				s.latestTraffic[summary.PID] = entry
			}
			applyTraffic(&summary, entry.Sample)
		}
		s.latestSummaries = append(s.latestSummaries, summary)

		// Update or create the in-memory minute bucket for this process.
		// 更新或创建该进程的内存分钟桶。
		bucketKey := bucketKey(minuteStart, key)
		bucket := s.minuteBuckets[bucketKey]
		// Empty ProcessKey means the map entry was zero-value → initialize.
		// ProcessKey 为空表示 map 项为零值 → 初始化。
		if bucket.ProcessKey == "" {
			bucket = ProcessConnectionMinute{
				MinuteStart: minuteStart,
				PID:         first.PID,
				ProcessName: first.ProcessName,
				ProcessPath: first.ProcessPath,
				ProcessKey:  key,
			}
		}
		// Track peak concurrent connections within the minute.
		// 记录该分钟内峰值并发连接数。
		if len(details) > bucket.MaxConnectionCount {
			bucket.MaxConnectionCount = len(details)
		}
		// Increment how many samples saw this process this minute.
		// 增加该分钟内采样到该进程的次数。
		bucket.SampleCount++
		if lastSeen.After(bucket.LastSeen) {
			bucket.LastSeen = lastSeen
		}
		s.minuteBuckets[bucketKey] = bucket
	}
	// Sort for stable top-N API responses.
	// 排序以保证 top-N API 响应稳定。
	sortSummaries(s.latestSummaries)
}

// LatestSummaries returns a copy of process rows, optionally truncated.
// LatestSummaries 返回进程行的拷贝，可选截断。
func (s *State) LatestSummaries(limit int) []ProcessConnectionSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Copy so callers cannot mutate internal slices.
	// 拷贝以免调用方修改内部 slice。
	items := append([]ProcessConnectionSummary(nil), s.latestSummaries...)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

// UpdateTraffic merges new traffic samples into cache and live summaries.
// UpdateTraffic 将新流量样本合并进缓存与实时摘要。
func (s *State) UpdateTraffic(samples []ProcessTrafficSample) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Index this batch by PID for applying to summaries.
	// 按 PID 索引本批样本以便套到摘要上。
	byPID := map[int]ProcessTrafficSample{}
	for _, sample := range samples {
		if sample.PID <= 0 {
			continue
		}
		byPID[sample.PID] = sample
		entry := trafficCacheEntry{Sample: sample}
		for _, summary := range s.latestSummaries {
			if summary.PID == sample.PID {
				entry.ProcessKey = summary.ProcessKey
				break
			}
		}
		s.latestTraffic[sample.PID] = entry
	}
	if len(byPID) == 0 {
		return
	}

	// Patch currently visible summaries that match this batch.
	// 修补当前可见且匹配本批的摘要。
	for i := range s.latestSummaries {
		sample, ok := byPID[s.latestSummaries[i].PID]
		if !ok {
			continue
		}
		applyTraffic(&s.latestSummaries[i], sample)
	}
}

// SetTrafficBackendStatus replaces the optional backend health snapshot.
// SetTrafficBackendStatus 替换可选流量后端的健康状态。
func (s *State) SetTrafficBackendStatus(status TrafficBackendStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trafficStatus = status
}

// TrafficBackendStatus returns a copy of the backend health snapshot.
// TrafficBackendStatus 返回流量后端健康状态的副本。
func (s *State) TrafficBackendStatus() TrafficBackendStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.trafficStatus
}

// ClearTraffic removes cached attribution and clears rates from live rows.
// ClearTraffic 清除缓存归因及实时行中的速率。
func (s *State) ClearTraffic() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latestTraffic = map[int]trafficCacheEntry{}
	for i := range s.latestSummaries {
		clearTraffic(&s.latestSummaries[i])
	}
}

// LatestConnections returns a copy of socket details for one process_key.
// LatestConnections 返回某 process_key 的套接字明细拷贝。
func (s *State) LatestConnections(processKey string) []ProcessConnectionDetail {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return append([]ProcessConnectionDetail(nil), s.latestConnections[processKey]...)
}

// FlushCompleted removes and returns minutes strictly before the current minute.
// FlushCompleted 移除并返回严格早于当前分钟的分钟数据。
func (s *State) FlushCompleted(before time.Time) []ProcessConnectionMinute {
	return s.flush(before.Truncate(time.Minute), false)
}

// FlushBefore is a test/helper path that can include the previous minute edge.
// FlushBefore 是测试/辅助路径，可包含上一分钟边界。
func (s *State) FlushBefore(before time.Time) []ProcessConnectionMinute {
	return s.flush(before.Truncate(time.Minute), true)
}

// DrainAllMinutes removes and returns every pending minute, including current.
// DrainAllMinutes 移除并返回所有待刷分钟，包括当前分钟。
func (s *State) DrainAllMinutes() []ProcessConnectionMinute {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ProcessConnectionMinute, 0, len(s.minuteBuckets))
	for key, bucket := range s.minuteBuckets {
		out = append(out, bucket)
		delete(s.minuteBuckets, key)
	}
	sortMinutes(out)
	return out
}

// RestoreMinutes merges drained minutes back after persistence failure.
// RestoreMinutes 在持久化失败后把已取出的分钟合并回内存。
func (s *State) RestoreMinutes(minutes []ProcessConnectionMinute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, minute := range minutes {
		key := bucketKey(minute.MinuteStart, minute.ProcessKey)
		existing, ok := s.minuteBuckets[key]
		if !ok {
			s.minuteBuckets[key] = minute
			continue
		}
		if minute.MaxConnectionCount > existing.MaxConnectionCount {
			existing.MaxConnectionCount = minute.MaxConnectionCount
		}
		existing.SampleCount += minute.SampleCount
		if minute.LastSeen.After(existing.LastSeen) {
			existing.LastSeen = minute.LastSeen
		}
		if existing.ProcessName == "" {
			existing.ProcessName = minute.ProcessName
		}
		if existing.ProcessPath == "" {
			existing.ProcessPath = minute.ProcessPath
		}
		s.minuteBuckets[key] = existing
	}
}

// flush implements the shared minute-bucket eviction logic.
// flush 实现共享的分钟桶淘汰逻辑。
func (s *State) flush(cutoff time.Time, includeCutoff bool) []ProcessConnectionMinute {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := []ProcessConnectionMinute{}
	for key, bucket := range s.minuteBuckets {
		// Default: only minutes that started before cutoff (completed minutes).
		// 默认：仅起点早于 cutoff 的分钟（已完成分钟）。
		flush := bucket.MinuteStart.Before(cutoff)
		// Optional edge case used by tests / alternate flush policies.
		// 测试或其它刷写策略使用的可选边界情况。
		if includeCutoff && bucket.MinuteStart.Equal(cutoff.Add(-time.Minute)) {
			flush = true
		}
		if !flush {
			continue
		}
		out = append(out, bucket)
		delete(s.minuteBuckets, key)
	}
	// Stable order for deterministic DB writes/tests.
	// 稳定顺序，保证写库/测试可预期。
	sortMinutes(out)
	return out
}

func sortMinutes(items []ProcessConnectionMinute) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].MinuteStart.Equal(items[j].MinuteStart) {
			return items[i].ProcessKey < items[j].ProcessKey
		}
		return items[i].MinuteStart.Before(items[j].MinuteStart)
	})
}

// sortSummaries orders by connection count desc, then name, then PID.
// sortSummaries 按连接数降序，再按名称、PID 排序。
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

// applyTraffic copies rate fields onto a summary and may enrich name/path.
// applyTraffic 将速率字段拷到摘要上，并可能补全名称/路径。
func applyTraffic(summary *ProcessConnectionSummary, sample ProcessTrafficSample) {
	summary.RXBytes = sample.RXBytes
	summary.TXBytes = sample.TXBytes
	summary.RXBps = sample.RXBps
	summary.TXBps = sample.TXBps
	summary.TrafficSource = sample.Source
	summary.TrafficAvailable = true
	// Prefer a real name from nettop if connection sampling only had "unknown".
	// 若连接采样只有 "unknown"，优先用 nettop 的真实名。
	if (summary.ProcessName == "" || summary.ProcessName == "unknown") && sample.ProcessName != "" {
		summary.ProcessName = sample.ProcessName
	}
	if summary.ProcessPath == "" && sample.ProcessPath != "" {
		summary.ProcessPath = sample.ProcessPath
	}
	// Traffic sample time can refresh last_seen.
	// 流量样本时间可刷新 last_seen。
	if sample.SeenAt.After(summary.LastSeen) {
		summary.LastSeen = sample.SeenAt
	}
}

func clearTraffic(summary *ProcessConnectionSummary) {
	summary.RXBytes = 0
	summary.TXBytes = 0
	summary.RXBps = 0
	summary.TXBps = 0
	summary.TrafficSource = ""
	summary.TrafficAvailable = false
}

func trafficFresh(sample ProcessTrafficSample, now time.Time) bool {
	if sample.SeenAt.IsZero() {
		return false
	}
	age := now.Sub(sample.SeenAt)
	return age >= 0 && age < trafficTTL
}

// bucketKey uniquely identifies a process within a minute for the map.
// bucketKey 在 map 中唯一标识某分钟内的某进程。
func bucketKey(minuteStart time.Time, processKey string) string {
	return minuteStart.Format(time.RFC3339) + "|" + processKey
}
