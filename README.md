# BytePulse

[简体中文](README.zh-CN.md)

BytePulse is a **local, on-demand network troubleshooting tool** — open it when you want to see who is using bandwidth, not a product you must leave running 24×7.

It samples OS network interface counters (no packet capture), stores history in SQLite, and shows real-time speed plus rolling totals through **CLI**, **TUI**, and a local **Web** dashboard. On supported platforms it also shows which processes hold network connections and optional per-process RX/TX rates.

A single **`daemon`** is the shared collector and in-memory process-state producer. Start it when you need live process data; stop it when you are done. **Only one daemon is allowed at a time** — a second `daemon` is refused until you `stop` the first. TUI / Web / CLI never auto-start the daemon: if it is down, they wait or print a clear error with a start hint.

## Features

- Real-time download, upload, and total speed.
- Automatic rate units: `B/s`, `KB/s`, `MB/s`, `GB/s` (optional `--bits` for bits/s).
- Rolling traffic summaries: `1h` … `15d`, plus hourly/daily aggregation APIs.
- CLI, TUI, and local Web dashboard (shared daemon API for process views).
- **Single daemon only:** exclusive PID-file lock; second start is blocked with a stop-then-start hint.
- Per-interface filtering with `--interface`.
- Process connection discovery (name, full path, PID, connection count, last seen).
- Optional per-process RX/TX: macOS `nettop`, Windows TCP ESTATS (`--process-traffic auto`).
- Hides BytePulse itself from process views by default (`--exclude-self`).
- Local SQLite storage; optional YAML config; UI language `en` / `zh` (logs stay English).
- No packet capture — reads operating-system counters only.

## Platform Support

| Platform | NIC speed & history | Process connections | Per-process RX/TX |
| --- | --- | --- | --- |
| **macOS** | Yes | Yes | Yes (`nettop` / `auto`) |
| **Windows 10+** | Yes | Yes | Yes, TCP only (`estats` / `auto`; UDP bytes not available) |
| **Linux** | Yes | Not yet | Not yet |

- **Core** (interfaces, SQLite, CLI / TUI / Web for NIC data) works on macOS, Linux, and Windows via Go + `gopsutil`.
- **Process monitoring** is implemented for macOS and Windows. Linux process discovery/traffic is deferred (not a current goal).
- Per-process RX/TX is **off by default** (`--process-traffic off`). Enable with `auto`, or platform-specific `nettop` / `estats`.
- When attribution is unavailable, process views still show connection counts and print `--` for `RX/s` / `TX/s`.

## Design notes (what BytePulse is not)

- **Not a 24×7 background service by default.** The daemon is optional: run it while troubleshooting, then `stop`. Installers / `launchd` / systemd integration are optional future work, not required for normal use.
- **One collector only (enforced).** `daemon` refuses a second start when: the PID file points at a live process, the PID file is exclusively locked, or the daemon API already answers `/api/health`. Message tells you to `bytepulse stop` first, then start again. Multiple viewers (TUI, Web, CLI) against one daemon are fine.
- **No auto-start from TUI/Web.** If the daemon is down, TUI shows a wait screen; Web still serves NIC charts from SQLite but the process table waits; CLI process commands exit with a clear start hint.
- **No packet capture** and **no always-on kernel agent** — OS counters and platform APIs only.
- **Linux process monitoring** is out of scope for the current phase.

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

### Typical troubleshooting session

```bash
# 1) Start the shared collector (only one allowed)
./bytepulse --process-traffic auto daemon

# 2) In another terminal, open a viewer
./bytepulse tui
# or
./bytepulse web --addr 127.0.0.1:8989
# or
./bytepulse processes --watch

# 3) When finished
./bytepulse stop
```

Foreground daemon: stop with `Ctrl+C`. Background example:

```bash
./bytepulse daemon > bytepulse.log 2>&1 &
./bytepulse stop
```

A second `daemon` while one is already running **fails immediately** (does not replace the first). Stop all collectors first, then start a new one:

```bash
./bytepulse stop
./bytepulse daemon
```

### Commands

Show the latest NIC speed / rolling report (reads SQLite; daemon optional for history that already exists):

```bash
./bytepulse status
./bytepulse report --range 24h
./bytepulse interfaces
```

