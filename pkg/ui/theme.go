package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type ThemePalette struct {
	Name        string
	Primary     lipgloss.Color
	Secondary   lipgloss.Color
	Accent      lipgloss.Color
	Warning     lipgloss.Color
	Muted       lipgloss.Color
	BgDark      lipgloss.Color
	BgLight     lipgloss.Color
	Text        lipgloss.Color
	BorderColor lipgloss.Color
}

var Themes = map[string]ThemePalette{
	"matrix": {
		Name:        "Matrix Movie Hacker",
		Primary:     lipgloss.Color("#00FF41"),
		Secondary:   lipgloss.Color("#008F11"),
		Accent:      lipgloss.Color("#55FF55"),
		Warning:     lipgloss.Color("#00FF66"),
		Muted:       lipgloss.Color("#006600"),
		BgDark:      lipgloss.Color("#0A0E0A"),
		BgLight:     lipgloss.Color("#001A00"),
		Text:        lipgloss.Color("#00FF41"),
		BorderColor: lipgloss.Color("#008F11"),
	},
	"tokyo-night": {
		Name:        "Tokyo Night",
		Primary:     lipgloss.Color("#7D56F4"),
		Secondary:   lipgloss.Color("#04B575"),
		Accent:      lipgloss.Color("#FF5F87"),
		Warning:     lipgloss.Color("#E0AF68"),
		Muted:       lipgloss.Color("#565F89"),
		BgDark:      lipgloss.Color("#1A1B26"),
		BgLight:     lipgloss.Color("#24283B"),
		Text:        lipgloss.Color("#C0CAF5"),
		BorderColor: lipgloss.Color("#3B4261"),
	},
	"catppuccin": {
		Name:        "Catppuccin Mocha",
		Primary:     lipgloss.Color("#CBA6F7"),
		Secondary:   lipgloss.Color("#A6E3A1"),
		Accent:      lipgloss.Color("#F38BA8"),
		Warning:     lipgloss.Color("#F9E2AF"),
		Muted:       lipgloss.Color("#6C7086"),
		BgDark:      lipgloss.Color("#1E1E2E"),
		BgLight:     lipgloss.Color("#313244"),
		Text:        lipgloss.Color("#CDD6F4"),
		BorderColor: lipgloss.Color("#45475A"),
	},
	"dracula": {
		Name:        "Dracula",
		Primary:     lipgloss.Color("#BD93F9"),
		Secondary:   lipgloss.Color("#50FA7B"),
		Accent:      lipgloss.Color("#FF79C6"),
		Warning:     lipgloss.Color("#F1FA8C"),
		Muted:       lipgloss.Color("#6272A4"),
		BgDark:      lipgloss.Color("#282A36"),
		BgLight:     lipgloss.Color("#44475A"),
		Text:        lipgloss.Color("#F8F8F2"),
		BorderColor: lipgloss.Color("#6272A4"),
	},
	"nord": {
		Name:        "Nord",
		Primary:     lipgloss.Color("#88C0D0"),
		Secondary:   lipgloss.Color("#A3BE8C"),
		Accent:      lipgloss.Color("#BF616A"),
		Warning:     lipgloss.Color("#EBCB8B"),
		Muted:       lipgloss.Color("#4C566A"),
		BgDark:      lipgloss.Color("#2E3440"),
		BgLight:     lipgloss.Color("#3B4252"),
		Text:        lipgloss.Color("#ECEFF4"),
		BorderColor: lipgloss.Color("#434C5E"),
	},
	"cyberpunk": {
		Name:        "Cyberpunk Neon",
		Primary:     lipgloss.Color("#00F0FF"),
		Secondary:   lipgloss.Color("#00FF66"),
		Accent:      lipgloss.Color("#FF0055"),
		Warning:     lipgloss.Color("#FFE600"),
		Muted:       lipgloss.Color("#711C91"),
		BgDark:      lipgloss.Color("#0D0221"),
		BgLight:     lipgloss.Color("#1B053A"),
		Text:        lipgloss.Color("#00F0FF"),
		BorderColor: lipgloss.Color("#FF0055"),
	},
	"gruvbox": {
		Name:        "Gruvbox Dark",
		Primary:     lipgloss.Color("#FE8019"),
		Secondary:   lipgloss.Color("#B8BB26"),
		Accent:      lipgloss.Color("#FB4934"),
		Warning:     lipgloss.Color("#FABD2F"),
		Muted:       lipgloss.Color("#928374"),
		BgDark:      lipgloss.Color("#282828"),
		BgLight:     lipgloss.Color("#3C3836"),
		Text:        lipgloss.Color("#EBDBB2"),
		BorderColor: lipgloss.Color("#504945"),
	},
}

var CurrentTheme = "tokyo-night"

func ApplyTheme(name string) bool {
	themeKey := strings.ToLower(strings.TrimSpace(name))
	palette, exists := Themes[themeKey]
	if !exists {
		return false
	}
	CurrentTheme = themeKey

	PrimaryColor = palette.Primary
	SecondaryColor = palette.Secondary
	AccentColor = palette.Accent
	WarningColor = palette.Warning
	MutedColor = palette.Muted
	BgDark = palette.BgDark
	BgLight = palette.BgLight

	TitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#000000")).Background(PrimaryColor).Padding(0, 1)
	SubTitleStyle = lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)

	BadgeOnline = lipgloss.NewStyle().Bold(true).Foreground(SecondaryColor).SetString("[ONLINE]")
	BadgeOffline = lipgloss.NewStyle().Bold(true).Foreground(AccentColor).SetString("[OFFLINE]")

	HeaderBox = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(palette.BorderColor).
		Padding(0, 1)

	SidebarStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(palette.BorderColor).
		Padding(0, 1)

	ChatBoxStyle = lipgloss.NewStyle().Padding(0, 1)

	StatusBar = lipgloss.NewStyle().
		Foreground(palette.Text).
		Background(palette.BgLight).
		Padding(0, 1)

	SenderMeStyle = lipgloss.NewStyle().Bold(true).Foreground(PrimaryColor)
	SenderPeerStyle = lipgloss.NewStyle().Bold(true).Foreground(SecondaryColor)
	SenderBotStyle = lipgloss.NewStyle().Bold(true).Foreground(AccentColor)
	SenderSystemStyle = lipgloss.NewStyle().Bold(true).Foreground(WarningColor)

	TimeStyle = lipgloss.NewStyle().Foreground(MutedColor)
	MessageText = lipgloss.NewStyle().Foreground(palette.Text)

	FileNoticeStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(AccentColor).
		Background(palette.BgLight).
		Padding(0, 1)

	ErrorNoticeStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(AccentColor)

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

	return true
}
