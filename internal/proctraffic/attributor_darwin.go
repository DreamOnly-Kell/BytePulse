//go:build darwin

// macOS nettop-backed process traffic attributor.
// 基于 macOS nettop 的进程流量归因器。
package proctraffic

import (
	"context"
	"errors"
	"os/exec"
)

// nettopAttributor runs the system `nettop` binary and parses its CSV stream.
// nettopAttributor 运行系统 `nettop` 二进制并解析其 CSV 流。
type nettopAttributor struct {
	// command is injectable for tests; production builds nettop args.
	// command 可注入以便测试；生产构建 nettop 参数。
	command func(context.Context) (*exec.Cmd, error)
}

// NewNettopAttributor returns the live nettop attributor on macOS.
// NewNettopAttributor 在 macOS 上返回实时 nettop 归因器。
func NewNettopAttributor() Attributor {
	return nettopAttributor{
		command: func(ctx context.Context) (*exec.Cmd, error) {
			// CommandContext kills nettop when the daemon context cancels.
			// CommandContext 在 daemon context 取消时杀掉 nettop。
			return exec.CommandContext(ctx, "nettop", nettopArgs()...), nil
		},
	}
}

// nettopArgs selects process mode, CSV, deltas, and 1-second samples.
// nettopArgs 选择进程模式、CSV、增量，以及 1 秒采样。
//
//	-P  per-process
//	-L 0 continuous listing
//	-x  CSV
//	-n  numeric hosts/ports
//	-d  delta values (per interval, closer to rates)
//	-s 1 sample every 1 second
//
// 含义：-P 按进程；-L 0 持续列出；-x CSV；-n 数字地址端口；-d 增量；-s 1 每秒采样。
func nettopArgs() []string {
	return []string{"-P", "-L", "0", "-x", "-n", "-d", "-s", "1"}
}

// Run starts nettop, scans stdout, and waits for process exit.
// Run 启动 nettop、扫描 stdout，并等待进程退出。
func (a nettopAttributor) Run(ctx context.Context, onSample func([]Sample)) error {
	// Build the command (or a test double).
	// 构建命令（或测试替身）。
	cmd, err := a.command(ctx)
	if err != nil {
		return err
	}
	// Stream stdout into the CSV scanner.
	// 将 stdout 流式交给 CSV 扫描器。
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	// Start nettop asynchronously.
	// 异步启动 nettop。
	if err := cmd.Start(); err != nil {
		return err
	}
	// Parse until EOF/cancel; then wait for the child.
	// 解析直到 EOF/取消；然后等待子进程。
	err = scanNettopCSV(ctx, stdout, onSample)
	waitErr := cmd.Wait()
	// Context cancel is the normal shutdown path.
	// Context 取消是正常关闭路径。
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	// Prefer scan errors over wait errors when both exist.
	// 两者都存在时优先返回扫描错误。
	if err != nil {
		return err
	}
	return waitErr
}