Show processes currently using the network (**requires a running daemon**):

```bash
./bytepulse processes
./bytepulse processes --watch
./bytepulse processes --range 24h
```

Process views show both `NAME` and `PATH`. `NAME` is the short executable name; `PATH` keeps the full path when the platform provides it.

Enable per-process realtime traffic on the daemon:

```bash
# macOS
./bytepulse --process-traffic nettop daemon
# or: --process-traffic auto

# Windows 10+
bytepulse.exe --process-traffic auto daemon
# or: --process-traffic estats
```

Windows rates come from TCP ESTATS (best-effort; some connections need a short warm-up). UDP byte rates are not available on Windows via this API.

TUI / Web:

```bash
./bytepulse tui
./bytepulse web --addr 127.0.0.1:8989
# → http://127.0.0.1:8989
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

Use a custom daemon PID file (single-instance lock is on this file; default `~/.bytepulse/bytepulse.pid`):

```bash
./bytepulse --pid-file ./bytepulse.pid daemon
./bytepulse --pid-file ./bytepulse.pid stop
```

Use a custom daemon API address (default `127.0.0.1:8988`; a healthy API also blocks a second daemon):

```bash
./bytepulse --daemon-api-addr 127.0.0.1:8988 daemon
./bytepulse --daemon-api-addr 127.0.0.1:8988 processes --watch
```

Logging (default level is `error`; raise for diagnostics). Tables still go to stdout; logs go to stderr or a file:

```bash
./bytepulse --log-level info daemon
./bytepulse --log-level debug --log-file ~/.bytepulse/bytepulse.log daemon
./bytepulse --log-level info --log-format json daemon
```

Optional YAML config. Copy the commented sample and edit:

```bash
mkdir -p ~/.bytepulse
cp config.example.yaml ~/.bytepulse/config.yaml
# edit ~/.bytepulse/config.yaml
```

- Default path if the file exists: `~/.bytepulse/config.yaml`
- Or: `--config /path/to.yaml`
- Precedence: built-in defaults &lt; config file &lt; CLI flags
- Full annotated sample: [`config.example.yaml`](config.example.yaml)

```bash
./bytepulse --config ./my.yaml daemon
```

UI language (`lang` / `--lang`): `en` (default) or `zh` for TUI, Web labels, and CLI user prompts. **Logs always stay English.** Command `--help` text is bilingual in the binary (not switched by `lang`).

```bash
./bytepulse --lang zh tui
```

**Daemon down behavior:** TUI stays on a wait/retry screen (start `bytepulse daemon` in another terminal). Web still serves NIC charts from SQLite; the process table shows a waiting message. CLI `processes` prints an error and a start command. Viewers never auto-start the daemon.

Hide BytePulse from process views (default is on). Matching uses the daemon PID and the executable name `bytepulse` / `bytepulse.exe`:

```bash
./bytepulse daemon
./bytepulse processes
```

Show BytePulse itself in process views (debugging):

```bash
./bytepulse --exclude-self=false daemon
./bytepulse --exclude-self=false processes
./bytepulse --exclude-self=false processes --range 24h
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

Process connection monitoring also samples every 1 second, but it does not write raw per-second connection snapshots to SQLite. The **daemon** keeps the latest process connection state **in memory** for realtime CLI / TUI / Web views and flushes **minute-level** process rollups to SQLite for historical process reports (`processes --range`, top APIs).

Rolling summaries include samples by their sample timestamp. With the default 1-second interval, boundary error is at most about one sample interval. Daily buckets are currently grouped by Unix day boundaries.

**Only one collector daemon may run at a time** (PID-file lock + live-PID check + API health). A second `daemon` exits with an error until you `bytepulse stop`. Multiple viewers against one daemon are fine. Default PID path:

```text
~/.bytepulse/bytepulse.pid
```


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

- Interface include/exclude rules.
- CSV and JSON export.
- Optional OS service helpers (`launchd` / systemd) for users who want a long-running collector.
- Minute/hour/day aggregate tables for lower long-term storage usage.
- Linux process connection discovery and traffic attribution (deferred).
- More robust per-process traffic attribution backends.
- Desktop tray or widget integration.

## License

MIT
