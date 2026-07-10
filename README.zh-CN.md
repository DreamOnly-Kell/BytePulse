# BytePulse

[English](README.md)

BytePulse 是**本地、按需使用的网络排障工具**——偶尔打开看「谁在占带宽」即可，不是必须 7×24 常驻的产品。

它读取操作系统网卡计数器（不抓包），把历史写入 SQLite，并通过 **CLI**、**TUI** 和本地 **Web** 展示实时网速与滚动窗口流量。在支持的平台上还可显示联网进程，以及可选的每进程 RX/TX 速率。

机器上只需一个 **`daemon`**：它是共享的采集器与进程状态（内存）生产者。需要看进程时再启动，用完可 `stop`。**同一时间只允许一个 daemon**——再启第二个会被拒绝，需先 `stop` 再启。TUI / Web / CLI **不会**自动拉起 daemon：未启动时等待或给出明确的启动提示。

## 功能

- 实时显示下载、上传和总速度。
- 自动单位 `B/s`、`KB/s`、`MB/s`、`GB/s`（可用 `--bits` 显示 bits/s）。
- 滚动窗口流量统计：`1h` … `15d`，以及每小时/每天聚合 API。
- CLI、TUI、本地 Web（进程视图共用 daemon API）。
- **单实例 daemon：** PID 文件排他锁与 daemon 实例身份；第二次启动会被拦住，并提示先 stop 再 start。
- 通过 `--interface` 过滤指定网卡。
- 进程连接发现：进程名、完整路径、PID、连接数、最后出现时间。
- 可选每进程 RX/TX：macOS `nettop`，Windows TCP ESTATS（`--process-traffic auto`）。
- 默认在进程视图中隐藏 BytePulse 自身（`--exclude-self`）。
- 本地 SQLite；可选 YAML 配置；界面语言 `en` / `zh`（**日志始终英文**）。
- 不抓包，只读系统计数器与平台 API。

## 平台支持

| 平台 | 网卡速度与历史 | 进程连接 | 每进程 RX/TX |
| --- | --- | --- | --- |
| **macOS** | 支持 | 支持 | 支持（`nettop` / `auto`） |
| **Windows 10+** | 支持 | 支持 | 支持，仅 TCP（`estats` / `auto`；无 UDP 字节速率） |
| **Linux** | 支持 | 暂无 | 暂无 |

- **核心**（网卡、SQLite、CLI / TUI / Web 网卡数据）基于 Go + `gopsutil`，预期可在 macOS、Linux、Windows 运行。
- **进程监控**已实现 macOS 与 Windows。Linux 进程发现/流量**本期不做**（延后）。
- 每进程 RX/TX **默认关闭**（`--process-traffic off`）。用 `auto`，或平台专用的 `nettop` / `estats` 开启。
- 无法归因时仍显示连接数，`RX/s` / `TX/s` 为 `--`。

## 设计说明（明确不做 / 不做默认）

- **默认不是 7×24 后台服务。** daemon 是可选的：排障时启动，结束后 `stop`。`launchd` / systemd 等安装方式是后续可选项，不是日常用法的前提。
- **只允许一个采集 daemon（强制）。** PID 文件已被排他锁定，或配置的 daemon API 已能响应 `/api/health` 时，第二次 `daemon` 会被拒绝。未锁定的陈旧 PID 文件会被安全替换，即使旧 PID 已被其他进程复用。多个查看端（TUI、Web、CLI）共用一个 daemon 是正常用法。
- **停止前校验身份。** PID 文件保存随机 daemon 实例 ID。只有 `/api/health` 返回的 PID 和实例 ID 都与 PID 文件一致时，`bytepulse stop` 才会发送停止信号，避免陈旧 PID 文件误停其他进程。
- **TUI/Web 不会自动启动 daemon。** daemon 未就绪时：TUI 流量页和 Web 网卡图仍可读取 SQLite，进程视图等待 daemon；CLI 进程命令报错并提示如何启动。
- **不抓包**，也**不依赖常驻内核代理**——只用系统计数器与平台 API。
- **Linux 进程监控**不在当前阶段范围内。

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

### 典型排障流程

```bash
# 1）启动共享采集器（同一时间只允许一个）
./bytepulse --process-traffic auto daemon

# 2）另开终端打开查看端
./bytepulse tui
# 或
./bytepulse web --addr 127.0.0.1:8989
# 或
./bytepulse processes --watch

# 3）用完结束
./bytepulse stop
```

前台 daemon 用 `Ctrl+C` 停止。后台示例：

```bash
./bytepulse daemon > bytepulse.log 2>&1 &
./bytepulse stop
```

