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

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	cfg := config.Default()

	cmd := &cobra.Command{
		Use:   "bytepulse",
		Short: "Monitor local network traffic",
	}

	cmd.PersistentFlags().StringVar(&cfg.DBPath, "db", cfg.DBPath, "SQLite database path")
	cmd.PersistentFlags().StringVar(&cfg.PIDPath, "pid-file", cfg.PIDPath, "daemon PID file path")
	cmd.PersistentFlags().StringVar(&cfg.Interface, "interface", "", "interface name to query; empty means all non-loopback interfaces")
	cmd.PersistentFlags().BoolVar(&cfg.UseBits, "bits", false, "display rates as bits/s instead of bytes/s")
	cmd.PersistentFlags().DurationVar(&cfg.Retention, "retention", cfg.Retention, "data retention period")
	cmd.PersistentFlags().IntVar(&cfg.TopN, "top-n", cfg.TopN, "default number of rows for process views")
	cmd.PersistentFlags().DurationVar(&cfg.ProcessInterval, "process-interval", cfg.ProcessInterval, "process connection sampling interval")
	cmd.PersistentFlags().StringVar(&cfg.DaemonAPIAddr, "daemon-api-addr", cfg.DaemonAPIAddr, "daemon local API address")
	cmd.PersistentFlags().StringVar(&cfg.ProcessTraffic, "process-traffic", cfg.ProcessTraffic, "process traffic attribution mode: off, nettop")

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

func openStore(cfg *config.Config) (*storage.Store, error) {
	store, err := storage.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func newDaemonCommand(cfg *config.Config) *cobra.Command {
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Collect network traffic every second",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := writePIDFile(cfg.PIDPath); err != nil {
				return err
			}
			defer removePIDFile(cfg.PIDPath)

			store, err := openStore(cfg)
			if err != nil {
				return err
			}
			defer store.Close()

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			runCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			c := collector.New(store, collector.Options{
				Interval:  interval,
				Interface: cfg.Interface,
				Retention: cfg.Retention,
			})
			procState := processstate.New()
			pc := collector.NewProcessConnectionCollector(
				store,
				proc.NewSampler(),
				procState,
				collector.ProcessConnectionOptions{
					Interval:  cfg.ProcessInterval,
					Retention: cfg.Retention,
				},
			)
			api := daemonapi.NewServer(procState, store, *cfg)
			apiServer := &http.Server{
				Addr:    cfg.DaemonAPIAddr,
				Handler: api.Handler(),
			}
			if cfg.ProcessTraffic != "off" && cfg.ProcessTraffic != "nettop" {
				return fmt.Errorf("unsupported process traffic mode %q; use off or nettop", cfg.ProcessTraffic)
			}

			errCh := make(chan error, 3)
			go func() {
				if err := apiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errCh <- fmt.Errorf("daemon API: %w", err)
				}
			}()
			go func() {
				if err := c.Run(runCtx); err != nil {
					errCh <- fmt.Errorf("collector: %w", err)
				}
			}()
			go func() {
				if err := pc.Run(runCtx); err != nil {
					errCh <- fmt.Errorf("process collector: %w", err)
				}
			}()
			if cfg.ProcessTraffic == "nettop" {
				pt := collector.NewProcessTrafficCollector(proctraffic.NewNettopAttributor(), procState)
				go func() {
					if err := pt.Run(runCtx); err != nil {
						errCh <- fmt.Errorf("process traffic collector: %w", err)
					}
				}()
			}

			fmt.Printf("bytepulse daemon started, db=%s, interval=%s, process_interval=%s, process_traffic=%s, api=http://%s\n",
				cfg.DBPath, interval, cfg.ProcessInterval, cfg.ProcessTraffic, cfg.DaemonAPIAddr)
			select {
			case <-ctx.Done():
				cancel()
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer shutdownCancel()
				_ = apiServer.Shutdown(shutdownCtx)
				return nil
			case err := <-errCh:
				cancel()
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer shutdownCancel()
				_ = apiServer.Shutdown(shutdownCtx)
				return err
			}
		},
	}

	cmd.Flags().DurationVar(&interval, "interval", time.Second, "sampling interval")
	return cmd
}

