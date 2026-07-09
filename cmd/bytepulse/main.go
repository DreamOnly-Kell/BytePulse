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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newRootCommand builds the root command, global flags, and subcommands.
// newRootCommand 构建根命令、全局 flag 与子命令。
func newRootCommand() *cobra.Command {
	// Start from package defaults (~/.bytepulse paths, etc.).
	// 从包内默认值起步（~/.bytepulse 路径等）。
	cfg := config.Default()

	// Root command metadata shown in help.
	// 帮助信息中展示的根命令元数据。
	cmd := &cobra.Command{
		Use:   "bytepulse",
		Short: "Monitor local network traffic",
	}

	// Persistent flags apply to all subcommands.
	// Persistent flags 对所有子命令生效。
	cmd.PersistentFlags().StringVar(&cfg.DBPath, "db", cfg.DBPath, "SQLite database path")
	cmd.PersistentFlags().StringVar(&cfg.PIDPath, "pid-file", cfg.PIDPath, "daemon PID file path")
	cmd.PersistentFlags().StringVar(&cfg.Interface, "interface", "", "interface name to query; empty means all non-loopback interfaces")
	cmd.PersistentFlags().BoolVar(&cfg.UseBits, "bits", false, "display rates as bits/s instead of bytes/s")
	cmd.PersistentFlags().DurationVar(&cfg.Retention, "retention", cfg.Retention, "data retention period")
	cmd.PersistentFlags().IntVar(&cfg.TopN, "top-n", cfg.TopN, "default number of rows for process views")
	cmd.PersistentFlags().DurationVar(&cfg.ProcessInterval, "process-interval", cfg.ProcessInterval, "process connection sampling interval")
	cmd.PersistentFlags().StringVar(&cfg.DaemonAPIAddr, "daemon-api-addr", cfg.DaemonAPIAddr, "daemon local API address")
	cmd.PersistentFlags().StringVar(&cfg.ProcessTraffic, "process-traffic", cfg.ProcessTraffic, "process traffic attribution mode: off, nettop")
	cmd.PersistentFlags().BoolVar(&cfg.ExcludeSelf, "exclude-self", cfg.ExcludeSelf, "hide bytepulse itself from process views (default true)")

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
		return nil, err
	}
	// Apply CREATE TABLE / PRAGMA / column upgrades.
	// 执行 CREATE TABLE / PRAGMA / 列升级。
	if err := store.Migrate(); err != nil {
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
	// Interface sampling interval (flag-local; process interval is global).
	// 网卡采样间隔（本命令 flag；进程间隔为全局 flag）。
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Collect network traffic every second",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Record our PID so `bytepulse stop` can signal us.
			// 记录自身 PID，便于 `bytepulse stop` 发信号。
			if err := writePIDFile(cfg.PIDPath); err != nil {
				return err
			}
			// Always remove the PID file on exit (success or failure).
			// 退出时始终删除 PID 文件（成功或失败）。
			defer removePIDFile(cfg.PIDPath)

			// Open shared SQLite store for interface + process minute data.
			// 打开共享 SQLite，供网卡样本与进程分钟数据使用。
			store, err := openStore(cfg)
			if err != nil {
				return err
			}
			defer store.Close()

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
			// Only "off" and "nettop" are valid process-traffic modes.
			// process-traffic 仅允许 "off" 与 "nettop"。
			if cfg.ProcessTraffic != "off" && cfg.ProcessTraffic != "nettop" {
				return fmt.Errorf("unsupported process traffic mode %q; use off or nettop", cfg.ProcessTraffic)
			}

			// Buffered error channel from background workers (API + collectors).
			// 后台 worker（API + 采集器）共用的带缓冲错误通道。
			errCh := make(chan error, 3)
			// Serve the daemon API until shutdown.
			// 提供 daemon API 直到关闭。
			go func() {
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
			// Optionally start macOS nettop-based per-process rates.
			// 可选启动基于 macOS nettop 的每进程速率。
			if cfg.ProcessTraffic == "nettop" {
				pt := collector.NewProcessTrafficCollector(proctraffic.NewNettopAttributor(), procState)
				go func() {
					if err := pt.Run(runCtx); err != nil {
						errCh <- fmt.Errorf("process traffic collector: %w", err)
					}
				}()
			}

			// Announce startup configuration on stdout.
			// 在 stdout 打印启动配置。
			fmt.Printf("bytepulse daemon started, db=%s, interval=%s, process_interval=%s, process_traffic=%s, api=http://%s\n",
				cfg.DBPath, interval, cfg.ProcessInterval, cfg.ProcessTraffic, cfg.DaemonAPIAddr)
			// Wait for signal shutdown or a worker failure.
			// 等待信号关闭或某个 worker 失败。
			select {
			case <-ctx.Done():
				// Cooperative stop of collectors.
				// 协作式停止采集器。
				cancel()
				// Give HTTP server a short window to drain.
				// 给 HTTP 服务短暂时间排空连接。
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer shutdownCancel()
				_ = apiServer.Shutdown(shutdownCtx)
				return nil
			case err := <-errCh:
				// Propagate the first worker error after cleanup.
				// 清理后向上返回第一个 worker 错误。
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
	cmd.Flags().DurationVar(&interval, "interval", time.Second, "sampling interval")
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
		Short: "Show processes currently using the network",
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
					if err := printRealtimeProcesses(cmd.Context(), client, limit, cfg.UseBits); err != nil {
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
			return printRealtimeProcesses(cmd.Context(), client, limit, cfg.UseBits)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", cfg.TopN, "number of process rows to show")
	cmd.Flags().StringVar(&rangeText, "range", "", "historical range: 1h,2h,3h,5h,10h,12h,24h,2d,3d,7d,15d")
	cmd.Flags().BoolVar(&watch, "watch", false, "refresh realtime process view every second")
	return cmd
}

// printRealtimeProcesses fetches process summaries from the daemon API.
// printRealtimeProcesses 从 daemon API 拉取进程摘要。
func printRealtimeProcesses(ctx context.Context, client *daemonclient.Client, limit int, bits bool) error {
	// Query GET /api/processes.
	// 请求 GET /api/processes。
	items, err := client.Processes(ctx, limit)
	if err != nil {
		// Guide the user to start the daemon when the API is unreachable.
		// API 不可达时引导用户启动 daemon。
		return fmt.Errorf("daemon API unavailable; start it with: bytepulse daemon")
	}
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
		return err
	}
	items = storage.FilterSelfSummaries(items, cfg.ExcludeSelf)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
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
		Short: "Stop a daemon started with this PID file",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load PID written by `daemon`.
			// 读取 `daemon` 写入的 PID。
			pid, err := readPIDFile(cfg.PIDPath)
			if err != nil {
				return err
			}
			// Resolve an os.Process handle (Unix: always succeeds for any pid).
			// 解析 os.Process 句柄（Unix：任意 pid 通常都能“找到”）。
			proc, err := os.FindProcess(pid)
			if err != nil {
				return err
			}
			// Prefer graceful SIGINT so collectors can flush/shutdown.
			// 优先优雅 SIGINT，便于采集器刷盘/关闭。
			if err := proc.Signal(os.Interrupt); err != nil {
				// Fall back to Kill if Interrupt fails.
				// Interrupt 失败则回退 Kill。
				if killErr := proc.Kill(); killErr != nil {
					return fmt.Errorf("failed to stop pid %d: interrupt error: %v; kill error: %v", pid, err, killErr)
				}
			}
			// Best-effort cleanup of the PID file after signaling.
			// 发信号后尽力清理 PID 文件。
			_ = removePIDFile(cfg.PIDPath)
			fmt.Printf("bytepulse daemon stopped, pid=%d\n", pid)
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
		Short: "Show the latest sampled network speed",
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
		Short: "Show traffic totals and average rates for a time range",
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

	cmd.Flags().StringVar(&rangeText, "range", "24h", "time range: 1h,2h,3h,5h,10h,12h,24h,2d,3d,7d,15d")
	return cmd
}

// newInterfacesCommand lists OS network interfaces and cumulative counters.
// newInterfacesCommand 列出操作系统网卡及累计计数器。
func newInterfacesCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "interfaces",
		Short: "List network interfaces visible to the collector",
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
		Short: "Open the terminal dashboard",
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
	// Listen address is local to this subcommand.
	// 监听地址仅对本子命令生效。
	var addr string

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start the local web dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore(cfg)
			if err != nil {
				return err
			}
			defer store.Close()

			s := web.New(store, *cfg)
			fmt.Printf("bytepulse web listening on http://%s\n", addr)
			// Blocks until the server exits (or bind fails).
			// 阻塞直到服务退出（或绑定失败）。
			return s.ListenAndServe(addr)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8989", "HTTP listen address")
	return cmd
}