已有 daemon 在跑时，再执行一次 `daemon` 会**立刻失败**（不会顶替已有进程）。请先停掉全部采集器，再启新的：

```bash
./bytepulse stop
./bytepulse daemon
```

### 常用命令

查看当前网卡速度 / 滚动报表（读 SQLite；已有历史时不一定要 daemon）：

```bash
./bytepulse status
./bytepulse report --range 24h
./bytepulse interfaces
```

查看当前联网进程（**需要 daemon 在运行**）：

```bash
./bytepulse processes
./bytepulse processes --watch
./bytepulse processes --range 24h
```

进程视图同时显示 `NAME` 与 `PATH`。`NAME` 为短名；`PATH` 在平台能提供时保留完整路径。

在 daemon 上启用每进程实时流量：

```bash
# macOS
./bytepulse --process-traffic nettop daemon
# 或：--process-traffic auto

# Windows 10+
bytepulse.exe --process-traffic auto daemon
# 或：--process-traffic estats
```

Windows 速率来自 TCP ESTATS（尽力而为；部分连接需短暂预热）。该 API **不含** UDP 字节速率。

TUI / Web：

```bash
./bytepulse tui
./bytepulse web --addr 127.0.0.1:8989
# → http://127.0.0.1:8989
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

指定 daemon PID 文件（单实例锁加在该文件上；默认 `~/.bytepulse/bytepulse.pid`）：

```bash
./bytepulse --pid-file ./bytepulse.pid daemon
./bytepulse --pid-file ./bytepulse.pid stop
```

指定 daemon API 地址（默认 `127.0.0.1:8988`；API 已健康时也会拦截第二个 daemon）：

```bash
./bytepulse --daemon-api-addr 127.0.0.1:8988 daemon
./bytepulse --daemon-api-addr 127.0.0.1:8988 processes --watch
```

日志（默认级别 `error`；排错时提高）。业务表格仍走 stdout；日志走 stderr 或文件：

```bash
./bytepulse --log-level info daemon
./bytepulse --log-level debug --log-file ~/.bytepulse/bytepulse.log daemon
./bytepulse --log-level info --log-format json daemon
```

可选 YAML 配置。复制带注释的示例再改：

```bash
mkdir -p ~/.bytepulse
cp config.example.yaml ~/.bytepulse/config.yaml
# 编辑 ~/.bytepulse/config.yaml
```

- 默认路径（文件存在时自动加载）：`~/.bytepulse/config.yaml`
- 或：`--config /path/to.yaml`
- 优先级：内置默认值 &lt; 配置文件 &lt; 命令行 flag
- 完整注释示例：[`config.example.yaml`](config.example.yaml)

```bash
./bytepulse --config ./my.yaml daemon
```

界面语言（`lang` / `--lang`）：`en`（默认）或 `zh`，影响 TUI、Web 文案与 CLI 用户提示。**日志始终为英文。** 命令 `--help` 在程序内中英双语写死，不随 `lang` 切换。

```bash
./bytepulse --lang zh tui
```

**daemon 未启动时：** TUI 流量页和 Web 网卡图仍可读取 SQLite；进程视图显示等待/重试状态。CLI `processes` 报错并给出启动命令。查看端**不会**自动拉起 daemon。

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

进程连接监控同样每 1 秒采样，但不会把每秒原始连接快照写入 SQLite。**daemon 在内存中**保存最新进程连接状态，供 CLI / TUI / Web 实时视图使用；SQLite 只写入**分钟级**进程聚合，供历史报表（`processes --range`、top API）使用。正常退出时也会刷写当前尚未结束的分钟。数据保留清理在启动时执行一次，之后最多每小时执行一次，不再每秒执行。

滚动统计按采样时间戳归属样本。默认 1 秒间隔下，窗口边界误差最多约为一个采样间隔。每日聚合当前按 Unix 日边界分桶。

对于同一组 PID 文件/API 配置，**同一时间只允许一个采集 daemon**（PID 文件锁 + API 健康检查）。PID 文件还保存随机实例 ID，供 `bytepulse stop` 校验目标身份。再启第二个会直接报错，需先停止已有 daemon。一个 daemon 上挂多个查看端是正常用法。默认 PID 路径：

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

`/api/health` 会返回 daemon 身份，以及每进程流量后端状态：`disabled`、`starting`、`healthy` 或 `degraded`。

## 后续计划

- 网卡包含 / 排除规则。
- CSV 和 JSON 导出。
- 可选的系统服务安装（`launchd` / systemd），给希望长期采集的用户。
- 分钟 / 小时 / 天聚合表，降低长期存储占用。
- Linux 进程连接发现与流量归因（延后）。
- 更稳妥的按进程流量归因后端。
- 桌面托盘或小组件。

## 协议

MIT
