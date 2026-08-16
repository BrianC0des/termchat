package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"termchat/pkg/network"
	"termchat/pkg/system"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type ChatMessage struct {
	SenderID   string
	SenderName string
	Content    string
	Timestamp  time.Time
	IsMe       bool
	IsSystem   bool
	IsFile     bool
}

type SharedFileItem struct {
	Index    int
	FileName string
	SizeStr  string
	URL      string
	Sender   string
	Time     time.Time
}

type Model struct {
	manager       *network.Manager
	messages      []ChatMessage
	transfers     map[string]network.FileTransferProgress
	viewport      viewport.Model
	textInput     textinput.Model
	filePicker    *FilePicker
	width         int
	height        int
	ready         bool
	showHelp            bool
	showQR              bool
	qrContent           string
	showMembersDropdown bool
	peerBatteries       map[string]system.BatteryInfo

	sharedFiles     []SharedFileItem
	showFilesModal  bool
	selectedFileIdx int
}

// Custom Tea Messages
type incomingMsg struct {
	senderID   string
	senderName string
	text       string
	ts         time.Time
}

type peerUpdateMsg struct {
	joined bool
	id     string
	name   string
	addr   string
}

type systemNoticeMsg struct {
	text string
}

type fileProgressMsg struct {
	progress network.FileTransferProgress
}

type fileReceivedMsg struct {
	fileName   string
	savedPath  string
	size       int64
	senderName string
}

type batteryMsg struct {
	senderName string
	info       system.BatteryInfo
}

type execOutputMsg struct {
	senderName string
	cmd        string
	output     string
	isError    bool
}

