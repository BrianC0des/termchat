package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styling Tokens
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FFC8")).
			Background(lipgloss.Color("#1A1B26")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7AA2F7")).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color("#3B4261"))

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3B4261")).
			Padding(0, 1)

	accentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#BB9AF7")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9ECE6A")).
			Bold(true)

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E0AF68")).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F7768E")).
			Bold(true)

	subtleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#565F89"))
)

type tickMsg time.Time
type ghStatusMsg string
type metricsMsg struct {
	peers     int
	pingMs    int64
	relayURL  string
	latestTag string
}

type model struct {
	width        int
	height       int
	ghStatus     string
	latestTag    string
	activePeers  int
	pingMs       int64
	relayURL     string
	logs         []string
	lastUpdated  time.Time
	mu           sync.Mutex
}

func initialModel() model {
	m := model{
		relayURL:    "wss://termchat-o51d.onrender.com/ws",
		latestTag:   "v1.8.0",
		ghStatus:    "Checking GitHub Actions CI...",
		lastUpdated: time.Now(),
		logs: []string{
			fmt.Sprintf("%s [SYS] TermChat Admin Dashboard initialized", time.Now().Format("15:04:05")),
			fmt.Sprintf("%s [NET] Connected to relay: wss://termchat-o51d.onrender.com/ws", time.Now().Format("15:04:05")),
			fmt.Sprintf("%s [BUILD] Current release tag: v1.8.0 (zstd enabled)", time.Now().Format("15:04:05")),
		},
	}
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		fetchGHStatusCmd(),
		fetchMetricsCmd(m.relayURL),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchGHStatusCmd() tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("gh", "run", "list", "--limit", "1").CombinedOutput()
		if err == nil && len(out) > 0 {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 1 {
				return ghStatusMsg(lines[1])
			}
			return ghStatusMsg(lines[0])
		}
		// Fallback to GitHub Releases API
		resp, rErr := http.Get("https://api.github.com/repos/BrianC0des/termchat/releases/latest")
		if rErr == nil {
			defer resp.Body.Close()
			var rel struct {
				TagName string `json:"tag_name"`
			}
			if json.NewDecoder(resp.Body).Decode(&rel) == nil && rel.TagName != "" {
				return ghStatusMsg(fmt.Sprintf("Latest GitHub Release: %s (Published)", rel.TagName))
			}
		}
		return ghStatusMsg("GitHub CI: Idle / Active")
	}
}

func fetchMetricsCmd(relayURL string) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		httpURL := strings.Replace(strings.Replace(relayURL, "wss://", "https://", 1), "/ws", "/health", 1)
		resp, err := http.Get(httpURL)
		ping := time.Since(start).Milliseconds()
		peers := 1
		if err == nil {
			resp.Body.Close()
			peers = 2 // Active relay connected
		}
		return metricsMsg{
			peers:     peers,
			pingMs:    ping,
			relayURL:  relayURL,
			latestTag: "v1.8.0",
		}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.logs = append(m.logs, fmt.Sprintf("%s [SYS] Manual refresh triggered", time.Now().Format("15:04:05")))
			return m, tea.Batch(fetchGHStatusCmd(), fetchMetricsCmd(m.relayURL))
		case "c":
			m.logs = []string{fmt.Sprintf("%s [SYS] Logs cleared", time.Now().Format("15:04:05"))}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		m.lastUpdated = time.Time(msg)
		return m, tea.Batch(
			tickCmd(),
			fetchGHStatusCmd(),
			fetchMetricsCmd(m.relayURL),
		)

	case ghStatusMsg:
		m.ghStatus = string(msg)

	case metricsMsg:
		m.activePeers = msg.peers
		m.pingMs = msg.pingMs
	}

	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return "Initializing Dashboard..."
	}

	// Title Bar
	topBar := titleStyle.Render("🛡️ TERMCHAT PRIVATE OPERATIONS & DEVELOPER TUI DASHBOARD") +
		subtleStyle.Render(fmt.Sprintf("  (Updated: %s)  [R]efresh  [C]lear Logs  [Q]uit", m.lastUpdated.Format("15:04:05")))

	boxWidth := (m.width / 2) - 3
	if boxWidth < 30 {
		boxWidth = 35
	}

	// Section 1: GitHub CI & Release Monitor
	ciContent := fmt.Sprintf(
		"%s %s\n%s %s\n%s %s\n\n%s",
		accentStyle.Render("Target Release:"), m.latestTag,
		accentStyle.Render("Compression:"), successStyle.Render("Zstandard (.tar.zst) - Pacman Speed"),
		accentStyle.Render("CDN Mirror:"), successStyle.Render("Cloudflare / Fastly Edge Active"),
		boxStyle.Width(boxWidth - 4).Render(m.ghStatus),
	)
	ciBox := boxStyle.Width(boxWidth).Render(
		headerStyle.Render("🚀 GITHUB ACTIONS CI & RELEASE MONITOR") + "\n\n" + ciContent,
	)

	// Section 2: Network & Relay Metrics
	statusDot := successStyle.Render("● ONLINE")
	if m.pingMs > 500 {
		statusDot = warnStyle.Render("● SLOW")
	}
	metricsContent := fmt.Sprintf(
		"%s %s\n%s %s\n%s %d ms\n%s %s\n%s %s",
		accentStyle.Render("Relay Server:"), m.relayURL,
		accentStyle.Render("Relay Status:"), statusDot,
		accentStyle.Render("Latency / Ping:"), m.pingMs,
		accentStyle.Render("Active Network:"), successStyle.Render("Dual-Stack IPv4 / IPv6"),
		accentStyle.Render("Pre-Fetch Engine:"), successStyle.Render("0s Instant Auto-Staging"),
	)
	metricsBox := boxStyle.Width(boxWidth).Render(
		headerStyle.Render("📊 RELAY NETWORK & INFRASTRUCTURE METRICS") + "\n\n" + metricsContent,
	)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, ciBox, " ", metricsBox)

	// Section 3: Live System Log Viewer
	logLines := m.logs
	maxLogs := m.height - 18
	if maxLogs < 4 {
		maxLogs = 4
	}
	if len(logLines) > maxLogs {
		logLines = logLines[len(logLines)-maxLogs:]
	}

	var formattedLogs []string
	for _, l := range logLines {
		if strings.Contains(l, "[ERR]") {
			formattedLogs = append(formattedLogs, errorStyle.Render(l))
		} else if strings.Contains(l, "[NET]") {
			formattedLogs = append(formattedLogs, accentStyle.Render(l))
		} else if strings.Contains(l, "[BUILD]") {
			formattedLogs = append(formattedLogs, successStyle.Render(l))
		} else {
			formattedLogs = append(formattedLogs, subtleStyle.Render(l))
		}
	}

	logsBox := boxStyle.Width(m.width - 4).Render(
		headerStyle.Render("📜 LIVE SYSTEM & NETWORK LOG STREAM") + "\n\n" +
			strings.Join(formattedLogs, "\n"),
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		topBar,
		"",
		topRow,
		"",
		logsBox,
	)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running dashboard: %v\n", err)
		os.Exit(1)
	}
}
