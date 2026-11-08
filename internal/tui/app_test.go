package tui_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"user-prompt-checker/internal/history"
	"user-prompt-checker/internal/tui"
)

func TestTUIModelNavigation(t *testing.T) {
	sum := history.Summary{
		TotalPrompts: 2,
		PassedCount:  1,
		PassRate:     50.0,
		GainPairs: []history.GainPair{
			{
				Before: history.Record{Prompt: "bad", Scores: map[string]float64{"a": 0.2}},
				After:  history.Record{Prompt: "good", Scores: map[string]float64{"a": 0.8}},
				Gain:   0.6,
			},
		},
		TopPrompts: []history.Record{
			{Prompt: "top prompt", Scores: map[string]float64{"a": 0.95}, Timestamp: time.Now()},
		},
	}

	m := tui.New(sum)

	// Window resize
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	view := m.View()
	if !strings.Contains(view, "PROMPT QUALITY GAIN DASHBOARD") {
		t.Fatalf("expected title in view:\n%s", view)
	}
	if !strings.Contains(view, "50.0%") {
		t.Fatalf("expected pass rate in view:\n%s", view)
	}

	// Tab switch
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	view = m.View()
	if !strings.Contains(view, "TOP PROMPT") {
		t.Fatalf("expected top prompt view after tab switch:\n%s", view)
	}

	// Narrow window resize (vertical layout test)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 75, Height: 25})
	viewNarrow := m.View()
	if !strings.Contains(viewNarrow, "TOP PROMPT") {
		t.Fatalf("expected top prompt view in narrow mode:\n%s", viewNarrow)
	}

	// Quit
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatalf("expected quit command on 'q'")
	}
}
