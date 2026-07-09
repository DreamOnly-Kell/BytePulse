// Package proctraffic attributes per-process RX/TX rates (optional backends).
// proctraffic 包做每进程 RX/TX 速率归因（可选后端）。
package proctraffic

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
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
	// Line scanner over the process stdout (or test buffer).
	// 对进程 stdout（或测试 buffer）的行扫描器。
	scanner := bufio.NewScanner(reader)
	var header string
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
			continue
		}
		// Rebuild a tiny CSV document: header + one data row.
		// 重建微型 CSV 文档：表头 + 一行数据。
		block := header + "\n" + line + "\n"
		samples, err := ParseNettopCSV(bytes.NewBufferString(block), time.Now())
		// Ignore parse misses; keep reading subsequent lines.
		// 忽略解析失败；继续读后续行。
		if err != nil || len(samples) == 0 {
			continue
		}
		// Deliver this batch to the collector callback.
		// 将本批交付给采集器回调。
		onSample(samples)
	}
	// Propagate scanner I/O errors (not EOF).
	// 传播 scanner 的 I/O 错误（非 EOF）。
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
