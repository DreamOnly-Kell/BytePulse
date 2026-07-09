// nettop CSV parser for per-process byte deltas / rates.
// 用于每进程字节增量/速率的 nettop CSV 解析器。
package proctraffic

import (
	"encoding/csv"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Sample is one process traffic observation from a nettop row.
// Sample 是 nettop 一行对应的一条进程流量观测。
type Sample struct {
	PID         int       `json:"pid"`
	ProcessName string    `json:"process_name"`
	ProcessPath string    `json:"process_path"`
	// RXBytes / TXBytes are values from the nettop columns (often deltas with -d).
	// RXBytes / TXBytes 来自 nettop 列（配合 -d 时常为增量）。
	RXBytes uint64 `json:"rx_bytes"`
	TXBytes uint64 `json:"tx_bytes"`
	// RXBps / TXBps are currently set equal to RX/TX when -s 1 (approx B/s).
	// RXBps / TXBps 在 -s 1 时当前直接等于 RX/TX（近似 B/s）。
	RXBps  float64   `json:"rx_bps"`
	TXBps  float64   `json:"tx_bps"`
	SeenAt time.Time `json:"seen_at"`
	// Source is always "nettop" for this parser.
	// 本解析器的 Source 恒为 "nettop"。
	Source string `json:"source"`
}

// ParseNettopCSV parses a full CSV document (header + rows) into Samples.
// ParseNettopCSV 将完整 CSV 文档（表头 + 行）解析为 Sample 列表。
func ParseNettopCSV(r io.Reader, seenAt time.Time) ([]Sample, error) {
	// Flexible field count: nettop columns vary by version/flags.
	// 字段数灵活：nettop 列随版本/参数变化。
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	// Need at least header + one data row.
	// 至少需要表头 + 一行数据。
	if len(records) < 2 {
		return []Sample{}, nil
	}

	// Normalize headers for case/spacing tolerant matching.
	// 规范化表头以便大小写/空格宽松匹配。
	header := normalizeHeader(records[0])
	// Prefer explicit pid / process columns when present.
	// 有显式 pid / process 列时优先使用。
	pidIdx := findColumn(header, "pid")
	processIdx := findColumn(header, "process", "command", "proc", "name")
	// Some nettop modes use a combined "process.pid" style column.
	// 某些 nettop 模式使用合并的 "process.pid" 风格列。
	processPIDIdx := -1
	if pidIdx < 0 || processIdx < 0 {
		processPIDIdx = findProcessPIDColumn(header)
	}
	// RX/TX column name candidates across nettop versions.
	// 跨 nettop 版本的 RX/TX 列名候选。
	rxIdx := findColumn(header, "bytes_in", "rx_bytes", "rxbytes", "rx", "in")
	txIdx := findColumn(header, "bytes_out", "tx_bytes", "txbytes", "tx", "out")
	// Cannot attribute without byte columns and some process identity column.
	// 没有字节列和进程身份列则无法归因。
	if rxIdx < 0 || txIdx < 0 || (processPIDIdx < 0 && (pidIdx < 0 || processIdx < 0)) {
		return []Sample{}, nil
	}

	out := []Sample{}
	for _, record := range records[1:] {
		// Skip short/malformed rows.
		// 跳过过短/畸形行。
		requiredIdx := maxIndex(rxIdx, txIdx, pidIdx, processIdx, processPIDIdx)
		if len(record) <= requiredIdx {
			continue
		}
		// Resolve PID and process path/name fields.
		// 解析 PID 与进程路径/名称字段。
		pid, path, ok := processFields(record, pidIdx, processIdx, processPIDIdx)
		if !ok {
			continue
		}
		rx, err := parseUint(record[rxIdx])
		if err != nil {
			continue
		}
		tx, err := parseUint(record[txIdx])
		if err != nil {
			continue
		}
		name := processName(path)
		out = append(out, Sample{
			PID:         pid,
			ProcessName: name,
			ProcessPath: path,
			RXBytes:     rx,
			TXBytes:     tx,
			// With 1s delta mode, byte delta ≈ bytes/sec.
			// 在 1 秒增量模式下，字节增量 ≈ 字节/秒。
			RXBps:  float64(rx),
			TXBps:  float64(tx),
			SeenAt: seenAt,
			Source: "nettop",
		})
	}
	return out, nil
}

// findProcessPIDColumn finds an empty-named column used as process.pid carrier.
// findProcessPIDColumn 查找用作 process.pid 载体的空名列。
func findProcessPIDColumn(header []string) int {
	for i, col := range header {
		// Skip index 0; look for an unnamed column after the first.
		// 跳过下标 0；在首列之后查找未命名列。
		if i == 0 {
			continue
		}
		if col == "" {
			return i
		}
	}
	return -1
}

// processFields extracts PID and path from either split or combined columns.
// processFields 从分离列或合并列提取 PID 与路径。
func processFields(record []string, pidIdx, processIdx, processPIDIdx int) (int, string, bool) {
	// Combined "Name.12345" column path.
	// 合并的 "Name.12345" 列路径。
	if processPIDIdx >= 0 {
		return splitProcessPID(record[processPIDIdx])
	}
	// Separate pid + process columns.
	// 分离的 pid + process 列。
	pid, err := strconv.Atoi(strings.TrimSpace(record[pidIdx]))
	if err != nil || pid <= 0 {
		return 0, "", false
	}
	return pid, strings.TrimSpace(record[processIdx]), true
}

// splitProcessPID parses "process.name.1234" → path + pid (last dotted number).
// splitProcessPID 解析 "process.name.1234" → 路径 + pid（最后一个点号后数字）。
func splitProcessPID(text string) (int, string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, "", false
	}
	// PID is after the last '.' in nettop's process.pid style fields.
	// nettop 的 process.pid 风格字段中 PID 在最后一个 '.' 之后。
	dot := strings.LastIndex(text, ".")
	if dot <= 0 || dot == len(text)-1 {
		return 0, "", false
	}
	pid, err := strconv.Atoi(text[dot+1:])
	if err != nil || pid <= 0 {
		return 0, "", false
	}
	return pid, text[:dot], true
}

// normalizeHeader lowercases and replaces spaces/hyphens with underscores.
// normalizeHeader 小写化，并将空格/连字符替换为下划线。
func normalizeHeader(header []string) []string {
	out := make([]string, len(header))
	for i, col := range header {
		col = strings.ToLower(strings.TrimSpace(col))
		col = strings.ReplaceAll(col, " ", "_")
		col = strings.ReplaceAll(col, "-", "_")
		out[i] = col
	}
	return out
}

// findColumn returns the first matching header index among candidate names.
// findColumn 在候选名中返回第一个匹配的表头下标。
func findColumn(header []string, names ...string) int {
	for _, name := range names {
		for i, col := range header {
			if col == name {
				return i
			}
		}
	}
	return -1
}

// parseUint parses a non-negative integer, allowing comma thousands separators.
// parseUint 解析非负整数，允许千分位逗号。
func parseUint(text string) (uint64, error) {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, ",", "")
	return strconv.ParseUint(text, 10, 64)
}

// processName returns the basename of a process path for display.
// processName 返回进程路径的 basename 用于展示。
func processName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "unknown"
	}
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return path
	}
	return name
}

// maxIndex returns the largest integer among values (for bounds checks).
// maxIndex 返回 values 中的最大整数（用于边界检查）。
func maxIndex(values ...int) int {
	max := values[0]
	for _, value := range values[1:] {
		if value > max {
			max = value
		}
	}
	return max
}
