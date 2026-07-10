// Package storage implements SQLite persistence for interface and process data.
// storage 包用 SQLite 持久化网卡样本与进程数据。
package storage

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bytepulse/internal/proc"

	// Pure-Go SQLite driver (no CGO).
	// 纯 Go SQLite 驱动（无 CGO）。
	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a query expects a row but none exists.
// ErrNotFound 在查询期望有行却不存在时返回。
var ErrNotFound = errors.New("not found")

// Sample is one interface interval stored in the samples table.
// Sample 是 samples 表中一条网卡区间样本。
type Sample struct {
	// Timestamp is when this interval ended (sample time).
	// Timestamp 是本区间结束时刻（采样时间）。
	Timestamp time.Time `json:"timestamp"`
	// Interface is the OS NIC name, or "all" for aggregates.
	// Interface 是操作系统网卡名，聚合时可能为 "all"。
	Interface string `json:"interface"`
	// RXBytes / TXBytes are byte deltas for this interval (not cumulative).
	// RXBytes / TXBytes 是本区间字节增量（非累计值）。
	RXBytes uint64 `json:"rx_bytes"`
	TXBytes uint64 `json:"tx_bytes"`
	// RXSpeedBps / TXSpeedBps are average B/s over IntervalSec.
	// RXSpeedBps / TXSpeedBps 是 IntervalSec 上的平均 B/s。
	RXSpeedBps float64 `json:"rx_speed_bps"`
	TXSpeedBps float64 `json:"tx_speed_bps"`
	// IntervalSec is the measured duration used for rate calculation.
	// IntervalSec 是用于算速率的实测时长（秒）。
	IntervalSec float64 `json:"interval_sec"`
}

// SummaryResult is a range total used by report/CLI/web summary APIs.
// SummaryResult 是 report/CLI/web 汇总 API 使用的区间合计。
type SummaryResult struct {
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	RXBytes uint64    `json:"rx_bytes"`
	TXBytes uint64    `json:"tx_bytes"`
	// DurationSec is the requested window length (end-start), not busy time.
	// DurationSec 是请求窗口长度（end-start），不是繁忙时间。
	DurationSec float64 `json:"duration_sec"`
}

// ProcessConnectionMinute is one process's activity within one clock minute.
// ProcessConnectionMinute 是某进程在某个时钟分钟内的活跃聚合。
type ProcessConnectionMinute struct {
	// MinuteStart is truncated to the minute boundary.
	// MinuteStart 截断到分钟边界。
	MinuteStart time.Time `json:"minute_start"`
	PID         int       `json:"pid"`
	ProcessName string    `json:"process_name"`
	ProcessPath string    `json:"process_path"`
	// ProcessKey is typically "pid:create_time_ms" to reduce PID reuse confusion.
	// ProcessKey 通常为 "pid:create_time_ms"，降低 PID 复用混淆。
	ProcessKey string `json:"process_key"`
	// MaxConnectionCount is the peak connections observed in any single sample.
	// MaxConnectionCount 是任意单次采样中观察到的峰值连接数。
	MaxConnectionCount int `json:"max_connection_count"`
	// SampleCount is how many 1s samples saw this process in the minute (≤~60).
	// SampleCount 是该分钟内采样到该进程的次数（约 ≤60）。
	SampleCount int       `json:"sample_count"`
	LastSeen    time.Time `json:"last_seen"`
}

// ProcessConnectionSummary is a historical top-process query row.
// ProcessConnectionSummary 是历史进程排行查询的一行。
type ProcessConnectionSummary struct {
	PID         int    `json:"pid"`
	ProcessName string `json:"process_name"`
	ProcessPath string `json:"process_path"`
	ProcessKey  string `json:"process_key"`
	// ConnectionCount is the peak concurrent socket count in the range
	// (MAX of max_connection_count). Matches the CONNS column semantics.
	// ConnectionCount 是区间内峰值并发套接字数（max_connection_count 的 MAX）。
	// 与 CONNS 列语义一致。
	ConnectionCount int `json:"connection_count"`
	// SampleCount is how many 1s samples saw this process (SUM of sample_count).
	// Useful as an activity/duration signal, not as connection count.
	// SampleCount 是采样到该进程的次数之和（sample_count 的 SUM）。
	// 表示活跃时长信号，不是连接数。
	SampleCount int       `json:"sample_count"`
	LastSeen    time.Time `json:"last_seen"`
}

