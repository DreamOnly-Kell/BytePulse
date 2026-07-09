// Package main is the bytepulse CLI entrypoint (Cobra subcommands).
// main 包是 bytepulse 的 CLI 入口（Cobra 子命令）。
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bytepulse/internal/collector"
	"bytepulse/internal/config"
	"bytepulse/internal/daemonapi"
	"bytepulse/internal/daemonclient"
	"bytepulse/internal/i18n"
	"bytepulse/internal/logx"
	"bytepulse/internal/proc"
	"bytepulse/internal/processstate"
	"bytepulse/internal/proctraffic"
	"bytepulse/internal/storage"
	"bytepulse/internal/tui"
	"bytepulse/internal/units"
	"bytepulse/internal/web"

	"github.com/spf13/cobra"
)

// main wires the root command and exits non-zero on failure.
// main 挂接根命令，失败时以非零状态退出。
func main() {
	// Execute the Cobra command tree; print any error to stderr.
	// 执行 Cobra 命令树；将错误打印到 stderr。
	if err := newRootCommand().Execute(); err != nil {
		logx.Error("command failed", "err", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newRootCommand builds the root command, global flags, and subcommands.
// newRootCommand 构建根命令、全局 flag 与子命令。
func newRootCommand() *cobra.Command {
	// Defaults < config file < CLI flags (file loaded before flag registration).
	// 默认值 < 配置文件 < CLI flag（注册 flag 前加载文件）。
	cfg := config.Default()
	configFlag := config.PeekConfigPath(os.Args[1:])
	configPath := config.ResolveConfigPath(configFlag)
	if configPath != "" {
		if err := config.LoadFile(configPath, &cfg); err != nil {
			// Fail early with a clear message before Cobra runs.
			// Cobra 运行前用清晰错误直接失败。
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		cfg.ConfigPath = configPath
	}

	// Root command metadata shown in help.
	// 帮助信息中展示的根命令元数据。
	cmd := &cobra.Command{
		Use:   "bytepulse",
		Short: "Monitor local network traffic / 监控本机网络流量",
		// PersistentPreRunE initializes logging and UI language after flags are parsed.
		// PersistentPreRunE 在 flag 解析后初始化日志与界面语言。
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			i18n.SetLang(cfg.Lang)
			if err := logx.Init(logx.Options{
				Level:  cfg.LogLevel,
				Format: cfg.LogFormat,
				File:   cfg.LogFile,
			}); err != nil {
				return err
			}
			if cfg.ConfigPath != "" {
				logx.Info("config loaded", "component", "config", "path", cfg.ConfigPath)
			}
			logx.Debug("ui language", "component", "i18n", "lang", i18n.Lang())
			return nil
		},
	}

	// Persistent flags apply to all subcommands (defaults already include file values).
	// Persistent flags 对所有子命令生效（默认值已含配置文件）。
	cmd.PersistentFlags().StringVar(&configFlag, "config", configFlag, "config file path (YAML); default ~/.bytepulse/config.yaml if present / 配置文件路径，默认存在则读 ~/.bytepulse/config.yaml")
	cmd.PersistentFlags().StringVar(&cfg.DBPath, "db", cfg.DBPath, "SQLite database path / SQLite 数据库路径")
	cmd.PersistentFlags().StringVar(&cfg.PIDPath, "pid-file", cfg.PIDPath, "daemon PID file path / daemon PID 文件路径")
	cmd.PersistentFlags().StringVar(&cfg.Interface, "interface", cfg.Interface, "interface name; empty = all non-loopback / 网卡名，空=全部非回环")
	cmd.PersistentFlags().BoolVar(&cfg.UseBits, "bits", cfg.UseBits, "display rates as bits/s / 速率以 bits/s 显示")
	cmd.PersistentFlags().DurationVar(&cfg.Retention, "retention", cfg.Retention, "data retention period / 数据保留时长")
	cmd.PersistentFlags().IntVar(&cfg.TopN, "top-n", cfg.TopN, "default process list rows / 进程列表默认行数")
	cmd.PersistentFlags().DurationVar(&cfg.ProcessInterval, "process-interval", cfg.ProcessInterval, "process connection sampling interval / 进程连接采样间隔")
	cmd.PersistentFlags().StringVar(&cfg.DaemonAPIAddr, "daemon-api-addr", cfg.DaemonAPIAddr, "daemon local API address / daemon 本机 API 地址")
	cmd.PersistentFlags().StringVar(&cfg.ProcessTraffic, "process-traffic", cfg.ProcessTraffic, "process traffic: off, auto, nettop (macOS), estats (Windows) / 进程流量模式")
	cmd.PersistentFlags().BoolVar(&cfg.ExcludeSelf, "exclude-self", cfg.ExcludeSelf, "hide bytepulse from process views / 进程视图中隐藏自身")
	cmd.PersistentFlags().StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error (default error; logs always English) / 日志级别（日志内容固定英文）")
	cmd.PersistentFlags().StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "log format: text or json / 日志格式")
	cmd.PersistentFlags().StringVar(&cfg.LogFile, "log-file", cfg.LogFile, "log file path; empty = stderr / 日志文件，空=stderr")
	cmd.PersistentFlags().StringVar(&cfg.Lang, "lang", cfg.Lang, "UI language: en or zh (TUI/Web/CLI prompts; not logs) / 界面语言 en|zh（不含日志）")

	// Register each user-facing subcommand.
	// 注册各面向用户的子命令。
	cmd.AddCommand(newDaemonCommand(&cfg))
	cmd.AddCommand(newStopCommand(&cfg))
	cmd.AddCommand(newStatusCommand(&cfg))
	cmd.AddCommand(newReportCommand(&cfg))
	cmd.AddCommand(newInterfacesCommand(&cfg))
	cmd.AddCommand(newProcessesCommand(&cfg))
	cmd.AddCommand(newTUICommand(&cfg))
	cmd.AddCommand(newWebCommand(&cfg))

	return cmd
}

// openStore opens SQLite and runs schema migrations.
// openStore 打开 SQLite 并执行 schema 迁移。
func openStore(cfg *config.Config) (*storage.Store, error) {
	// Open (or create) the database file.
	// 打开（或创建）数据库文件。
	store, err := storage.Open(cfg.DBPath)
	if err != nil {
		logx.Error("open database failed", "component", "storage", "path", cfg.DBPath, "err", err)
		return nil, err
	}
	// Apply CREATE TABLE / PRAGMA / column upgrades.
	// 执行 CREATE TABLE / PRAGMA / 列升级。
	if err := store.Migrate(); err != nil {
		logx.Error("database migrate failed", "component", "storage", "path", cfg.DBPath, "err", err)
		// Close on migrate failure to avoid leaking the handle.
		// 迁移失败时关闭连接，避免句柄泄漏。
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

// newDaemonCommand starts collectors, process sampling, and the local API.
// newDaemonCommand 启动采集器、进程采样与本机 API。
func newDaemonCommand(cfg *config.Config) *cobra.Command {
	// Interface sampling interval (defaults from config file / Default).
	// 网卡采样间隔（默认来自配置文件 / Default）。
	interval := cfg.DaemonInterval
	if interval <= 0 {
		interval = time.Second
	}

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Collect network traffic every second / 每秒采集网络流量",
		// Avoid dumping full flag help on runtime errors (e.g. already running).
		// 运行时错误（如已有实例）时不刷完整 flag 帮助。
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Single-instance: exclusive PID lock + refuse if API already healthy.
			// 单实例：PID 排他锁；若 API 已健康则拒绝第二个 daemon。
			inst, err := acquireDaemonInstance(cfg.PIDPath, cfg.DaemonAPIAddr)
			if err != nil {
				logx.Error("daemon already running or lock failed", "component", "daemon", "path", cfg.PIDPath, "api", cfg.DaemonAPIAddr, "err", err)
				return err
			}
			// Always unlock and remove the PID file on exit (success or failure).
			// 退出时始终解锁并删除 PID 文件（成功或失败）。
			defer func() {
				if err := inst.Release(); err != nil {
					logx.Warn("release daemon lock failed", "component", "daemon", "path", cfg.PIDPath, "err", err)
				}
			}()

			// Open shared SQLite store for interface + process minute data.
			// 打开共享 SQLite，供网卡样本与进程分钟数据使用。
			store, err := openStore(cfg)
			if err != nil {
				return err
			}
			defer store.Close()
			logx.Info("database ready", "component", "storage", "path", cfg.DBPath)

			// Cancel on Ctrl+C / SIGTERM.
			// 在 Ctrl+C / SIGTERM 时取消。
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			// Child context used to stop worker goroutines together.
			// 子 context 用于统一停止工作协程。
			runCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			// Interface counter collector (gopsutil deltas → samples table).
			// 网卡计数采集器（gopsutil 差值 → samples 表）。
			c := collector.New(store, collector.Options{
				Interval:  interval,
				Interface: cfg.Interface,
				Retention: cfg.Retention,
			})
			// In-memory process connection + traffic state for the API.
			// 供 API 使用的进程连接/流量内存态。
			procState := processstate.New()
			// Process connection sampler + minute rollup flusher.
			// 进程连接采样器 + 分钟聚合落库。
			pc := collector.NewProcessConnectionCollector(
				store,
				proc.NewSampler(),
				procState,
				collector.ProcessConnectionOptions{
					Interval:    cfg.ProcessInterval,
					Retention:   cfg.Retention,
					ExcludeSelf: cfg.ExcludeSelf,
					SelfPID:     os.Getpid(),
				},
			)
			// Local HTTP API for realtime process views.
			// 进程实时视图使用的本机 HTTP API。
			api := daemonapi.NewServer(procState, store, *cfg)
			apiServer := &http.Server{
				Addr:    cfg.DaemonAPIAddr,
				Handler: api.Handler(),
			}
			// Resolve optional per-process traffic backend (platform-specific).
			// 解析可选的每进程流量后端（平台相关）。
			trafficAttr, err := proctraffic.NewAttributor(cfg.ProcessTraffic)
			if err != nil {
				logx.Error("invalid process traffic mode", "component", "proctraffic", "mode", cfg.ProcessTraffic, "err", err)
				return err
			}

			// Buffered error channel from background workers (API + collectors).
			// 后台 worker（API + 采集器）共用的带缓冲错误通道。
			errCh := make(chan error, 3)
			// Serve the daemon API until shutdown.
			// 提供 daemon API 直到关闭。
			go func() {
				logx.Info("daemon API listening", "component", "daemonapi", "addr", cfg.DaemonAPIAddr)
				if err := apiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errCh <- fmt.Errorf("daemon API: %w", err)
				}
			}()
			// Run interface sampling loop.
			// 运行网卡采样循环。
			go func() {
				if err := c.Run(runCtx); err != nil {
					errCh <- fmt.Errorf("collector: %w", err)
				}
			}()
			// Run process connection sampling loop.
			// 运行进程连接采样循环。
			go func() {
				if err := pc.Run(runCtx); err != nil {
					errCh <- fmt.Errorf("process collector: %w", err)
				}
			}()
			// Optional per-process RX/TX: nettop on macOS, TCP ESTATS on Windows.
			// 可选每进程 RX/TX：macOS 为 nettop，Windows 为 TCP ESTATS。
			if trafficAttr != nil {
				logx.Info("process traffic enabled", "component", "proctraffic", "mode", cfg.ProcessTraffic)
				pt := collector.NewProcessTrafficCollector(trafficAttr, procState)
				go func() {
					if err := pt.Run(runCtx); err != nil {
						errCh <- fmt.Errorf("process traffic collector: %w", err)
					}
				}()
			} else {
				logx.Info("process traffic disabled", "component", "proctraffic", "mode", cfg.ProcessTraffic)
			}

			// Announce startup configuration on stdout + structured log.
			// 在 stdout 与结构化日志中公布启动配置。
			fmt.Printf("bytepulse daemon started, db=%s, interval=%s, process_interval=%s, process_traffic=%s, api=http://%s\n",
				cfg.DBPath, interval, cfg.ProcessInterval, cfg.ProcessTraffic, cfg.DaemonAPIAddr)
			logx.Info("daemon started",
				"component", "daemon",
				"db", cfg.DBPath,
				"interval", interval.String(),
				"process_interval", cfg.ProcessInterval.String(),
				"process_traffic", cfg.ProcessTraffic,
				"exclude_self", cfg.ExcludeSelf,
				"api", cfg.DaemonAPIAddr,
				"log_level", cfg.LogLevel,
				"log_file", cfg.LogFile,
			)
			// Wait for signal shutdown or a worker failure.
			// 等待信号关闭或某个 worker 失败。
			select {
			case <-ctx.Done():
				// Cooperative stop of collectors.
				// 协作式停止采集器。
				cancel()
				logx.Info("daemon shutting down", "component", "daemon", "reason", "signal")
				// Give HTTP server a short window to drain.
				// 给 HTTP 服务短暂时间排空连接。
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer shutdownCancel()
				_ = apiServer.Shutdown(shutdownCtx)
				return nil
			case err := <-errCh:
				// Propagate the first worker error after cleanup.
				// 清理后向上返回第一个 worker 错误。
				logx.Error("daemon worker failed", "component", "daemon", "err", err)
				cancel()
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer shutdownCancel()
				_ = apiServer.Shutdown(shutdownCtx)
				return err
			}
		},
	}

	// Default interface sample interval is 1 second.
	// 默认网卡采样间隔为 1 秒。
	cmd.Flags().DurationVar(&interval, "interval", interval, "sampling interval / 采样间隔")
	return cmd
}

// newProcessesCommand shows realtime or historical process connection rows.
// newProcessesCommand 展示实时或历史进程连接行。
func newProcessesCommand(cfg *config.Config) *cobra.Command {
	// Local flags: row limit, historical range, watch mode.
	// 本地 flag：行数、历史范围、watch 模式。
	var limit int
	var rangeText string
	var watch bool

	cmd := &cobra.Command{
		Use:   "processes",
		Short: "Show processes using the network / 显示联网进程",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fall back to global TopN when limit is unset/invalid.
			// limit 未设/无效时回退到全局 TopN。
			if limit <= 0 {
				limit = cfg.TopN
			}
			// Historical path reads SQLite minute rollups (no daemon required).
			// 历史路径读 SQLite 分钟聚合（不依赖 daemon）。
			if rangeText != "" {
				return printHistoricalProcesses(cfg, rangeText, limit)
			}
			// Realtime path requires the daemon local API.
			// 实时路径需要 daemon 本机 API。
			client := daemonclient.New(cfg.DaemonAPIAddr)
			logx.Debug("processes command", "component", "cli", "watch", watch, "limit", limit, "api", cfg.DaemonAPIAddr)
			if watch {
				// Refresh once per second like a simple top-style view.
				// 每秒刷新一次，类似简易 top 视图。
				ticker := time.NewTicker(time.Second)
				defer ticker.Stop()
				for {
					// ANSI clear screen + cursor home for redraw.
					// ANSI 清屏并把光标移到左上角以便重绘。
					fmt.Print("\033[H\033[2J")
					// Print current snapshot; keep looping even if API is down.
					// 打印当前快照；API 不可用时也继续循环。
					if err := printRealtimeProcesses(cmd.Context(), client, limit, cfg.UseBits, cfg); err != nil {
						logx.WarnEvery(30*time.Second, "cli.processes.watch", "realtime processes failed", "component", "cli", "err", err)
						fmt.Println(err)
					}
					select {
					// Exit cleanly when the CLI context is cancelled.
					// CLI context 取消时干净退出。
					case <-cmd.Context().Done():
						return nil
					// Wait for the next tick.
					// 等待下一拍。
					case <-ticker.C:
					}
				}
			}
			// One-shot realtime dump.
			// 单次实时输出。
			return printRealtimeProcesses(cmd.Context(), client, limit, cfg.UseBits, cfg)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", cfg.TopN, "number of process rows / 进程行数")
	cmd.Flags().StringVar(&rangeText, "range", "", "historical range / 历史范围: 1h,2h,...,15d")
	cmd.Flags().BoolVar(&watch, "watch", false, "refresh every second / 每秒刷新")
	return cmd
}

// printRealtimeProcesses fetches process summaries from the daemon API.
// printRealtimeProcesses 从 daemon API 拉取进程摘要。
func printRealtimeProcesses(ctx context.Context, client *daemonclient.Client, limit int, bits bool, cfg *config.Config) error {
	// Prefer health check for a clearer offline message.
	// 优先 health 检查以给出更清晰的离线提示。
	if err := client.Health(ctx); err != nil {
		logx.Debug("daemon health failed", "component", "cli", "err", err)
		hint := config.DaemonStartHint(*cfg)
		return fmt.Errorf("%s", i18n.Tf("cli.daemon_down", map[string]string{
			"api": cfg.DaemonAPIAddr,
			"cmd": hint,
		}))
	}
	// Query GET /api/processes.
	// 请求 GET /api/processes。
	items, err := client.Processes(ctx, limit)
	if err != nil {
		logx.Debug("daemon processes API call failed", "component", "cli", "err", err, "limit", limit)
		hint := config.DaemonStartHint(*cfg)
		return fmt.Errorf("%s", i18n.Tf("cli.daemon_err", map[string]string{
			"err": err.Error(),
			"cmd": hint,
		}))
	}
	logx.Debug("realtime processes fetched", "component", "cli", "rows", len(items), "limit", limit)
	printProcessRows(items, bits)
	return nil
}

// printHistoricalProcesses queries SQLite for process activity in a range.
// printHistoricalProcesses 在给定时间范围内查询 SQLite 进程活跃度。
func printHistoricalProcesses(cfg *config.Config, rangeText string, limit int) error {
	// Parse range token such as "24h".
	// 解析诸如 "24h" 的范围标记。
	d, err := config.ParseRange(rangeText)
	if err != nil {
		return err
	}
	store, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer store.Close()

	now := time.Now()
	// Over-fetch slightly when excluding self so limit still fills after filter.
	// 排除自身时略多取一些，过滤后仍尽量凑满 limit。
	fetchLimit := limit
	if cfg.ExcludeSelf && limit > 0 {
		fetchLimit = limit + 5
	}
	// Rank processes by rolled-up minute stats (see storage package semantics).
	// 按分钟聚合统计排序进程（语义见 storage 包）。
	items, err := store.TopProcessConnectionMinutes(now.Add(-d), now, fetchLimit)
	if err != nil {
		logx.Error("historical processes query failed", "component", "cli", "range", rangeText, "err", err)
		return err
	}
	before := len(items)
	items = storage.FilterSelfSummaries(items, cfg.ExcludeSelf)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	logx.Info("historical processes query", "component", "cli", "range", rangeText, "raw_rows", before, "rows", len(items), "exclude_self", cfg.ExcludeSelf)
	printStorageProcessRows(items)
	return nil
}

// printProcessRows renders realtime rows including optional RX/TX rates.
// printProcessRows 渲染实时行，含可选 RX/TX 速率。
func printProcessRows(items []processstate.ProcessConnectionSummary, bits bool) {
	// Fixed-width header aligned with data rows.
	// 与数据行对齐的固定宽度表头。
	fmt.Printf("%-7s %-18s %-6s %-12s %-12s %-8s %s\n", "PID", "NAME", "CONNS", "RX/s", "TX/s", "LAST", "PATH")
	for _, item := range items {
		fmt.Printf("%-7d %-18s %-6d %-12s %-12s %-8s %s\n",
			item.PID,
			// Truncate long names so columns stay aligned.
			// 截断过长名称以保持列对齐。
			truncateText(item.ProcessName, 18),
			item.ConnectionCount,
			// Show "--" when nettop attribution is off/unavailable.
			// nettop 归因关闭/不可用时显示 "--"。
			formatOptionalRate(item.RXBps, item.TrafficAvailable, bits),
			formatOptionalRate(item.TXBps, item.TrafficAvailable, bits),
			item.LastSeen.Local().Format("15:04:05"),
			// Prefer full path; fall back to short name.
			// 优先完整路径；否则回退短名。
			displayPath(item.ProcessPath, item.ProcessName),
		)
	}
	if len(items) == 0 {
		fmt.Println("no process connection samples yet")
	}
}

// printStorageProcessRows renders historical rows (no per-process rates stored).
// printStorageProcessRows 渲染历史行（库中不存每进程速率）。
func printStorageProcessRows(items []storage.ProcessConnectionSummary) {
	fmt.Printf("%-7s %-18s %-6s %-12s %-12s %-8s %s\n", "PID", "NAME", "CONNS", "RX/s", "TX/s", "LAST", "PATH")
	for _, item := range items {
		fmt.Printf("%-7d %-18s %-6d %-12s %-12s %-8s %s\n",
			item.PID,
			truncateText(item.ProcessName, 18),
			item.ConnectionCount,
			// Historical process minutes do not include RX/TX speeds.
			// 历史进程分钟数据不含 RX/TX 速率。
			"--",
			"--",
			item.LastSeen.Local().Format("15:04:05"),
			displayPath(item.ProcessPath, item.ProcessName),
		)
	}
	if len(items) == 0 {
		fmt.Println("no process connection history yet")
	}
}

// formatOptionalRate returns "--" or a human rate string.
// formatOptionalRate 返回 "--" 或人类可读速率字符串。
func formatOptionalRate(rate float64, ok bool, bits bool) string {
	// ok=false means attribution was not available for this process.
	// ok=false 表示该进程没有可用的流量归因。
	if !ok {
		return "--"
	}
	return units.FormatRate(rate, bits)
}

// displayPath prefers a non-empty executable path over the short name.
// displayPath 优先使用非空可执行路径，否则用短名。
func displayPath(path, fallback string) string {
	if path != "" {
		return path
	}
	return fallback
}

// truncateText shortens text to width, adding "..." when clipped.
// truncateText 将文本截到 width，截断时加 "..."。
// Note: uses byte length, not runes (CJK may split mid-character).
// 注意：按字节长度截断，不是按 rune（中文可能截到半个字符）。
func truncateText(text string, width int) string {
	if len(text) <= width {
		return text
	}
	// Very narrow widths: hard cut without ellipsis.
	// 极窄宽度：直接硬截，不加省略号。
	if width <= 3 {
		return text[:width]
	}
	return text[:width-3] + "..."
}

// newStopCommand signals a daemon via its PID file (Interrupt, then Kill).
// newStopCommand 通过 PID 文件向 daemon 发信号（先 Interrupt，再 Kill）。
func newStopCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop daemon via PID file / 通过 PID 文件停止 daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load PID written by `daemon`.
			// 读取 `daemon` 写入的 PID。
			pid, err := readPIDFile(cfg.PIDPath)
			if err != nil {
				logx.Error("read pid file failed", "component", "cli", "path", cfg.PIDPath, "err", err)
				return err
			}
			logx.Info("stopping daemon", "component", "cli", "pid", pid, "path", cfg.PIDPath)
			// Resolve an os.Process handle (Unix: always succeeds for any pid).
			// 解析 os.Process 句柄（Unix：任意 pid 通常都能“找到”）。
			proc, err := os.FindProcess(pid)
			if err != nil {
				logx.Error("find process failed", "component", "cli", "pid", pid, "err", err)
				return err
			}
			// Prefer graceful SIGINT so collectors can flush/shutdown.
			// 优先优雅 SIGINT，便于采集器刷盘/关闭。
			if err := proc.Signal(os.Interrupt); err != nil {
				logx.Warn("interrupt signal failed, trying kill", "component", "cli", "pid", pid, "err", err)
				// Fall back to Kill if Interrupt fails.
				// Interrupt 失败则回退 Kill。
				if killErr := proc.Kill(); killErr != nil {
					logx.Error("kill process failed", "component", "cli", "pid", pid, "err", killErr)
					return fmt.Errorf("failed to stop pid %d: interrupt error: %v; kill error: %v", pid, err, killErr)
				}
			}
			// Best-effort cleanup of the PID file after signaling.
			// 发信号后尽力清理 PID 文件。
			_ = removePIDFile(cfg.PIDPath)
			fmt.Printf("bytepulse daemon stopped, pid=%d\n", pid)
			logx.Info("stop signal sent", "component", "cli", "pid", pid)
			return nil
		},
	}
	return cmd
}

