// Package tui implements the Bubbletea terminal dashboard.
// tui 包实现 Bubbletea 终端看板。
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bytepulse/internal/config"
	"bytepulse/internal/daemonclient"
	"bytepulse/internal/i18n"
	"bytepulse/internal/processstate"
	"bytepulse/internal/storage"
	"bytepulse/internal/units"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// model is the Bubbletea application state refreshed once per second.
// model 是每秒刷新一次的 Bubbletea 应用状态。
type model struct {
	// store provides SQLite reads for interface stats.
	// store 提供网卡统计的 SQLite 读取。
	store *storage.Store
	// cfg holds interface filter, bits mode, TopN, daemon API addr.
	// cfg 持有网卡过滤、bits 模式、TopN、daemon API 地址。
	cfg config.Config
	// width / height track the terminal size for layout.
	// width / height 记录终端尺寸用于布局。
	width  int
	height int
	// latest is the most recent aggregated interface sample.
	// latest 是最近一次聚合网卡样本。
	latest storage.Sample
	// ranges holds preconfigured traffic window summaries.
	// ranges 保存预配置时间窗口的流量汇总。
	ranges []rangeStat
	// series is the last ~60s of samples for the sparkline.
	// series 是 sparkline 用的约最近 60 秒样本。
	series []storage.Sample
	// err is the last interface data error (empty DB, etc.).
	// err 是最近一次网卡数据错误（空库等）。
	err error
	// loaded becomes true after the first successful interface fetch.
	// loaded 在首次成功拉取网卡数据后为 true。
	loaded bool
	// processClient talks to the daemon API for process rows.
	// processClient 通过 daemon API 获取进程行。
	processClient *daemonclient.Client
	// procs is the latest realtime process list.
	// procs 是最新实时进程列表。
	procs []processstate.ProcessConnectionSummary
	// showProc toggles interface view vs process view (Tab).
	// showProc 在网卡视图与进程视图间切换（Tab）。
	showProc bool
	// processErr is set when the daemon API is unreachable.
	// processErr 在 daemon API 不可达时设置。
	processErr error
	// daemonOK is true after a successful /api/health check.
	// daemonOK 在 /api/health 成功后为 true。
	daemonOK bool
	// waitTicks counts seconds spent waiting for the daemon.
	// waitTicks 统计等待 daemon 的秒数。
	waitTicks int
}

// rangeStat pairs a range label with its SummaryResult.
// rangeStat 将范围标签与 SummaryResult 配对。
type rangeStat struct {
	Label   string
	Summary storage.SummaryResult
}

// tickMsg is delivered every second to trigger refresh().
// tickMsg 每秒投递一次以触发 refresh()。
type tickMsg time.Time

// Lipgloss styles for title, labels, values, and errors.
// 标题、标签、数值与错误的 Lipgloss 样式。
var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle = lipgloss.NewStyle().Bold(true)
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

// Run starts the full-screen TUI until the user quits.
// Run 启动全屏 TUI，直到用户退出。
func Run(store *storage.Store, cfg config.Config) error {
	// Alt screen avoids scrolling the user's prior terminal content.
	// Alt screen 避免滚动用户原先的终端内容。
	p := tea.NewProgram(newModel(store, cfg), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// newModel initializes static range labels and the daemon client.
// newModel 初始化静态范围标签与 daemon 客户端。
func newModel(store *storage.Store, cfg config.Config) model {
	return model{
		store:         store,
		cfg:           cfg,
		processClient: daemonclient.New(cfg.DaemonAPIAddr),
		// Fixed set of windows shown on the main dashboard.
		// 主看板展示的固定时间窗口集合。
		ranges: []rangeStat{
			{Label: "1h"},
			{Label: "2h"},
			{Label: "3h"},
			{Label: "5h"},
			{Label: "10h"},
			{Label: "12h"},
			{Label: "24h"},
			{Label: "2d"},
			{Label: "3d"},
			{Label: "7d"},
			{Label: "15d"},
		},
	}
}

// Init runs an immediate refresh, then schedules one-second ticks.
// Without the immediate tick, the first frame always showed the wait screen
// even when the daemon was already up (daemonOK starts false).
// Init 立即刷新一次，再调度每秒 tick。
// 若只有延迟 tick，首帧会因 daemonOK 默认为 false 而误显示等待屏。
func (m model) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return tickMsg(time.Now()) },
		tick(),
	)
}

