package ui

import (
	"fmt"
	"strings"

	"termchat/pkg/network"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing TermChat..."
	}

	// 1. Render Modals
	if m.filePicker.Active {
		return m.filePicker.View(m.width, m.height)
	}

	if m.showQR {
		return m.renderQRView()
	}

	if m.showHelp {
		return m.renderHelpView()
	}

	// 2. Header
	peers := m.manager.GetPeers()
	peerCount := len(peers)

	var statusBadge string
	if peerCount > 0 {
		statusBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true).
			Render(fmt.Sprintf("● %d PEER(S) ONLINE", peerCount))
	} else {
		statusBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Bold(true).
			Render("○ SEARCHING ON WI-FI...")
	}

	headerLeft := lipgloss.JoinHorizontal(
		lipgloss.Center,
		TitleStyle.Render("⚡ TERMCHAT"),
		" ",
		SubTitleStyle.Render(fmt.Sprintf("[%s]", m.manager.LocalName)),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Render(fmt.Sprintf(" :%d", m.manager.TCPPort)),
	)

	// Encryption status badge in header
	var lockBadge string
	if m.manager.EncryptionKey != nil {
		lockBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#BB9AF7")).Bold(true).Render(" 🔒 E2EE")
	}

	headerRight := lipgloss.JoinHorizontal(
		lipgloss.Center,
		statusBadge,
		lockBadge,
		"  ",
		HelpKeyStyle.Render("F1"),
		HelpDescStyle.Render(" Help"),
		" ",
		HelpKeyStyle.Render("^O"),
		HelpDescStyle.Render(" Files"),
	)

	gap := m.width - lipgloss.Width(headerLeft) - lipgloss.Width(headerRight) - 2
	if gap < 1 {
		gap = 1
	}

	headerBar := HeaderBox.Width(m.width).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, headerLeft, strings.Repeat(" ", gap), headerRight),
	)

	// 3. Body (Sidebar + Chat or Single-Column for Mobile)
	var body string
	if m.width >= 70 {
		// Desktop two-column layout
		sidebarContent := m.renderSidebar(peers)
		sidebar := SidebarStyle.
			Width(22).
			Height(m.viewport.Height).
			Render(sidebarContent)

		chatBox := ChatBoxStyle.
			Width(m.viewport.Width).
			Height(m.viewport.Height).
			Render(m.viewport.View())

		body = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, chatBox)
	} else {
		// Mobile narrow layout (Termux)
		body = ChatBoxStyle.
			Width(m.width).
			Height(m.viewport.Height).
			Render(m.viewport.View())
	}

	// 4. File Transfers Bar (if active)
	var transferBar string
	if len(m.transfers) > 0 {
		var lines []string
		for _, t := range m.transfers {
			pct := 0
			if t.TotalBytes > 0 {
				pct = int((float64(t.DoneBytes) / float64(t.TotalBytes)) * 100)
			}
			progressBar := renderProgressBar(pct, 16)
			prefix := "📥 Recv"
			if !t.IsIncoming {
				prefix = "📤 Send"
			}
			lines = append(lines, fmt.Sprintf("%s '%s': %s %d%% (%s/%s)",
				prefix,
				t.FileName,
				progressBar,
				pct,
				network.FormatBytes(t.DoneBytes),
				network.FormatBytes(t.TotalBytes),
			))
		}
		transferBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E0AF68")).
			Background(lipgloss.Color("#1F2335")).
			Padding(0, 1).
			Width(m.width).
			Render(strings.Join(lines, " | "))
	}

	// 5. Input Line
	prompt := InputPromptStyle.Render(fmt.Sprintf("[%s] ❯ ", m.manager.LocalName))
	inputBox := lipgloss.JoinHorizontal(lipgloss.Center, prompt, m.textInput.View())
	styledInput := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("#3B4261")).
		Width(m.width).
		Padding(0, 1).
		Render(inputBox)

	var layout []string
	layout = append(layout, headerBar, body)
	if transferBar != "" {
		layout = append(layout, transferBar)
	}
	layout = append(layout, styledInput)

	return lipgloss.JoinVertical(lipgloss.Left, layout...)
}

