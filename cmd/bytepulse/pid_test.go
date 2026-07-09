package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"bytepulse/internal/i18n"
)

func TestIsProcessRunningSelf(t *testing.T) {
	if !isProcessRunning(os.Getpid()) {
		t.Fatal("expected current process to be running")
	}
}

func TestIsProcessRunningBogus(t *testing.T) {
	// Extremely unlikely to be a live PID on a normal machine.
	// 正常机器上几乎不可能是存活 PID。
	if isProcessRunning(1<<30 - 3) {
		t.Fatal("expected bogus PID to be not running")
	}
	if isProcessRunning(0) || isProcessRunning(-1) {
		t.Fatal("expected non-positive PID to be not running")
	}
}

func TestAcquireDaemonInstanceSecondFails(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "bytepulse.pid")
	// Use a free high port that nothing answers on for health check.
	// 使用空闲高位端口，健康检查应失败。
	api := "127.0.0.1:59999"

	first, err := acquireDaemonInstance(pidPath, api)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer first.Release()

	// PID file should contain our PID.
	// PID 文件应包含当前进程 PID。
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read pid: %v", err)
	}
	got, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || got != os.Getpid() {
		t.Fatalf("pid file = %q, want %d", data, os.Getpid())
	}

	// Second acquire in another logical instance must fail.
	// Note: same-process second flock may behave differently on some OS;
	// we primarily assert the live-PID path by writing our PID (already done).
	// 第二次获取必须失败。
	// 注：同进程二次 flock 在部分 OS 上行为不同；此处已写入存活 PID，走 isProcessRunning 路径。
	_, err = acquireDaemonInstance(pidPath, api)
	if err == nil {
		t.Fatal("expected second acquire to fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "already running") && !strings.Contains(msg, "已有") {
		// Default lang is en.
		t.Fatalf("error should mention already running, got: %s", msg)
	}
	if !strings.Contains(msg, "stop") && !strings.Contains(msg, "停止") {
		t.Fatalf("error should mention stop, got: %s", msg)
	}
}

func TestAcquireDaemonInstanceReplacesStalePID(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "bytepulse.pid")
	api := "127.0.0.1:59998"

	// Write a dead PID (no process).
	// 写入一个已死的 PID（无对应进程）。
	stale := []byte("2147483000\n")
	if err := os.WriteFile(pidPath, stale, 0o644); err != nil {
		t.Fatal(err)
	}
	if isProcessRunning(2147483000) {
		t.Skip("stale PID unexpectedly alive on this host")
	}

	inst, err := acquireDaemonInstance(pidPath, api)
	if err != nil {
		t.Fatalf("acquire with stale pid: %v", err)
	}
	defer inst.Release()

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if got != os.Getpid() {
		t.Fatalf("pid = %d, want %d", got, os.Getpid())
	}
}

func TestAlreadyRunningErrorLocalized(t *testing.T) {
	i18n.SetLang("en")
	err := alreadyRunningError(12345, "127.0.0.1:8988")
	if err == nil || !strings.Contains(err.Error(), "12345") {
		t.Fatalf("en error missing pid: %v", err)
	}
	if !strings.Contains(err.Error(), "bytepulse stop") {
		t.Fatalf("en error missing stop hint: %v", err)
	}

	i18n.SetLang("zh")
	err = alreadyRunningError(12345, "127.0.0.1:8988")
	if err == nil || !strings.Contains(err.Error(), "已有") {
		t.Fatalf("zh error missing message: %v", err)
	}
	i18n.SetLang("en")
}
