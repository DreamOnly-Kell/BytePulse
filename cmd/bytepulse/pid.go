// Package main implements the bytepulse CLI entrypoint and helpers.
// main 包实现 bytepulse 命令行入口及辅助函数。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bytepulse/internal/config"
	"bytepulse/internal/daemonclient"
	"bytepulse/internal/i18n"
)

// errPIDLocked is returned when the PID file is already exclusively locked.
// errPIDLocked 表示 PID 文件已被排他锁定。
var errPIDLocked = errors.New("pid file already locked")

// daemonInstance holds an exclusive lock on the daemon PID file for the process lifetime.
// daemonInstance 在进程生命周期内持有 daemon PID 文件的排他锁。
type daemonInstance struct {
	file *os.File
	path string
}

// acquireDaemonInstance enforces a single daemon per PID file (and refuses if the API is already healthy).
// On success the caller must Release() on exit (removes the PID file and unlocks).
// acquireDaemonInstance 保证每个 PID 文件仅一个 daemon（若 API 已健康则拒绝启动）。
// 成功后调用方必须在退出时 Release()（删除 PID 文件并解锁）。
func acquireDaemonInstance(pidPath, apiAddr string) (*daemonInstance, error) {
	// If an existing daemon already answers on the API, refuse even with a different pid-file path.
	// 若已有 daemon 在 API 上响应，即使 PID 路径不同也拒绝（避免双采集）。
	if daemonAPIHealthy(apiAddr) {
		pid := readPIDFileLoose(pidPath)
		return nil, alreadyRunningError(pid, apiAddr)
	}

	// Stale PID file (process gone): allow replace after we take the lock.
	// 过期 PID 文件（进程已不在）：拿到锁后允许覆盖。
	if pid, err := readPIDFile(pidPath); err == nil && isProcessRunning(pid) {
		// Live process recorded in the PID file — refuse before racing on the lock.
		// PID 文件记录的进程仍存活 — 在抢锁前直接拒绝。
		return nil, alreadyRunningError(pid, apiAddr)
	}

	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		return nil, err
	}

	// Open (or create) the PID file; keep the handle open to hold the OS lock.
	// 打开（或创建）PID 文件；保持句柄打开以持有系统锁。
	f, err := os.OpenFile(pidPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	// Non-blocking exclusive lock: second daemon fails here.
	// 非阻塞排他锁：第二个 daemon 在此失败。
	if err := lockPIDFile(f); err != nil {
		_ = f.Close()
		if errors.Is(err, errPIDLocked) {
			pid := readPIDFileLoose(pidPath)
			// Also re-check API in case the other instance just became healthy.
			// 再次检查 API，以防另一实例刚变为健康。
			return nil, alreadyRunningError(pid, apiAddr)
		}
		return nil, err
	}

	// We own the lock. Record our PID for `bytepulse stop`.
	// 已持有锁。写入自身 PID 供 `bytepulse stop` 使用。
	if err := writePIDToFile(f); err != nil {
		_ = unlockPIDFile(f)
		_ = f.Close()
		return nil, err
	}

	return &daemonInstance{file: f, path: pidPath}, nil
}

// Release unlocks and removes the PID file. Safe to call once; further calls no-op.
// Release 解锁并删除 PID 文件。调用一次即可；重复调用为空操作。
func (d *daemonInstance) Release() error {
	if d == nil || d.file == nil {
		return nil
	}
	_ = unlockPIDFile(d.file)
	_ = d.file.Close()
	d.file = nil
	// Best-effort remove; ignore not-found (stop may have deleted it already).
	// 尽力删除；忽略不存在（stop 可能已删）。
	if err := os.Remove(d.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// writePIDToFile truncates the locked PID file and writes the current process ID.
// writePIDToFile 截断已锁定的 PID 文件并写入当前进程 ID。
func writePIDToFile(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	_, err := f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	if err != nil {
		return err
	}
	return f.Sync()
}

// writePIDFile records the current process ID so `stop` can find the daemon.
// Prefer acquireDaemonInstance for the daemon path (exclusive lock).
// writePIDFile 将当前进程 PID 写入文件，供 `stop` 命令定位 daemon。
// daemon 路径请优先使用 acquireDaemonInstance（带排他锁）。
func writePIDFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	pid := []byte(strconv.Itoa(os.Getpid()) + "\n")
	return os.WriteFile(path, pid, 0o644)
}

// readPIDFile loads and validates a daemon PID from disk.
// readPIDFile 从磁盘读取并校验 daemon 的 PID。
func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("daemon PID file not found at %s", path)
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid daemon PID file at %s", path)
	}
	return pid, nil
}

// readPIDFileLoose returns a PID or 0 when missing/invalid (for error messages only).
// readPIDFileLoose 返回 PID，缺失/无效时返回 0（仅用于错误提示）。
func readPIDFileLoose(path string) int {
	pid, err := readPIDFile(path)
	if err != nil {
		return 0
	}
	return pid
}

// removePIDFile deletes the PID file; ignoring "not found" is intentional.
// removePIDFile 删除 PID 文件；忽略“不存在”是预期行为。
func removePIDFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// daemonAPIHealthy returns true when GET /api/health succeeds quickly.
// daemonAPIHealthy 在 GET /api/health 快速成功时返回 true。
func daemonAPIHealthy(apiAddr string) bool {
	if strings.TrimSpace(apiAddr) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	return daemonclient.New(apiAddr).Health(ctx) == nil
}

// alreadyRunningError builds a localized message telling the user to stop first.
// alreadyRunningError 构造本地化提示，要求用户先停止再启动。
func alreadyRunningError(pid int, apiAddr string) error {
	stopHint := "bytepulse stop"
	// Prefer a short start hint; user may have custom flags via config.
	// 优先简短启动提示；用户可能通过配置使用自定义 flag。
	startHint := "bytepulse daemon"
	if apiAddr != "" {
		def := config.Default()
		if apiAddr != def.DaemonAPIAddr {
			startHint = fmt.Sprintf("bytepulse --daemon-api-addr %s daemon", apiAddr)
			stopHint = "bytepulse stop"
		}
	}

	if pid > 0 {
		return fmt.Errorf("%s", i18n.Tf("cli.daemon_already_running", map[string]string{
			"pid":   strconv.Itoa(pid),
			"api":   apiAddr,
			"stop":  stopHint,
			"start": startHint,
		}))
	}
	return fmt.Errorf("%s", i18n.Tf("cli.daemon_already_running_nopid", map[string]string{
		"api":   apiAddr,
		"stop":  stopHint,
		"start": startHint,
	}))
}
