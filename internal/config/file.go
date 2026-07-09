// Package config: YAML file loading helpers (see also config.go).
// config 包：YAML 配置文件加载（另见 config.go）。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// fileDTO is the YAML on-disk shape.
// Durations are strings (e.g. "720h") so the sample file stays human-editable.
// Pointers for bool/int distinguish "unset" from false/0 when merging.
//
// fileDTO 是磁盘上的 YAML 形态。
// 时长用字符串（如 "720h"）便于手写；bool/int 用指针区分「未设置」与 false/0。
type fileDTO struct {
	// DB is the SQLite path (yaml: db).
	// DB 为 SQLite 路径。
	DB string `yaml:"db"`
	// PIDFile is the daemon PID path (yaml: pid_file).
	// PIDFile 为 daemon PID 路径。
	PIDFile string `yaml:"pid_file"`
	// Interface filters NIC queries (yaml: interface).
	// Interface 过滤网卡查询。
	Interface string `yaml:"interface"`
	// Bits enables bits/s display when non-nil (yaml: bits).
	// Bits 非 nil 时启用 bits/s 显示。
	Bits *bool `yaml:"bits"`
	// Retention is a Go duration string (yaml: retention).
	// Retention 为 Go duration 字符串。
	Retention string `yaml:"retention"`
	// TopN limits process list rows (yaml: top_n).
	// TopN 限制进程列表行数。
	TopN *int `yaml:"top_n"`
	// ProcessInterval is process sampling interval (yaml: process_interval).
	// ProcessInterval 为进程采样间隔。
	ProcessInterval string `yaml:"process_interval"`
	// DaemonAPIAddr is the local process API (yaml: daemon_api_addr).
	// DaemonAPIAddr 为本机进程 API 地址。
	DaemonAPIAddr string `yaml:"daemon_api_addr"`
	// ProcessTraffic is off|auto|nettop|estats (yaml: process_traffic).
	// ProcessTraffic 为 off|auto|nettop|estats。
	ProcessTraffic string `yaml:"process_traffic"`
	// ExcludeSelf hides this process from process views (yaml: exclude_self).
	// ExcludeSelf 为 true 时从进程视图隐藏自身。
	ExcludeSelf *bool `yaml:"exclude_self"`
	// LogLevel is debug|info|warn|error (yaml: log_level).
	// LogLevel 为 debug|info|warn|error。
	LogLevel string `yaml:"log_level"`
	// LogFormat is text|json (yaml: log_format).
	// LogFormat 为 text|json。
	LogFormat string `yaml:"log_format"`
	// LogFile is optional log path (yaml: log_file).
	// LogFile 为可选日志路径。
	LogFile string `yaml:"log_file"`
	// DaemonInterval is interface sample interval for daemon (yaml: daemon_interval).
	// DaemonInterval 为 daemon 网卡采样间隔。
	DaemonInterval string `yaml:"daemon_interval"`
	// WebAddr is the web listen address (yaml: web_addr).
	// WebAddr 为 web 监听地址。
	WebAddr string `yaml:"web_addr"`
	// Lang is UI language en|zh (yaml: lang).
	// Lang 为界面语言 en|zh。
	Lang string `yaml:"lang"`
}

// DefaultConfigPath returns ~/.bytepulse/config.yaml.
// If home is unknown, falls back to ./bytepulse-config.yaml.
// DefaultConfigPath 返回 ~/.bytepulse/config.yaml；无法取主目录时回退到当前目录文件名。
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", "bytepulse-config.yaml")
	}
	return filepath.Join(home, ".bytepulse", "config.yaml")
}

// PeekConfigPath finds --config or --config=path in raw args (before Cobra parse).
// Used so the file can be loaded and become flag defaults.
// PeekConfigPath 在 Cobra 解析前从原始 args 取出 --config，以便先加载文件作为 flag 默认值。
func PeekConfigPath(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config" || a == "-config":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--config="):
			return strings.TrimPrefix(a, "--config=")
		}
	}
	return ""
}

// ResolveConfigPath returns explicit if set; else default path only when that file exists.
// Empty return means "do not load a file".
// ResolveConfigPath：有显式路径则用它；否则仅当默认文件存在时返回默认路径；空表示不加载。
func ResolveConfigPath(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	p := DefaultConfigPath()
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

// LoadFile reads YAML from path and merges into cfg.
// path == "" is a no-op success (no file configured).
// LoadFile 读取 path 的 YAML 并合并进 cfg；path 为空表示不加载且成功。
func LoadFile(path string, cfg *Config) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	var dto fileDTO
	if err := yaml.Unmarshal(data, &dto); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	return applyDTO(dto, cfg)
}

// applyDTO copies non-empty / non-nil DTO fields onto cfg.
// applyDTO 将 DTO 中非空/非 nil 字段拷贝到 cfg。
func applyDTO(dto fileDTO, cfg *Config) error {
	if dto.DB != "" {
		cfg.DBPath = expandHome(dto.DB)
	}
	if dto.PIDFile != "" {
		cfg.PIDPath = expandHome(dto.PIDFile)
	}
	if dto.Interface != "" {
		cfg.Interface = dto.Interface
	}
	if dto.Bits != nil {
		cfg.UseBits = *dto.Bits
	}
	if dto.Retention != "" {
		d, err := time.ParseDuration(dto.Retention)
		if err != nil {
			return fmt.Errorf("retention: %w", err)
		}
		cfg.Retention = d
	}
	if dto.TopN != nil {
		cfg.TopN = *dto.TopN
	}
	if dto.ProcessInterval != "" {
		d, err := time.ParseDuration(dto.ProcessInterval)
		if err != nil {
			return fmt.Errorf("process_interval: %w", err)
		}
		cfg.ProcessInterval = d
	}
	if dto.DaemonAPIAddr != "" {
		cfg.DaemonAPIAddr = dto.DaemonAPIAddr
	}
	if dto.ProcessTraffic != "" {
		cfg.ProcessTraffic = dto.ProcessTraffic
	}
	if dto.ExcludeSelf != nil {
		cfg.ExcludeSelf = *dto.ExcludeSelf
	}
	if dto.LogLevel != "" {
		cfg.LogLevel = dto.LogLevel
	}
	if dto.LogFormat != "" {
		cfg.LogFormat = dto.LogFormat
	}
	if dto.LogFile != "" {
		cfg.LogFile = expandHome(dto.LogFile)
	}
	if dto.DaemonInterval != "" {
		d, err := time.ParseDuration(dto.DaemonInterval)
		if err != nil {
			return fmt.Errorf("daemon_interval: %w", err)
		}
		cfg.DaemonInterval = d
	}
	if dto.WebAddr != "" {
		cfg.WebAddr = dto.WebAddr
	}
	if dto.Lang != "" {
		cfg.Lang = dto.Lang
	}
	return nil
}

// expandHome replaces a leading ~/ with the user home directory.
// expandHome 将前缀 ~/ 展开为用户主目录。
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// DaemonStartHint returns a shell command users can run to start the collector.
// When db/api match defaults, returns a short "bytepulse daemon".
// DaemonStartHint 返回启动采集器的命令提示；db/api 为默认时用简短形式。
func DaemonStartHint(cfg Config) string {
	def := Default()
	if cfg.DaemonAPIAddr == def.DaemonAPIAddr && cfg.DBPath == def.DBPath {
		return "bytepulse daemon"
	}
	return fmt.Sprintf("bytepulse --db %s --daemon-api-addr %s daemon", cfg.DBPath, cfg.DaemonAPIAddr)
}