func (m *Model) renderSidebar(peers []network.PeerConnection) string {
	var sb strings.Builder

	totalCount := len(peers) + 1
	dropdownIcon := "▾"
	if !m.showMembersDropdown {
		dropdownIcon = "▸"
	}

	headerText := fmt.Sprintf("MEMBERS (%d) %s", totalCount, dropdownIcon)
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7AA2F7")).Render(headerText))
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Render(" [F2]"))
	sb.WriteString("\n")

	if m.showMembersDropdown {
		// Show Local User
		myName := m.manager.LocalName
		if len(myName) > 9 {
			myName = myName[:7] + ".."
		}
		sb.WriteString(fmt.Sprintf("%s %s %s\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A")).Render("●"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#C0CAF5")).Render(myName),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#E0AF68")).Render("(You)"),
		))

		// Show Connected Peers
		if len(peers) == 0 {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Render("  (Waiting for peers)\n"))
		} else {
			for _, p := range peers {
				peerName := p.Name
				battStr := ""
				if batt, ok := m.peerBatteries[p.Name]; ok {
					battStr = fmt.Sprintf(" 🔋%d%%", batt.Percentage)
				}
				if len(peerName) > 10 {
					peerName = peerName[:8] + ".."
				}
				sb.WriteString(fmt.Sprintf("%s %s%s\n",
					lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A")).Render("●"),
					lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Render(peerName),
					lipgloss.NewStyle().Foreground(lipgloss.Color("#E0AF68")).Render(battStr),
				))
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7AA2F7")).Render("QUICK ACTIONS"))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("%s /clip\n%s /browse\n%s /battery\n%s /ring\n%s /qr",
		HelpKeyStyle.Render("•"),
		HelpKeyStyle.Render("•"),
		HelpKeyStyle.Render("•"),
		HelpKeyStyle.Render("•"),
		HelpKeyStyle.Render("•"),
	))

	sb.WriteString("\n\n")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7AA2F7")).Render("DOWNLOADS"))
	sb.WriteString("\n")
	dirStr := m.manager.DownloadDir
	if len(dirStr) > 18 {
		dirStr = "..." + dirStr[len(dirStr)-15:]
	}
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Render(dirStr))

	return sb.String()
}

func (m *Model) renderHelpView() string {
	boxWidth := min(m.width-4, 76)
	boxHeight := min(m.height-2, 28)

	helpBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(PrimaryColor).
		Padding(1, 2).
		Width(boxWidth).
		Height(boxHeight)

	title := TitleStyle.Render("⚡ TERMCHAT COMMAND CHEATSHEET")
	content := fmt.Sprintf(`
%s

  %s %s
  %s %s
  %s %s
  %s %s
  %s %s
  %s %s
  %s %s
  %s %s
  %s %s
  %s %s
  %s %s
  %s %s
  %s %s
  %s %s

  %s
`,
		title,
		HelpKeyStyle.Render("Ctrl+O / /browse "), HelpDescStyle.Render("Interactive file explorer to pick & send files"),
		HelpKeyStyle.Render("/send <file_path>"), HelpDescStyle.Render("Send file (supports Tab path autocompletion)"),
		HelpKeyStyle.Render("/clip / /c       "), HelpDescStyle.Render("Sync current clipboard to connected device"),
		HelpKeyStyle.Render("/battery         "), HelpDescStyle.Render("Query peer device battery % and charging state"),
		HelpKeyStyle.Render("/notify <text>   "), HelpDescStyle.Render("Send popup notification to phone lock screen"),
		HelpKeyStyle.Render("/ring / /find    "), HelpDescStyle.Render("Ring/vibrate connected device at max volume"),
		HelpKeyStyle.Render("/open <url>      "), HelpDescStyle.Render("Open URL directly in peer device's browser"),
		HelpKeyStyle.Render("/play / /next    "), HelpDescStyle.Render("Control media playback (playerctl / media player)"),
		HelpKeyStyle.Render("/exec <command>  "), HelpDescStyle.Render("Execute remote shell command & stream back stdout"),
		HelpKeyStyle.Render("/auth <pass>     "), HelpDescStyle.Render("Enable End-to-End AES-256 Encryption"),
		HelpKeyStyle.Render("/qr              "), HelpDescStyle.Render("Show ASCII QR Code for instant phone pairing"),
		HelpKeyStyle.Render("/connect <ip>    "), HelpDescStyle.Render("Manually connect to IP (e.g. /connect 192.168.1.5)"),
		HelpKeyStyle.Render("/dir <path>      "), HelpDescStyle.Render("Set download directory for incoming files"),
		HelpKeyStyle.Render("/clear / /quit   "), HelpDescStyle.Render("Clear screen / Quit TermChat"),
		lipgloss.NewStyle().Foreground(SecondaryColor).Render("Press ESC, F1, or Enter to return to chat..."),
	)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		helpBox.Render(content),
	)
}

func (m *Model) renderQRView() string {
	boxWidth := min(m.width-4, 60)
	qrBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(PrimaryColor).
		Padding(1, 2).
		Width(boxWidth)

	title := TitleStyle.Render("📶 WI-FI PAIRING QR CODE")
	content := fmt.Sprintf("%s\n\n%s\n\n%s",
		title,
		m.qrContent,
		lipgloss.NewStyle().Foreground(SecondaryColor).Render("Press ESC or Enter to close..."),
	)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		qrBox.Render(content),
	)
}

func renderProgressBar(pct, width int) string {
	if width < 5 {
		width = 5
	}
	filledLen := (pct * width) / 100
	if filledLen > width {
		filledLen = width
	}
	emptyLen := width - filledLen

	filled := strings.Repeat("█", filledLen)
	empty := strings.Repeat("░", emptyLen)

	return lipgloss.NewStyle().Foreground(SecondaryColor).Render(filled) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#3B4261")).Render(empty)
}
