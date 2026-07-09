//go:build windows

// Package main: exclusive PID-file lock for Windows (LockFileEx).
// main 包：Windows 上的 PID 文件排他锁（LockFileEx）。
package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockPIDFile takes a non-blocking exclusive lock on the first byte of the file.
// Returns errPIDLocked when another process already holds the lock.
// lockPIDFile 对文件首字节加非阻塞排他锁。
// 若其他进程已持有锁则返回 errPIDLocked。
func lockPIDFile(f *os.File) error {
	// Lock one byte from offset 0; FAIL_IMMEDIATELY matches flock LOCK_NB.
	// 从偏移 0 锁 1 字节；FAIL_IMMEDIATELY 等价于 flock 的 LOCK_NB。
	var ol windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&ol,
	)
	if err != nil {
		return errPIDLocked
	}
	return nil
}

// unlockPIDFile releases the LockFileEx region on the PID file.
// unlockPIDFile 释放 PID 文件上的 LockFileEx 区域。
func unlockPIDFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}
