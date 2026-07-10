package proc

import (
	"path/filepath"
	"strings"
)

// selfProcessNames are executable basenames treated as this program.
// selfProcessNames 视为本程序的可执行文件 basename。
var selfProcessNames = map[string]bool{
	"bytepulse":     true,
	"bytepulse.exe": true,
}

// IsSelfProcess reports whether a process looks like the bytepulse daemon/CLI.
// Match by selfPID (when > 0) or by process name / path basename "bytepulse".
// IsSelfProcess 判断进程是否像 bytepulse daemon/CLI。
// selfPID>0 时按 PID 匹配，或按进程名/路径 basename 为 bytepulse 匹配。
func IsSelfProcess(pid int, processName, processPath string, selfPID int) bool {
	if selfPID > 0 && pid == selfPID {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(processName))
	if selfProcessNames[name] {
		return true
	}
	// Normalize Windows separators so basename works on any GOOS.
	// 规范化 Windows 分隔符，使任意 GOOS 上 basename 都正确。
	path := strings.ReplaceAll(strings.TrimSpace(processPath), "\\", "/")
	base := strings.ToLower(filepath.Base(path))
	return selfProcessNames[base]
}

// FilterSelfConnections drops self processes when excludeSelf is true.
// FilterSelfConnections 在 excludeSelf 为 true 时丢弃自身进程连接。
func FilterSelfConnections(conns []Connection, excludeSelf bool, selfPID int) []Connection {
	if !excludeSelf || len(conns) == 0 {
		return conns
	}
	out := make([]Connection, 0, len(conns))
	for _, conn := range conns {
		if IsSelfProcess(conn.PID, conn.ProcessName, conn.ProcessPath, selfPID) {
			continue
		}
		out = append(out, conn)
	}
	return out
}
