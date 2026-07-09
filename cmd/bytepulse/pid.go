// Package main implements the bytepulse CLI entrypoint and helpers.
// main 包实现 bytepulse 命令行入口及辅助函数。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// writePIDFile records the current process ID so `stop` can find the daemon.
// writePIDFile 将当前进程 PID 写入文件，供 `stop` 命令定位 daemon。
func writePIDFile(path string) error {
	// Ensure the parent directory exists (e.g. ~/.bytepulse).
	// 确保父目录存在（例如 ~/.bytepulse）。
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Encode PID as decimal text plus trailing newline.
	// 将 PID 编码为十进制文本并附加换行。
	pid := []byte(strconv.Itoa(os.Getpid()) + "\n")
	// Overwrite the PID file with mode 0644.
	// 以 0644 权限覆盖写入 PID 文件。
	return os.WriteFile(path, pid, 0o644)
}

// readPIDFile loads and validates a daemon PID from disk.
// readPIDFile 从磁盘读取并校验 daemon 的 PID。
func readPIDFile(path string) (int, error) {
	// Read the entire PID file contents.
	// 读取整个 PID 文件内容。
	data, err := os.ReadFile(path)
	if err != nil {
		// Missing file usually means no daemon was started (or already cleaned up).
		// 文件不存在通常表示 daemon 未启动（或已被清理）。
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("daemon PID file not found at %s", path)
		}
		return 0, err
	}
	// Parse trimmed text into an integer PID.
	// 将去掉空白后的文本解析为整数 PID。
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	// Reject non-numeric content and non-positive values.
	// 拒绝非数字内容以及非正数。
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid daemon PID file at %s", path)
	}
	return pid, nil
}

// removePIDFile deletes the PID file; ignoring "not found" is intentional.
// removePIDFile 删除 PID 文件；忽略“不存在”是预期行为。
func removePIDFile(path string) error {
	// Remove the file; treat already-deleted as success.
	// 删除文件；若已不存在则视为成功。
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
