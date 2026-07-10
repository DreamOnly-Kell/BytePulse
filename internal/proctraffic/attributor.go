// Package proctraffic attributes per-process RX/TX rates (optional backends).
// proctraffic 包做每进程 RX/TX 速率归因（可选后端）。
package proctraffic

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// ErrNotSupported means this OS/backend cannot attribute process traffic.
// ErrNotSupported 表示当前 OS/后端无法做进程流量归因。
var ErrNotSupported = errors.New("process traffic attribution is not supported on this platform")

// Attributor streams process traffic samples until ctx is cancelled.
// Attributor 在 ctx 取消前持续流式输出进程流量样本。
type Attributor interface {
	// Run calls onSample for each parsed batch; blocks until done or error.
	// Run 每解析一批调用 onSample；阻塞直到结束或出错。
	Run(ctx context.Context, onSample func([]Sample)) error
}

// unsupportedAttributor is the default stub implementation.
// unsupportedAttributor 是默认桩实现。
type unsupportedAttributor struct{}

// Run always returns ErrNotSupported.
// Run 始终返回 ErrNotSupported。
func (unsupportedAttributor) Run(context.Context, func([]Sample)) error {
	return ErrNotSupported
}

// readerAttributor parses a pre-opened nettop-style CSV stream (tests/helpers).
// readerAttributor 解析已打开的 nettop 风格 CSV 流（测试/辅助）。
type readerAttributor struct {
	reader io.Reader
}

// NewReaderAttributor wraps an arbitrary reader as an Attributor.
// NewReaderAttributor 将任意 reader 包装为 Attributor。
func NewReaderAttributor(reader io.Reader) Attributor {
	return readerAttributor{reader: reader}
}

// Run scans CSV lines from the injected reader.
// Run 从注入的 reader 扫描 CSV 行。
func (a readerAttributor) Run(ctx context.Context, onSample func([]Sample)) error {
	return scanNettopCSV(ctx, a.reader, onSample)
}

// scanNettopCSV reads nettop CSV output line-by-line and emits samples.
// scanNettopCSV 逐行读取 nettop CSV 输出并产出样本。
//
// nettop with -L 0 prints a header then continuous data lines. We pair the
// first header line with each subsequent data line for ParseNettopCSV.
// nettop -L 0 会先打印表头再持续输出数据行。我们将首行表头与后续每行数据配对交给 ParseNettopCSV。
func scanNettopCSV(ctx context.Context, reader io.Reader, onSample func([]Sample)) error {
	return scanNettopCSVWithClock(ctx, reader, onSample, time.Now)
}

func scanNettopCSVWithClock(ctx context.Context, reader io.Reader, onSample func([]Sample), now func() time.Time) error {
	// Line scanner over the process stdout (or test buffer).
	// 对进程 stdout（或测试 buffer）的行扫描器。
	scanner := bufio.NewScanner(reader)
	var header string
	var rows []string
	var epochStartedAt time.Time
	firstEpoch := true
	flush := func(boundary time.Time) {
		if firstEpoch {
			firstEpoch = false
			rows = rows[:0]
			return
		}
		if len(rows) == 0 {
			return
		}
		var block strings.Builder
		block.WriteString(header)
		block.WriteByte('\n')
		for _, row := range rows {
			block.WriteString(row)
			block.WriteByte('\n')
		}
		elapsed := boundary.Sub(epochStartedAt)
		samples, err := parseNettopCSV(strings.NewReader(block.String()), boundary, elapsed)
		if err == nil && len(samples) > 0 && onSample != nil {
			onSample(samples)
		}
		rows = rows[:0]
	}
	for scanner.Scan() {
		// Cooperative cancel between lines.
		// 行与行之间协作式取消。
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line := scanner.Text()
		// Skip blank lines from the tool.
		// 跳过工具输出的空行。
		if line == "" {
			continue
		}
		// First non-empty line is treated as the CSV header.
		// 第一个非空行视为 CSV 表头。
		if header == "" {
			header = line
			epochStartedAt = now()
			continue
		}
		if line == header {
			boundary := now()
			flush(boundary)
			epochStartedAt = boundary
			continue
		}
		rows = append(rows, line)
	}
	// Propagate scanner I/O errors (not EOF).
	// 传播 scanner 的 I/O 错误（非 EOF）。
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(rows) > 0 {
		flush(now())
	}
	return nil
}