// FilterSelfSummaries removes bytepulse-like rows when excludeSelf is true.
// Historical rows use name/path only (selfPID is 0).
// FilterSelfSummaries 在 excludeSelf 为 true 时去掉类 bytepulse 行。
// 历史行仅按名称/路径匹配（selfPID 为 0）。
func FilterSelfSummaries(items []ProcessConnectionSummary, excludeSelf bool) []ProcessConnectionSummary {
	if !excludeSelf || len(items) == 0 {
		return items
	}
	out := make([]ProcessConnectionSummary, 0, len(items))
	for _, item := range items {
		if proc.IsSelfProcess(item.PID, item.ProcessName, item.ProcessPath, 0) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// AvgRXBps returns average download B/s over the summary window.
// AvgRXBps 返回汇总窗口内的平均下载 B/s。
func (s SummaryResult) AvgRXBps() float64 {
	if s.DurationSec <= 0 {
		return 0
	}
	return float64(s.RXBytes) / s.DurationSec
}

// AvgTXBps returns average upload B/s over the summary window.
// AvgTXBps 返回汇总窗口内的平均上传 B/s。
func (s SummaryResult) AvgTXBps() float64 {
	if s.DurationSec <= 0 {
		return 0
	}
	return float64(s.TXBytes) / s.DurationSec
}

// AvgTotalBps is download + upload average rates.
// AvgTotalBps 为下载 + 上传平均速率之和。
func (s SummaryResult) AvgTotalBps() float64 {
	return s.AvgRXBps() + s.AvgTXBps()
}

// Bucket is an hourly/daily aggregation cell.
// Bucket 是小时/日聚合单元。
type Bucket struct {
	Start   time.Time `json:"start"`
	RXBytes uint64    `json:"rx_bytes"`
	TXBytes uint64    `json:"tx_bytes"`
	// DurationSec is the overlap of this bucket with the query window.
	// DurationSec 是本桶与查询窗口的重叠秒数。
	DurationSec float64 `json:"duration_sec"`
}

// AvgRXBps for a bucket uses clipped DurationSec as the denominator.
// 桶的 AvgRXBps 用裁剪后的 DurationSec 作分母。
func (b Bucket) AvgRXBps() float64 {
	if b.DurationSec <= 0 {
		return 0
	}
	return float64(b.RXBytes) / b.DurationSec
}

// AvgTXBps for a bucket uses clipped DurationSec as the denominator.
// 桶的 AvgTXBps 用裁剪后的 DurationSec 作分母。
func (b Bucket) AvgTXBps() float64 {
	if b.DurationSec <= 0 {
		return 0
	}
	return float64(b.TXBytes) / b.DurationSec
}

// Store wraps a single SQLite connection (MaxOpenConns=1).
// Store 包装单条 SQLite 连接（MaxOpenConns=1）。
type Store struct {
	db *sql.DB
}

// Open creates parent dirs, opens the DB, and limits concurrency to one conn.
// Open 创建父目录、打开数据库，并将并发限制为单连接。
func Open(path string) (*Store, error) {
	// Ensure ~/.bytepulse (or custom parent) exists.
	// 确保 ~/.bytepulse（或自定义父目录）存在。
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// Driver name "sqlite" is registered by modernc.org/sqlite.
	// 驱动名 "sqlite" 由 modernc.org/sqlite 注册。
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Single connection avoids SQLite write contention surprises.
	// 单连接避免 SQLite 写争用带来的意外。
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}

// Close closes the underlying database handle.
// Close 关闭底层数据库句柄。
func (s *Store) Close() error {
	return s.db.Close()
}

// Migrate applies PRAGMAs, creates tables/indexes, and column upgrades.
// Migrate 应用 PRAGMA、创建表/索引，并做列升级。
func (s *Store) Migrate() error {
	// Exec multiple statements: WAL, busy timeout, tables, indexes.
	// 执行多条语句：WAL、busy 超时、表、索引。
	_, err := s.db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA busy_timeout = 5000;
		CREATE TABLE IF NOT EXISTS samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER NOT NULL,
			interface_name TEXT NOT NULL,
			rx_bytes INTEGER NOT NULL,
			tx_bytes INTEGER NOT NULL,
			rx_speed_bps REAL NOT NULL,
			tx_speed_bps REAL NOT NULL,
			interval_sec REAL NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_samples_ts ON samples(ts);
		CREATE INDEX IF NOT EXISTS idx_samples_interface_ts ON samples(interface_name, ts);
		CREATE TABLE IF NOT EXISTS process_connection_minutes (
			minute_start INTEGER NOT NULL,
			process_key TEXT NOT NULL,
			pid INTEGER NOT NULL,
			process_name TEXT NOT NULL,
			process_path TEXT NOT NULL DEFAULT '',
			max_connection_count INTEGER NOT NULL,
			sample_count INTEGER NOT NULL,
			last_seen INTEGER NOT NULL,
			PRIMARY KEY (minute_start, process_key)
		);
		CREATE INDEX IF NOT EXISTS idx_pcm_minute ON process_connection_minutes(minute_start);
		CREATE INDEX IF NOT EXISTS idx_pcm_process_minute ON process_connection_minutes(process_key, minute_start);
	`)
	if err != nil {
		return err
	}
	// Older DBs may lack process_path; add it idempotently.
	// 旧库可能没有 process_path；幂等添加。
	return s.ensureProcessPathColumn()
}

// ensureProcessPathColumn adds process_path for DBs created before that column.
// ensureProcessPathColumn 为创建于该列之前的库添加 process_path。
func (s *Store) ensureProcessPathColumn() error {
	// SQLite has no IF NOT EXISTS for ADD COLUMN; ignore duplicate errors.
	// SQLite 的 ADD COLUMN 无 IF NOT EXISTS；忽略重复列错误。
	_, err := s.db.Exec(`ALTER TABLE process_connection_minutes ADD COLUMN process_path TEXT NOT NULL DEFAULT ''`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return err
	}
	return nil
}

// InsertSamples writes a batch of interface samples in one transaction.
// InsertSamples 在一个事务中写入一批网卡样本。
func (s *Store) InsertSamples(samples []Sample) error {
	// No-op for empty batches.
	// 空批次直接返回。
	if len(samples) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	// Rollback is a no-op after Commit.
	// Commit 之后 Rollback 为 no-op。
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO samples (
			ts, interface_name, rx_bytes, tx_bytes, rx_speed_bps, tx_speed_bps, interval_sec
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sample := range samples {
		// Store timestamps as UTC unix milliseconds.
		// 时间戳存为 UTC Unix 毫秒。
		_, err := stmt.Exec(
			toMillis(sample.Timestamp),
			sample.Interface,
			sample.RXBytes,
			sample.TXBytes,
			sample.RXSpeedBps,
			sample.TXSpeedBps,
			sample.IntervalSec,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Cleanup deletes samples older than now-retention.
// Cleanup 删除早于 now-retention 的样本。
func (s *Store) Cleanup(now time.Time, retention time.Duration) error {
	// Safety default if callers pass zero/negative retention.
	// 调用方传入零/负保留期时的安全默认值。
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	cutoff := now.Add(-retention)
	_, err := s.db.Exec(`DELETE FROM samples WHERE ts < ?`, toMillis(cutoff))
	return err
}

// UpsertProcessConnectionMinutes merges minute rollups on primary key conflict.
// UpsertProcessConnectionMinutes 在主键冲突时合并分钟聚合。
func (s *Store) UpsertProcessConnectionMinutes(items []ProcessConnectionMinute) error {
	if len(items) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// ON CONFLICT: take max peak connections, sum sample counts, max last_seen.
	// ON CONFLICT：峰值连接取 max，采样次数相加，last_seen 取 max。
	stmt, err := tx.Prepare(`
		INSERT INTO process_connection_minutes (
			minute_start, process_key, pid, process_name, process_path, max_connection_count, sample_count, last_seen
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(minute_start, process_key) DO UPDATE SET
			pid = excluded.pid,
			process_name = excluded.process_name,
			process_path = excluded.process_path,
			max_connection_count = MAX(process_connection_minutes.max_connection_count, excluded.max_connection_count),
			sample_count = process_connection_minutes.sample_count + excluded.sample_count,
			last_seen = MAX(process_connection_minutes.last_seen, excluded.last_seen)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, item := range items {
		// Normalize minute_start to the minute boundary before insert.
		// 插入前将 minute_start 规范到分钟边界。
		_, err := stmt.Exec(
			toMillis(item.MinuteStart.Truncate(time.Minute)),
			item.ProcessKey,
			item.PID,
			item.ProcessName,
			item.ProcessPath,
			item.MaxConnectionCount,
			item.SampleCount,
			toMillis(item.LastSeen),
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CleanupProcessConnectionMinutes deletes expired process minute rows.
// CleanupProcessConnectionMinutes 删除过期的进程分钟行。
func (s *Store) CleanupProcessConnectionMinutes(now time.Time, retention time.Duration) error {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	cutoff := now.Add(-retention)
	_, err := s.db.Exec(`DELETE FROM process_connection_minutes WHERE minute_start < ?`, toMillis(cutoff))
	return err
}

// TopProcessConnectionMinutes ranks processes by peak connections in [start, end].
// TopProcessConnectionMinutes 在 [start, end] 内按峰值连接数对进程排序。
func (s *Store) TopProcessConnectionMinutes(start, end time.Time, limit int) ([]ProcessConnectionSummary, error) {
	if limit <= 0 {
		limit = 30
	}
	// connection_count = peak sockets; sample_count = activity ticks (secondary rank).
	// connection_count = 峰值套接字；sample_count = 活跃采样次数（次要排序）。
	rows, err := s.db.Query(`
		SELECT
			process_key,
			pid,
			process_name,
			process_path,
			COALESCE(MAX(max_connection_count), 0) AS connection_count,
			COALESCE(SUM(sample_count), 0) AS sample_count,
			COALESCE(MAX(last_seen), 0) AS last_seen
		FROM process_connection_minutes
		WHERE minute_start >= ? AND minute_start <= ?
		GROUP BY process_key, pid, process_name, process_path
		ORDER BY connection_count DESC, sample_count DESC, process_name ASC
		LIMIT ?
	`, toMillis(start.Truncate(time.Minute)), toMillis(end), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ProcessConnectionSummary{}
	for rows.Next() {
		var lastSeen int64
		var item ProcessConnectionSummary
		if err := rows.Scan(
			&item.ProcessKey,
			&item.PID,
			&item.ProcessName,
			&item.ProcessPath,
			&item.ConnectionCount,
			&item.SampleCount,
			&lastSeen,
		); err != nil {
			return nil, err
		}
		item.LastSeen = fromMillis(lastSeen)
		out = append(out, item)
	}
	return out, rows.Err()
}

// LatestAggregateSample sums all NICs (or one) at the newest timestamp.
// LatestAggregateSample 在最新时间戳上汇总全部（或单块）网卡。
func (s *Store) LatestAggregateSample(interfaceName string) (Sample, error) {
	// Find the maximum ts matching the interface filter.
	// 查找匹配网卡过滤条件的最大 ts。
	var latest int64
	row := s.db.QueryRow(latestTimestampSQL(interfaceName), latestTimestampArgs(interfaceName)...)
	if err := row.Scan(&latest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Sample{}, ErrNotFound
		}
		return Sample{}, err
	}
	// COALESCE(MAX(ts),0) returns 0 when the table is empty.
	// 表为空时 COALESCE(MAX(ts),0) 返回 0。
	if latest == 0 {
		return Sample{}, ErrNotFound
	}

	args := []any{latest}
	// Sum deltas/speeds across interfaces sharing that timestamp.
	// 对共享该时间戳的各网卡增量/速率求和。
	query := `
		SELECT
			COALESCE(SUM(rx_bytes), 0),
			COALESCE(SUM(tx_bytes), 0),
			COALESCE(SUM(rx_speed_bps), 0),
			COALESCE(SUM(tx_speed_bps), 0),
			COALESCE(MAX(interval_sec), 0)
		FROM samples
		WHERE ts = ?
	`
	if interfaceName != "" {
		query += ` AND interface_name = ?`
		args = append(args, interfaceName)
	}

	var sample Sample
	sample.Timestamp = fromMillis(latest)
	sample.Interface = interfaceName
	// Label multi-interface aggregates as "all".
	// 多网卡聚合标记为 "all"。
	if sample.Interface == "" {
		sample.Interface = "all"
	}

	err := s.db.QueryRow(query, args...).Scan(
		&sample.RXBytes,
		&sample.TXBytes,
		&sample.RXSpeedBps,
		&sample.TXSpeedBps,
		&sample.IntervalSec,
	)
	if err != nil {
		return Sample{}, err
	}
	return sample, nil
}

// Summary sums RX/TX byte deltas in [start, end] for an optional interface.
// Summary 在 [start, end] 内对可选网卡汇总 RX/TX 字节增量。
func (s *Store) Summary(start, end time.Time, interfaceName string) (SummaryResult, error) {
	query := `
		SELECT COALESCE(SUM(rx_bytes), 0), COALESCE(SUM(tx_bytes), 0)
		FROM samples
		WHERE ts >= ? AND ts <= ?
	`
	args := []any{toMillis(start), toMillis(end)}
	if interfaceName != "" {
		query += ` AND interface_name = ?`
		args = append(args, interfaceName)
	}

	result := SummaryResult{
		Start: start,
		End:   end,
		// Duration is the requested window, used for average rates.
		// Duration 是请求窗口，用于平均速率。
		DurationSec: end.Sub(start).Seconds(),
	}
	if err := s.db.QueryRow(query, args...).Scan(&result.RXBytes, &result.TXBytes); err != nil {
		return SummaryResult{}, err
	}
	return result, nil
}

// RecentSeries returns per-timestamp aggregates for charting (e.g. last hour).
// RecentSeries 返回按时间戳聚合的序列，用于画图（例如最近一小时）。
func (s *Store) RecentSeries(start, end time.Time, interfaceName string) ([]Sample, error) {
	query := `
		SELECT
			ts,
			COALESCE(SUM(rx_bytes), 0),
			COALESCE(SUM(tx_bytes), 0),
			COALESCE(SUM(rx_speed_bps), 0),
			COALESCE(SUM(tx_speed_bps), 0),
			COALESCE(MAX(interval_sec), 0)
		FROM samples
		WHERE ts >= ? AND ts <= ?
	`
	args := []any{toMillis(start), toMillis(end)}
	if interfaceName != "" {
		query += ` AND interface_name = ?`
		args = append(args, interfaceName)
	}
	// One row per sample timestamp (all interfaces collapsed when filter empty).
	// 每个采样时间戳一行（未过滤时合并全部网卡）。
	query += ` GROUP BY ts ORDER BY ts ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Sample
	for rows.Next() {
		var ts int64
		sample := Sample{Interface: interfaceName}
		if sample.Interface == "" {
			sample.Interface = "all"
		}
		if err := rows.Scan(
			&ts,
			&sample.RXBytes,
			&sample.TXBytes,
			&sample.RXSpeedBps,
			&sample.TXSpeedBps,
			&sample.IntervalSec,
		); err != nil {
			return nil, err
		}
		sample.Timestamp = fromMillis(ts)
		out = append(out, sample)
	}
	return out, rows.Err()
}

// Hourly buckets traffic into 3600-second (UTC-based) windows.
// Hourly 将流量按 3600 秒（基于 UTC）分桶。
func (s *Store) Hourly(start, end time.Time, interfaceName string) ([]Bucket, error) {
	return s.buckets(start, end, interfaceName, 3600)
}

// Daily buckets traffic into 86400-second Unix-day windows (not local midnight).
// Daily 将流量按 86400 秒 Unix 日分桶（非本地零点）。
func (s *Store) Daily(start, end time.Time, interfaceName string) ([]Bucket, error) {
	return s.buckets(start, end, interfaceName, 86400)
}

// buckets groups samples by floor(ts / size) integer time buckets.
// buckets 按 floor(ts / size) 整数时间桶分组样本。
func (s *Store) buckets(start, end time.Time, interfaceName string, sizeSec int64) ([]Bucket, error) {
	// Integer division on ms timestamps forms stable bucket starts.
	// 对毫秒时间戳做整数除法得到稳定的桶起点。
	query := `
		SELECT
			(ts / (? * 1000)) * (? * 1000) AS bucket_ts,
			COALESCE(SUM(rx_bytes), 0),
			COALESCE(SUM(tx_bytes), 0)
		FROM samples
		WHERE ts >= ? AND ts <= ?
	`
	args := []any{sizeSec, sizeSec, toMillis(start), toMillis(end)}
	if interfaceName != "" {
		query += ` AND interface_name = ?`
		args = append(args, interfaceName)
	}
	query += ` GROUP BY bucket_ts ORDER BY bucket_ts ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Bucket
	for rows.Next() {
		var ts int64
		var b Bucket
		if err := rows.Scan(&ts, &b.RXBytes, &b.TXBytes); err != nil {
			return nil, err
		}
		b.Start = fromMillis(ts)
		// Clip duration to the intersection of bucket and query range.
		// 将时长裁剪为桶与查询区间的交集。
		b.DurationSec = clippedDuration(b.Start, time.Duration(sizeSec)*time.Second, start, end)
		out = append(out, b)
	}
	return out, rows.Err()
}

// clippedDuration returns seconds of overlap between [bucketStart, +size) and [start, end].
// clippedDuration 返回 [bucketStart, +size) 与 [start, end] 的重叠秒数。
func clippedDuration(bucketStart time.Time, bucketSize time.Duration, start, end time.Time) float64 {
	from := bucketStart
	// Raise lower bound if the query starts mid-bucket.
	// 若查询从桶中间开始，抬高下界。
	if start.After(from) {
		from = start
	}
	to := bucketStart.Add(bucketSize)
	// Lower upper bound if the query ends mid-bucket.
	// 若查询在桶中间结束，压低下界上端。
	if end.Before(to) {
		to = end
	}
	// Empty intersection → zero duration (avoid negative averages).
	// 无交集 → 零时长（避免负平均）。
	if !to.After(from) {
		return 0
	}
	return to.Sub(from).Seconds()
}

// latestTimestampSQL builds the MAX(ts) query with optional interface filter.
// latestTimestampSQL 构建带可选网卡过滤的 MAX(ts) 查询。
func latestTimestampSQL(interfaceName string) string {
	if interfaceName == "" {
		return `SELECT COALESCE(MAX(ts), 0) FROM samples`
	}
	return `SELECT COALESCE(MAX(ts), 0) FROM samples WHERE interface_name = ?`
}

// latestTimestampArgs returns bind args for latestTimestampSQL.
// latestTimestampArgs 返回 latestTimestampSQL 的绑定参数。
func latestTimestampArgs(interfaceName string) []any {
	if interfaceName == "" {
		return nil
	}
	return []any{interfaceName}
}

// toMillis stores times as UTC unix milliseconds for stable SQL ordering.
// toMillis 将时间存为 UTC Unix 毫秒，保证 SQL 排序稳定。
func toMillis(t time.Time) int64 {
	return t.UTC().UnixMilli()
}

// fromMillis converts stored ms back to local Time for display.
// fromMillis 将存储的毫秒转回本地 Time 以便展示。
func fromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).Local()
}
