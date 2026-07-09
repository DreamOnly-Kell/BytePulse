//go:build windows

// Package main: process liveness helpers for Windows.
// main 包：Windows 上的进程存活检测。
package main

import (
	"golang.org/x/sys/windows"
)

// isProcessRunning reports whether a process with the given PID currently exists.
// Opens the process with query rights; failure means it is gone or inaccessible.
// isProcessRunning 报告给定 PID 的进程当前是否存在。
// 以查询权限 OpenProcess；失败表示已退出或不可访问。
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	// PROCESS_QUERY_LIMITED_INFORMATION is enough to test existence on modern Windows.
	// 在现代 Windows 上 PROCESS_QUERY_LIMITED_INFORMATION 足以判断是否存在。
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}
