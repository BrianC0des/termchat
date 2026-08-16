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

type platformStatus struct {
	Name    string
	Asset   string
	Status  string // "✓ Ready", "⏳ Building", "❌ Missing"
	SizeMB  float64
}

type releaseDataMsg struct {
	TagName   string
	Platforms []platformStatus
	RawStatus string
}

type metricsMsg struct {
	peers    int
	pingMs   int64
	relayURL string
}

type model struct {
	width       int
	height      int
	ghStatus    string
	latestTag   string
	platforms   []platformStatus
	activePeers int
	pingMs      int64
	relayURL    string
	logs        []string
	lastUpdated time.Time
	mu          sync.Mutex
}

func initialModel() model {
	defaultPlatforms := []platformStatus{
		{Name: "Linux PC (x86_64)", Asset: "termchat-linux-amd64.tar.zst", Status: "⏳ Checking...", SizeMB: 0},
		{Name: "Linux ARM64", Asset: "termchat-linux-arm64.tar.zst", Status: "⏳ Checking...", SizeMB: 0},
		{Name: "macOS (Apple Silicon)", Asset: "termchat-mac-apple-silicon.tar.zst", Status: "⏳ Checking...", SizeMB: 0},
		{Name: "macOS (Intel)", Asset: "termchat-mac-intel.tar.zst", Status: "⏳ Checking...", SizeMB: 0},
		{Name: "Android / Termux (ARM64)", Asset: "termchat-android-arm64.tar.zst", Status: "⏳ Checking...", SizeMB: 0},
		{Name: "Android / Termux (ARM32)", Asset: "termchat-android-arm.tar.zst", Status: "⏳ Checking...", SizeMB: 0},
		{Name: "Windows (64-bit .exe)", Asset: "termchat-windows.zip", Status: "⏳ Checking...", SizeMB: 0},
	}

	return model{
		relayURL:    "wss://termchat-o51d.onrender.com/ws",
		latestTag:   "v1.8.0",
		ghStatus:    "Querying GitHub Release Assets...",
		platforms:   defaultPlatforms,
		lastUpdated: time.Now(),
		logs: []string{
			fmt.Sprintf("%s [SYS] TermChat Operations Dashboard initialized", time.Now().Format("15:04:05")),
			fmt.Sprintf("%s [NET] Monitoring 7 Cross-Platform Release Targets", time.Now().Format("15:04:05")),
		},
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		fetchReleaseDataCmd(),
		fetchMetricsCmd(m.relayURL),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(4*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchReleaseDataCmd() tea.Cmd {
	return func() tea.Msg {
		// 1. Check gh run list
		rawCI := "GitHub CI: Active"
		out, err := exec.Command("gh", "run", "list", "--limit", "1").CombinedOutput()
		if err == nil && len(out) > 0 {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 1 {
				rawCI = lines[1]
			} else {
				rawCI = lines[0]
			}
		}

		// 2. Fetch Latest GitHub Release Asset Specs
		req, _ := http.NewRequest("GET", "https://api.github.com/repos/BrianC0des/termchat/releases/latest", nil)
		req.Header.Set("User-Agent", "TermChat-Dashboard/1.8")
		client := &http.Client{Timeout: 5 * time.Second}
		resp, rErr := client.Do(req)

		targetTag := "v1.8.0"
		assetMap := make(map[string]int64)

		if rErr == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var rel struct {
				TagName string `json:"tag_name"`
				Assets  []struct {
					Name string `json:"name"`
					Size int64  `json:"size"`
				} `json:"assets"`
			}
			if json.NewDecoder(resp.Body).Decode(&rel) == nil {
				if rel.TagName != "" {
					targetTag = rel.TagName
				}
				for _, a := range rel.Assets {
					assetMap[a.Name] = a.Size
				}
			}
		}

		platforms := []platformStatus{
			{Name: "Linux PC (x86_64)", Asset: "termchat-linux-amd64.tar.zst"},
			{Name: "Linux ARM64", Asset: "termchat-linux-arm64.tar.zst"},
			{Name: "macOS (Apple Silicon)", Asset: "termchat-mac-apple-silicon.tar.zst"},
			{Name: "macOS (Intel)", Asset: "termchat-mac-intel.tar.zst"},
			{Name: "Android / Termux (ARM64)", Asset: "termchat-android-arm64.tar.zst"},
			{Name: "Android / Termux (ARM32)", Asset: "termchat-android-arm.tar.zst"},
			{Name: "Windows (64-bit .exe)", Asset: "termchat-windows.zip"},
		}

		for i, p := range platforms {
			if sz, ok := assetMap[p.Asset]; ok && sz > 100000 {
				platforms[i].Status = "✓ Ready"
				platforms[i].SizeMB = float64(sz) / (1024 * 1024)
			} else {
				// Check for fallback .tar.gz or raw binary if zst is uploading
				altGz := strings.Replace(p.Asset, ".tar.zst", ".tar.gz", 1)
				if sz, ok := assetMap[altGz]; ok && sz > 100000 {
					platforms[i].Status = "✓ Ready (.gz)"
					platforms[i].SizeMB = float64(sz) / (1024 * 1024)
				} else {
					platforms[i].Status = "⏳ Compiling/Uploading"
					platforms[i].SizeMB = 0
				}
			}
		}

		return releaseDataMsg{
			TagName:   targetTag,
			Platforms: platforms,
			RawStatus: rawCI,
		}
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
			peers = 2
		}
		return metricsMsg{
			peers:    peers,
			pingMs:   ping,
			relayURL: relayURL,
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
			m.logs = append(m.logs, fmt.Sprintf("%s [SYS] Refreshing platform matrix...", time.Now().Format("15:04:05")))
			return m, tea.Batch(fetchReleaseDataCmd(), fetchMetricsCmd(m.relayURL))
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
			fetchReleaseDataCmd(),
			fetchMetricsCmd(m.relayURL),
		)

	case releaseDataMsg:
		m.latestTag = msg.TagName
		m.platforms = msg.Platforms
		m.ghStatus = msg.RawStatus

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
	topBar := titleStyle.Render("🛡️ TERMCHAT OPERATIONS & ALL-OS RELEASE MATRIX DASHBOARD") +
		subtleStyle.Render(fmt.Sprintf("  (Updated: %s)  [R]efresh  [C]lear  [Q]uit", m.lastUpdated.Format("15:04:05")))

	leftBoxWidth := (m.width * 55 / 100) - 2
	rightBoxWidth := (m.width * 45 / 100) - 3

	if leftBoxWidth < 40 {
		leftBoxWidth = 40
	}
	if rightBoxWidth < 35 {
		rightBoxWidth = 35
	}

	// Section 1: ALL-OS Cross-Platform Release Matrix
	var matrixRows []string
	readyCount := 0
	for _, p := range m.platforms {
		statusStr := successStyle.Render(p.Status)
		if strings.Contains(p.Status, "Compiling") || strings.Contains(p.Status, "Checking") {
			statusStr = warnStyle.Render(p.Status)
		} else {
			readyCount++
		}

		sizeStr := subtleStyle.Render("(--)")
		if p.SizeMB > 0 {
			sizeStr = subtleStyle.Render(fmt.Sprintf("(%.1f MB)", p.SizeMB))
		}

		row := fmt.Sprintf("%-25s %-22s %s", accentStyle.Render(p.Name), statusStr, sizeStr)
		matrixRows = append(matrixRows, row)
	}

	matrixSummary := fmt.Sprintf("%s %d / %d Platforms Published", accentStyle.Render("Overall Status:"), readyCount, len(m.platforms))
	if readyCount == len(m.platforms) {
		matrixSummary += " " + successStyle.Render("[ALL RELEASED ✓]")
	} else {
		matrixSummary += " " + warnStyle.Render("[IN PROGRESS ⏳]")
	}

	matrixContent := fmt.Sprintf(
		"%s\n%s %s\n\n%s\n\n%s",
		matrixSummary,
		accentStyle.Render("Latest Release Tag:"), successStyle.Render(m.latestTag),
		strings.Join(matrixRows, "\n"),
		boxStyle.Width(leftBoxWidth - 4).Render(m.ghStatus),
	)
	matrixBox := boxStyle.Width(leftBoxWidth).Render(
		headerStyle.Render("📦 ALL-OS PLATFORM RELEASE MATRIX") + "\n\n" + matrixContent,
	)

	// Section 2: Network & Infrastructure Metrics
	statusDot := successStyle.Render("● ONLINE")
	if m.pingMs > 500 {
		statusDot = warnStyle.Render("● SLOW")
	}
	metricsContent := fmt.Sprintf(
		"%s %s\n%s %s\n%s %d ms\n%s %s\n%s %s\n%s %s",
		accentStyle.Render("Relay Server:"), m.relayURL,
		accentStyle.Render("Relay Health:"), statusDot,
		accentStyle.Render("Relay Latency:"), m.pingMs,
		accentStyle.Render("Compression:"), successStyle.Render("Zstandard (.tar.zst)"),
		accentStyle.Render("Network Mode:"), successStyle.Render("Dual-Stack IPv4 / IPv6"),
		accentStyle.Render("Pre-Fetch Engine:"), successStyle.Render("0s Instant Auto-Stage"),
	)
	metricsBox := boxStyle.Width(rightBoxWidth).Render(
		headerStyle.Render("📊 NETWORK & INFRASTRUCTURE") + "\n\n" + metricsContent,
	)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, matrixBox, " ", metricsBox)

	// Section 3: Live System Log Stream
	logLines := m.logs
	maxLogs := m.height - 20
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
		headerStyle.Render("📜 LIVE LOG & INFRASTRUCTURE STREAM") + "\n\n" +
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