func NewModel(mgr *network.Manager) *Model {
	ti := textinput.New()
	ti.Placeholder = "Type message, /help, /browse, /clip, /battery..."
	ti.Focus()
	ti.CharLimit = 100000
	ti.Width = 60

	// Load chat history for the current room
	history := system.LoadHistory(mgr.RoomName, 60)
	var initialMsgs []ChatMessage
	for _, h := range history {
		initialMsgs = append(initialMsgs, ChatMessage{
			SenderID:   h.SenderID,
			SenderName: h.SenderName,
			Content:    h.Content,
			Timestamp:  h.Timestamp,
			IsMe:       h.IsMe,
			IsSystem:   h.IsSystem,
			IsFile:     h.IsFile,
		})
	}

	cfg := system.LoadConfig()
	if cfg.LastRemotePeer != "" {
		go func(addr string) {
			time.Sleep(1 * time.Second)
			mgr.ConnectTo(addr)
		}(cfg.LastRemotePeer)
	}

	return &Model{
		manager:       mgr,
		messages:      initialMsgs,
		transfers:     make(map[string]network.FileTransferProgress),
		textInput:     ti,
		filePicker:          NewFilePicker(),
		showHelp:            false,
		showQR:              false,
		showMembersDropdown: true,
		peerBatteries:       make(map[string]system.BatteryInfo),
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.EnterAltScreen,
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
		cmds  []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Modal controls when FilePicker is open
		if m.filePicker.Active {
			switch msg.Type {
			case tea.KeyEsc:
				m.filePicker.Close()
				return m, nil
			case tea.KeyUp, tea.KeyCtrlK:
				m.filePicker.MoveUp()
				return m, nil
			case tea.KeyDown, tea.KeyCtrlJ:
				m.filePicker.MoveDown()
				return m, nil
			case tea.KeyBackspace:
				m.filePicker.CurrentDir = filepath.Dir(m.filePicker.CurrentDir)
				m.filePicker.Refresh()
				return m, nil
			case tea.KeyEnter:
				selectedFile, isSelected := m.filePicker.Select()
				if isSelected {
					err := m.manager.SendFile(selectedFile)
					if err != nil {
						m.addSystemMsg(fmt.Sprintf("❌ Send file error: %v", err))
					} else {
						m.addSystemMsg(fmt.Sprintf("📤 Uploading '%s' to connected peers...", filepath.Base(selectedFile)))
					}
				}
				return m, nil
			}
			return m, nil
		}

		// Modal controls when Room Files Vault is open
		if m.showFilesModal {
			switch msg.Type {
			case tea.KeyEsc, tea.KeyCtrlC, tea.KeyCtrlF:
				m.showFilesModal = false
				return m, nil
			case tea.KeyUp:
				if m.selectedFileIdx > 0 {
					m.selectedFileIdx--
				}
				return m, nil
			case tea.KeyDown:
				if m.selectedFileIdx < len(m.sharedFiles)-1 {
					m.selectedFileIdx++
				}
				return m, nil
			case tea.KeyEnter:
				if len(m.sharedFiles) > 0 && m.selectedFileIdx >= 0 && m.selectedFileIdx < len(m.sharedFiles) {
					item := m.sharedFiles[m.selectedFileIdx]
					m.showFilesModal = false
					m.addSystemMsg(fmt.Sprintf("📥 Downloading '%s'...", item.FileName))
					go func(url string) {
						savedPath, err := m.manager.DownloadFileFromURL(url)
						if err != nil {
							m.addSystemMsg(fmt.Sprintf("❌ Download failed: %v", err))
						} else {
							m.addSystemMsg(fmt.Sprintf("✅ Download complete! Saved to:\n   📁 %s", savedPath))
						}
					}(item.URL)
				}
				return m, nil
			case tea.KeyRunes:
				if msg.String() == "o" || msg.String() == "O" {
					if len(m.sharedFiles) > 0 && m.selectedFileIdx >= 0 && m.selectedFileIdx < len(m.sharedFiles) {
						_ = system.OpenURL(m.sharedFiles[m.selectedFileIdx].URL)
						m.addSystemMsg("🌐 Opened in browser: " + m.sharedFiles[m.selectedFileIdx].URL)
					}
					return m, nil
				}
			}
			return m, nil
		}

		// Modal controls when QR is open
		if m.showQR {
			if msg.Type == tea.KeyEsc || msg.Type == tea.KeyEnter || msg.Type == tea.KeyCtrlC {
				m.showQR = false
				return m, nil
			}
		}

		// Modal controls when Help is open
		if m.showHelp {
			if msg.Type == tea.KeyEsc || msg.Type == tea.KeyF1 || msg.Type == tea.KeyCtrlC {
				m.showHelp = false
				return m, nil
			}
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			m.manager.Stop()
			return m, tea.Quit

		case tea.KeyF1:
			m.showHelp = !m.showHelp
			return m, nil

		case tea.KeyF2:
			m.showMembersDropdown = !m.showMembersDropdown
			return m, nil

		case tea.KeyCtrlF:
			m.refreshSharedFiles()
			m.showFilesModal = !m.showFilesModal
			return m, nil

		case tea.KeyCtrlO:
			m.filePicker.Open()
			return m, nil

		case tea.KeyTab:
			m.handleTabComplete()
			return m, nil

		case tea.KeyCtrlV:
			clipText, err := system.ReadClipboard()
			if err == nil && clipText != "" {
				current := m.textInput.Value()
				pos := m.textInput.Position()
				if pos > len(current) {
					pos = len(current)
				}
				newVal := current[:pos] + clipText + current[pos:]
				m.textInput.SetValue(newVal)
				m.textInput.SetCursor(pos + len(clipText))
			}
			return m, nil

		case tea.KeyEnter:
			input := strings.TrimSpace(m.textInput.Value())
			if input != "" {
				m.handleInput(input)
				m.textInput.SetValue("")
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := 4
		inputHeight := 3
		transferHeight := 0
		if len(m.transfers) > 0 {
			transferHeight = 2
		}

		vpHeight := m.height - headerHeight - inputHeight - transferHeight - 2
		if vpHeight < 4 {
			vpHeight = 4
		}

		vpWidth := m.width - 2
		if m.width >= 70 {
			vpWidth = m.width - 24 // Account for sidebar
		}

		if !m.ready {
			m.viewport = viewport.New(vpWidth, vpHeight)
			m.viewport.SetContent(m.renderMessages())
			m.ready = true
		} else {
			m.viewport.Width = vpWidth
			m.viewport.Height = vpHeight
			m.viewport.SetContent(m.renderMessages())
		}
		m.textInput.Width = m.width - 10
		m.viewport.GotoBottom()

	case incomingMsg:
		msgEntry := ChatMessage{
			SenderID:   msg.senderID,
			SenderName: msg.senderName,
			Content:    msg.text,
			Timestamp:  msg.ts,
			IsMe:       false,
			IsSystem:   false,
		}
		m.messages = append(m.messages, msgEntry)
		system.AppendHistory(m.manager.RoomName, system.HistoryEntry{
			SenderID:   msg.senderID,
			SenderName: msg.senderName,
			Content:    msg.text,
			Timestamp:  msg.ts,
			IsMe:       false,
			IsSystem:   false,
		})
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()

		// Trigger AGY if mention received and AGY is locally available
		if msg.senderID != "agy-bot" && (strings.Contains(strings.ToLower(msg.text), "@agy") || strings.HasPrefix(strings.ToLower(msg.text), "/agy")) {
			m.handleAGYMention(msg.text)
		}

	case peerUpdateMsg:
		action := "joined"
		if !msg.joined {
			action = "left"
		}
		m.messages = append(m.messages, ChatMessage{
			SenderName: "SYSTEM",
			Content:    fmt.Sprintf("👋 %s (%s) has %s", msg.name, msg.addr, action),
			Timestamp:  time.Now(),
			IsSystem:   true,
		})
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()

	case systemNoticeMsg:
		m.messages = append(m.messages, ChatMessage{
			SenderName: "SYSTEM",
			Content:    msg.text,
			Timestamp:  time.Now(),
			IsSystem:   true,
		})
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()

	case batteryMsg:
		m.peerBatteries[msg.senderName] = msg.info
		m.addSystemMsg(fmt.Sprintf("🔋 %s Battery: %d%% (%s, %s)", msg.senderName, msg.info.Percentage, msg.info.Status, msg.info.Plugged))

	case execOutputMsg:
		status := "✅ Output"
		if msg.isError {
			status = "❌ Failed"
		}
		m.addSystemMsg(fmt.Sprintf("💻 %s from %s for `%s`:\n```\n%s\n```", status, msg.senderName, msg.cmd, msg.output))

	case fileProgressMsg:
		p := msg.progress
		if p.IsDone || p.Error != "" {
			delete(m.transfers, p.FileID)
		} else {
			m.transfers[p.FileID] = p
		}
		m.viewport.SetContent(m.renderMessages())

	case fileReceivedMsg:
		notice := fmt.Sprintf("📥 Received '%s' (%s) from %s\n   📁 Saved to: %s", msg.fileName, network.FormatBytes(msg.size), msg.senderName, msg.savedPath)
		m.messages = append(m.messages, ChatMessage{
			SenderName: "SYSTEM",
			Content:    notice,
			Timestamp:  time.Now(),
			IsSystem:   true,
			IsFile:     true,
		})
		system.AppendHistory(m.manager.RoomName, system.HistoryEntry{
			SenderName: msg.senderName,
			Content:    notice,
			Timestamp:  time.Now(),
			IsSystem:   true,
			IsFile:     true,
		})
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
	}

	if !m.filePicker.Active {
		m.textInput, tiCmd = m.textInput.Update(msg)
	}
	m.viewport, vpCmd = m.viewport.Update(msg)

	cmds = append(cmds, tiCmd, vpCmd)
	return m, tea.Batch(cmds...)
}

func (m *Model) handleTabComplete() {
	val := m.textInput.Value()
	if strings.HasPrefix(val, "/send ") || strings.HasPrefix(val, "/file ") {
		parts := strings.Fields(val)
		prefix := ""
		if len(parts) > 1 {
			prefix = parts[1]
		}
		// Expand ~
		searchPath := prefix
		if strings.HasPrefix(searchPath, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				searchPath = filepath.Join(home, strings.TrimPrefix(searchPath, "~"))
			}
		}

		dir := filepath.Dir(searchPath)
		base := filepath.Base(searchPath)
		if dir == "." && !strings.Contains(prefix, "/") {
			dir = "."
		}

		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if strings.HasPrefix(strings.ToLower(e.Name()), strings.ToLower(base)) {
					completed := filepath.Join(dir, e.Name())
					if e.IsDir() {
						completed += "/"
					}
					m.textInput.SetValue(fmt.Sprintf("%s %s", parts[0], completed))
					m.textInput.SetCursor(len(m.textInput.Value()))
					break
				}
			}
		}
	}
}

