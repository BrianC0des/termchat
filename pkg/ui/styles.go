package ui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	// Color Palette
	PrimaryColor   = lipgloss.Color("#7D56F4") // Purple/Indigo accent
	SecondaryColor = lipgloss.Color("#04B575") // Vibrant Emerald Green
	AccentColor    = lipgloss.Color("#FF5F87") // Coral / Pink
	WarningColor   = lipgloss.Color("#E0AF68") // Warm Amber
	MutedColor     = lipgloss.Color("#565F89") // Slate Gray
	BgDark         = lipgloss.Color("#1A1B26") // Deep Charcoal
	BgLight        = lipgloss.Color("#24283B") // Surface Dark

	// Header Styles
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(PrimaryColor).
			Padding(0, 1)

	SubTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7AA2F7")).
			Bold(true)

	BadgeOnline = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#10B981")).
			SetString("● ONLINE")

	BadgeOffline = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#EF4444")).
			SetString("○ OFFLINE")

	HeaderBox = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("#3B4261")).
			Padding(0, 1)

	// Layout Containers
	SidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("#3B4261")).
			Padding(0, 1)

	ChatBoxStyle = lipgloss.NewStyle().
			Padding(0, 1)

	StatusBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A9B1D6")).
			Background(lipgloss.Color("#1F2335")).
			Padding(0, 1)

	// Messages Styling
	SenderMeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7AA2F7"))

	SenderPeerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#9ECE6A"))

	SenderBotStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#BB9AF7"))

	SenderSystemStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#E0AF68"))

	TimeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#565F89"))

	MessageText = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C0CAF5"))

	FileNoticeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#BB9AF7")).
			Background(lipgloss.Color("#24283B")).
			Padding(0, 1)

	ErrorNoticeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F7768E"))

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
			Foreground(lipgloss.Color("#7DCFFF"))

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#565F89"))
)
