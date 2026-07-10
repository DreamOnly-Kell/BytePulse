//go:build !windows

// Package main: exclusive PID-file lock for Unix-like systems (flock).
// main 包：Unix 类系统上的 PID 文件排他锁（flock）。
package main

import (
	"os"
	"syscall"
)

// lockPIDFile takes a non-blocking exclusive flock on an open PID file.
// Returns errPIDLocked when another process already holds the lock.
// lockPIDFile 对已打开的 PID 文件加非阻塞排他 flock。
// 若其他进程已持有锁则返回 errPIDLocked。
func lockPIDFile(f *os.File) error {
	// LOCK_EX exclusive; LOCK_NB fail immediately instead of blocking.
	// LOCK_EX 排他；LOCK_NB 立即失败而非阻塞。
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		return errPIDLocked
	}
	return nil
}

// unlockPIDFile releases a flock held on the PID file.
// unlockPIDFile 释放 PID 文件上的 flock。
func unlockPIDFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
