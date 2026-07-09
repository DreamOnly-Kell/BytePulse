# BytePulse

[English](README.md)

BytePulse 是一个本地网络流量监控工具，提供 CLI、TUI 和本地 Web 仪表盘。它读取系统网络接口计数器，将流量数据保存到 SQLite，并提供实时网速和滚动时间窗口统计。

## 功能

- 实时显示下载、上传和总速度。
- 自动显示 `B/s`、`KB/s`、`MB/s`、`GB/s`。
- 可通过 `--bits` 使用 bits/s 显示速率。
- 支持滚动窗口流量统计：`1h`、`2h`、`3h`、`5h`、`10h`、`12h`、`24h`、`2d`、`3d`、`7d`、`15d`。
- 支持每小时和每天聚合 API。
- 提供 CLI、TUI 和本地 Web 仪表盘。
- 支持通过 `--interface` 查看指定网卡。
- macOS 支持进程连接发现：进程名、完整进程路径、PID、连接数和最后出现时间。
- 默认在进程视图中隐藏 BytePulse 自身（`--exclude-self`；设为 `false` 可显示）。
- CLI、TUI、Web 的进程视图通过 daemon API 每 1 秒实时刷新。
- macOS 可选通过 `nettop` 显示按进程实时流量。
- 使用本地 SQLite 存储数据。
- 不抓包，只读取操作系统网络计数器。
- macOS 可通过 `--process-traffic nettop` 选择性显示按进程 RX/TX 速率。

## 平台支持

BytePulse 设计目标是多平台支持。当前实现使用 Go 和 `gopsutil` 读取网络接口计数器，因此核心 CLI、存储、TUI 和 Web 仪表盘预期可以运行在 macOS、Linux 和 Windows 上。

| 平台 | 状态 |
| --- | --- |
| macOS | 核心监控已验证；已实现进程连接发现 |
| Linux | 核心监控预期支持；进程连接发现当前禁用 |
| Windows | 核心监控预期支持；进程连接发现当前禁用 |

Phase 2A 的进程监控显示“哪些进程存在网络连接”。macOS 可以选择解析 `nettop` 来估算按进程实时流量；该能力默认关闭，因为 `nettop` 是系统工具而不是稳定 SDK API。Linux 和 Windows 的进程发现会作为后续平台适配实现。

## 构建

为当前平台构建：

```bash
go mod tidy
go build -o bytepulse ./cmd/bytepulse
```

## 发布构建

先创建输出目录：

```bash
mkdir -p dist
```

构建 macOS 可执行文件：

```bash
GOOS=darwin GOARCH=arm64 go build -o dist/bytepulse-darwin-arm64 ./cmd/bytepulse
GOOS=darwin GOARCH=amd64 go build -o dist/bytepulse-darwin-amd64 ./cmd/bytepulse
```

构建 Linux 可执行文件：

```bash
GOOS=linux GOARCH=amd64 go build -o dist/bytepulse-linux-amd64 ./cmd/bytepulse
GOOS=linux GOARCH=arm64 go build -o dist/bytepulse-linux-arm64 ./cmd/bytepulse
```

构建 Windows 可执行文件：

```bash
GOOS=windows GOARCH=amd64 go build -o dist/bytepulse-windows-amd64.exe ./cmd/bytepulse
GOOS=windows GOARCH=arm64 go build -o dist/bytepulse-windows-arm64.exe ./cmd/bytepulse
```

打包发布文件：

```bash
tar -czf dist/bytepulse-darwin-arm64.tar.gz -C dist bytepulse-darwin-arm64
tar -czf dist/bytepulse-darwin-amd64.tar.gz -C dist bytepulse-darwin-amd64
tar -czf dist/bytepulse-linux-amd64.tar.gz -C dist bytepulse-linux-amd64
tar -czf dist/bytepulse-linux-arm64.tar.gz -C dist bytepulse-linux-arm64
zip -j dist/bytepulse-windows-amd64.zip dist/bytepulse-windows-amd64.exe
zip -j dist/bytepulse-windows-arm64.zip dist/bytepulse-windows-arm64.exe
```

## 使用

启动采集器：

```bash
./bytepulse daemon
```

前台运行时可以用 `Ctrl+C` 停止。

后台运行采集器：

```bash
./bytepulse daemon > bytepulse.log 2>&1 &
```

停止后台采集器：

```bash
./bytepulse stop
```

