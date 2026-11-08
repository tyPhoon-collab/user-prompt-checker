package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"user-prompt-checker/internal/history"
)

type model struct {
	summary    history.Summary
	cursor     int
	tab        int // 0: Gains, 1: Prompts
	splitMode  int // 0: auto, 1: force-vertical, 2: force-horizontal
	gainSort   int // 0: Gain ↓, 1: Gain ↑, 2: Recent ↓
	promptSort int // 0: Score ↓, 1: Score ↑, 2: Recent ↓
	width      int
	height     int
}

func New(sum history.Summary) tea.Model {
	return model{
		summary:    sum,
		cursor:     0,
		tab:        0,
		splitMode:  0,
		gainSort:   0,
		promptSort: 0,
		width:      80,
		height:     24,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit

		case "tab", "l", "right":
			m.tab = (m.tab + 1) % 2
			m.cursor = 0

		case "shift+tab", "h", "left":
			m.tab = (m.tab + 1) % 2
			m.cursor = 0

		case "s":
			if m.tab == 0 {
				m.gainSort = (m.gainSort + 1) % 3
			} else {
				m.promptSort = (m.promptSort + 1) % 3
			}
			m.cursor = 0

		case "v":
			m.splitMode = (m.splitMode + 1) % 3

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			maxItems := m.itemCount()
			if m.cursor < maxItems-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m model) sortedGains() []history.GainPair {
	res := make([]history.GainPair, len(m.summary.GainPairs))
	copy(res, m.summary.GainPairs)
	switch m.gainSort {
	case 0: // Gain ↓
		sort.Slice(res, func(i, j int) bool { return res[i].Gain > res[j].Gain })
	case 1: // Gain ↑
		sort.Slice(res, func(i, j int) bool { return res[i].Gain < res[j].Gain })
	case 2: // Recent ↓
		sort.Slice(res, func(i, j int) bool { return res[i].After.Timestamp.After(res[j].After.Timestamp) })
	}
	return res
}

func (m model) sortedPrompts() []history.Record {
	src := m.summary.AllPrompts
	if len(src) == 0 {
		src = m.summary.TopPrompts
	}
	res := make([]history.Record, len(src))
	copy(res, src)
	switch m.promptSort {
	case 0: // Score ↓
		sort.Slice(res, func(i, j int) bool { return calcAvg(res[i].Scores) > calcAvg(res[j].Scores) })
	case 1: // Score ↑
		sort.Slice(res, func(i, j int) bool { return calcAvg(res[i].Scores) < calcAvg(res[j].Scores) })
	case 2: // Recent ↓
		sort.Slice(res, func(i, j int) bool { return res[i].Timestamp.After(res[j].Timestamp) })
	}
	return res
}

func (m model) itemCount() int {
	if m.tab == 0 {
		return len(m.summary.GainPairs)
	}
	if len(m.summary.AllPrompts) > 0 {
		return len(m.summary.AllPrompts)
	}
	return len(m.summary.TopPrompts)
}

func (m model) View() string {
	if m.width < 50 || m.height < 15 {
		return "Terminal too small for dashboard."
	}

	// 1. Header & Summary Stats
	headerTitle := titleStyle.Render("PROMPT QUALITY GAIN DASHBOARD")
	passRateStr := fmt.Sprintf("%.1f%%", m.summary.PassRate)
	statsBar := statBoxStyle.Render(fmt.Sprintf(
		"Total: %d  |  Passed: %d  |  Blocked: %d  |  Pass Rate: %s",
		m.summary.TotalPrompts,
		m.summary.PassedCount,
		m.summary.TotalPrompts-m.summary.PassedCount,
		greenText.Render(passRateStr),
	))
	header := lipgloss.JoinHorizontal(lipgloss.Center, headerTitle, " ", statsBar)

	// 2. Tabs with Sort Indicators
	gainSortLabel := []string{"Gain ↓", "Gain ↑", "Recent ↓"}[m.gainSort]
	promptSortLabel := []string{"Score ↓", "Score ↑", "Recent ↓"}[m.promptSort]

	promptCount := len(m.summary.AllPrompts)
	if promptCount == 0 {
		promptCount = len(m.summary.TopPrompts)
	}

	tab0Text := fmt.Sprintf("[1] Prompt Gains (%d) [%s]", len(m.summary.GainPairs), gainSortLabel)
	tab1Text := fmt.Sprintf("[2] Prompts (%d) [%s]", promptCount, promptSortLabel)

	var t0, t1 string
	if m.tab == 0 {
		t0 = activeTabStyle.Render(tab0Text)
		t1 = inactiveTabStyle.Render(tab1Text)
	} else {
		t0 = inactiveTabStyle.Render(tab0Text)
		t1 = activeTabStyle.Render(tab1Text)
	}
	tabs := lipgloss.JoinHorizontal(lipgloss.Left, t0, "  ", t1)

	// 3. Responsive Layout (Vertical if narrow, Horizontal if wide)
	isNarrow := m.width < 110
	if m.splitMode == 1 {
		isNarrow = true
	} else if m.splitMode == 2 {
		isNarrow = false
	}
	var panes string

	if isNarrow {
		topHeight := (m.height - 7) / 2
		if topHeight < 5 {
			topHeight = 5
		}
		bottomHeight := m.height - 7 - topHeight
		if bottomHeight < 6 {
			bottomHeight = 6
		}
		fullWidth := m.width - 2
		if fullWidth < 20 {
			fullWidth = 20
		}

		topList := m.renderLeftList(fullWidth, topHeight)
		bottomDetail := m.renderRightDetail(fullWidth, bottomHeight)

		panes = lipgloss.JoinVertical(lipgloss.Left,
			paneBorder.Width(fullWidth).Height(topHeight).Render(topList),
			paneBorder.Width(fullWidth).Height(bottomHeight).Render(bottomDetail),
		)
	} else {
		contentHeight := m.height - 7
		if contentHeight < 8 {
			contentHeight = 8
		}
		leftWidth := (m.width - 4) / 2
		rightWidth := m.width - leftWidth - 4

		leftList := m.renderLeftList(leftWidth, contentHeight)
		rightDetail := m.renderRightDetail(rightWidth, contentHeight)

		panes = lipgloss.JoinHorizontal(lipgloss.Top,
			paneBorder.Width(leftWidth).Height(contentHeight).Render(leftList),
			paneBorder.Width(rightWidth).Height(contentHeight).Render(rightDetail),
		)
	}

	// 4. Footer
	splitLabel := "auto"
	if m.splitMode == 1 {
		splitLabel = "vertical"
	} else if m.splitMode == 2 {
		splitLabel = "horizontal"
	}
	footer := subtleStyle.Render(fmt.Sprintf(" ↑/k: up  •  ↓/j: down  •  tab: switch tab  •  s: sort  •  v: split (%s)  •  q/esc: quit", splitLabel))

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		tabs,
		panes,
		footer,
	)
}