// newStatusCommand prints the latest aggregated interface speeds from SQLite.
// newStatusCommand 从 SQLite 打印最新聚合网卡速率。
func newStatusCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show latest network speed / 显示最新网速",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore(cfg)
			if err != nil {
				return err
			}
			defer store.Close()

			// Sum RX/TX across interfaces at the newest timestamp (or one NIC).
			// 在最新时间戳上汇总各网卡 RX/TX（或单网卡）。
			sample, err := store.LatestAggregateSample(cfg.Interface)
			if err != nil {
				// Friendlier message when the DB is still empty.
				// 库仍为空时给出更友好的提示。
				if errors.Is(err, storage.ErrNotFound) {
					return fmt.Errorf("no samples yet; start collection with: bytepulse daemon")
				}
				return err
			}

			fmt.Printf("Time: %s\n", sample.Timestamp.Local().Format(time.RFC3339))
			fmt.Printf("Download: %s\n", units.FormatRate(sample.RXSpeedBps, cfg.UseBits))
			fmt.Printf("Upload:   %s\n", units.FormatRate(sample.TXSpeedBps, cfg.UseBits))
			fmt.Printf("Total:    %s\n", units.FormatRate(sample.RXSpeedBps+sample.TXSpeedBps, cfg.UseBits))
			return nil
		},
	}
	return cmd
}