func newProcessesCommand(cfg *config.Config) *cobra.Command {
	var limit int
	var rangeText string
	var watch bool

	cmd := &cobra.Command{
		Use:   "processes",
		Short: "Show processes currently using the network",
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit <= 0 {
				limit = cfg.TopN
			}
			if rangeText != "" {
				return printHistoricalProcesses(cfg, rangeText, limit)
			}
			client := daemonclient.New(cfg.DaemonAPIAddr)
			if watch {
				ticker := time.NewTicker(time.Second)
				defer ticker.Stop()
				for {
					fmt.Print("\033[H\033[2J")
					if err := printRealtimeProcesses(cmd.Context(), client, limit, cfg.UseBits); err != nil {
						fmt.Println(err)
					}
					select {
					case <-cmd.Context().Done():
						return nil
					case <-ticker.C:
					}
				}
			}
			return printRealtimeProcesses(cmd.Context(), client, limit, cfg.UseBits)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", cfg.TopN, "number of process rows to show")
	cmd.Flags().StringVar(&rangeText, "range", "", "historical range: 1h,2h,3h,5h,10h,12h,24h,2d,3d,7d,15d")
	cmd.Flags().BoolVar(&watch, "watch", false, "refresh realtime process view every second")
	return cmd
}

func printRealtimeProcesses(ctx context.Context, client *daemonclient.Client, limit int, bits bool) error {
	items, err := client.Processes(ctx, limit)
	if err != nil {
		return fmt.Errorf("daemon API unavailable; start it with: bytepulse daemon")
	}
	printProcessRows(items, bits)
	return nil
}

func printHistoricalProcesses(cfg *config.Config, rangeText string, limit int) error {
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
	items, err := store.TopProcessConnectionMinutes(now.Add(-d), now, limit)
	if err != nil {
		return err
	}
	printStorageProcessRows(items)
	return nil
}

func printProcessRows(items []processstate.ProcessConnectionSummary, bits bool) {
	fmt.Printf("%-7s %-18s %-6s %-12s %-12s %-8s %s\n", "PID", "NAME", "CONNS", "RX/s", "TX/s", "LAST", "PATH")
	for _, item := range items {
		fmt.Printf("%-7d %-18s %-6d %-12s %-12s %-8s %s\n",
			item.PID,
			truncateText(item.ProcessName, 18),
			item.ConnectionCount,
			formatOptionalRate(item.RXBps, item.TrafficAvailable, bits),
			formatOptionalRate(item.TXBps, item.TrafficAvailable, bits),
			item.LastSeen.Local().Format("15:04:05"),
			displayPath(item.ProcessPath, item.ProcessName),
		)
	}
	if len(items) == 0 {
		fmt.Println("no process connection samples yet")
	}
}

func printStorageProcessRows(items []storage.ProcessConnectionSummary) {
	fmt.Printf("%-7s %-18s %-6s %-12s %-12s %-8s %s\n", "PID", "NAME", "CONNS", "RX/s", "TX/s", "LAST", "PATH")
	for _, item := range items {
		fmt.Printf("%-7d %-18s %-6d %-12s %-12s %-8s %s\n",
			item.PID,
			truncateText(item.ProcessName, 18),
			item.ConnectionCount,
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

func formatOptionalRate(rate float64, ok bool, bits bool) string {
	if !ok {
		return "--"
	}
	return units.FormatRate(rate, bits)
}

func displayPath(path, fallback string) string {
	if path != "" {
		return path
	}
	return fallback
}

func truncateText(text string, width int) string {
	if len(text) <= width {
		return text
	}
	if width <= 3 {
		return text[:width]
	}
	return text[:width-3] + "..."
}

func newStopCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop a daemon started with this PID file",
		RunE: func(cmd *cobra.Command, args []string) error {
			pid, err := readPIDFile(cfg.PIDPath)
			if err != nil {
				return err
			}
			proc, err := os.FindProcess(pid)
			if err != nil {
				return err
			}
			if err := proc.Signal(os.Interrupt); err != nil {
				if killErr := proc.Kill(); killErr != nil {
					return fmt.Errorf("failed to stop pid %d: interrupt error: %v; kill error: %v", pid, err, killErr)
				}
			}
			_ = removePIDFile(cfg.PIDPath)
			fmt.Printf("bytepulse daemon stopped, pid=%d\n", pid)
			return nil
		},
	}
	return cmd
}

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

			sample, err := store.LatestAggregateSample(cfg.Interface)
			if err != nil {
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

func newReportCommand(cfg *config.Config) *cobra.Command {
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
			summary, err := store.Summary(now.Add(-d), now, cfg.Interface)
			if err != nil {
				return err
			}

			fmt.Printf("Range: %s\n", rangeText)
			fmt.Printf("Interface: %s\n", config.InterfaceLabel(cfg.Interface))
			fmt.Printf("Download: %s\n", units.FormatBytes(summary.RXBytes))
			fmt.Printf("Upload:   %s\n", units.FormatBytes(summary.TXBytes))
			fmt.Printf("Total:    %s\n", units.FormatBytes(summary.RXBytes+summary.TXBytes))
			fmt.Printf("Avg Down: %s\n", units.FormatRate(summary.AvgRXBps(), cfg.UseBits))
			fmt.Printf("Avg Up:   %s\n", units.FormatRate(summary.AvgTXBps(), cfg.UseBits))
			fmt.Printf("Avg Total: %s\n", units.FormatRate(summary.AvgTotalBps(), cfg.UseBits))
			return nil
		},
	}

	cmd.Flags().StringVar(&rangeText, "range", "24h", "time range: 1h,2h,3h,5h,10h,12h,24h,2d,3d,7d,15d")
	return cmd
}

func newInterfacesCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "interfaces",
		Short: "List network interfaces visible to the collector",
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshot, err := collector.ReadAllCounters()
			if err != nil {
				return err
			}
			for _, iface := range snapshot {
				fmt.Printf("%-16s rx=%s tx=%s loopback=%v\n",
					iface.Name,
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
			return tui.Run(store, *cfg)
		},
	}
	return cmd
}

func newWebCommand(cfg *config.Config) *cobra.Command {
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
			return s.ListenAndServe(addr)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8989", "HTTP listen address")
	return cmd
}