func (m *Model) handleInput(text string) {
	if strings.HasPrefix(text, "/") {
		m.handleSlashCommand(text)
		return
	}

	// Normal Chat Message
	err := m.manager.SendChat(text)
	if err != nil {
		m.messages = append(m.messages, ChatMessage{
			SenderName: "SYSTEM",
			Content:    fmt.Sprintf("⚠️ Could not send: %v (No peers connected yet)", err),
			Timestamp:  time.Now(),
			IsSystem:   true,
		})
	} else {
		m.messages = append(m.messages, ChatMessage{
			SenderID:   m.manager.LocalID,
			SenderName: m.manager.LocalName,
			Content:    text,
			Timestamp:  time.Now(),
			IsMe:       true,
		})
		system.AppendHistory(m.manager.RoomName, system.HistoryEntry{
			SenderID:   m.manager.LocalID,
			SenderName: m.manager.LocalName,
			Content:    text,
			Timestamp:  time.Now(),
			IsMe:       true,
		})
	}
	m.viewport.SetContent(m.renderMessages())
	m.viewport.GotoBottom()

	if strings.Contains(strings.ToLower(text), "@agy") || strings.HasPrefix(strings.ToLower(text), "/agy") {
		m.handleAGYMention(text)
	}
}

func (m *Model) handleSlashCommand(cmdStr string) {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])

	switch command {
	case "/help":
		m.showHelp = !m.showHelp

	case "/browse", "/explorer":
		m.filePicker.Open()

	case "/copy", "/cp":
		if len(m.messages) == 0 {
			m.addSystemMsg("📋 No messages to copy")
			return
		}
		targetText := ""
		if len(parts) > 1 && strings.ToLower(parts[1]) == "all" {
			var sb strings.Builder
			for _, msg := range m.messages {
				sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", msg.Timestamp.Format("15:04:05"), msg.SenderName, msg.Content))
			}
			targetText = sb.String()
		} else if len(parts) > 1 && strings.ToLower(parts[1]) != "last" {
			searchQuery := strings.ToLower(strings.Join(parts[1:], " "))
			for i := len(m.messages) - 1; i >= 0; i-- {
				if strings.Contains(strings.ToLower(m.messages[i].Content), searchQuery) {
					targetText = m.messages[i].Content
					break
				}
			}
		} else {
			// Copy latest message
			for i := len(m.messages) - 1; i >= 0; i-- {
				if !m.messages[i].IsSystem {
					targetText = m.messages[i].Content
					break
				}
			}
			if targetText == "" && len(m.messages) > 0 {
				targetText = m.messages[len(m.messages)-1].Content
			}
		}

		if targetText != "" {
			_ = system.WriteClipboard(targetText)
			preview := targetText
			if len(preview) > 50 {
				preview = preview[:47] + "..."
			}
			m.addSystemMsg(fmt.Sprintf("📋 Copied message to clipboard: \"%s\"", preview))
		} else {
			m.addSystemMsg("📋 No matching message found to copy")
		}

	case "/paste", "/p":
		clipText, err := system.ReadClipboard()
		if err != nil || clipText == "" {
			m.addSystemMsg("📋 Clipboard is empty")
			return
		}
		m.textInput.SetValue(m.textInput.Value() + clipText)
		m.textInput.SetCursor(len(m.textInput.Value()))

	case "/clip", "/c":
		clipText, err := system.ReadClipboard()
		if err != nil || clipText == "" {
			m.addSystemMsg("📋 Local clipboard is empty or inaccessible")
			return
		}
		p := &network.Packet{
			Type:      network.MsgTypeClipboard,
			SenderID:  m.manager.LocalID,
			Sender:    m.manager.LocalName,
			Timestamp: time.Now(),
			Content:   clipText,
		}
		if err := m.manager.SendPacket(p); err != nil {
			m.addSystemMsg(fmt.Sprintf("❌ Could not sync clipboard: %v", err))
		} else {
			preview := clipText
			if len(preview) > 60 {
				preview = preview[:57] + "..."
			}
			m.addSystemMsg(fmt.Sprintf("📋 Synced local clipboard to peers: \"%s\"", preview))
		}

	case "/battery", "/batt":
		p := &network.Packet{
			Type:      network.MsgTypeBatteryReq,
			SenderID:  m.manager.LocalID,
			Sender:    m.manager.LocalName,
			Timestamp: time.Now(),
		}
		if err := m.manager.SendPacket(p); err != nil {
			m.addSystemMsg("❌ Could not request battery: no peers connected")
		} else {
			m.addSystemMsg("🔋 Requesting battery status from peers...")
		}

	case "/notify":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /notify <message>")
			return
		}
		msg := strings.Join(parts[1:], " ")
		p := &network.Packet{
			Type:      network.MsgTypeNotify,
			SenderID:  m.manager.LocalID,
			Sender:    m.manager.LocalName,
			Timestamp: time.Now(),
			Content:   msg,
		}
		_ = m.manager.SendPacket(p)
		m.addSystemMsg(fmt.Sprintf("🔔 Notification sent: \"%s\"", msg))

	case "/ring", "/vibrate", "/find":
		p := &network.Packet{
			Type:      network.MsgTypeRing,
			SenderID:  m.manager.LocalID,
			Sender:    m.manager.LocalName,
			Timestamp: time.Now(),
		}
		_ = m.manager.SendPacket(p)
		m.addSystemMsg("🔔 Triggered ring/vibrate alert on connected devices!")

	case "/open", "/url":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /open <url> (e.g., /open https://google.com)")
			return
		}
		url := parts[1]
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "https://" + url
		}
		p := &network.Packet{
			Type:      network.MsgTypeOpenUrl,
			SenderID:  m.manager.LocalID,
			Sender:    m.manager.LocalName,
			Timestamp: time.Now(),
			URL:       url,
		}
		_ = m.manager.SendPacket(p)
		m.addSystemMsg(fmt.Sprintf("🌐 Opening URL on peer: %s", url))

	case "/play", "/pause", "/next", "/prev":
		action := "play-pause"
		if command == "/next" {
			action = "next"
		} else if command == "/prev" {
			action = "previous"
		}
		p := &network.Packet{
			Type:      network.MsgTypeMedia,
			SenderID:  m.manager.LocalID,
			Sender:    m.manager.LocalName,
			Timestamp: time.Now(),
			Action:    action,
		}
		_ = m.manager.SendPacket(p)
		m.addSystemMsg(fmt.Sprintf("🎵 Sent media command: %s", action))

	case "/exec", "/sh":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /exec <command> (e.g., /exec uname -a)")
			return
		}
		shCmd := strings.Join(parts[1:], " ")
		p := &network.Packet{
			Type:      network.MsgTypeExecReq,
			SenderID:  m.manager.LocalID,
			Sender:    m.manager.LocalName,
			Timestamp: time.Now(),
			Content:   shCmd,
		}
		_ = m.manager.SendPacket(p)
		m.addSystemMsg(fmt.Sprintf("💻 Running remote command: `%s`...", shCmd))

	case "/auth", "/pass":
		if len(parts) < 2 {
			m.manager.SetEncryptionPassphrase("")
			m.addSystemMsg("🔓 Encryption disabled (plain LAN mode)")
			return
		}
		pass := parts[1]
		m.manager.SetEncryptionPassphrase(pass)
		m.addSystemMsg("🔒 AES-256 End-to-End Encryption enabled!")

	case "/files", "/shared", "/vault", "/downloads":
		m.refreshSharedFiles()
		if len(m.sharedFiles) == 0 {
			m.addSystemMsg("📁 No shared files in this room yet. Press `Ctrl + O` or type `/send <path>` to share a file!")
		} else {
			m.showFilesModal = true
		}

	case "/get", "/download", "/dl", "/fetch":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /get <file_number_or_url>\nExamples:\n  • /get 1  (download file #1 from room list)\n  • /get https://termchat-o51d.onrender.com/files/...")
			return
		}
		target := parts[1]
		fileURL := target

		// Check if user passed a file index number like `/get 1`
		m.refreshSharedFiles()
		if num, err := strconv.Atoi(target); err == nil {
			if num >= 1 && num <= len(m.sharedFiles) {
				fileURL = m.sharedFiles[num-1].URL
				m.addSystemMsg(fmt.Sprintf("📥 Selected file #%d: '%s'", num, m.sharedFiles[num-1].FileName))
			} else {
				m.addSystemMsg(fmt.Sprintf("❌ Invalid file number #%d. Room has %d shared files. Press Ctrl+F to browse.", num, len(m.sharedFiles)))
				return
			}
		}

		m.addSystemMsg(fmt.Sprintf("📥 Downloading file from %s...", fileURL))
		go func(url string) {
			savedPath, err := m.manager.DownloadFileFromURL(url)
			if err != nil {
				m.addSystemMsg(fmt.Sprintf("❌ Download failed: %v", err))
			} else {
				m.addSystemMsg(fmt.Sprintf("✅ Download complete! Saved to:\n   📁 %s", savedPath))
			}
		}(fileURL)

	case "/qr":
		ips := network.GetLocalIPs()
		mainIP := "127.0.0.1"
		if len(ips) > 0 {
			mainIP = ips[0]
		}
		qrStr := fmt.Sprintf("termchat://%s:%d", mainIP, m.manager.TCPPort)
		asciiQR, err := system.GenerateAsciiQR(qrStr)
		if err != nil {
			m.addSystemMsg(fmt.Sprintf("❌ Error generating QR: %v", err))
		} else {
			m.qrContent = fmt.Sprintf("Scan or connect to:\n%s:%d\n\n%s", mainIP, m.manager.TCPPort, asciiQR)
			m.showQR = true
		}

	case "/leave", "/offline":
		m.manager.LeaveRoom()
		var roomMsgs []ChatMessage
		history := system.LoadHistory("", 60)
		for _, h := range history {
			roomMsgs = append(roomMsgs, ChatMessage{
				SenderID:   h.SenderID,
				SenderName: h.SenderName,
				Content:    h.Content,
				Timestamp:  h.Timestamp,
				IsMe:       h.IsMe,
				IsSystem:   h.IsSystem,
				IsFile:     h.IsFile,
			})
		}
		m.messages = roomMsgs
		m.viewport.SetContent(m.renderMessages())
		m.addSystemMsg("🏠 Switched to Offline Local Wi-Fi mode (Isolated LAN only).")

	case "/mode":
		if len(parts) < 2 {
			current := "🏠 Offline Local Wi-Fi (LAN)"
			if m.manager.RoomName != "" {
				current = fmt.Sprintf("🌐 Online Cloud Room #%s", m.manager.RoomName)
			}
			m.addSystemMsg(fmt.Sprintf("Current Mode: %s\nUsage: `/mode offline` (Local Wi-Fi) or `/mode online <room_name> [password]`", current))
			return
		}
		modeType := strings.ToLower(parts[1])
		if modeType == "offline" || modeType == "lan" || modeType == "local" {
			m.manager.LeaveRoom()
			var roomMsgs []ChatMessage
			history := system.LoadHistory("", 60)
			for _, h := range history {
				roomMsgs = append(roomMsgs, ChatMessage{
					SenderID:   h.SenderID,
					SenderName: h.SenderName,
					Content:    h.Content,
					Timestamp:  h.Timestamp,
					IsMe:       h.IsMe,
					IsSystem:   h.IsSystem,
					IsFile:     h.IsFile,
				})
			}
			m.messages = roomMsgs
			m.viewport.SetContent(m.renderMessages())
			m.addSystemMsg("🏠 Switched to Offline Local Wi-Fi mode (Isolated LAN only).")
		} else if modeType == "online" || modeType == "cloud" {
			if len(parts) < 3 {
				m.addSystemMsg("Usage: /mode online <room_name> [optional_password]")
				return
			}
			newRoom := parts[2]
			relay := m.manager.RelayURL
			if relay == "" {
				relay = "wss://termchat-o51d.onrender.com/ws"
			}
			var roomMsgs []ChatMessage
			history := system.LoadHistory(newRoom, 60)
			for _, h := range history {
				roomMsgs = append(roomMsgs, ChatMessage{
					SenderID:   h.SenderID,
					SenderName: h.SenderName,
					Content:    h.Content,
					Timestamp:  h.Timestamp,
					IsMe:       h.IsMe,
					IsSystem:   h.IsSystem,
					IsFile:     h.IsFile,
				})
			}
			m.messages = roomMsgs
			m.viewport.SetContent(m.renderMessages())
			m.manager.ConnectRelay(relay, newRoom)
			if len(parts) >= 4 {
				m.manager.SetEncryptionPassphrase(parts[3])
				m.addSystemMsg(fmt.Sprintf("☁️ Switched to Online Cloud Room #%s (🔒 Encrypted)", newRoom))
			} else {
				m.manager.SetEncryptionPassphrase("")
				m.addSystemMsg(fmt.Sprintf("☁️ Switched to Online Cloud Room #%s", newRoom))
			}
		}

	case "/room", "/create", "/join", "/channel":
		if len(parts) < 2 {
			if m.manager.RoomName != "" {
				lockInfo := ""
				if m.manager.EncryptionKey != nil {
					lockInfo = " (🔒 Encrypted)"
				}
				m.addSystemMsg(fmt.Sprintf("☁️ Currently in Room: #%s%s", m.manager.RoomName, lockInfo))
			} else {
				m.addSystemMsg("Currently in: 🏠 Offline Local Wi-Fi (LAN)\nUsage: /room <room_name> [optional_password]\nExample: /room squad secret123\nTo leave: /leave")
			}
			return
		}
		if parts[1] == "leave" || parts[1] == "exit" {
			m.manager.LeaveRoom()
			var roomMsgs []ChatMessage
			history := system.LoadHistory("", 60)
			for _, h := range history {
				roomMsgs = append(roomMsgs, ChatMessage{
					SenderID:   h.SenderID,
					SenderName: h.SenderName,
					Content:    h.Content,
					Timestamp:  h.Timestamp,
					IsMe:       h.IsMe,
					IsSystem:   h.IsSystem,
					IsFile:     h.IsFile,
				})
			}
			m.messages = roomMsgs
			m.viewport.SetContent(m.renderMessages())
			m.addSystemMsg("🏠 Left room. Switched to Offline Local Wi-Fi mode.")
			return
		}
		newRoom := parts[1]
		relay := m.manager.RelayURL
		if relay == "" {
			relay = "wss://termchat-o51d.onrender.com/ws"
		}

		// Load isolated history for this room
		var roomMsgs []ChatMessage
		history := system.LoadHistory(newRoom, 60)
		for _, h := range history {
			roomMsgs = append(roomMsgs, ChatMessage{
				SenderID:   h.SenderID,
				SenderName: h.SenderName,
				Content:    h.Content,
				Timestamp:  h.Timestamp,
				IsMe:       h.IsMe,
				IsSystem:   h.IsSystem,
				IsFile:     h.IsFile,
			})
		}
		m.messages = roomMsgs
		m.viewport.SetContent(m.renderMessages())

		m.manager.ConnectRelay(relay, newRoom)

		// If password is provided in the same command: /room <name> <password>
		if len(parts) >= 3 {
			pass := parts[2]
			m.manager.SetEncryptionPassphrase(pass)
			m.addSystemMsg(fmt.Sprintf("☁️ Switched to Room #%s with 🔒 AES-256 Encryption!", newRoom))
		} else {
			m.manager.SetEncryptionPassphrase("")
			m.addSystemMsg(fmt.Sprintf("☁️ Switched to Room #%s", newRoom))
		}

	case "/clear":
		m.messages = []ChatMessage{}
		m.viewport.SetContent("")

	case "/nick", "/name":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /nick <new_name>")
			return
		}
		newName := strings.Join(parts[1:], " ")
		m.manager.SetName(newName)
		m.addSystemMsg(fmt.Sprintf("🏷️ Changed nickname to '%s'", newName))

	case "/connect":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /connect <ip:port>  (e.g., /connect 100.67.227.44:7332 or atomic:7332)")
			return
		}
		target := parts[1]
		if !strings.Contains(target, ":") {
			target = fmt.Sprintf("%s:7332", target)
		}
		m.addSystemMsg(fmt.Sprintf("🔗 Connecting to %s...", target))
		cfg := system.LoadConfig()
		cfg.LastRemotePeer = target
		system.SaveConfig(cfg)
		go m.manager.ConnectTo(target)

	case "/send", "/file":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /send <file_path>  (or press Ctrl+O / type /browse for file explorer)")
			return
		}
		filePath := strings.Join(parts[1:], " ")
		if strings.HasPrefix(filePath, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				filePath = filepath.Join(home, strings.TrimPrefix(filePath, "~"))
			}
		}

		err := m.manager.SendFile(filePath)
		if err != nil {
			m.addSystemMsg(fmt.Sprintf("❌ Send file error: %v", err))
		} else {
			m.addSystemMsg(fmt.Sprintf("📤 Uploading '%s' to connected peers...", filepath.Base(filePath)))
		}

	case "/dir":
		if len(parts) < 2 {
			m.addSystemMsg(fmt.Sprintf("📁 Current downloads folder: %s\nChange with: /dir <path>", m.manager.DownloadDir))
			return
		}
		newDir := strings.Join(parts[1:], " ")
		if strings.HasPrefix(newDir, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				newDir = filepath.Join(home, strings.TrimPrefix(newDir, "~"))
			}
		}
		_ = os.MkdirAll(newDir, 0755)
		m.manager.DownloadDir = newDir
		m.addSystemMsg(fmt.Sprintf("📁 Download directory updated to: %s", newDir))

	case "/peers", "/members", "/users", "/who":
		peers := m.manager.GetPeers()
		var sb strings.Builder
		roomInfo := "Direct LAN"
		if m.manager.RoomName != "" {
			roomInfo = "#" + m.manager.RoomName
		}
		sb.WriteString(fmt.Sprintf("👥 Room Members in %s (%d online):\n", roomInfo, len(peers)+1))
		sb.WriteString(fmt.Sprintf("   • %s (You) [👑 Online]\n", m.manager.LocalName))
		for _, p := range peers {
			extra := ""
			if batt, ok := m.peerBatteries[p.Name]; ok {
				extra = fmt.Sprintf(" [🔋 %d%%]", batt.Percentage)
			}
			sb.WriteString(fmt.Sprintf("   • %s (%s)%s\n", p.Name, p.RemoteIP, extra))
		}
		m.addSystemMsg(strings.TrimRight(sb.String(), "\n"))

	case "/ip":
		ips := network.GetLocalIPs()
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("🌐 Local IP Addresses (Port %d):\n", m.manager.TCPPort))
		for _, ip := range ips {
			sb.WriteString(fmt.Sprintf("   • %s:%d\n", ip, m.manager.TCPPort))
		}
		m.addSystemMsg(strings.TrimRight(sb.String(), "\n"))

	case "/agy", "/ai":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /agy <your question or prompt>  (or mention @agy in any chat)")
			return
		}
		prompt := strings.Join(parts[1:], " ")
		m.handleInput(fmt.Sprintf("@agy %s", prompt))

	case "/quit", "/exit":
		m.manager.Stop()
		os.Exit(0)

	default:
		m.addSystemMsg(fmt.Sprintf("❓ Unknown command '%s'. Type /help for available commands.", command))
	}
}

