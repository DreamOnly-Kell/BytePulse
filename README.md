# BytePulse

[简体中文](README.zh-CN.md)

BytePulse is a local network traffic monitor with CLI, TUI, and Web dashboards. It samples network interface counters, stores traffic data in SQLite, and reports real-time speed plus rolling traffic totals.

## Features

- Real-time download, upload, and total speed.
- Automatic rate units: `B/s`, `KB/s`, `MB/s`, `GB/s`.
- Optional bits/s display with `--bits`.
- Rolling traffic summaries for `1h`, `2h`, `3h`, `5h`, `10h`, `12h`, `24h`, `2d`, `3d`, `7d`, and `15d`.
- Hourly and daily aggregation APIs.
- CLI, TUI, and local Web dashboard.
- Per-interface filtering with `--interface`.
- Process connection discovery on macOS: process name, full process path, PID, connection count, and last seen time.
- 1-second realtime process refresh through the daemon API for CLI, TUI, and Web.
- Local SQLite storage.
- No packet capture; BytePulse reads operating-system network counters.
- No per-process RX/TX byte attribution yet.

## Platform Support

BytePulse is designed to be cross-platform. The current implementation uses Go and `gopsutil` for network interface counters, so the core CLI, storage, TUI, and Web dashboard are expected to work on macOS, Linux, and Windows.

| Platform | Status |
| --- | --- |
| macOS | Core monitoring tested; process connection discovery implemented |
| Linux | Core monitoring expected; process connection discovery currently disabled |
| Windows | Core monitoring expected; process connection discovery currently disabled |

Phase 2A process monitoring shows which processes have network connections. It does not yet attribute exact per-process traffic bytes or speeds; that requires platform-specific traffic attribution work. Linux and Windows process discovery are planned as platform-specific follow-ups.

## Build

Build for the current platform:

```bash
go mod tidy
go build -o bytepulse ./cmd/bytepulse
```

## Release Builds

Create the output directory first:

```bash
mkdir -p dist
```

Build macOS binaries:

```bash
GOOS=darwin GOARCH=arm64 go build -o dist/bytepulse-darwin-arm64 ./cmd/bytepulse
GOOS=darwin GOARCH=amd64 go build -o dist/bytepulse-darwin-amd64 ./cmd/bytepulse
```

Build Linux binaries:

```bash
GOOS=linux GOARCH=amd64 go build -o dist/bytepulse-linux-amd64 ./cmd/bytepulse
GOOS=linux GOARCH=arm64 go build -o dist/bytepulse-linux-arm64 ./cmd/bytepulse
```

Build Windows binaries:

```bash
GOOS=windows GOARCH=amd64 go build -o dist/bytepulse-windows-amd64.exe ./cmd/bytepulse
GOOS=windows GOARCH=arm64 go build -o dist/bytepulse-windows-arm64.exe ./cmd/bytepulse
```

Package release files:

```bash
tar -czf dist/bytepulse-darwin-arm64.tar.gz -C dist bytepulse-darwin-arm64
tar -czf dist/bytepulse-darwin-amd64.tar.gz -C dist bytepulse-darwin-amd64
tar -czf dist/bytepulse-linux-amd64.tar.gz -C dist bytepulse-linux-amd64
tar -czf dist/bytepulse-linux-arm64.tar.gz -C dist bytepulse-linux-arm64
zip -j dist/bytepulse-windows-amd64.zip dist/bytepulse-windows-amd64.exe
zip -j dist/bytepulse-windows-arm64.zip dist/bytepulse-windows-arm64.exe
```

## Usage

Start the collector:

```bash
./bytepulse daemon
```

Stop a foreground collector with `Ctrl+C`.

Run the collector in the background:

```bash
./bytepulse daemon > bytepulse.log 2>&1 &
```

Stop a background collector:

```bash
./bytepulse stop
```

Show the latest speed:

```bash
./bytepulse status
```

Show a traffic report:

```bash
./bytepulse report --range 24h
```

Show processes currently using the network:

```bash
./bytepulse processes
./bytepulse processes --watch
./bytepulse processes --range 24h
```

Process views show both `NAME` and `PATH`. `NAME` is the short executable name; `PATH` preserves the full process path when the platform provides it.

List network interfaces:

```bash
./bytepulse interfaces
```

Open the TUI dashboard:

```bash
./bytepulse tui
```

Start the Web dashboard:

```bash
./bytepulse web --addr 127.0.0.1:8989
```

Then open:

```text
http://127.0.0.1:8989
```

## Options

Use a custom database:

```bash
./bytepulse --db ./bytepulse.db daemon
```

Monitor a specific interface:

```bash
./bytepulse --interface en0 status
./bytepulse --interface en0 report --range 24h
```

Display rates as bits/s:

```bash
./bytepulse --bits status
```

Use a custom daemon PID file:

```bash
./bytepulse --pid-file ./bytepulse.pid daemon
./bytepulse --pid-file ./bytepulse.pid stop
```

Use a custom daemon API address:

```bash
./bytepulse --daemon-api-addr 127.0.0.1:8988 daemon
./bytepulse --daemon-api-addr 127.0.0.1:8988 processes --watch
```

## Resource Usage

In tools such as `htop`, `VIRT` can be much larger than actual memory use. BytePulse is a Go program and uses SQLite; the process can reserve a large virtual address range without physically using that memory. Use `RES`/resident memory and sustained CPU usage to judge real resource cost.

Expected idle behavior is low CPU usage with small resident memory. If `RES` or CPU grows continuously while the collector is idle, please capture the command, platform, Go version, and `htop` values.

## Data

Default database path:

```text
~/.bytepulse/bytepulse.db
```

BytePulse keeps up to 30 days of samples by default. The default collector interval is 1 second, but it can be changed with `daemon --interval`. Each interface sample stores timestamp, interface name, received bytes, transmitted bytes, receive speed, transmit speed, and sample interval.

Process connection monitoring also samples every 1 second, but it does not write raw per-second connection snapshots to SQLite. The daemon keeps the latest process connection state in memory for realtime views and flushes minute-level process rollups to SQLite for historical process reports.

Rolling summaries include samples by their sample timestamp. With the default 1-second interval, boundary error is at most about one sample interval. Daily buckets are currently grouped by Unix day boundaries.

Run one collector daemon for a database. Multiple collectors writing to the same database can make the latest combined interface view ambiguous.

## Web API

```text
GET /api/realtime
GET /api/summary?range=24h
GET /api/ranges
GET /api/hourly
GET /api/daily
GET /api/series
GET /api/processes
GET /api/processes/top?range=24h
```

All API endpoints accept `?interface=<name>`. For example:

```text
GET /api/realtime?interface=en0
GET /api/summary?range=24h&interface=en0
```

`/api/hourly` returns the latest 24 hours. `/api/daily` returns the latest 15 days.

The daemon-local API also exposes:

```text
GET /api/health
GET /api/processes?limit=30
GET /api/processes/connections?process_key=<key>
GET /api/processes/top?range=24h&limit=30
```

## Roadmap

- Configuration file support.
- Interface include/exclude rules.
- CSV and JSON export.
- macOS `launchd` service setup.
- Minute/hour/day aggregate tables for lower long-term storage usage.
- Linux and Windows process connection discovery.
- Per-process traffic byte attribution as a separate future phase.
- Desktop tray or widget integration.

## License

MIT
