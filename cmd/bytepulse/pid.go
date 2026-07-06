package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func writePIDFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	pid := []byte(strconv.Itoa(os.Getpid()) + "\n")
	return os.WriteFile(path, pid, 0o644)
}

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

func removePIDFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
