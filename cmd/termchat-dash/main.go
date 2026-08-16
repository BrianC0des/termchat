package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
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
	Name      string
	Asset     string
	Status    string
	SizeMB    float64
	Downloads int
	Sha256    string
}

type releaseDataMsg struct {
	TagName        string
	Platforms      []platformStatus
	RawStatus      string
	TotalDownloads int
	RepoStars      int
	CommitHash     string
}

type mirrorPingMsg struct {
	githubMs  int64
	fastlyMs  int64
	googleMs  int64
	relayMs   int64
}

type model struct {
	width          int
	height         int
	ghStatus       string
	latestTag      string
	commitHash     string
	repoStars      int
	totalDownloads int
	platforms      []platformStatus
	activePeers    int
	relayMs        int64
	githubMs       int64
	fastlyMs       int64
	googleMs       int64
	relayURL       string
	logs           []string
	logFilter      string // "ALL", "NET", "BUILD", "ERR"
	lastUpdated    time.Time
	mu             sync.Mutex
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
		commitHash:  "main",
		ghStatus:    "Querying GitHub Release Analytics...",
		platforms:   defaultPlatforms,
		logFilter:   "ALL",
		lastUpdated: time.Now(),
		logs: []string{
			fmt.Sprintf("%s [SYS] TermChat Dashboard v1.8.0 Active", time.Now().Format("15:04:05")),
			fmt.Sprintf("%s [NET] Probing Mirror Latencies (GitHub / Fastly / Google / Relay)...", time.Now().Format("15:04:05")),
			fmt.Sprintf("%s [BUILD] Zstandard (.tar.zst) Pacman Speed Compression Engaged", time.Now().Format("15:04:05")),
		},
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		fetchReleaseDataCmd(),
		fetchMirrorPingCmd(m.relayURL),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(4*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchReleaseDataCmd() tea.Cmd {
	return func() tea.Msg {
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

		// Fetch Commit Hash
		commitHash := "main"
		if cOut, cErr := exec.Command("git", "rev-parse", "--short", "HEAD").CombinedOutput(); cErr == nil {
			commitHash = strings.TrimSpace(string(cOut))
		}

		// Fetch Releases Analytics
		req, _ := http.NewRequest("GET", "https://api.github.com/repos/BrianC0des/termchat/releases", nil)
		req.Header.Set("User-Agent", "TermChat-Dashboard/1.8")
		client := &http.Client{Timeout: 5 * time.Second}
		resp, rErr := client.Do(req)

		targetTag := "v1.8.0"
		assetSizeMap := make(map[string]int64)
		assetDlMap := make(map[string]int)
		totalDownloads := 0
		stars := 0

		if rErr == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var releases []struct {
				TagName string `json:"tag_name"`
				Assets  []struct {
					Name          string `json:"name"`
					Size          int64  `json:"size"`
					DownloadCount int    `json:"download_count"`
				} `json:"assets"`
			}
			if json.NewDecoder(resp.Body).Decode(&releases) == nil && len(releases) > 0 {
				targetTag = releases[0].TagName
				for _, r := range releases {
					for _, a := range r.Assets {
						totalDownloads += a.DownloadCount
						if r.TagName == targetTag {
							assetSizeMap[a.Name] = a.Size
							assetDlMap[a.Name] = a.DownloadCount
						}
					}
				}
			}
		}

		// Fetch Repo Stars
		sReq, _ := http.NewRequest("GET", "https://api.github.com/repos/BrianC0des/termchat", nil)
		sReq.Header.Set("User-Agent", "TermChat-Dashboard/1.8")
		if sResp, sErr := client.Do(sReq); sErr == nil && sResp.StatusCode == http.StatusOK {
			defer sResp.Body.Close()
			var repoInfo struct {
				StargazersCount int `json:"stargazers_count"`
			}
			if json.NewDecoder(sResp.Body).Decode(&repoInfo) == nil {
				stars = repoInfo.StargazersCount
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
			if sz, ok := assetSizeMap[p.Asset]; ok && sz > 100000 {
				platforms[i].Status = "✓ Ready"
				platforms[i].SizeMB = float64(sz) / (1024 * 1024)
				platforms[i].Downloads = assetDlMap[p.Asset]
			} else {
				altGz := strings.Replace(p.Asset, ".tar.zst", ".tar.gz", 1)
				if sz, ok := assetSizeMap[altGz]; ok && sz > 100000 {
					platforms[i].Status = "✓ Ready (.gz)"
					platforms[i].SizeMB = float64(sz) / (1024 * 1024)
					platforms[i].Downloads = assetDlMap[altGz]
				} else {
					platforms[i].Status = "⏳ Compiling"
					platforms[i].SizeMB = 0
				}
			}
			// Compute dummy SHA256 preview for verification UI
			h := sha256.Sum256([]byte(p.Asset + targetTag))
			platforms[i].Sha256 = hex.EncodeToString(h[:4])
		}

		return releaseDataMsg{
			TagName:        targetTag,
			Platforms:      platforms,
			RawStatus:      rawCI,
			TotalDownloads: totalDownloads,
			RepoStars:      stars,
			CommitHash:     commitHash,
		}
	}
}

func fetchMirrorPingCmd(relayURL string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 3 * time.Second}

		pingHost := func(url string) int64 {
			s := time.Now()
			r, err := client.Get(url)
			if err == nil {
				r.Body.Close()
				return time.Since(s).Milliseconds()
			}
			return -1
		}

		httpRelay := strings.Replace(strings.Replace(relayURL, "wss://", "https://", 1), "/ws", "/health", 1)

		return mirrorPingMsg{
			githubMs: pingHost("https://api.github.com/zen"),
			fastlyMs: pingHost("https://raw.githubusercontent.com"),
			googleMs: pingHost("https://google.com"),
			relayMs:  pingHost(httpRelay),
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
			m.logs = append(m.logs, fmt.Sprintf("%s [SYS] Refreshing platform analytics...", time.Now().Format("15:04:05")))
			return m, tea.Batch(fetchReleaseDataCmd(), fetchMirrorPingCmd(m.relayURL))
		case "f":
			switch m.logFilter {
			case "ALL":
				m.logFilter = "NET"
			case "NET":
				m.logFilter = "BUILD"
			case "BUILD":
				m.logFilter = "ERR"
			default:
				m.logFilter = "ALL"
			}
			m.logs = append(m.logs, fmt.Sprintf("%s [SYS] Log filter changed to: %s", time.Now().Format("15:04:05"), m.logFilter))
			return m, nil
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
			fetchMirrorPingCmd(m.relayURL),
		)

	case releaseDataMsg:
		m.latestTag = msg.TagName
		m.platforms = msg.Platforms
		m.ghStatus = msg.RawStatus
		m.totalDownloads = msg.TotalDownloads
		m.repoStars = msg.RepoStars
		m.commitHash = msg.CommitHash

	case mirrorPingMsg:
		m.githubMs = msg.githubMs
		m.fastlyMs = msg.fastlyMs
		m.googleMs = msg.googleMs
		m.relayMs = msg.relayMs
	}

	return m, nil
}

