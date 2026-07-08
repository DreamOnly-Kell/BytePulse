package proctraffic

import (
	"encoding/csv"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Sample struct {
	PID         int       `json:"pid"`
	ProcessName string    `json:"process_name"`
	ProcessPath string    `json:"process_path"`
	RXBytes     uint64    `json:"rx_bytes"`
	TXBytes     uint64    `json:"tx_bytes"`
	RXBps       float64   `json:"rx_bps"`
	TXBps       float64   `json:"tx_bps"`
	SeenAt      time.Time `json:"seen_at"`
	Source      string    `json:"source"`
}

func ParseNettopCSV(r io.Reader, seenAt time.Time) ([]Sample, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return []Sample{}, nil
	}

	header := normalizeHeader(records[0])
	pidIdx := findColumn(header, "pid")
	processIdx := findColumn(header, "process", "command", "proc", "name")
	processPIDIdx := -1
	if pidIdx < 0 || processIdx < 0 {
		processPIDIdx = findProcessPIDColumn(header)
	}
	rxIdx := findColumn(header, "bytes_in", "rx_bytes", "rxbytes", "rx", "in")
	txIdx := findColumn(header, "bytes_out", "tx_bytes", "txbytes", "tx", "out")
	if rxIdx < 0 || txIdx < 0 || (processPIDIdx < 0 && (pidIdx < 0 || processIdx < 0)) {
		return []Sample{}, nil
	}

	out := []Sample{}
	for _, record := range records[1:] {
		requiredIdx := maxIndex(rxIdx, txIdx, pidIdx, processIdx, processPIDIdx)
		if len(record) <= requiredIdx {
			continue
		}
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
			RXBps:       float64(rx),
			TXBps:       float64(tx),
			SeenAt:      seenAt,
			Source:      "nettop",
		})
	}
	return out, nil
}

func findProcessPIDColumn(header []string) int {
	for i, col := range header {
		if i == 0 {
			continue
		}
		if col == "" {
			return i
		}
	}
	return -1
}

func processFields(record []string, pidIdx, processIdx, processPIDIdx int) (int, string, bool) {
	if processPIDIdx >= 0 {
		return splitProcessPID(record[processPIDIdx])
	}
	pid, err := strconv.Atoi(strings.TrimSpace(record[pidIdx]))
	if err != nil || pid <= 0 {
		return 0, "", false
	}
	return pid, strings.TrimSpace(record[processIdx]), true
}

func splitProcessPID(text string) (int, string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, "", false
	}
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

func parseUint(text string) (uint64, error) {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, ",", "")
	return strconv.ParseUint(text, 10, 64)
}

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

func maxIndex(values ...int) int {
	max := values[0]
	for _, value := range values[1:] {
		if value > max {
			max = value
		}
	}
	return max
}