func (m model) renderLeftList(width, height int) string {
	var lines []string
	if m.tab == 0 {
		gains := m.sortedGains()
		if len(gains) == 0 {
			return subtleStyle.Render("No gain improvement events detected yet.")
		}
		for i, gp := range gains {
			sign := "+"
			if gp.Gain < 0 {
				sign = ""
			}
			gainBadge := fmt.Sprintf("[%s%.3f]", sign, gp.Gain)
			pText := truncateStr(gp.After.Prompt, width-14)
			line := fmt.Sprintf("%s %s", gainBadge, pText)

			if i == m.cursor {
				lines = append(lines, selectedItemStyle.Width(width-2).Render(line))
			} else {
				if gp.Gain > 0 {
					lines = append(lines, fmt.Sprintf("%s %s", greenText.Render(gainBadge), pText))
				} else {
					lines = append(lines, fmt.Sprintf("%s %s", redText.Render(gainBadge), pText))
				}
			}
		}
	} else {
		prompts := m.sortedPrompts()
		if len(prompts) == 0 {
			return subtleStyle.Render("No prompts recorded.")
		}
		for i, p := range prompts {
			avg := calcAvg(p.Scores)
			badge := fmt.Sprintf("★ %.3f", avg)
			if !p.Pass {
				badge = fmt.Sprintf("✖ %.3f", avg)
			}
			pText := truncateStr(p.Prompt, width-12)
			line := fmt.Sprintf("%s %s", badge, pText)

			if i == m.cursor {
				lines = append(lines, selectedItemStyle.Width(width-2).Render(line))
			} else {
				if p.Pass {
					lines = append(lines, fmt.Sprintf("%s %s", yellowText.Render(badge), pText))
				} else {
					lines = append(lines, fmt.Sprintf("%s %s", redText.Render(badge), pText))
				}
			}
		}
	}

	// Simple scrolling window
	start := 0
	if m.cursor >= height {
		start = m.cursor - height + 1
	}
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		start = 0
	}

	return strings.Join(lines[start:end], "\n")
}

