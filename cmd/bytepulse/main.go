package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bytepulse/internal/collector"
	"bytepulse/internal/config"
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

	cmd.AddCommand(newDaemonCommand(&cfg))
	cmd.AddCommand(newStopCommand(&cfg))
	cmd.AddCommand(newStatusCommand(&cfg))
	cmd.AddCommand(newReportCommand(&cfg))
	cmd.AddCommand(newInterfacesCommand(&cfg))
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

			c := collector.New(store, collector.Options{
				Interval:  interval,
				Interface: cfg.Interface,
			})
			fmt.Printf("bytepulse daemon started, db=%s, interval=%s\n", cfg.DBPath, interval)
			return c.Run(ctx)
		},
	}

	cmd.Flags().DurationVar(&interval, "interval", time.Second, "sampling interval")
	return cmd
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