// newReportCommand prints traffic totals and average rates for a time range.
// newReportCommand 打印某时间范围内的流量合计与平均速率。
func newReportCommand(cfg *config.Config) *cobra.Command {
	// Default report window is 24 hours.
	// 默认报告窗口为 24 小时。
	var rangeText string

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Show traffic totals for a range / 显示时间范围内流量合计",
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := config.ParseRange(rangeText)
			if err != nil {
				return err
			}

			store, err := openStore(cfg)
			if err != nil {
				return err
			}
			defer store.Close()

			now := time.Now()
			// Sum byte deltas in [now-d, now].
			// 汇总 [now-d, now] 内的字节增量。
			summary, err := store.Summary(now.Add(-d), now, cfg.Interface)
			if err != nil {
				return err
			}

			fmt.Printf("Range: %s\n", rangeText)
			fmt.Printf("Interface: %s\n", config.InterfaceLabel(cfg.Interface))
			fmt.Printf("Download: %s\n", units.FormatBytes(summary.RXBytes))
			fmt.Printf("Upload:   %s\n", units.FormatBytes(summary.TXBytes))
			fmt.Printf("Total:    %s\n", units.FormatBytes(summary.RXBytes+summary.TXBytes))
			// Averages divide totals by the requested window duration.
			// 平均值用总量除以请求窗口时长。
			fmt.Printf("Avg Down: %s\n", units.FormatRate(summary.AvgRXBps(), cfg.UseBits))
			fmt.Printf("Avg Up:   %s\n", units.FormatRate(summary.AvgTXBps(), cfg.UseBits))
			fmt.Printf("Avg Total: %s\n", units.FormatRate(summary.AvgTotalBps(), cfg.UseBits))
			return nil
		},
	}

	cmd.Flags().StringVar(&rangeText, "range", "24h", "time range / 时间范围: 1h,2h,...,15d")
	return cmd
}

