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

	if m.showFilesModal {
		return m.renderFilesModal()
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

	var modeBadge string
	var statusBadge string
	if m.manager.RoomName != "" {
		modeBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7")).Bold(true).
			Render(fmt.Sprintf("🌐 #%s", m.manager.RoomName))
		if peerCount > 0 {
			statusBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true).
				Render(fmt.Sprintf("● %d IN ROOM", peerCount+1))
		} else {
			statusBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Bold(true).
				Render("☁️ CONNECTED (1)")
		}
	} else {
		modeBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A")).Bold(true).
			Render("🏠 Local Wi-Fi")
		if peerCount > 0 {
			statusBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true).
				Render(fmt.Sprintf("● %d ON LAN", peerCount))
		} else {
			statusBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Bold(true).
				Render("○ SEARCHING LAN...")
		}
	}

	headerLeft := lipgloss.JoinHorizontal(
		lipgloss.Center,
		TitleStyle.Render("⚡ TERMCHAT"),
		" ",
		modeBadge,
		" ",
		SubTitleStyle.Render(fmt.Sprintf("[%s]", m.manager.LocalName)),
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
		HelpKeyStyle.Render("F2"),
		HelpDescStyle.Render(" Members"),
		" ",
		HelpKeyStyle.Render("F3"),
		HelpDescStyle.Render(fmt.Sprintf(" Sidebar (%s)", m.getSidebarModeLabel())),
		" ",
		HelpKeyStyle.Render("^F"),
		HelpDescStyle.Render(" Vault"),
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

	// 3. Body (Sidebar + Chat or Single-Column for Mobile / Zen mode)
	var body string
	if m.width >= 70 && m.sidebarMode != SidebarHidden {
		sidebarWidth := 22
		if m.sidebarMode == SidebarWide {
			sidebarWidth = 34
		}
		sidebarContent := m.renderSidebar(peers, sidebarWidth)
		sidebar := SidebarStyle.
			Width(sidebarWidth).
			Height(m.viewport.Height).
			Render(sidebarContent)

		chatBox := ChatBoxStyle.
			Width(m.viewport.Width).
			Height(m.viewport.Height).
			Render(m.viewport.View())

		body = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, chatBox)
	} else {
		// Fullscreen / Mobile / Zen mode layout
		body = ChatBoxStyle.
			Width(m.viewport.Width).
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

func (m *Model) getSidebarModeLabel() string {
	switch m.sidebarMode {
	case SidebarWide:
		return "Wide"
	case SidebarHidden:
		return "Zen"
	default:
		return "Normal"
	}
}

func (m *Model) renderSidebar(peers []network.PeerConnection, width int) string {
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
		maxNameLen := 9
		if width >= 30 {
			maxNameLen = 14
		}
		if len(myName) > maxNameLen {
			myName = myName[:maxNameLen-2] + ".."
		}
		statusStr := ""
		if m.myStatus != "" {
			statusStr = " " + m.myStatus
			maxStatusLen := 10
			if width >= 30 {
				maxStatusLen = 14
			}
			if len(statusStr) > maxStatusLen {
				statusStr = statusStr[:maxStatusLen-2] + ".."
			}
		}
		sb.WriteString(fmt.Sprintf("%s %s %s%s\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A")).Render("●"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#C0CAF5")).Render(myName),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#E0AF68")).Render("(You)"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7")).Render(statusStr),
		))

		// Show Connected Peers
		if len(peers) == 0 {
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Render("  (No other peers)\n"))
		} else {
			for _, p := range peers {
				peerName := p.Name
				peerStatus := ""
				if st, ok := m.userStatuses[p.Name]; ok && st != "" {
					peerStatus = " " + st
					maxStatusLen := 10
					if width >= 30 {
						maxStatusLen = 14
					}
					if len(peerStatus) > maxStatusLen {
						peerStatus = peerStatus[:maxStatusLen-2] + ".."
					}
				}
				if len(peerName) > maxNameLen {
					peerName = peerName[:maxNameLen-2] + ".."
				}
				sb.WriteString(fmt.Sprintf("%s %s%s\n",
					lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A")).Render("●"),
					lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Render(peerName),
					lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7")).Render(peerStatus),
				))
			}
		}
	}

	if width >= 30 {
		// Wide Mode Extra Panels
		if m.roomTopic != "" {
			sb.WriteString("\n")
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E0AF68")).Render("TOPIC"))
			sb.WriteString("\n")
			topicSnippet := m.roomTopic
			if len(topicSnippet) > 28 {
				topicSnippet = topicSnippet[:25] + "..."
			}
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#C0CAF5")).Render(topicSnippet))
			sb.WriteString("\n")
		}

		if len(m.pinnedMsgs) > 0 {
			sb.WriteString("\n")
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#BB9AF7")).Render(fmt.Sprintf("PINS (%d)", len(m.pinnedMsgs))))
			sb.WriteString("\n")
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Render("Type /pins to view all\n"))
		}
	}

	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7AA2F7")).Render("QUICK ACTIONS"))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("%s /reply <#> [Reply]\n%s /theme [Themes]\n%s /status [Status]\n%s /pin [Pins]\n%s /copy <#> [Copy]\n%s /files [Ctrl+F]\n%s /browse [Ctrl+O]",
		HelpKeyStyle.Render("•"),
		HelpKeyStyle.Render("•"),
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
	maxDirLen := 18
	if width >= 30 {
		maxDirLen = 28
	}
	if len(dirStr) > maxDirLen {
		dirStr = "..." + dirStr[len(dirStr)-(maxDirLen-3):]
	}
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Render(dirStr))

	return sb.String()
}