func (m *Model) handleAGYMention(text string) {
	if !system.IsAGYInstalled() {
		return
	}

	// Clean prompt
	cleanPrompt := strings.TrimSpace(text)
	// Remove @agy prefix if present
	for _, prefix := range []string{"@agy", "@AGY", "/agy", "/AGY", "@ai", "@AI"} {
		cleanPrompt = strings.TrimPrefix(cleanPrompt, prefix)
	}
	cleanPrompt = strings.TrimSpace(cleanPrompt)
	if cleanPrompt == "" {
		cleanPrompt = "Hello! How can I help you today?"
	}

	m.addSystemMsg("🤖 AGY is thinking...")

	go func() {
		reply, err := system.QueryAGY(cleanPrompt)
		if err != nil {
			errText := fmt.Sprintf("⚠️ AGY error: %v", err)
			_ = m.manager.SendBotChat("🤖 AGY", errText)
			m.messages = append(m.messages, ChatMessage{
				SenderID:   "agy-bot",
				SenderName: "🤖 AGY",
				Content:    errText,
				Timestamp:  time.Now(),
			})
		} else {
			_ = m.manager.SendBotChat("🤖 AGY", reply)
			m.messages = append(m.messages, ChatMessage{
				SenderID:   "agy-bot",
				SenderName: "🤖 AGY",
				Content:    reply,
				Timestamp:  time.Now(),
			})
			system.AppendHistory(m.manager.RoomName, system.HistoryEntry{
				SenderID:   "agy-bot",
				SenderName: "🤖 AGY",
				Content:    reply,
				Timestamp:  time.Now(),
			})
		}
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
	}()
}

