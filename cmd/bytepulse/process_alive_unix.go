//go:build !windows

// Package main: process liveness helpers for Unix-like systems.
// main 包：Unix 类系统上的进程存活检测。
package main

import (
	"os"
	"syscall"
)

// isProcessRunning reports whether a process with the given PID currently exists.
// Uses signal 0 (existence check) without delivering a real signal.
// isProcessRunning 报告给定 PID 的进程当前是否存在。
// 使用 signal 0（存在性检查）而不会真正投递信号。
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	// FindProcess always succeeds on Unix; Signal(0) probes existence.
	// Unix 上 FindProcess 总会成功；Signal(0) 用于探测是否存在。
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil
}