查看当前网速：

```bash
./bytepulse status
```

查看流量报表：

```bash
./bytepulse report --range 24h
```

查看当前联网进程：

```bash
./bytepulse processes
./bytepulse processes --watch
./bytepulse processes --range 24h
```

进程视图同时显示 `NAME` 和 `PATH`。`NAME` 是短进程名；`PATH` 在平台能提供时保留完整进程路径。

启用 macOS 按进程实时流量：

```bash
./bytepulse --process-traffic nettop daemon
./bytepulse processes --watch
```

当流量归因不可用时，进程视图仍显示连接数，`RX/s` 和 `TX/s` 显示为 `--`。

列出网络接口：

```bash
./bytepulse interfaces
```

打开 TUI：

```bash
./bytepulse tui
```

启动 Web 仪表盘：

```bash
./bytepulse web --addr 127.0.0.1:8989
```

然后访问：

```text
http://127.0.0.1:8989
```

## 参数

指定数据库：

```bash
./bytepulse --db ./bytepulse.db daemon
```

查看指定网卡：

```bash
./bytepulse --interface en0 status
./bytepulse --interface en0 report --range 24h
```

使用 bits/s：

```bash
./bytepulse --bits status
```

指定 daemon PID 文件：

```bash
./bytepulse --pid-file ./bytepulse.pid daemon
./bytepulse --pid-file ./bytepulse.pid stop
```

指定 daemon API 地址：

```bash
./bytepulse --daemon-api-addr 127.0.0.1:8988 daemon
./bytepulse --daemon-api-addr 127.0.0.1:8988 processes --watch
```

从进程视图中隐藏 BytePulse 自身（默认开启）。匹配依据为 daemon 自身 PID，以及可执行名 `bytepulse` / `bytepulse.exe`：

```bash
./bytepulse daemon
./bytepulse processes
```

在进程视图中显示 BytePulse 自身（便于调试）：

```bash
./bytepulse --exclude-self=false daemon
./bytepulse --exclude-self=false processes
./bytepulse --exclude-self=false processes --range 24h
```

## 资源占用

在 `htop` 等工具中，`VIRT` 可能明显大于实际内存占用。BytePulse 是 Go 程序，并使用 SQLite；进程可能保留较大的虚拟地址空间，但这不代表实际占用了同等物理内存。判断真实资源占用时，应优先看 `RES` 常驻内存和持续 CPU 占用。

正常空闲状态下，BytePulse 应保持较低 CPU 占用和较小常驻内存。如果 `RES` 或 CPU 在空闲时持续增长，请记录运行命令、平台、Go 版本和 `htop` 数值。

## 数据

默认数据库路径：

```text
~/.bytepulse/bytepulse.db
```

默认保留最近 30 天的采样数据。默认采集间隔是 1 秒，也可以通过 `daemon --interval` 修改。每条网卡采样包含时间戳、网卡名称、接收字节、发送字节、接收速度、发送速度和采样间隔。

进程连接监控同样每 1 秒采样，但不会把每秒原始连接快照写入 SQLite。daemon 在内存中保存最新进程连接状态，供实时视图使用；SQLite 只保存分钟级进程连接聚合，供历史进程报表使用。

滚动统计按采样时间戳归属样本。默认 1 秒间隔下，窗口边界误差最多约为一个采样间隔。每日聚合当前按 Unix 日边界分桶。

同一个数据库建议只运行一个采集 daemon。多个采集器同时写入同一个数据库时，合并网卡的最新视图可能不明确。

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

所有 API 都支持 `?interface=<name>`。例如：

```text
GET /api/realtime?interface=en0
GET /api/summary?range=24h&interface=en0
```

`/api/hourly` 返回最近 24 小时。`/api/daily` 返回最近 15 天。

daemon 本地 API 还提供：

```text
GET /api/health
GET /api/processes?limit=30
GET /api/processes/connections?process_key=<key>
GET /api/processes/top?range=24h&limit=30
```

## 后续计划

- 支持配置文件。
- 支持网卡包含和排除规则。
- 支持 CSV 和 JSON 导出。
- 支持 macOS `launchd` 后台服务安装。
- 增加分钟、小时、天聚合表，降低长期存储占用。
- 增加 Linux 和 Windows 进程连接发现。
- 增加更稳定的按进程流量归因后端。
- 增加桌面托盘或桌面小组件。

## 协议

MIT