func (m *Model) addSystemMsg(text string) {
	m.messages = append(m.messages, ChatMessage{
		SenderName: "SYSTEM",
		Content:    text,
		Timestamp:  time.Now(),
		IsSystem:   true,
	})
	m.viewport.SetContent(m.renderMessages())
	m.viewport.GotoBottom()
}

func (m *Model) renderMessages() string {
	wrapWidth := m.viewport.Width - 16
	if wrapWidth < 20 {
		wrapWidth = 20
	}
	bodyStyle := MessageText.Width(wrapWidth)

	var sb strings.Builder
	for _, msg := range m.messages {
		timeStr := TimeStyle.Render(msg.Timestamp.Format("15:04:05"))
		if msg.IsSystem {
			if msg.IsFile {
				sb.WriteString(fmt.Sprintf("%s %s\n\n", timeStr, FileNoticeStyle.Width(wrapWidth).Render(msg.Content)))
			} else {
				sb.WriteString(fmt.Sprintf("%s %s %s\n\n", timeStr, SenderSystemStyle.Render("SYSTEM ❯"), bodyStyle.Render(msg.Content)))
			}
		} else if msg.SenderName == "🤖 AGY" || strings.Contains(msg.SenderName, "AGY") {
			nameTag := SenderBotStyle.Render(fmt.Sprintf("[%s]", msg.SenderName))
			sb.WriteString(fmt.Sprintf("%s %s:\n%s\n\n", timeStr, nameTag, bodyStyle.Render(msg.Content)))
		} else if msg.IsMe {
			nameTag := SenderMeStyle.Render(fmt.Sprintf("[%s]", msg.SenderName))
			sb.WriteString(fmt.Sprintf("%s %s: %s\n\n", timeStr, nameTag, bodyStyle.Render(msg.Content)))
		} else {
			nameTag := SenderPeerStyle.Render(fmt.Sprintf("[%s]", msg.SenderName))
			sb.WriteString(fmt.Sprintf("%s %s: %s\n\n", timeStr, nameTag, bodyStyle.Render(msg.Content)))
		}
	}
	return sb.String()
}