// Update handles keys, resize, and periodic refresh ticks.
// Update 处理按键、尺寸变化与周期刷新 tick。
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		// Quit keys.
		// 退出键。
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		// Toggle process vs traffic view.
		// 在进程视图与流量视图间切换。
		case "tab":
			m.showProc = !m.showProc
		}
	case tea.WindowSizeMsg:
		// Store dimensions for path column width / bar width.
		// 保存尺寸用于路径列宽 / 条形图宽度。
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		// Reload all dashboard data, then schedule the next tick.
		// 重新加载看板全部数据，再调度下一 tick。
		m.refresh()
		return m, tick()
	}
	return m, nil
}

// refresh loads interface stats from SQLite and processes from the daemon API.
// refresh 从 SQLite 加载网卡统计，从 daemon API 加载进程。
func (m *model) refresh() {
	// Probe daemon health every tick until it is up (then keep probing lightly).
	// 每拍探测 daemon health，起来之后仍会每拍检查。
	if m.processClient != nil {
		if err := m.processClient.Health(context.Background()); err != nil {
			m.daemonOK = false
			m.processErr = err
			m.procs = nil
			m.waitTicks++
		} else {
			if !m.daemonOK {
				m.waitTicks = 0
			}
			m.daemonOK = true
			m.processErr = nil
		}
	}

	// While daemon is down, stay on the wait screen (still allow quit).
	// daemon 未启动时停留在等待屏（仍可退出）。
	if !m.daemonOK {
		return
	}

	// Clear previous interface error each tick.
	// 每拍清除上一次网卡错误。
	m.err = nil
	latest, err := m.store.LatestAggregateSample(m.cfg.Interface)
	if err != nil {
		// Stop early; process refresh is skipped when interface data fails.
		// 提前返回；网卡数据失败时跳过进程刷新。
		m.err = err
		return
	}
	m.latest = latest
	m.loaded = true

	now := time.Now()
	// Fill each traffic window summary from SQLite.
	// 从 SQLite 填充每个流量窗口汇总。
	for i := range m.ranges {
		d, err := config.ParseRange(m.ranges[i].Label)
		if err != nil {
			m.err = err
			return
		}
		summary, err := m.store.Summary(now.Add(-d), now, m.cfg.Interface)
		if err != nil {
			m.err = err
			return
		}
		m.ranges[i].Summary = summary
	}

	// Sparkline uses roughly the last minute of per-second samples.
	// Sparkline 使用大约最近一分钟的每秒样本。
	series, err := m.store.RecentSeries(now.Add(-60*time.Second), now, m.cfg.Interface)
	if err != nil {
		m.err = err
		return
	}
	m.series = series

	// Process list from daemon API.
	// 从 daemon API 拉进程列表。
	if m.processClient != nil {
		procs, err := m.processClient.Processes(context.Background(), m.cfg.TopN)
		if err != nil {
			m.procs = nil
			m.processErr = err
			m.daemonOK = false
		} else {
			m.procs = procs
			m.processErr = nil
		}
	}
}

// View renders either the process table or the main traffic dashboard.
// View 渲染进程表或主流量看板。
func (m model) View() string {
	// Full-screen wait until background collector is healthy.
	// 后台采集就绪前全屏等待。
	if !m.daemonOK {
		return m.waitDaemonView()
	}

	if m.showProc {
		return m.processView()
	}

	var b strings.Builder
	// Title + current interface filter label.
	// 标题 + 当前网卡过滤标签。
	b.WriteString(titleStyle.Render(i18n.T("tui.wait.title")))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render(config.InterfaceLabel(m.cfg.Interface)))
	b.WriteString("\n\n")

	// Empty-database guidance (daemon is up but no samples yet).
	// 空库引导（daemon 已起但尚无样本）。
	if m.err != nil {
		b.WriteString(errorStyle.Render(i18n.Tf("tui.main.no_samples", map[string]string{"err": m.err.Error()})))
		b.WriteString("\n\n")
		b.WriteString(i18n.T("tui.main.wait_samples"))
		b.WriteString("\n\nq: quit\n")
		return b.String()
	}
	// Before the first successful tick completes.
	// 首次成功 tick 完成前。
	if !m.loaded {
		b.WriteString(i18n.T("tui.main.loading"))
		b.WriteString("\n")
		return b.String()
	}

	// Live rates, sparkline, multi-window totals, help footer.
	// 实时速率、sparkline、多窗口总量、帮助页脚。
	b.WriteString(renderRates(m.latest, m.cfg.UseBits))
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render(i18n.T("tui.main.last60")))
	b.WriteString("\n")
	b.WriteString(renderBars(m.series, max(20, m.width-4)))
	b.WriteString("\n\n")
	b.WriteString(renderRanges(m.ranges, m.cfg.UseBits))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render(i18n.T("tui.main.footer")))
	b.WriteString("\n")
	return b.String()
}

