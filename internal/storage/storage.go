package storage

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Sample struct {
	Timestamp   time.Time `json:"timestamp"`
	Interface   string    `json:"interface"`
	RXBytes     uint64    `json:"rx_bytes"`
	TXBytes     uint64    `json:"tx_bytes"`
	RXSpeedBps  float64   `json:"rx_speed_bps"`
	TXSpeedBps  float64   `json:"tx_speed_bps"`
	IntervalSec float64   `json:"interval_sec"`
}

type SummaryResult struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	RXBytes     uint64    `json:"rx_bytes"`
	TXBytes     uint64    `json:"tx_bytes"`
	DurationSec float64   `json:"duration_sec"`
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

type ProcessConnectionSummary struct {
	PID             int       `json:"pid"`
	ProcessName     string    `json:"process_name"`
	ProcessPath     string    `json:"process_path"`
	ProcessKey      string    `json:"process_key"`
	ConnectionCount int       `json:"connection_count"`
	LastSeen        time.Time `json:"last_seen"`
}

func (s SummaryResult) AvgRXBps() float64 {
	if s.DurationSec <= 0 {
		return 0
	}
	return float64(s.RXBytes) / s.DurationSec
}

func (s SummaryResult) AvgTXBps() float64 {
	if s.DurationSec <= 0 {
		return 0
	}
	return float64(s.TXBytes) / s.DurationSec
}

func (s SummaryResult) AvgTotalBps() float64 {
	return s.AvgRXBps() + s.AvgTXBps()
}

type Bucket struct {
	Start       time.Time `json:"start"`
	RXBytes     uint64    `json:"rx_bytes"`
	TXBytes     uint64    `json:"tx_bytes"`
	DurationSec float64   `json:"duration_sec"`
}

func (b Bucket) AvgRXBps() float64 {
	if b.DurationSec <= 0 {
		return 0
	}
	return float64(b.RXBytes) / b.DurationSec
}

func (b Bucket) AvgTXBps() float64 {
	if b.DurationSec <= 0 {
		return 0
	}
	return float64(b.TXBytes) / b.DurationSec
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Migrate() error {
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
	return s.ensureProcessPathColumn()
}

func (s *Store) ensureProcessPathColumn() error {
	_, err := s.db.Exec(`ALTER TABLE process_connection_minutes ADD COLUMN process_path TEXT NOT NULL DEFAULT ''`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return err
	}
	return nil
}

func (s *Store) InsertSamples(samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
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

func (s *Store) Cleanup(now time.Time, retention time.Duration) error {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	cutoff := now.Add(-retention)
	_, err := s.db.Exec(`DELETE FROM samples WHERE ts < ?`, toMillis(cutoff))
	return err
}

func (s *Store) UpsertProcessConnectionMinutes(items []ProcessConnectionMinute) error {
	if len(items) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

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

func (s *Store) CleanupProcessConnectionMinutes(now time.Time, retention time.Duration) error {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	cutoff := now.Add(-retention)
	_, err := s.db.Exec(`DELETE FROM process_connection_minutes WHERE minute_start < ?`, toMillis(cutoff))
	return err
}

func (s *Store) TopProcessConnectionMinutes(start, end time.Time, limit int) ([]ProcessConnectionSummary, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.Query(`
		SELECT
			process_key,
			pid,
			process_name,
			process_path,
			COALESCE(SUM(sample_count), 0) AS connection_count,
			COALESCE(MAX(last_seen), 0) AS last_seen,
			COALESCE(MAX(max_connection_count), 0) AS max_connection_count
		FROM process_connection_minutes
		WHERE minute_start >= ? AND minute_start <= ?
		GROUP BY process_key, pid, process_name, process_path
		ORDER BY connection_count DESC, max_connection_count DESC, process_name ASC
		LIMIT ?
	`, toMillis(start.Truncate(time.Minute)), toMillis(end), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ProcessConnectionSummary{}
	for rows.Next() {
		var lastSeen int64
		var ignoredMax int
		var item ProcessConnectionSummary
		if err := rows.Scan(
			&item.ProcessKey,
			&item.PID,
			&item.ProcessName,
			&item.ProcessPath,
			&item.ConnectionCount,
			&lastSeen,
			&ignoredMax,
		); err != nil {
			return nil, err
		}
		item.LastSeen = fromMillis(lastSeen)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) LatestAggregateSample(interfaceName string) (Sample, error) {
	var latest int64
	row := s.db.QueryRow(latestTimestampSQL(interfaceName), latestTimestampArgs(interfaceName)...)
	if err := row.Scan(&latest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Sample{}, ErrNotFound
		}
		return Sample{}, err
	}
	if latest == 0 {
		return Sample{}, ErrNotFound
	}

	args := []any{latest}
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
		Start:       start,
		End:         end,
		DurationSec: end.Sub(start).Seconds(),
	}
	if err := s.db.QueryRow(query, args...).Scan(&result.RXBytes, &result.TXBytes); err != nil {
		return SummaryResult{}, err
	}
	return result, nil
}

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

func (s *Store) Hourly(start, end time.Time, interfaceName string) ([]Bucket, error) {
	return s.buckets(start, end, interfaceName, 3600)
}

func (s *Store) Daily(start, end time.Time, interfaceName string) ([]Bucket, error) {
	return s.buckets(start, end, interfaceName, 86400)
}

func (s *Store) buckets(start, end time.Time, interfaceName string, sizeSec int64) ([]Bucket, error) {
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
		b.DurationSec = clippedDuration(b.Start, time.Duration(sizeSec)*time.Second, start, end)
		out = append(out, b)
	}
	return out, rows.Err()
}

func clippedDuration(bucketStart time.Time, bucketSize time.Duration, start, end time.Time) float64 {
	from := bucketStart
	if start.After(from) {
		from = start
	}
	to := bucketStart.Add(bucketSize)
	if end.Before(to) {
		to = end
	}
	if !to.After(from) {
		return 0
	}
	return to.Sub(from).Seconds()
}

func latestTimestampSQL(interfaceName string) string {
	if interfaceName == "" {
		return `SELECT COALESCE(MAX(ts), 0) FROM samples`
	}
	return `SELECT COALESCE(MAX(ts), 0) FROM samples WHERE interface_name = ?`
}

func latestTimestampArgs(interfaceName string) []any {
	if interfaceName == "" {
		return nil
	}
	return []any{interfaceName}
}

func toMillis(t time.Time) int64 {
	return t.UTC().UnixMilli()
}

func fromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).Local()
}