func SetupEventBridge(p *tea.Program) network.NetworkEvents {
	return network.NetworkEvents{
		OnMessage: func(senderID, senderName, text string, ts time.Time) {
			p.Send(incomingMsg{
				senderID:   senderID,
				senderName: senderName,
				text:       text,
				ts:         ts,
			})
		},
		OnPeerJoin: func(id, name, addr string) {
			p.Send(peerUpdateMsg{
				joined: true,
				id:     id,
				name:   name,
				addr:   addr,
			})
		},
		OnPeerLeave: func(id, name string) {
			p.Send(peerUpdateMsg{
				joined: false,
				id:     id,
				name:   name,
			})
		},
		OnSystemMsg: func(text string) {
			p.Send(systemNoticeMsg{text: text})
		},
		OnFileProgress: func(prog network.FileTransferProgress) {
			p.Send(fileProgressMsg{progress: prog})
		},
		OnFileReceived: func(fileName, savedPath string, size int64, senderName string) {
			p.Send(fileReceivedMsg{
				fileName:   fileName,
				savedPath:  savedPath,
				size:       size,
				senderName: senderName,
			})
		},
		OnBattery: func(senderName string, info system.BatteryInfo) {
			p.Send(batteryMsg{
				senderName: senderName,
				info:       info,
			})
		},
		OnExecOutput: func(senderName, cmd, output string, isError bool) {
			p.Send(execOutputMsg{
				senderName: senderName,
				cmd:        cmd,
				output:     output,
				isError:    isError,
			})
		},
	}
}

func (m *Model) refreshSharedFiles() {
	var list []SharedFileItem
	idx := 1
	for _, msg := range m.messages {
		if strings.Contains(msg.Content, "📦 Shared file:") && strings.Contains(msg.Content, "🔗 http") {
			lines := strings.Split(msg.Content, "\n")
			fileName := "Shared File"
			fileURL := ""
			for _, l := range lines {
				if strings.HasPrefix(l, "📦 Shared file:") {
					fileName = strings.TrimSpace(strings.TrimPrefix(l, "📦 Shared file:"))
				} else if strings.HasPrefix(l, "🔗 ") {
					fileURL = strings.TrimSpace(strings.TrimPrefix(l, "🔗 "))
				}
			}
			if fileURL != "" {
				list = append(list, SharedFileItem{
					Index:    idx,
					FileName: fileName,
					URL:      fileURL,
					Sender:   msg.SenderName,
					Time:     msg.Timestamp,
				})
				idx++
			}
		}
	}
	m.sharedFiles = list
	if m.selectedFileIdx >= len(m.sharedFiles) {
		m.selectedFileIdx = len(m.sharedFiles) - 1
	}
	if m.selectedFileIdx < 0 {
		m.selectedFileIdx = 0
	}
}