func (m *Model) renderHelpView() string {
	boxWidth := min(m.width-4, 78)
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
  %s %s
  %s %s

  %s
`,
		title,
		HelpKeyStyle.Render("Ctrl+O / /browse "), HelpDescStyle.Render("Interactive TUI file explorer to send files"),
		HelpKeyStyle.Render("/send <file> [exp]"), HelpDescStyle.Render("Send file/folder with optional expiration (10m, 1h, 1d, 7d)"),
		HelpKeyStyle.Render("Ctrl+F / /files  "), HelpDescStyle.Render("Open Shared Files Vault sidebar to select & download"),
		HelpKeyStyle.Render("/get <url|idx>   "), HelpDescStyle.Render("Download file from cloud URL or # vault index number"),
		HelpKeyStyle.Render("/room <name> [pw]"), HelpDescStyle.Render("Switch cloud relay room with optional E2E password"),
		HelpKeyStyle.Render("/lan / /offline  "), HelpDescStyle.Render("Switch to offline local Wi-Fi P2P mode (50-80 MB/s)"),
		HelpKeyStyle.Render("/nick <name>     "), HelpDescStyle.Render("Set and permanently save default user nickname"),
		HelpKeyStyle.Render("/reply <#> <msg> "), HelpDescStyle.Render("Reply to specific message ID in chat"),
		HelpKeyStyle.Render("/copy <#>        "), HelpDescStyle.Render("Copy specific message text to system clipboard"),
		HelpKeyStyle.Render("/pin <#>         "), HelpDescStyle.Render("Pin important message to the top header banner"),
		HelpKeyStyle.Render("/clip / /c       "), HelpDescStyle.Render("Sync current system clipboard content with peers"),
		HelpKeyStyle.Render("F3 / Ctrl+B      "), HelpDescStyle.Render("Toggle sidebar (Normal, Wide, Zen Fullscreen)"),
		HelpKeyStyle.Render("/theme <name>    "), HelpDescStyle.Render("Switch color theme (catppuccin, dracula, nord, cyber, etc.)"),
		HelpKeyStyle.Render("/dir <path>      "), HelpDescStyle.Render("Change destination directory for incoming downloads"),
		HelpKeyStyle.Render("/qr              "), HelpDescStyle.Render("Display ASCII QR Code for fast mobile pairing"),
		HelpKeyStyle.Render("/clear / /quit   "), HelpDescStyle.Render("Clear message buffer / Quit TermChat"),
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

func (m *Model) renderFilesModal() string {
	boxWidth := min(m.width-4, 70)
	filesBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(PrimaryColor).
		Padding(1, 2).
		Width(boxWidth)

	var sb strings.Builder
	title := TitleStyle.Render("📁 ROOM SHARED FILES")
	sb.WriteString(fmt.Sprintf("%s (Total: %d)\n", title, len(m.sharedFiles)))
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Render("Use ↑/↓ to select, [Enter] to download, [O] open in browser\n\n"))

	if len(m.sharedFiles) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7")).Render("No files shared in this room yet.\nPress Ctrl+O or type `/send <path>` to share a file!\n\n"))
	} else {
		for i, f := range m.sharedFiles {
			cursor := "  "
			itemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#C0CAF5"))
			if i == m.selectedFileIdx {
				cursor = "❯ "
				itemStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("#7AA2F7")).
					Background(lipgloss.Color("#24283B"))
			}

			senderStr := lipgloss.NewStyle().Foreground(lipgloss.Color("#E0AF68")).Render(fmt.Sprintf("by %s", f.Sender))
			timeStr := lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Render(f.Time.Format("15:04"))

			line := fmt.Sprintf("%s#%d  %-25s  %s  %s", cursor, f.Index, f.FileName, senderStr, timeStr)
			sb.WriteString(itemStyle.Render(line))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(SecondaryColor).Render("[Enter] Download to ~/Downloads    [O] Open Link    [Esc] Close"))

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		filesBox.Render(sb.String()),
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