// waitDaemonView asks the user to start the background collector and keeps polling.
// waitDaemonView 提示用户启动后台采集，并持续轮询。
func (m model) waitDaemonView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(i18n.T("tui.wait.title")))
	b.WriteString("\n\n")
	b.WriteString(errorStyle.Render(i18n.T("tui.wait.headline")))
	b.WriteString("\n\n")
	b.WriteString(i18n.T("tui.wait.need_api"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  %s\n\n", m.cfg.DaemonAPIAddr))
	b.WriteString(i18n.T("tui.wait.start_other"))
	b.WriteString("\n")
	b.WriteString(valueStyle.Render("  " + config.DaemonStartHint(m.cfg)))
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render(i18n.Tf("tui.wait.waiting", map[string]string{
		"seconds": fmt.Sprintf("%d", m.waitTicks),
	})))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render(i18n.T("tui.wait.footer")))
	b.WriteString("\n")
	return b.String()
}

// processView renders the Tab process connection table.
// processView 渲染 Tab 进程连接表。
func (m model) processView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(i18n.T("tui.proc.title")))
	b.WriteString("\n\n")
	// Should be rare: health passed then processes failed mid-session.
	// 少见：health 通过后 processes 中途失败。
	if m.processErr != nil {
		b.WriteString(errorStyle.Render(i18n.T("tui.proc.api_error")))
		b.WriteString("\n")
		b.WriteString(labelStyle.Render(i18n.Tf("tui.proc.start", map[string]string{"cmd": config.DaemonStartHint(m.cfg)})))
		b.WriteString("\n\n")
		b.WriteString(labelStyle.Render(i18n.T("tui.main.footer")))
		b.WriteString("\n")
		return b.String()
	}
	// No rows yet (sampler still warming up or unsupported platform).
	// 尚无行（采样器预热中或不支持的平台）。
	if len(m.procs) == 0 {
		b.WriteString(i18n.T("tui.proc.waiting_data"))
		b.WriteString("\n\n")
		b.WriteString(labelStyle.Render(i18n.T("tui.main.footer")))
		b.WriteString("\n")
		return b.String()
	}
	// PATH column grows with terminal width.
	// PATH 列随终端宽度增长。
	pathWidth := max(16, m.width-74)
	// Keep fixed English column keys for width; labels via i18n header string.
	// 列宽仍按固定格式；表头整行走 i18n。
	b.WriteString(i18n.T("tui.proc.header"))
	b.WriteString("\n")
	for _, item := range m.procs {
		b.WriteString(fmt.Sprintf("%-7d %-16s %-6d %-11s %-11s %-8s %s\n",
			item.PID,
			truncate(item.ProcessName, 16),
			item.ConnectionCount,
			// Optional rates from nettop when enabled.
			// 启用 nettop 时的可选速率。
			truncate(formatOptionalRate(item.RXBps, item.TrafficAvailable, m.cfg.UseBits), 11),
			truncate(formatOptionalRate(item.TXBps, item.TrafficAvailable, m.cfg.UseBits), 11),
			item.LastSeen.Local().Format("15:04:05"),
			truncate(displayPath(item.ProcessPath, item.ProcessName), pathWidth),
		))
	}
	b.WriteString("\n")
	b.WriteString(labelStyle.Render(i18n.T("tui.main.footer")))
	b.WriteString("\n")
	return b.String()
}

// displayPath prefers full path over short name.
// displayPath 优先完整路径而非短名。
func displayPath(path, fallback string) string {
	if path != "" {
		return path
	}
	return fallback
}