func (m model) renderRightDetail(width, height int) string {
	if m.tab == 0 {
		gains := m.sortedGains()
		if len(gains) == 0 || m.cursor >= len(gains) {
			return subtleStyle.Render("No detail available.")
		}
		gp := gains[m.cursor]
		var b strings.Builder

		b.WriteString(blueText.Render(fmt.Sprintf("GAIN IMPROVEMENT: %+.3f", gp.Gain)) + "\n\n")

		b.WriteString(redText.Render("[-] Before (Blocked)") + "\n")
		b.WriteString(lipgloss.NewStyle().Width(width-4).Render("Prompt: "+gp.Before.Prompt) + "\n")
		b.WriteString("Avg Score: " + fmt.Sprintf("%.3f", calcAvg(gp.Before.Scores)) + "\n")
		if len(gp.Before.Clarifications) > 0 {
			b.WriteString(subtleStyle.Render("Clarifications:") + "\n")
			for _, c := range gp.Before.Clarifications {
				b.WriteString("  • " + c + "\n")
			}
		}

		b.WriteString("\n" + greenText.Render("[+] After (Passed)") + "\n")
		b.WriteString(lipgloss.NewStyle().Width(width-4).Render("Prompt: "+gp.After.Prompt) + "\n")
		b.WriteString("Avg Score: " + fmt.Sprintf("%.3f", calcAvg(gp.After.Scores)) + "\n")
		b.WriteString(fmt.Sprintf("Latency: %.1fms\n\n", gp.After.ElapsedMS))

		b.WriteString(subtleStyle.Render("Score Breakdown (After):") + "\n")
		renderScoresBlock(&b, gp.After.Scores)

		renderContextSection(&b, gp.After, width)

		return b.String()
	}

	prompts := m.sortedPrompts()
	if len(prompts) == 0 || m.cursor >= len(prompts) {
		return subtleStyle.Render("No detail available.")
	}
	tp := prompts[m.cursor]
	var b strings.Builder

	statusBadge := greenText.Render("[PASSED]")
	if !tp.Pass {
		statusBadge = redText.Render("[BLOCKED]")
	}
	b.WriteString(blueText.Render(fmt.Sprintf("PROMPT DETAIL (Avg: %.3f) ", calcAvg(tp.Scores))) + statusBadge + "\n\n")

	promptBox := lipgloss.NewStyle().Width(width - 4).Render("Prompt:\n" + tp.Prompt)
	b.WriteString(promptBox + "\n\n")

	b.WriteString(fmt.Sprintf("Timestamp : %s\n", tp.Timestamp.Local().Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("Session   : %s\n", tp.SessionID))
	b.WriteString(fmt.Sprintf("Agent     : %s\n", tp.Agent))
	b.WriteString(fmt.Sprintf("Latency   : %.1fms\n\n", tp.ElapsedMS))

	if len(tp.Clarifications) > 0 {
		b.WriteString(subtleStyle.Render("Clarifications:") + "\n")
		for _, c := range tp.Clarifications {
			b.WriteString("  • " + c + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(subtleStyle.Render("Scores:") + "\n")
	renderScoresBlock(&b, tp.Scores)

	renderContextSection(&b, tp, width)

	return b.String()
}

func renderScoresBlock(b *strings.Builder, scores map[string]float64) {
	keys := make([]string, 0, len(scores))
	for k := range scores {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("  %-20s : %.3f\n", k, scores[k]))
	}
}

func renderContextSection(b *strings.Builder, rec history.Record, width int) {
	ctx, err := LoadContext(rec)
	if err != nil || len(ctx) == 0 {
		return
	}
	b.WriteString("\n" + subtleStyle.Render("Conversation Context (Prior Turns):") + "\n")
	maxContent := width - 14
	if maxContent < 15 {
		maxContent = 15
	}
	for _, m := range ctx {
		roleLabel := "[" + strings.ToUpper(m.Role) + "]"
		if m.Role == "user" {
			roleLabel = greenText.Render(roleLabel)
		} else {
			roleLabel = blueText.Render(roleLabel)
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", roleLabel, truncateStr(m.Content, maxContent)))
	}
}

func calcAvg(scores map[string]float64) float64 {
	if len(scores) == 0 {
		return 0
	}
	sum := 0.0
	for _, s := range scores {
		sum += s
	}
	return math.Round((sum/float64(len(scores)))*1000) / 1000
}

func truncateStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > maxLen {
		if maxLen > 3 {
			return string(runes[:maxLen-3]) + "..."
		}
		return string(runes[:maxLen])
	}
	return s
}

func Run(sum history.Summary) error {
	p := tea.NewProgram(New(sum), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
