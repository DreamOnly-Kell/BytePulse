package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bytepulse/internal/config"
	"bytepulse/internal/daemonclient"
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

	// PID file should contain our instance identity.
	// PID 文件应包含当前实例身份。
	record, err := readPIDRecord(pidPath)
	if err != nil {
		t.Fatalf("read pid: %v", err)
	}
	if record.PID != os.Getpid() || record.InstanceID == "" || record.APIAddr != api || record.StartedAt.IsZero() {
		t.Fatalf("pid record = %+v", record)
	}

	// Second acquire must fail while the first holds the exclusive PID lock.
	// 第一个实例持有排他锁时，第二次获取必须失败。
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

	// A stale unlocked record must be replaced even when its PID is currently alive.
	// 即使旧记录的 PID 当前存活，只要文件未被锁定也必须可替换。
	stale := daemonPIDRecord{
		PID:        os.Getpid(),
		InstanceID: "old-instance",
		APIAddr:    api,
		StartedAt:  time.Now().Add(-time.Hour),
	}
	if err := writePIDRecordFile(pidPath, stale); err != nil {
		t.Fatal(err)
	}

	inst, err := acquireDaemonInstance(pidPath, api)
	if err != nil {
		t.Fatalf("acquire with stale pid: %v", err)
	}
	defer inst.Release()

	record, err := readPIDRecord(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	if record.PID != os.Getpid() || record.InstanceID == stale.InstanceID {
		t.Fatalf("record = %+v", record)
	}
}

func TestPIDRecordRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bytepulse.pid")
	want := daemonPIDRecord{
		PID:        1234,
		InstanceID: "instance-123",
		APIAddr:    "127.0.0.1:8988",
		StartedAt:  time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
	}
	if err := writePIDRecordFile(path, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readPIDRecord(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != want {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
}

func TestReadPIDRecordRejectsLegacyOrIncompleteContent(t *testing.T) {
	for _, content := range []string{
		"1234\n",
		`{"pid":1234}`,
		`{"pid":1234,"instance_id":"x","api_addr":"127.0.0.1:8988"}`,
	} {
		path := filepath.Join(t.TempDir(), "bytepulse.pid")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readPIDRecord(path); err == nil {
			t.Fatalf("content %q should be rejected", content)
		}
	}
}

func TestStopDaemonRequiresMatchingHealthIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bytepulse.pid")
	record := daemonPIDRecord{
		PID:        4321,
		InstanceID: "expected-instance",
		APIAddr:    "127.0.0.1:8988",
		StartedAt:  time.Now(),
	}
	if err := writePIDRecordFile(path, record); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PIDPath = path

	var signaled int
	health := func(context.Context, string) (daemonclient.HealthResponse, error) {
		return daemonclient.HealthResponse{OK: true, PID: record.PID, InstanceID: record.InstanceID}, nil
	}
	signal := func(pid int) error {
		signaled = pid
		return nil
	}
	if err := stopDaemon(context.Background(), &cfg, health, signal); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if signaled != record.PID {
		t.Fatalf("signaled=%d want=%d", signaled, record.PID)
	}
}

func TestStopDaemonRefusesMismatchedOrUnavailableHealth(t *testing.T) {
	// Use the current PID so "API unavailable" still refuses (process is alive).
	// 使用当前 PID，使 “API 不可用” 仍拒绝停止（进程仍存活）。
	path := filepath.Join(t.TempDir(), "bytepulse.pid")
	record := daemonPIDRecord{
		PID:        os.Getpid(),
		InstanceID: "expected-instance",
		APIAddr:    "127.0.0.1:8988",
		StartedAt:  time.Now(),
	}
	if err := writePIDRecordFile(path, record); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PIDPath = path

	tests := []struct {
		name   string
		health func(context.Context, string) (daemonclient.HealthResponse, error)
	}{
		{
			name: "instance mismatch",
			health: func(context.Context, string) (daemonclient.HealthResponse, error) {
				return daemonclient.HealthResponse{OK: true, PID: record.PID, InstanceID: "other"}, nil
			},
		},
		{
			name: "pid mismatch",
			health: func(context.Context, string) (daemonclient.HealthResponse, error) {
				return daemonclient.HealthResponse{OK: true, PID: record.PID + 1, InstanceID: record.InstanceID}, nil
			},
		},
		{
			name: "api unavailable process alive",
			health: func(context.Context, string) (daemonclient.HealthResponse, error) {
				return daemonclient.HealthResponse{}, context.DeadlineExceeded
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Rewrite PID file each subtest (stale cleanup may remove it).
			// 每个子测试重写 PID 文件（过期清理可能已删除它）。
			if err := writePIDRecordFile(path, record); err != nil {
				t.Fatal(err)
			}
			signaled := false
			err := stopDaemon(context.Background(), &cfg, tt.health, func(int) error {
				signaled = true
				return nil
			})
			if err == nil {
				t.Fatal("expected refusal")
			}
			if signaled {
				t.Fatal("must not signal an unverified process")
			}
		})
	}
}

func TestStopDaemonRemovesStalePIDWhenProcessGone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bytepulse.pid")
	// Use a PID that is extremely unlikely to be alive.
	// 使用几乎不可能存活的 PID。
	record := daemonPIDRecord{
		PID:        1<<30 - 3,
		InstanceID: "stale-instance",
		APIAddr:    "127.0.0.1:8988",
		StartedAt:  time.Now(),
	}
	if isProcessRunning(record.PID) {
		t.Skip("stale PID unexpectedly alive on this host")
	}
	if err := writePIDRecordFile(path, record); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PIDPath = path

	err := stopDaemon(context.Background(), &cfg,
		func(context.Context, string) (daemonclient.HealthResponse, error) {
			return daemonclient.HealthResponse{}, context.DeadlineExceeded
		},
		func(int) error {
			t.Fatal("must not signal when process is gone")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale PID file should be removed, stat err=%v", err)
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