// formatOptionalRate shows "--" when traffic attribution is unavailable.
// formatOptionalRate 在流量归因不可用时显示 "--"。
func formatOptionalRate(rate float64, ok bool, bits bool) string {
	if !ok {
		return "--"
	}
	return units.FormatRate(rate, bits)
}

// renderRates formats download/upload/total/updated lines.
// renderRates 格式化下载/上传/总计/更新时间行。
func renderRates(sample storage.Sample, bits bool) string {
	rows := []string{
		fmt.Sprintf("%-10s %s", i18n.T("tui.rate.download"), valueStyle.Render(units.FormatRate(sample.RXSpeedBps, bits))),
		fmt.Sprintf("%-10s %s", i18n.T("tui.rate.upload"), valueStyle.Render(units.FormatRate(sample.TXSpeedBps, bits))),
		fmt.Sprintf("%-10s %s", i18n.T("tui.rate.total"), valueStyle.Render(units.FormatRate(sample.RXSpeedBps+sample.TXSpeedBps, bits))),
		fmt.Sprintf("%-10s %s", i18n.T("tui.rate.updated"), sample.Timestamp.Local().Format("15:04:05")),
	}
	return strings.Join(rows, "\n")
}

// renderRanges prints multi-window traffic totals and average rates.
// renderRanges 打印多窗口流量总量与平均速率。
func renderRanges(stats []rangeStat, bits bool) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render(i18n.T("tui.main.windows")))
	b.WriteString("\n")
	for _, stat := range stats {
		total := stat.Summary.RXBytes + stat.Summary.TXBytes
		// Keep short en/zh-neutral tokens down/up/total/avg for column scanability.
		// 保留 down/up/total/avg 短词便于扫列（中英界面共用）。
		b.WriteString(fmt.Sprintf(
			"%-4s ↓ %-10s ↑ %-10s Σ %-10s avg %s\n",
			stat.Label,
			units.FormatBytes(stat.Summary.RXBytes),
			units.FormatBytes(stat.Summary.TXBytes),
			units.FormatBytes(total),
			units.FormatRate(stat.Summary.AvgTotalBps(), bits),
		))
	}
	return b.String()
}

// renderBars draws a unicode sparkline of total speed over recent samples.
// renderBars 用 unicode sparkline 绘制近期样本的总速率。
func renderBars(series []storage.Sample, width int) string {
	if len(series) == 0 {
		return "(no recent samples)"
	}
	// Cap visual width so ultra-wide terminals stay readable.
	// 限制视觉宽度，超宽终端仍可读。
	if width > 80 {
		width = 80
	}
	// Keep only the rightmost `width` samples (most recent).
	// 只保留最右侧 `width` 个样本（最近）。
	if len(series) > width {
		series = series[len(series)-width:]
	}

	// Find peak total speed for normalization.
	// 找峰值总速率用于归一化。
	var peak float64
	values := make([]float64, len(series))
	for i, sample := range series {
		values[i] = sample.RXSpeedBps + sample.TXSpeedBps
		if values[i] > peak {
			peak = values[i]
		}
	}
	// All zeros → flat low bar.
	// 全零 → 平坦低条。
	if peak <= 0 {
		return strings.Repeat("▁", len(values))
	}

	// Block elements from low to high.
	// 从低到高的方块字符。
	levels := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, v := range values {
		// Map value into [0, len(levels)-1].
		// 将数值映射到 [0, len(levels)-1]。
		idx := int((v / peak) * float64(len(levels)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(levels) {
			idx = len(levels) - 1
		}
		b.WriteRune(levels[idx])
	}
	return b.String()
}

// tick returns a Cmd that fires tickMsg after one second.
// tick 返回一秒后触发 tickMsg 的 Cmd。
func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// max returns the larger of two ints.
// max 返回两个 int 中较大者。
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// truncate shortens text to width with an ellipsis when needed.
// truncate 在需要时用省略号将文本截到 width。
// Note: byte-based, not rune-based.
// 注意：按字节而非 rune。
func truncate(text string, width int) string {
	if len(text) <= width {
		return text
	}
	if width <= 3 {
		return text[:width]
	}
	return text[:width-3] + "..."
}
