package tui_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"user-prompt-checker/internal/history"
	"user-prompt-checker/internal/tui"
)

func TestTUIFrameFitsTerminalHeight(t *testing.T) {
	for _, size := range [][2]int{{50, 15}, {80, 24}, {100, 50}, {110, 25}, {120, 30}} {
		for splitMode := 0; splitMode < 3; splitMode++ {
			t.Run(fmt.Sprintf("%dx%d/split%d", size[0], size[1], splitMode), func(t *testing.T) {
				m := tui.New(history.Summary{})
				m, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				for i := 0; i < splitMode; i++ {
					m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
				}
				if height := lipgloss.Height(m.View()); height > size[1] {
					t.Fatalf("frame height %d exceeds terminal height %d", height, size[1])
				}
			})
		}
	}
}

func TestTUIDetailScrollStaysInsidePane(t *testing.T) {
	sum := history.Summary{TopPrompts: []history.Record{{
		Prompt: strings.Repeat("長い日本語の詳細とemoji🙂\n", 100) + "END_OF_PROMPT",
	}}}
	for _, size := range [][2]int{{50, 25}, {80, 24}, {130, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := tui.New(sum)
			m, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
			foundEnd := false
			for i := 0; i < 400; i++ {
				view := m.View()
				if lipgloss.Height(view) > size[1] || lipgloss.Width(view) > size[0] {
					t.Fatalf("scroll %d: view %dx%d exceeds terminal %dx%d", i, lipgloss.Width(view), lipgloss.Height(view), size[0], size[1])
				}
				if !strings.Contains(view, "[1] Prompt Gains") || !strings.Contains(view, "[2] Prompts") {
					t.Fatal("tabs disappeared while scrolling detail")
				}
				foundEnd = foundEnd || strings.Contains(view, "END_OF_PROMPT")
				m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
			}
			if !foundEnd {
				t.Fatal("could not scroll to end of prompt")
			}
			for i := 0; i < 400; i++ {
				m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
			}
			if !strings.Contains(m.View(), "PROMPT DETAIL") {
				t.Fatal("could not scroll back to start of detail")
			}
		})
	}
}

func TestTUIJapaneseListFitsTerminal(t *testing.T) {
	sum := history.Summary{}
	for i := 0; i < 100; i++ {
		sum.GainPairs = append(sum.GainPairs, history.GainPair{
			Before: history.Record{Prompt: "before"},
			After:  history.Record{Prompt: strings.Repeat("日本語の長いプロンプト", 20)},
		})
	}
	for _, size := range [][2]int{{100, 80}, {130, 80}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := tui.New(sum)
			m, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for i := 0; i < len(sum.GainPairs); i++ {
				view := m.View()
				if height := lipgloss.Height(view); height > size[1] {
					t.Fatalf("cursor %d: view height %d exceeds terminal height %d", i, height, size[1])
				}
				m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
			}
		})
	}
}

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
	if !strings.Contains(view, "PROMPT DETAIL") {
		t.Fatalf("expected prompt detail view after tab switch:\n%s", view)
	}

	// Sort toggle ('s')
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	view = m.View()
	if !strings.Contains(view, "[Score ↑]") {
		t.Fatalf("expected Score ↑ sort order after 's':\n%s", view)
	}

	// Narrow window resize (vertical layout test)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 75, Height: 25})
	viewNarrow := m.View()
	if !strings.Contains(viewNarrow, "PROMPT DETAIL") {
		t.Fatalf("expected prompt detail view in narrow mode:\n%s", viewNarrow)
	}

	// Quit
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatalf("expected quit command on 'q'")
	}
}
