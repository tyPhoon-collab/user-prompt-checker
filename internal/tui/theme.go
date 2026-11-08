package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Gruvbox Dark Palette
	gruvBg0    = lipgloss.Color("#282828") // dark0
	gruvBg1    = lipgloss.Color("#3c3836") // dark1
	gruvBg2    = lipgloss.Color("#504945") // dark2
	gruvFg0    = lipgloss.Color("#fbf1c7") // light0
	gruvFg1    = lipgloss.Color("#ebdbb2") // light1
	gruvGray   = lipgloss.Color("#928374") // gray
	gruvRed    = lipgloss.Color("#fb4934") // bright red
	gruvGreen  = lipgloss.Color("#b8bb26") // bright green
	gruvYellow = lipgloss.Color("#fabd2f") // bright yellow
	gruvBlue   = lipgloss.Color("#83a598") // bright blue
	gruvPurple = lipgloss.Color("#d3869b") // bright purple
	gruvAqua   = lipgloss.Color("#8ec07c") // bright aqua
	gruvOrange = lipgloss.Color("#fe8019") // bright orange

	subtleStyle = lipgloss.NewStyle().Foreground(gruvGray)
	titleStyle  = lipgloss.NewStyle().
			Bold(true).
			Foreground(gruvBg0).
			Background(gruvOrange).
			Padding(0, 1)

	statBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(gruvBg2).
			Padding(0, 1)

	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(gruvBg0).
			Background(gruvBlue).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(gruvGray).
				Padding(0, 1)

	paneBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(gruvBg2).
			Align(lipgloss.Left).
			Padding(0, 1)

	selectedItemStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(gruvBg0).
				Background(gruvGreen).
				Padding(0, 1)

	greenText  = lipgloss.NewStyle().Foreground(gruvGreen).Bold(true)
	redText    = lipgloss.NewStyle().Foreground(gruvRed).Bold(true)
	blueText   = lipgloss.NewStyle().Foreground(gruvBlue).Bold(true)
	yellowText = lipgloss.NewStyle().Foreground(gruvYellow).Bold(true)
)
