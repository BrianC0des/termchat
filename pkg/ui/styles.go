package ui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	// Color Palette — GitHub Dark Primer
	PrimaryColor   = lipgloss.Color("#58A6FF") // GitHub Blue
	SecondaryColor = lipgloss.Color("#3FB950") // GitHub Green
	AccentColor    = lipgloss.Color("#BC8CFF") // GitHub Purple
	WarningColor   = lipgloss.Color("#D29922") // GitHub Amber / Yellow
	MutedColor     = lipgloss.Color("#8B949E") // GitHub Muted Gray
	BgDark         = lipgloss.Color("#0D1117") // GitHub Canvas Default
	BgLight        = lipgloss.Color("#161B22") // GitHub Canvas Subtle

	// Header Styles
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(PrimaryColor).
			Padding(0, 1)

	SubTitleStyle = lipgloss.NewStyle().
			Foreground(PrimaryColor).
			Bold(true)

	BadgeOnline = lipgloss.NewStyle().
			Bold(true).
			Foreground(SecondaryColor).
			SetString("● ONLINE")

	BadgeOffline = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F85149")).
			SetString("○ OFFLINE")

	HeaderBox = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("#30363D")).
			Padding(0, 1)

	// Layout Containers
	SidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("#30363D")).
			Padding(0, 1)

	ChatBoxStyle = lipgloss.NewStyle().
			Padding(0, 1)

	StatusBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E6EDF3")).
			Background(lipgloss.Color("#161B22")).
			Padding(0, 1)

	// Messages Styling
	SenderMeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(PrimaryColor)

	SenderPeerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(SecondaryColor)

	SenderBotStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(AccentColor)

	SenderSystemStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(WarningColor)

	TimeStyle = lipgloss.NewStyle().
			Foreground(MutedColor)

	MessageText = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E6EDF3"))

	FileNoticeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(AccentColor).
			Background(BgLight).
			Padding(0, 1)

	ErrorNoticeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F85149"))

	// Input Box
	InputPromptStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(PrimaryColor)

	InputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(PrimaryColor).
			Padding(0, 1)

	HelpKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(PrimaryColor)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(MutedColor)
)