// newInterfacesCommand lists OS network interfaces and cumulative counters.
// newInterfacesCommand 列出操作系统网卡及累计计数器。
func newInterfacesCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "interfaces",
		Short: "List network interfaces / 列出网卡",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Include loopback; used for discovery, not continuous sampling.
			// 包含回环；用于发现，不是持续采样。
			snapshot, err := collector.ReadAllCounters()
			if err != nil {
				return err
			}
			for _, iface := range snapshot {
				fmt.Printf("%-16s rx=%s tx=%s loopback=%v\n",
					iface.Name,
					// Counters here are cumulative OS totals, not per-interval deltas.
					// 这里的计数是操作系统累计值，不是区间增量。
					units.FormatBytes(iface.RXBytes),
					units.FormatBytes(iface.TXBytes),
					iface.IsLoopback,
				)
			}
			return nil
		},
	}
	return cmd
}

// newTUICommand launches the Bubbletea terminal dashboard.
// newTUICommand 启动 Bubbletea 终端看板。
func newTUICommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Open terminal dashboard / 打开终端看板",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore(cfg)
			if err != nil {
				return err
			}
			defer store.Close()
			// TUI reads interface stats from SQLite; processes via daemon API.
			// TUI 从 SQLite 读网卡统计；进程数据走 daemon API。
			return tui.Run(store, *cfg)
		},
	}
	return cmd
}