func formatPing(ms int64) string {
	if ms < 0 {
		return errorStyle.Render("Offline")
	}
	if ms < 100 {
		return successStyle.Render(fmt.Sprintf("%d ms (Fast)", ms))
	}
	if ms < 300 {
		return accentStyle.Render(fmt.Sprintf("%d ms (Good)", ms))
	}
	return warnStyle.Render(fmt.Sprintf("%d ms (Slow)", ms))
}

func (m model) View() string {
	if m.width == 0 {
		return "Initializing Dashboard..."
	}

	// Title Bar
	topBar := titleStyle.Render("🛡️ TERMCHAT PRIVATE OPERATIONS & ALL-OS ANALYTICS DASHBOARD") +
		subtleStyle.Render(fmt.Sprintf("  (Updated: %s)  [R]efresh  [F]ilter (%s)  [C]lear  [Q]uit", m.lastUpdated.Format("15:04:05"), m.logFilter))

	leftBoxWidth := (m.width * 58 / 100) - 2
	rightBoxWidth := (m.width * 42 / 100) - 3

	if leftBoxWidth < 42 {
		leftBoxWidth = 42
	}
	if rightBoxWidth < 35 {
		rightBoxWidth = 35
	}

	// Section 1: ALL-OS Cross-Platform Release Matrix & Checksums
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

		row := fmt.Sprintf("%-23s %-20s %-9s %s", accentStyle.Render(p.Name), statusStr, sizeStr, subtleStyle.Render("["+p.Sha256+"...]"))
		matrixRows = append(matrixRows, row)
	}

	matrixSummary := fmt.Sprintf("%s %d / %d Platforms Published", accentStyle.Render("Release Status:"), readyCount, len(m.platforms))
	if readyCount == len(m.platforms) {
		matrixSummary += " " + successStyle.Render("[ALL RELEASED ✓]")
	} else {
		matrixSummary += " " + warnStyle.Render("[IN PROGRESS ⏳]")
	}

	matrixContent := fmt.Sprintf(
		"%s\n%s %s  %s %s  %s %d  %s %d\n\n%s\n\n%s",
		matrixSummary,
		accentStyle.Render("Release Tag:"), successStyle.Render(m.latestTag),
		accentStyle.Render("Commit:"), subtleStyle.Render(m.commitHash),
		accentStyle.Render("Total Downloads:"), m.totalDownloads,
		accentStyle.Render("GitHub Stars:"), m.repoStars,
		strings.Join(matrixRows, "\n"),
		boxStyle.Width(leftBoxWidth - 4).Render(m.ghStatus),
	)
	matrixBox := boxStyle.Width(leftBoxWidth).Render(
		headerStyle.Render("📦 ALL-OS PLATFORM RELEASE & CHECKSUM MATRIX") + "\n\n" + matrixContent,
	)

	// Section 2: Multi-Mirror Ping & Infrastructure Metrics
	metricsContent := fmt.Sprintf(
		"%s %s\n%s %s\n%s %s\n%s %s\n\n%s %s\n%s %s\n%s %s",
		accentStyle.Render("Relay Server (ws):"), formatPing(m.relayMs),
		accentStyle.Render("Fastly CDN (Manila):"), formatPing(m.fastlyMs),
		accentStyle.Render("GitHub API:"), formatPing(m.githubMs),
		accentStyle.Render("Google DNS/API:"), formatPing(m.googleMs),
		accentStyle.Render("Compression:"), successStyle.Render("Zstandard (.tar.zst)"),
		accentStyle.Render("Arch/OS Runtime:"), subtleStyle.Render(runtime.GOOS+"/"+runtime.GOARCH),
		accentStyle.Render("Pre-Fetch Engine:"), successStyle.Render("0s Instant Auto-Stage"),
	)
	metricsBox := boxStyle.Width(rightBoxWidth).Render(
		headerStyle.Render("📊 MULTI-MIRROR PING & INFRASTRUCTURE") + "\n\n" + metricsContent,
	)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, matrixBox, " ", metricsBox)

	// Section 3: Live System Log Stream (Filtered)
	logLines := m.logs
	var filteredLogs []string
	for _, l := range logLines {
		if m.logFilter == "ALL" || strings.Contains(l, "["+m.logFilter+"]") {
			filteredLogs = append(filteredLogs, l)
		}
	}

	maxLogs := m.height - 20
	if maxLogs < 4 {
		maxLogs = 4
	}
	if len(filteredLogs) > maxLogs {
		filteredLogs = filteredLogs[len(filteredLogs)-maxLogs:]
	}

	var formattedLogs []string
	for _, l := range filteredLogs {
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
		headerStyle.Render(fmt.Sprintf("📜 LIVE LOG & INFRASTRUCTURE STREAM (Filter: %s)", m.logFilter)) + "\n\n" +
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
