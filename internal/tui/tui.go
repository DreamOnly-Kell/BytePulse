package tui

import (
	"fmt"
	"strings"
	"time"

	"bytepulse/internal/config"
	"bytepulse/internal/storage"
	"bytepulse/internal/units"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	store  *storage.Store
	cfg    config.Config
	width  int
	height int
	latest storage.Sample
	ranges []rangeStat
	series []storage.Sample
	err    error
	loaded bool
}

type rangeStat struct {
	Label   string
	Summary storage.SummaryResult
}

type tickMsg time.Time

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle = lipgloss.NewStyle().Bold(true)
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

func Run(store *storage.Store, cfg config.Config) error {
	p := tea.NewProgram(newModel(store, cfg), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel(store *storage.Store, cfg config.Config) model {
	return model{
		store: store,
		cfg:   cfg,
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

func (m model) Init() tea.Cmd {
	return tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		m.refresh()
		return m, tick()
	}
	return m, nil
}

func (m *model) refresh() {
	m.err = nil
	latest, err := m.store.LatestAggregateSample(m.cfg.Interface)
	if err != nil {
		m.err = err
		return
	}
	m.latest = latest
	m.loaded = true

	now := time.Now()
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

	series, err := m.store.RecentSeries(now.Add(-60*time.Second), now, m.cfg.Interface)
	if err != nil {
		m.err = err
		return
	}
	m.series = series
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("BytePulse"))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render(config.InterfaceLabel(m.cfg.Interface)))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("No data: %v", m.err)))
		b.WriteString("\n\nStart collection in another terminal with: bytepulse daemon\n")
		b.WriteString("\nq: quit\n")
		return b.String()
	}
	if !m.loaded {
		b.WriteString("Loading...\n")
		return b.String()
	}

	b.WriteString(renderRates(m.latest, m.cfg.UseBits))
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render("Last 60 seconds"))
	b.WriteString("\n")
	b.WriteString(renderBars(m.series, max(20, m.width-4)))
	b.WriteString("\n\n")
	b.WriteString(renderRanges(m.ranges, m.cfg.UseBits))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("q: quit"))
	b.WriteString("\n")
	return b.String()
}

func renderRates(sample storage.Sample, bits bool) string {
	rows := []string{
		fmt.Sprintf("%-10s %s", "Download", valueStyle.Render(units.FormatRate(sample.RXSpeedBps, bits))),
		fmt.Sprintf("%-10s %s", "Upload", valueStyle.Render(units.FormatRate(sample.TXSpeedBps, bits))),
		fmt.Sprintf("%-10s %s", "Total", valueStyle.Render(units.FormatRate(sample.RXSpeedBps+sample.TXSpeedBps, bits))),
		fmt.Sprintf("%-10s %s", "Updated", sample.Timestamp.Local().Format("15:04:05")),
	}
	return strings.Join(rows, "\n")
}

func renderRanges(stats []rangeStat, bits bool) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render("Traffic windows"))
	b.WriteString("\n")
	for _, stat := range stats {
		total := stat.Summary.RXBytes + stat.Summary.TXBytes
		b.WriteString(fmt.Sprintf(
			"%-4s down %-10s up %-10s total %-10s avg %s\n",
			stat.Label,
			units.FormatBytes(stat.Summary.RXBytes),
			units.FormatBytes(stat.Summary.TXBytes),
			units.FormatBytes(total),
			units.FormatRate(stat.Summary.AvgTotalBps(), bits),
		))
	}
	return b.String()
}

func renderBars(series []storage.Sample, width int) string {
	if len(series) == 0 {
		return "(no recent samples)"
	}
	if width > 80 {
		width = 80
	}
	if len(series) > width {
		series = series[len(series)-width:]
	}

	var peak float64
	values := make([]float64, len(series))
	for i, sample := range series {
		values[i] = sample.RXSpeedBps + sample.TXSpeedBps
		if values[i] > peak {
			peak = values[i]
		}
	}
	if peak <= 0 {
		return strings.Repeat("▁", len(values))
	}

	levels := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, v := range values {
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

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