// newWebCommand starts the embedded HTTP dashboard and JSON APIs.
// newWebCommand 启动内嵌 HTTP 看板与 JSON API。
func newWebCommand(cfg *config.Config) *cobra.Command {
	// Listen address defaults from config file / Default.
	// 监听地址默认来自配置文件 / Default。
	addr := cfg.WebAddr
	if addr == "" {
		addr = "127.0.0.1:8989"
	}

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start local web dashboard / 启动本地 Web 看板",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore(cfg)
			if err != nil {
				return err
			}
			defer store.Close()

			// Non-blocking hint if daemon is down (page still serves NIC data).
			// daemon 未启动时给出提示（页面仍可提供网卡数据）。
			client := daemonclient.New(cfg.DaemonAPIAddr)
			if err := client.Health(context.Background()); err != nil {
				hint := config.DaemonStartHint(*cfg)
				fmt.Fprint(os.Stderr, i18n.Tf("cli.web_no_daemon", map[string]string{
					"api": cfg.DaemonAPIAddr,
					"cmd": hint,
				}))
				logx.Warn("web started without daemon", "component", "web", "api", cfg.DaemonAPIAddr, "err", err)
			}

			s := web.New(store, *cfg)
			fmt.Printf("bytepulse web listening on http://%s\n", addr)
			logx.Info("web server starting", "component", "web", "addr", addr, "db", cfg.DBPath, "daemon_api", cfg.DaemonAPIAddr)
			// Blocks until the server exits (or bind fails).
			// 阻塞直到服务退出（或绑定失败）。
			if err := s.ListenAndServe(addr); err != nil {
				logx.Error("web server stopped", "component", "web", "addr", addr, "err", err)
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&addr, "addr", addr, "HTTP listen address / HTTP 监听地址")
	return cmd
}
