package ui

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"termchat/pkg/ghbridge"
	"termchat/pkg/gitcollab"
	"termchat/pkg/network"
	"termchat/pkg/system"
	"termchat/pkg/workspace"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ChatMessage struct {
	SenderID      string
	SenderName    string
	Content       string
	Timestamp     time.Time
	IsMe          bool
	IsSystem      bool
	IsFile        bool
	ReplyToNum    int
	ReplyToSender string
	ReplyToText   string
	ExpiresAt     time.Time
}

type SharedFileItem struct {
	Index    int
	FileName string
	SizeStr  string
	URL      string
	Sender   string
	Time     time.Time
}

var userColorPalette = []lipgloss.Color{
	lipgloss.Color("#00E5FF"), // Vivid Cyan
	lipgloss.Color("#50FA7B"), // Mint Green
	lipgloss.Color("#FF79C6"), // Bright Pink
	lipgloss.Color("#BD93F9"), // Lavender Purple
	lipgloss.Color("#FFB86C"), // Tangerine Orange
	lipgloss.Color("#F1FA8C"), // Lemon Yellow
	lipgloss.Color("#8BE9FD"), // Sky Blue
	lipgloss.Color("#FF6E6E"), // Coral Red
	lipgloss.Color("#69FF94"), // Spring Green
	lipgloss.Color("#D6ACFF"), // Pastel Lilac
}

func getUserNameStyle(name string, isMe bool) lipgloss.Style {
	if isMe {
		return SenderMeStyle
	}
	if name == "" {
		return SenderPeerStyle
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	colorIdx := int(h.Sum32()) % len(userColorPalette)
	return lipgloss.NewStyle().Foreground(userColorPalette[colorIdx]).Bold(true)
}

type Model struct {
	manager             *network.Manager
	messages            []ChatMessage
	transfers           map[string]network.FileTransferProgress
	viewport            viewport.Model
	textInput           textinput.Model
	filePicker          *FilePicker
	width               int
	height              int
	ready               bool
	showHelp            bool
	showQR              bool
	qrContent           string
	showMembersDropdown bool
	peerBatteries       map[string]system.BatteryInfo

	sharedFiles     []SharedFileItem
	showFilesModal  bool
	selectedFileIdx int

	roomTopic     string
	pinnedMsgs    []ChatMessage
	userStatuses  map[string]string
	myStatus      string
	sidebarMode   SidebarMode
	roomExpiry    time.Time
	hasRoomExpiry bool
	autoDeleteTTL time.Duration
	updateStatus  string

	pastedSnippets   map[string]string
	pasteCounter     int
	expandCodeBlocks bool
}

type SidebarMode int

const (
	SidebarNormal SidebarMode = iota
	SidebarWide
	SidebarHidden
)

// Custom Tea Messages
type updateProgressMsg string

type incomingMsg struct {
	senderID      string
	senderName    string
	text          string
	ts            time.Time
	replyNum      int
	replySender   string
	replyText     string
}

type statusUpdateMsg struct {
	senderName string
	statusText string
}

type topicUpdateMsg struct {
	senderName string
	topicText  string
}

type pinUpdateMsg struct {
	senderName string
	pinText    string
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

func NewModel(mgr *network.Manager) *Model {
	ti := textinput.New()
	ti.Placeholder = "Type message, /help, /browse, /files, /reply, /room..."
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

	if cfg.Theme != "" {
		ApplyTheme(cfg.Theme)
	}

	m := &Model{
		manager:             mgr,
		messages:            initialMsgs,
		transfers:           make(map[string]network.FileTransferProgress),
		textInput:           ti,
		filePicker:          NewFilePicker(),
		showHelp:            false,
		showQR:              false,
		showMembersDropdown: true,
		peerBatteries:       make(map[string]system.BatteryInfo),
		userStatuses:        make(map[string]string),
		pinnedMsgs:          make([]ChatMessage, 0),
		pastedSnippets:      make(map[string]string),
	}

	// Pro Feature: Silent Background Pre-fetching of updates while user chats!
	system.CheckAndPreFetchUpdateAsync(func(msg string) {
		m.addSystemMsg(msg)
	})

	return m
}

func (m *Model) SetRoomTTL(d time.Duration) {
	if d > 0 {
		m.hasRoomExpiry = true
		m.roomExpiry = time.Now().Add(d)
		m.addSystemMsg(fmt.Sprintf("[TTL] Room set to self-destruct in %s (at %s).", d.Round(time.Second), m.roomExpiry.Format("15:04:05")))
	} else {
		m.hasRoomExpiry = false
		m.addSystemMsg("[TTL] Room self-destruct timer disabled.")
	}
}

func (m *Model) SetAutoDeleteTTL(d time.Duration) {
	m.autoDeleteTTL = d
}

type tickUIMsg time.Time

func tickUICmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return tickUIMsg(t)
	})
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.EnterAltScreen,
		tickUICmd(),
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
						m.addSystemMsg(fmt.Sprintf("[ERR] Send file error: %v", err))
					} else {
						m.addSystemMsg(fmt.Sprintf("[SEND] Uploading '%s' to connected peers...", filepath.Base(selectedFile)))
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
					m.addSystemMsg(fmt.Sprintf("[RECV] Downloading '%s'...", item.FileName))
					go func(url string) {
						savedPath, err := m.manager.DownloadFileFromURL(url)
						if err != nil {
							m.addSystemMsg(fmt.Sprintf("[ERR] Download failed: %v", err))
						} else {
							m.addSystemMsg(fmt.Sprintf("[OK] Download complete! Saved to:\n   [DIR] %s", savedPath))
						}
					}(item.URL)
				}
				return m, nil
			case tea.KeyRunes:
				if msg.String() == "o" || msg.String() == "O" {
					if len(m.sharedFiles) > 0 && m.selectedFileIdx >= 0 && m.selectedFileIdx < len(m.sharedFiles) {
						_ = system.OpenURL(m.sharedFiles[m.selectedFileIdx].URL)
						m.addSystemMsg("[NET] Opened in browser: " + m.sharedFiles[m.selectedFileIdx].URL)
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

		// 1. Intercept terminal native paste / bracketed paste / multi-character burst
		rawRunes := string(msg.Runes)
		// Ignore any leaked terminal color probes or responses
		if strings.Contains(rawRunes, "rgb:") || strings.Contains(rawRunes, "]11;") || strings.Contains(msg.String(), "]11;") {
			return m, nil
		}

		if len(msg.Runes) > 1 || strings.Contains(rawRunes, "\n") || strings.Contains(rawRunes, "\r") {
			clipText := rawRunes
			if clipText == "" {
				clipText = msg.String()
			}
			clipText = strings.ReplaceAll(clipText, "\r\n", "\n")
			clipText = strings.ReplaceAll(clipText, "\r", "\n")
			lines := strings.Split(clipText, "\n")

			if len(lines) > 1 || len(clipText) > 30 {
				m.pasteCounter++
				tokenKey := fmt.Sprintf("[Pasted text #%d +%d lines]", m.pasteCounter, len(lines))
				if m.pastedSnippets == nil {
					m.pastedSnippets = make(map[string]string)
				}
				m.pastedSnippets[tokenKey] = clipText

				current := m.textInput.Value()
				pos := m.textInput.Position()
				if pos > len(current) {
					pos = len(current)
				}
				newVal := current[:pos] + tokenKey + current[pos:]
				m.textInput.SetValue(newVal)
				m.textInput.SetCursor(pos + len(tokenKey))
				return m, nil
			}
		}

		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlK, tea.KeyCtrlL:
			if m.textInput.Value() != "" {
				m.textInput.SetValue("")
				m.pastedSnippets = make(map[string]string)
				return m, nil
			}

		case tea.KeyCtrlC:
			if m.textInput.Value() != "" {
				m.textInput.SetValue("")
				m.pastedSnippets = make(map[string]string)
				return m, nil
			}
			m.manager.Stop()
			return m, tea.Quit

		case tea.KeyF1:
			m.showHelp = !m.showHelp
			return m, nil

		case tea.KeyF2:
			m.showMembersDropdown = !m.showMembersDropdown
			return m, nil

		case tea.KeyF3, tea.KeyCtrlB:
			switch m.sidebarMode {
			case SidebarNormal:
				m.sidebarMode = SidebarWide
			case SidebarWide:
				m.sidebarMode = SidebarHidden
			case SidebarHidden:
				m.sidebarMode = SidebarNormal
			}
			m.recalculateViewport()
			return m, nil

		case tea.KeyCtrlE, tea.KeyF4:
			m.expandCodeBlocks = !m.expandCodeBlocks
			m.viewport.SetContent(m.renderMessages())
			return m, nil

		case tea.KeyUp:
			if m.textInput.Value() == "" {
				m.viewport.LineUp(1)
				return m, nil
			}

		case tea.KeyDown:
			if m.textInput.Value() == "" {
				m.viewport.LineDown(1)
				return m, nil
			}

		case tea.KeyPgUp:
			m.viewport.HalfViewUp()
			return m, nil

		case tea.KeyPgDown:
			m.viewport.HalfViewDown()
			return m, nil

		case tea.KeyCtrlU:
			if m.textInput.Value() != "" {
				m.textInput.SetValue("")
				m.pastedSnippets = make(map[string]string)
				return m, nil
			}
			m.viewport.HalfViewUp()
			return m, nil

		case tea.KeyCtrlD:
			m.viewport.HalfViewDown()
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
				clipText = strings.ReplaceAll(clipText, "\r\n", "\n")
				clipText = strings.ReplaceAll(clipText, "\r", "\n")
				lines := strings.Split(clipText, "\n")

				current := m.textInput.Value()
				pos := m.textInput.Position()
				if pos > len(current) {
					pos = len(current)
				}

				if len(lines) > 1 || len(clipText) > 30 {
					m.pasteCounter++
					tokenKey := fmt.Sprintf("[Pasted text #%d +%d lines]", m.pasteCounter, len(lines))
					if m.pastedSnippets == nil {
						m.pastedSnippets = make(map[string]string)
					}
					m.pastedSnippets[tokenKey] = clipText

					newVal := current[:pos] + tokenKey + current[pos:]
					m.textInput.SetValue(newVal)
					m.textInput.SetCursor(pos + len(tokenKey))
				} else {
					newVal := current[:pos] + clipText + current[pos:]
					m.textInput.SetValue(newVal)
					m.textInput.SetCursor(pos + len(clipText))
				}
			}
			return m, nil

		case tea.KeyEnter:
			rawInput := m.textInput.Value()
			// Expand any [Pasted text #X +Y lines] tokens back to full multiline snippet!
			expandedInput := rawInput
			for token, snippet := range m.pastedSnippets {
				if strings.Contains(expandedInput, token) {
					expandedInput = strings.ReplaceAll(expandedInput, token, snippet)
				}
			}
			m.pastedSnippets = make(map[string]string)

			input := strings.TrimSpace(expandedInput)
			if input != "" {
				m.handleInput(input)
				m.textInput.SetValue("")
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalculateViewport()
		m.textInput.Width = m.width - 10
		m.viewport.GotoBottom()

	case tickUIMsg:
		cmds = append(cmds, tickUICmd())

		// 1. Check Room TTL Expiration
		if m.hasRoomExpiry && time.Now().After(m.roomExpiry) {
			m.hasRoomExpiry = false
			// Secure RAM zeroing & history purge
			for i := range m.messages {
				m.messages[i].Content = ""
				m.messages[i].SenderName = ""
			}
			m.messages = []ChatMessage{}
			system.PurgeHistory(m.manager.RoomName)
			if m.manager.RoomName != "" {
				m.manager.LeaveRoom()
			}
			m.addSystemMsg("💥 [ROOM SELF-DESTRUCT] Room TTL has expired! Memory zero-filled, history files purged, and session closed.")
			m.viewport.SetContent(m.renderMessages())
		}

		// 2. Check and Purge Expired Disappearing Messages
		if len(m.messages) > 0 {
			now := time.Now()
			activeMsgs := make([]ChatMessage, 0, len(m.messages))
			changed := false
			for _, msg := range m.messages {
				if !msg.ExpiresAt.IsZero() && now.After(msg.ExpiresAt) {
					changed = true
					continue
				}
				activeMsgs = append(activeMsgs, msg)
			}
			if changed {
				m.messages = activeMsgs
				m.viewport.SetContent(m.renderMessages())
			} else if m.autoDeleteTTL > 0 || m.hasRoomExpiry {
				// Refresh countdown timers in view
				m.viewport.SetContent(m.renderMessages())
			}
		}

	case updateProgressMsg:
		m.updateStatus = string(msg)
		return m, nil

	case incomingMsg:
		var expiresAt time.Time
		if m.autoDeleteTTL > 0 {
			expiresAt = time.Now().Add(m.autoDeleteTTL)
		}
		msgEntry := ChatMessage{
			SenderID:      msg.senderID,
			SenderName:    msg.senderName,
			Content:       msg.text,
			Timestamp:     msg.ts,
			IsMe:          false,
			IsSystem:      false,
			ReplyToNum:    msg.replyNum,
			ReplyToSender: msg.replySender,
			ReplyToText:   msg.replyText,
			ExpiresAt:     expiresAt,
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

		// Check for @mentions of local user or @all
		myMention := "@" + strings.ToLower(m.manager.LocalName)
		lowerText := strings.ToLower(msg.text)
		if msg.senderID != m.manager.LocalID && (strings.Contains(lowerText, myMention) || strings.Contains(lowerText, "@all") || strings.Contains(lowerText, "@everyone")) {
			fmt.Print("\a") // Terminal audio chime
			_ = system.SendNotification(fmt.Sprintf("Mentioned by %s in TermChat", msg.senderName), msg.text)
		}

		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()

	case statusUpdateMsg:
		if m.userStatuses == nil {
			m.userStatuses = make(map[string]string)
		}
		if msg.statusText == "" {
			delete(m.userStatuses, msg.senderName)
		} else {
			m.userStatuses[msg.senderName] = msg.statusText
		}
		return m, nil

	case topicUpdateMsg:
		m.roomTopic = msg.topicText
		m.addSystemMsg(fmt.Sprintf("[TOPIC] %s set Room Topic to: \"%s\"", msg.senderName, msg.topicText))
		m.viewport.SetContent(m.renderMessages())
		return m, nil

	case pinUpdateMsg:
		m.pinnedMsgs = append(m.pinnedMsgs, ChatMessage{
			SenderName: msg.senderName,
			Content:    msg.pinText,
			Timestamp:  time.Now(),
		})
		m.addSystemMsg(fmt.Sprintf("[PIN] %s pinned: \"%s\"", msg.senderName, msg.pinText))
		return m, nil

	case peerUpdateMsg:
		if msg.name != "" {
			action := "joined"
			if !msg.joined {
				action = "left"
			}
			m.messages = append(m.messages, ChatMessage{
				SenderName: "SYSTEM",
				Content:    fmt.Sprintf("[NET] %s has %s", msg.name, action),
				Timestamp:  time.Now(),
				IsSystem:   true,
			})
		}
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

	case fileProgressMsg:
		p := msg.progress
		if p.IsDone || p.Error != "" {
			delete(m.transfers, p.FileID)
		} else {
			m.transfers[p.FileID] = p
		}
		m.viewport.SetContent(m.renderMessages())

	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			m.viewport.LineUp(3)
			return m, nil
		case tea.MouseWheelDown:
			m.viewport.LineDown(3)
			return m, nil
		}

	case fileReceivedMsg:
		notice := fmt.Sprintf("[RECV] Received '%s' (%s) from %s\n   [DIR] Saved to: %s", msg.fileName, network.FormatBytes(msg.size), msg.senderName, msg.savedPath)
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
		val := m.textInput.Value()
		if !strings.HasPrefix(val, "/") && (strings.Contains(val, "```") || (len(val) > 80 && !strings.Contains(val, "[Pasted text #"))) {
			m.pasteCounter++
			lines := strings.Split(val, "\n")
			lineCount := len(lines)
			if lineCount <= 1 {
				lineCount = (len(val) / 60) + 1
			}
			tokenKey := fmt.Sprintf("[Pasted text #%d +%d lines]", m.pasteCounter, lineCount)
			if m.pastedSnippets == nil {
				m.pastedSnippets = make(map[string]string)
			}
			m.pastedSnippets[tokenKey] = val
			m.textInput.SetValue(tokenKey)
			m.textInput.SetCursor(len(tokenKey))
		}
	}

	// Forward non-key events to viewport (mouse, resize, ticks)
	// Keystrokes are handled exclusively by textInput or dedicated KeyMsg bindings
	switch msg.(type) {
	case tea.KeyMsg:
		// Do not pass typing keys to viewport to prevent accidental scrolling while typing words!
	default:
		m.viewport, vpCmd = m.viewport.Update(msg)
	}

	cmds = append(cmds, tiCmd, vpCmd)
	return m, tea.Batch(cmds...)
}

const maxInMemMessages = 250

func (m *Model) trimMessagesBuffer() {
	if len(m.messages) > maxInMemMessages {
		m.messages = m.messages[len(m.messages)-maxInMemMessages:]
	}
}

func (m *Model) handleTabComplete() {
	val := m.textInput.Value()
	if val == "" {
		return
	}

	// 1. Auto-complete @mentions
	if idx := strings.LastIndex(val, "@"); idx != -1 {
		prefix := strings.ToLower(val[idx+1:])
		peers := m.manager.GetPeers()
		var candidateNames []string
		for _, p := range peers {
			if p.Name != "" {
				candidateNames = append(candidateNames, p.Name)
			}
		}
		candidateNames = append(candidateNames, "all", "everyone")

		for _, name := range candidateNames {
			if strings.HasPrefix(strings.ToLower(name), prefix) && strings.ToLower(name) != prefix {
				m.textInput.SetValue(val[:idx] + "@" + name + " ")
				m.textInput.SetCursor(len(m.textInput.Value()))
				return
			}
		}
	}

	// 2. Auto-complete slash command sub-arguments (like /theme <name>)
	if strings.HasPrefix(val, "/theme ") || strings.HasPrefix(val, "/themes ") {
		parts := strings.Fields(val)
		if len(parts) >= 2 {
			themePrefix := strings.ToLower(parts[1])
			for k := range Themes {
				if strings.HasPrefix(k, themePrefix) {
					m.textInput.SetValue(fmt.Sprintf("/theme %s", k))
					m.textInput.SetCursor(len(m.textInput.Value()))
					return
				}
			}
		}
	}

	// 3. Auto-complete slash commands
	if strings.HasPrefix(val, "/") && !strings.Contains(val, " ") {
		cmdList := []string{
			"/theme", "/themes", "/reply", "/status", "/afk", "/topic",
			"/pin", "/pins", "/unpin", "/copy", "/files", "/browse",
			"/clip", "/sidebar", "/clear", "/nick", "/room", "/init", "/repo", "/update",
			"/diff", "/patch", "/apply", "/branch", "/branches", "/checkout", "/switch",
			"/pr", "/issue", "/ci",
			"/help", "/qr", "/send", "/dir", "/connect",
		}
		lowerVal := strings.ToLower(val)
		for _, cmd := range cmdList {
			if strings.HasPrefix(cmd, lowerVal) && cmd != lowerVal {
				m.textInput.SetValue(cmd + " ")
				m.textInput.SetCursor(len(m.textInput.Value()))
				return
			}
		}
	}

	// 4. Auto-complete file paths for /send
	if strings.HasPrefix(val, "/send ") || strings.HasPrefix(val, "/file ") {
		parts := strings.Fields(val)
		prefix := ""
		if len(parts) > 1 {
			prefix = parts[1]
		}
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

	// Normal Chat Message - always append locally and save
	var expiresAt time.Time
	if m.autoDeleteTTL > 0 {
		expiresAt = time.Now().Add(m.autoDeleteTTL)
	}

	m.messages = append(m.messages, ChatMessage{
		SenderID:   m.manager.LocalID,
		SenderName: m.manager.LocalName,
		Content:    text,
		Timestamp:  time.Now(),
		IsMe:       true,
		ExpiresAt:  expiresAt,
	})
	system.AppendHistory(m.manager.RoomName, system.HistoryEntry{
		SenderID:   m.manager.LocalID,
		SenderName: m.manager.LocalName,
		Content:    text,
		Timestamp:  time.Now(),
		IsMe:       true,
	})

	_ = m.manager.SendChat(text)

	m.trimMessagesBuffer()
	m.viewport.SetContent(m.renderMessages())
	m.viewport.GotoBottom()
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
			m.addSystemMsg("[CLIP] No messages to copy")
			return
		}
		targetText := ""
		copiedLabel := ""

		if len(parts) > 1 {
			arg := parts[1]
			if strings.ToLower(arg) == "all" {
				var sb strings.Builder
				for _, msg := range m.messages {
					sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", msg.Timestamp.Format("15:04:05"), msg.SenderName, msg.Content))
				}
				targetText = sb.String()
				copiedLabel = "Full chat history"
			} else if num, err := strconv.Atoi(arg); err == nil {
				// Copy by numbered message index
				userMsgCount := 0
				for _, msg := range m.messages {
					if !msg.IsSystem {
						userMsgCount++
						if userMsgCount == num {
							targetText = msg.Content
							copiedLabel = fmt.Sprintf("Message #%d (%s)", num, msg.SenderName)
							break
						}
					}
				}
				if targetText == "" {
					if userMsgCount == 0 {
						m.addSystemMsg("[CLIP] No chat messages in this room yet. Send a message first to copy it!")
					} else {
						m.addSystemMsg(fmt.Sprintf("[ERR] Message #%d not found. This room has %d chat message(s).", num, userMsgCount))
					}
					return
				}
			} else {
				// Search by text query
				searchQuery := strings.ToLower(strings.Join(parts[1:], " "))
				for i := len(m.messages) - 1; i >= 0; i-- {
					if strings.Contains(strings.ToLower(m.messages[i].Content), searchQuery) {
						targetText = m.messages[i].Content
						copiedLabel = fmt.Sprintf("Search match (%s)", m.messages[i].SenderName)
						break
					}
				}
			}
		} else {
			// Copy latest message (prefer user chat, fallback to system notice)
			for i := len(m.messages) - 1; i >= 0; i-- {
				if !m.messages[i].IsSystem {
					targetText = m.messages[i].Content
					copiedLabel = fmt.Sprintf("Latest message from %s", m.messages[i].SenderName)
					break
				}
			}
			if targetText == "" && len(m.messages) > 0 {
				targetText = m.messages[len(m.messages)-1].Content
				copiedLabel = "Latest notice"
			}
		}

		if targetText != "" {
			_ = system.WriteClipboard(targetText)
			preview := targetText
			if len(preview) > 50 {
				preview = preview[:47] + "..."
			}
			m.addSystemMsg(fmt.Sprintf("[CLIP] Copied %s to clipboard:\n   \"%s\"", copiedLabel, preview))
		} else {
			m.addSystemMsg("[CLIP] No messages found to copy.")
		}

	case "/reply", "/r":
		if len(parts) < 3 {
			m.addSystemMsg("Usage: /reply <#msg> <your message>  (e.g., /reply 2 Yes, I agree!)")
			return
		}
		num, err := strconv.Atoi(parts[1])
		if err != nil {
			m.addSystemMsg("[ERR] Invalid message number. Usage: /reply <#msg> <your message>")
			return
		}
		userMsgCount := 0
		var targetMsg *ChatMessage
		for i := range m.messages {
			if !m.messages[i].IsSystem {
				userMsgCount++
				if userMsgCount == num {
					targetMsg = &m.messages[i]
					break
				}
			}
		}
		if targetMsg == nil {
			m.addSystemMsg(fmt.Sprintf("[ERR] Message #%d not found. This room has %d chat message(s).", num, userMsgCount))
			return
		}
		replyContent := strings.Join(parts[2:], " ")
		snippet := targetMsg.Content
		if len(snippet) > 40 {
			snippet = snippet[:37] + "..."
		}
		_ = m.manager.SendReply(num, targetMsg.SenderName, snippet, replyContent)
		m.messages = append(m.messages, ChatMessage{
			SenderID:      m.manager.LocalID,
			SenderName:    m.manager.LocalName,
			Content:       replyContent,
			Timestamp:     time.Now(),
			IsMe:          true,
			ReplyToNum:    num,
			ReplyToSender: targetMsg.SenderName,
			ReplyToText:   snippet,
		})
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()

	case "/theme", "/themes":
		if len(parts) < 2 {
			var sb strings.Builder
			sb.WriteString("[THEME] Available Themes:\n")
			for k, v := range Themes {
				marker := "  "
				if k == CurrentTheme {
					marker = "> "
				}
				sb.WriteString(fmt.Sprintf("%s • /theme %-12s - %s\n", marker, k, v.Name))
			}
			sb.WriteString(":: Type `/theme <name>` to switch instantly!")
			m.addSystemMsg(sb.String())
			return
		}
		targetTheme := strings.ToLower(parts[1])
		if ApplyTheme(targetTheme) {
			cfg := system.LoadConfig()
			cfg.Theme = targetTheme
			system.SaveConfig(cfg)
			m.addSystemMsg(fmt.Sprintf("[THEME] Theme switched to '%s' (saved as default)!", Themes[targetTheme].Name))
			m.viewport.SetContent(m.renderMessages())
		} else {
			m.addSystemMsg(fmt.Sprintf("[ERR] Unknown theme '%s'. Type `/themes` to see available palettes.", targetTheme))
		}

	case "/fold", "/expand", "/collapse":
		m.expandCodeBlocks = !m.expandCodeBlocks
		m.viewport.SetContent(m.renderMessages())

	case "/status":
		if len(parts) < 2 {
			if m.myStatus != "" {
				m.addSystemMsg(fmt.Sprintf("[STATUS] Current status: %s (Type `/status clear` to remove)", m.myStatus))
			} else {
				m.addSystemMsg("Usage: /status <custom status> (e.g., /status Coding in Go)")
			}
			return
		}
		newStatus := strings.Join(parts[1:], " ")
		if strings.ToLower(newStatus) == "clear" || strings.ToLower(newStatus) == "none" {
			newStatus = ""
		}
		m.myStatus = newStatus
		_ = m.manager.SendStatus(newStatus)
		if newStatus != "" {
			m.addSystemMsg(fmt.Sprintf("[STATUS] Status set to: %s", newStatus))
		} else {
			m.addSystemMsg("[STATUS] Status cleared.")
		}

	case "/afk":
		afkMsg := "[AFK] AFK"
		if len(parts) > 1 {
			afkMsg = "[AFK] AFK: " + strings.Join(parts[1:], " ")
		}
		m.myStatus = afkMsg
		_ = m.manager.SendStatus(afkMsg)
		m.addSystemMsg(fmt.Sprintf("[STATUS] Status set to: %s", afkMsg))

	case "/topic":
		if len(parts) < 2 {
			if m.roomTopic != "" {
				m.addSystemMsg(fmt.Sprintf("[PIN] Room Topic: \"%s\"", m.roomTopic))
			} else {
				m.addSystemMsg("Usage: /topic <room description/topic>")
			}
			return
		}
		newTopic := strings.Join(parts[1:], " ")
		if strings.ToLower(newTopic) == "clear" {
			newTopic = ""
		}
		m.roomTopic = newTopic
		_ = m.manager.SendTopic(newTopic)
		if newTopic != "" {
			m.addSystemMsg(fmt.Sprintf("[PIN] Room Topic updated to: \"%s\"", newTopic))
		} else {
			m.addSystemMsg("[PIN] Room Topic cleared.")
		}
		m.viewport.SetContent(m.renderMessages())

	case "/pin":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /pin <#msg>  (e.g., /pin 1)")
			return
		}
		num, err := strconv.Atoi(parts[1])
		if err != nil {
			m.addSystemMsg("[ERR] Invalid message number. Usage: /pin <#msg>")
			return
		}
		userMsgCount := 0
		var targetMsg *ChatMessage
		for i := range m.messages {
			if !m.messages[i].IsSystem {
				userMsgCount++
				if userMsgCount == num {
					targetMsg = &m.messages[i]
					break
				}
			}
		}
		if targetMsg == nil {
			m.addSystemMsg(fmt.Sprintf("[ERR] Message #%d not found.", num))
			return
		}
		m.pinnedMsgs = append(m.pinnedMsgs, *targetMsg)
		_ = m.manager.SendPin(fmt.Sprintf("[%s]: %s", targetMsg.SenderName, targetMsg.Content))
		m.addSystemMsg(fmt.Sprintf("[PIN] Pinned message #%d (%s: \"%s\")", num, targetMsg.SenderName, targetMsg.Content))

	case "/pins":
		if len(m.pinnedMsgs) == 0 {
			m.addSystemMsg("[PIN] No pinned messages in this room yet. (Use `/pin <#>` to pin)")
			return
		}
		var sb strings.Builder
		sb.WriteString("[PIN] Pinned Messages in this Room:\n")
		for i, pin := range m.pinnedMsgs {
			sb.WriteString(fmt.Sprintf("   [%d] [%s] %s: %s\n", i+1, pin.Timestamp.Format("15:04"), pin.SenderName, pin.Content))
		}
		m.addSystemMsg(sb.String())

	case "/unpin":
		if len(m.pinnedMsgs) == 0 {
			m.addSystemMsg("[PIN] No pinned messages to remove.")
			return
		}
		if len(parts) < 2 {
			m.pinnedMsgs = []ChatMessage{}
			m.addSystemMsg("[PIN] Cleared all pinned messages.")
			return
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil || idx < 1 || idx > len(m.pinnedMsgs) {
			m.addSystemMsg(fmt.Sprintf("[ERR] Invalid pin index. Range: 1-%d", len(m.pinnedMsgs)))
			return
		}
		m.pinnedMsgs = append(m.pinnedMsgs[:idx-1], m.pinnedMsgs[idx:]...)
	case "/sidebar", "/sb":
		if len(parts) < 2 {
			switch m.sidebarMode {
			case SidebarNormal:
				m.sidebarMode = SidebarWide
			case SidebarWide:
				m.sidebarMode = SidebarHidden
			case SidebarHidden:
				m.sidebarMode = SidebarNormal
			}
		} else {
			modeArg := strings.ToLower(parts[1])
			switch modeArg {
			case "wide", "expand", "max":
				m.sidebarMode = SidebarWide
			case "hide", "hidden", "off", "zen":
				m.sidebarMode = SidebarHidden
			case "normal", "show", "on", "default":
				m.sidebarMode = SidebarNormal
			default:
				m.addSystemMsg("Usage: /sidebar [normal | wide | hide | toggle]")
			}
		}
		m.recalculateViewport()

	case "/paste", "/p":
		clipText, err := system.ReadClipboard()
		if err != nil || clipText == "" {
			m.addSystemMsg("[CLIP] Clipboard is empty")
			return
		}
		m.textInput.SetValue(m.textInput.Value() + clipText)
		m.textInput.SetCursor(len(m.textInput.Value()))

	case "/clip", "/c":
		clipText, err := system.ReadClipboard()
		if err != nil || clipText == "" {
			m.addSystemMsg("[CLIP] Local clipboard is empty or inaccessible")
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
			m.addSystemMsg(fmt.Sprintf("[ERR] Could not sync clipboard: %v", err))
		} else {
			preview := clipText
			if len(preview) > 60 {
				preview = preview[:57] + "..."
			}
			m.addSystemMsg(fmt.Sprintf("[CLIP] Synced local clipboard to peers: \"%s\"", preview))
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
		m.addSystemMsg(fmt.Sprintf("[NOTIFY] Notification sent: \"%s\"", msg))

	case "/ring", "/vibrate", "/find":
		p := &network.Packet{
			Type:      network.MsgTypeRing,
			SenderID:  m.manager.LocalID,
			Sender:    m.manager.LocalName,
			Timestamp: time.Now(),
		}
		_ = m.manager.SendPacket(p)
		m.addSystemMsg("[NOTIFY] Triggered ring/vibrate alert on connected devices!")

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
		m.addSystemMsg(fmt.Sprintf("[NET] Opening URL on peer: %s", url))

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
		m.addSystemMsg(fmt.Sprintf("[AUDIO] Sent media command: %s", action))

	case "/update", "/upgrade":
		m.updateStatus = "⚡ [UPDATE] Checking for updates..."
		go func() {
			msg, err := system.UpdateSelfWithProgress(func(progressMsg string) {
				if activeProgram != nil {
					activeProgram.Send(updateProgressMsg(progressMsg))
				}
			})
			if err != nil {
				if activeProgram != nil {
					activeProgram.Send(updateProgressMsg(fmt.Sprintf("[ERR] %v", err)))
				}
			} else {
				if activeProgram != nil {
					activeProgram.Send(updateProgressMsg(msg))
				}
			}
		}()

	case "/auth", "/pass":
		if len(parts) < 2 {
			m.manager.SetEncryptionPassphrase("")
			m.addSystemMsg("[UNLOCKED] Encryption disabled (plain LAN mode)")
			return
		}
		pass := parts[1]
		m.manager.SetEncryptionPassphrase(pass)
		fingerprint := system.GenerateKeyFingerprint(m.manager.EncryptionKey)
		m.addSystemMsg(fmt.Sprintf("[AES-256] End-to-End Encryption enabled! Verification Key Code: [ %s ]", fingerprint))

	case "/expire", "/ttl":
		if len(parts) < 2 {
			if m.hasRoomExpiry {
				rem := time.Until(m.roomExpiry)
				if rem > 0 {
					m.addSystemMsg(fmt.Sprintf("[TTL] ⏳ Room expires in %s (at %s). Use `/expire off` to cancel.", rem.Round(time.Second), m.roomExpiry.Format("15:04:05")))
				} else {
					m.addSystemMsg("[TTL] Room has expired.")
				}
			} else {
				m.addSystemMsg("Usage: /expire <duration>  (e.g., /expire 30m, /expire 1h, /expire 24h, /expire off)")
			}
			return
		}
		arg := strings.ToLower(parts[1])
		if arg == "off" || arg == "clear" || arg == "disable" || arg == "0" {
			m.hasRoomExpiry = false
			m.addSystemMsg("[TTL] Room self-destruct timer disabled.")
		} else {
			dur, err := time.ParseDuration(arg)
			if err != nil || dur <= 0 {
				m.addSystemMsg(fmt.Sprintf("[ERR] Invalid duration '%s'. Examples: `/expire 10m`, `/expire 1h`, `/expire 24h`", parts[1]))
				return
			}
			m.hasRoomExpiry = true
			m.roomExpiry = time.Now().Add(dur)
			m.addSystemMsg(fmt.Sprintf("[TTL] ⏳ Room set to self-destruct in %s (at %s). Local history & buffers will be wiped upon expiration.", dur.Round(time.Second), m.roomExpiry.Format("15:04:05")))
		}

	case "/autodelete", "/burn", "/ephemeral":
		if len(parts) < 2 {
			if m.autoDeleteTTL > 0 {
				m.addSystemMsg(fmt.Sprintf("[AUTODELETE] Auto-delete is ACTIVE (%s). Messages disappear after %s. Use `/autodelete off` to disable.", m.autoDeleteTTL, m.autoDeleteTTL))
			} else {
				m.addSystemMsg("Usage: /autodelete <duration>  (e.g., /autodelete 30s, /autodelete 5m, /autodelete 1h, /autodelete off)")
			}
			return
		}
		arg := strings.ToLower(parts[1])
		if arg == "off" || arg == "clear" || arg == "disable" || arg == "0" {
			m.autoDeleteTTL = 0
			m.addSystemMsg("[AUTODELETE] Disappearing messages disabled.")
		} else {
			dur, err := time.ParseDuration(arg)
			if err != nil || dur <= 0 {
				m.addSystemMsg(fmt.Sprintf("[ERR] Invalid duration '%s'. Examples: `/autodelete 30s`, `/autodelete 5m`, `/autodelete 1h`", parts[1]))
				return
			}
			m.autoDeleteTTL = dur
			m.addSystemMsg(fmt.Sprintf("[AUTODELETE] ⏱️ New messages will automatically self-destruct %s after delivery.", dur.Round(time.Second)))
		}

	case "/files", "/shared", "/vault", "/downloads":
		m.refreshSharedFiles()
		if len(m.sharedFiles) == 0 {
			m.addSystemMsg("[FILES] No shared files in this room yet. Press `Ctrl + O` or type `/send <path>` to share a file!")
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
				m.addSystemMsg(fmt.Sprintf("[RECV] Selected file #%d: '%s'", num, m.sharedFiles[num-1].FileName))
			} else {
				m.addSystemMsg(fmt.Sprintf("[ERR] Invalid file number #%d. Room has %d shared files. Press Ctrl+F to browse.", num, len(m.sharedFiles)))
				return
			}
		}

		m.addSystemMsg(fmt.Sprintf("[RECV] Downloading file from %s...", fileURL))
		go func(url string) {
			savedPath, err := m.manager.DownloadFileFromURL(url)
			if err != nil {
				m.addSystemMsg(fmt.Sprintf("[ERR] Download failed: %v", err))
			} else {
				m.addSystemMsg(fmt.Sprintf("[OK] Download complete! Saved to:\n   [DIR] %s", savedPath))
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
			m.addSystemMsg(fmt.Sprintf("[ERR] Error generating QR: %v", err))
		} else {
			m.qrContent = fmt.Sprintf("Scan or connect to:\n%s:%d\n\n%s", mainIP, m.manager.TCPPort, asciiQR)
			m.showQR = true
		}

	case "/leave", "/offline", "/lan":
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
		m.addSystemMsg("[LAN] Switched to Offline Local Wi-Fi (LAN Direct Mode).")

	case "/mode":
		if len(parts) < 2 {
			current := "[LAN] Offline Local Wi-Fi"
			if m.manager.RoomName != "" {
				current = fmt.Sprintf("[ROOM] Online Cloud Room #%s", m.manager.RoomName)
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
			m.addSystemMsg("[LAN] Switched to Offline Local Wi-Fi mode.")
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
				m.addSystemMsg(fmt.Sprintf("[ROOM] Switched to Cloud Room #%s ([AES-256] Encrypted)", newRoom))
			} else {
				m.manager.SetEncryptionPassphrase("")
				m.addSystemMsg(fmt.Sprintf("[ROOM] Switched to Cloud Room #%s", newRoom))
			}
		}

	case "/room", "/create", "/join", "/channel":
		if len(parts) < 2 {
			if m.manager.RoomName != "" {
				lockInfo := ""
				if m.manager.EncryptionKey != nil {
					lockInfo = " ([AES-256] Encrypted)"
				}
				m.addSystemMsg(fmt.Sprintf("[ROOM] Currently in Room: #%s%s", m.manager.RoomName, lockInfo))
			} else {
				m.addSystemMsg("Currently in: [LAN] Offline Local Wi-Fi\nUsage: /room <room_name> [optional_password]\nExample: /room squad secret123\nTo leave: /leave")
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
			m.addSystemMsg("[LAN] Left room. Switched to Offline Local Wi-Fi mode.")
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
			m.addSystemMsg(fmt.Sprintf("[ROOM] Switched to Room #%s with [AES-256] Encryption!", newRoom))
		} else {
			m.manager.SetEncryptionPassphrase("")
			m.addSystemMsg(fmt.Sprintf("[ROOM] Switched to Room #%s", newRoom))
		}

	case "/init", "/repo", "/workspace":
		roomName := ""
		pass := ""
		if len(parts) > 1 {
			if parts[1] == "init" && len(parts) > 2 {
				roomName = parts[2]
				if len(parts) > 3 {
					pass = parts[3]
				}
			} else if parts[1] != "init" {
				roomName = parts[1]
				if len(parts) > 2 {
					pass = parts[2]
				}
			}
		}

		wsCfg, path, err := workspace.InitWorkspace("", "", roomName, pass)
		if err != nil {
			m.addSystemMsg(fmt.Sprintf("[ERR] Failed to initialize project room: %v", err))
			return
		}

		// Connect to the newly initialized room immediately
		if wsCfg.Passphrase != "" {
			m.manager.SetEncryptionPassphrase(wsCfg.Passphrase)
		}
		m.manager.ConnectRelay(wsCfg.Relay, wsCfg.Room)

		repoInfo := ""
		if wsCfg.Repo != "" {
			repoInfo = fmt.Sprintf("\n• Repository: %s", wsCfg.Repo)
		}
		m.addSystemMsg(fmt.Sprintf("[WORKSPACE] 🐙 Project Collab Room Initialized!\n• Config File: %s%s\n• Collab Room: #%s\n👉 Commit .termchat/room.json to git so teammates auto-join on 'git clone'!", path, repoInfo, wsCfg.Room))

	case "/diff", "/patch":
		staged := len(parts) > 1 && (parts[1] == "staged" || parts[1] == "--staged" || parts[1] == "--cached")
		diffRes, err := gitcollab.CaptureDiff("", staged)
		if err != nil {
			m.addSystemMsg(fmt.Sprintf("[DIFF] %v", err))
			return
		}

		filesList := strings.Join(diffRes.Files, ", ")
		if len(filesList) > 60 {
			filesList = filesList[:57] + "..."
		}

		diffType := "Uncommitted Working Tree"
		if staged {
			diffType = "Staged (Cached)"
		}

		cardMsg := fmt.Sprintf("📦 **[GIT PATCH #patch-%s]** %s\n• **Changes:** +%d / -%d in %d file(s) (`%s`)\n• **Apply:** Type `/apply %s` to apply this patch directly to your repository!\n```diff\n%s\n```",
			diffRes.PatchID, diffType, diffRes.Additions, diffRes.Deletions, len(diffRes.Files), filesList, diffRes.PatchID, diffRes.RawDiff)

		m.messages = append(m.messages, ChatMessage{
			SenderID:   m.manager.LocalID,
			SenderName: m.manager.LocalName,
			Content:    cardMsg,
			Timestamp:  time.Now(),
			IsMe:       true,
		})
		system.AppendHistory(m.manager.RoomName, system.HistoryEntry{
			SenderID:   m.manager.LocalID,
			SenderName: m.manager.LocalName,
			Content:    cardMsg,
			Timestamp:  time.Now(),
			IsMe:       true,
		})
		_ = m.manager.SendChat(cardMsg)
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()

	case "/apply":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /apply <patch_id>\nExample: /apply 7f8a9b1c (or /apply #patch-7f8a9b1c)")
			return
		}
		patchID := strings.TrimPrefix(parts[1], "#patch-")
		patchID = strings.TrimPrefix(patchID, "#")

		patchContent, ok := gitcollab.GetPatch(patchID)
		if !ok {
			// Search recent messages for matching diff block if not in local memory
			for i := len(m.messages) - 1; i >= 0; i-- {
				msg := m.messages[i].Content
				if strings.Contains(msg, "#patch-"+patchID) {
					if startIdx := strings.Index(msg, "```diff\n"); startIdx != -1 {
						raw := msg[startIdx+8:]
						if endIdx := strings.Index(raw, "\n```"); endIdx != -1 {
							patchContent = raw[:endIdx]
							ok = true
							break
						}
					}
				}
			}
		}

		if !ok || patchContent == "" {
			m.addSystemMsg(fmt.Sprintf("[GIT] Error: Patch #patch-%s not found in memory or recent chat.", patchID))
			return
		}

		msg, err := gitcollab.ApplyPatch("", patchContent)
		if err != nil {
			m.addSystemMsg(fmt.Sprintf("[GIT] %v", err))
			return
		}
		m.addSystemMsg(fmt.Sprintf("[GIT] ✓ Applied patch #patch-%s cleanly to your local workspace! (%s)", patchID, msg))

	case "/branch", "/branches":
		if out, err := exec.Command("git", "branch", "--show-current").Output(); err == nil && len(out) > 0 {
			curr := strings.TrimSpace(string(out))
			m.addSystemMsg(fmt.Sprintf("🌿 Active Git Branch: %s", curr))
		} else {
			m.addSystemMsg("[GIT] Not inside a git repository.")
		}

	case "/checkout", "/switch":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /checkout <branch_name_or_#PR>\nExample: /checkout feat/auth-tokens (or /checkout #12)")
			return
		}
		targetBranch := parts[1]
		if strings.HasPrefix(targetBranch, "#") {
			prNum, err := strconv.Atoi(strings.TrimPrefix(targetBranch, "#"))
			if err == nil {
				msg, chkErr := ghbridge.CheckoutPR(prNum)
				if chkErr != nil {
					m.addSystemMsg(fmt.Sprintf("[GH] %v", chkErr))
				} else {
					m.addSystemMsg(fmt.Sprintf("🌿 Switched to PR #%d branch! (%s)", prNum, msg))
				}
				return
			}
		}

		cmd := exec.Command("git", "checkout", targetBranch)
		out, err := cmd.CombinedOutput()
		if err != nil {
			m.addSystemMsg(fmt.Sprintf("[GIT] Failed to switch branch: %s", strings.TrimSpace(string(out))))
			return
		}
		m.addSystemMsg(fmt.Sprintf("🌿 Switched to Git branch '%s'!", targetBranch))

	case "/pr":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /pr <number> or /pr checkout <number>\nExample: /pr 12")
			return
		}
		if parts[1] == "checkout" && len(parts) > 2 {
			prNum, err := strconv.Atoi(strings.TrimPrefix(parts[2], "#"))
			if err != nil {
				m.addSystemMsg("Usage: /pr checkout <number>")
				return
			}
			msg, err := ghbridge.CheckoutPR(prNum)
			if err != nil {
				m.addSystemMsg(fmt.Sprintf("[GH] %v", err))
				return
			}
			m.addSystemMsg(fmt.Sprintf("🌿 Switched to PR #%d branch! (%s)", prNum, msg))
			return
		}

		prNum, err := strconv.Atoi(strings.TrimPrefix(parts[1], "#"))
		if err != nil {
			m.addSystemMsg("Usage: /pr <number>")
			return
		}

		repo := ""
		if wsCfg, _, err := workspace.FindWorkspace(""); err == nil && wsCfg.Repo != "" {
			repo = wsCfg.Repo
		}

		pr, err := ghbridge.FetchPR(repo, prNum)
		if err != nil {
			m.addSystemMsg(fmt.Sprintf("[GH] %v", err))
			return
		}

		stateIcon := "🟢"
		if pr.State == "MERGED" {
			stateIcon = "🟣"
		} else if pr.State == "CLOSED" {
			stateIcon = "🔴"
		}

		reviewStr := "Needs Review"
		if pr.ReviewState == "APPROVED" {
			reviewStr = "✅ Approved"
		} else if pr.ReviewState == "CHANGES_REQUESTED" {
			reviewStr = "⚠️ Changes Requested"
		}

		cardMsg := fmt.Sprintf("🐙 **[PULL REQUEST #%d]** %s\n• **Status:** %s %s • **Review:** %s\n• **Branches:** `🌿 %s` ➔ `%s`\n• **Changes:** +%d / -%d • Author: @%s\n• **Action:** Type `/checkout #%d` to checkout locally!\n• URL: %s",
			pr.Number, pr.Title, stateIcon, pr.State, reviewStr, pr.HeadRefName, pr.BaseRefName, pr.Additions, pr.Deletions, pr.Author, pr.Number, pr.URL)

		m.messages = append(m.messages, ChatMessage{
			SenderID:   m.manager.LocalID,
			SenderName: m.manager.LocalName,
			Content:    cardMsg,
			Timestamp:  time.Now(),
			IsMe:       true,
		})
		system.AppendHistory(m.manager.RoomName, system.HistoryEntry{
			SenderID:   m.manager.LocalID,
			SenderName: m.manager.LocalName,
			Content:    cardMsg,
			Timestamp:  time.Now(),
			IsMe:       true,
		})
		_ = m.manager.SendChat(cardMsg)
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()

	case "/issue":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /issue <number>\nExample: /issue 4")
			return
		}
		issueNum, err := strconv.Atoi(strings.TrimPrefix(parts[1], "#"))
		if err != nil {
			m.addSystemMsg("Usage: /issue <number>")
			return
		}

		repo := ""
		if wsCfg, _, err := workspace.FindWorkspace(""); err == nil && wsCfg.Repo != "" {
			repo = wsCfg.Repo
		}

		iss, err := ghbridge.FetchIssue(repo, issueNum)
		if err != nil {
			m.addSystemMsg(fmt.Sprintf("[GH] %v", err))
			return
		}

		stateIcon := "🟢"
		if iss.State == "CLOSED" {
			stateIcon = "🟣"
		}

		cardMsg := fmt.Sprintf("🐛 **[ISSUE #%d]** %s\n• **Status:** %s %s • **Author:** @%s\n• **Link:** %s",
			iss.Number, iss.Title, stateIcon, iss.State, iss.Author, iss.URL)

		m.messages = append(m.messages, ChatMessage{
			SenderID:   m.manager.LocalID,
			SenderName: m.manager.LocalName,
			Content:    cardMsg,
			Timestamp:  time.Now(),
			IsMe:       true,
		})
		system.AppendHistory(m.manager.RoomName, system.HistoryEntry{
			SenderID:   m.manager.LocalID,
			SenderName: m.manager.LocalName,
			Content:    cardMsg,
			Timestamp:  time.Now(),
			IsMe:       true,
		})
		_ = m.manager.SendChat(cardMsg)
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()

	case "/ci":
		repo := ""
		if wsCfg, _, err := workspace.FindWorkspace(""); err == nil && wsCfg.Repo != "" {
			repo = wsCfg.Repo
		}
		branch := ""
		if out, err := exec.Command("git", "branch", "--show-current").Output(); err == nil {
			branch = strings.TrimSpace(string(out))
		}

		status, err := ghbridge.FetchCIStatus(repo, branch)
		if err != nil {
			m.addSystemMsg(fmt.Sprintf("[CI] %v", err))
			return
		}
		m.addSystemMsg(fmt.Sprintf("[CI/CD] %s", status))




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
		cfg := system.LoadConfig()
		cfg.Nickname = newName
		system.SaveConfig(cfg)
		m.addSystemMsg(fmt.Sprintf("[NICK] Changed nickname to '%s' (saved as default)", newName))

	case "/connect":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /connect <ip:port>  (e.g., /connect 100.67.227.44:7332 or atomic:7332)")
			return
		}
		target := parts[1]
		if !strings.Contains(target, ":") {
			target = fmt.Sprintf("%s:7332", target)
		}
		m.addSystemMsg(fmt.Sprintf("[NET] Connecting to %s...", target))
		cfg := system.LoadConfig()
		cfg.LastRemotePeer = target
		system.SaveConfig(cfg)
		go m.manager.ConnectTo(target)

	case "/send", "/file":
		if len(parts) < 2 {
			m.addSystemMsg("Usage: /send <file_path> [optional_expiry: 10m, 1h, 1d, 7d]\nExamples:\n  /send notes.pdf\n  /send secret.zip 1h\n  /send database.sql 7d")
			return
		}
		
		filePath := strings.Join(parts[1:], " ")
		expiry := "24h"

		if len(parts) >= 3 {
			lastToken := strings.ToLower(parts[len(parts)-1])
			if strings.HasSuffix(lastToken, "m") || strings.HasSuffix(lastToken, "h") || strings.HasSuffix(lastToken, "d") || strings.HasSuffix(lastToken, "w") {
				var n int
				if _, err := fmt.Sscanf(lastToken[:len(lastToken)-1], "%d", &n); err == nil && n > 0 {
					candidate := strings.Join(parts[1:len(parts)-1], " ")
					checkPath := candidate
					if strings.HasPrefix(checkPath, "~") {
						if home, err := os.UserHomeDir(); err == nil {
							checkPath = filepath.Join(home, strings.TrimPrefix(checkPath, "~"))
						}
					}
					if _, err := os.Stat(checkPath); err == nil {
						filePath = candidate
						expiry = lastToken
					}
				}
			}
		}

		if strings.HasPrefix(filePath, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				filePath = filepath.Join(home, strings.TrimPrefix(filePath, "~"))
			}
		}

		err := m.manager.SendFileWithExpiry(filePath, expiry)
		if err != nil {
			m.addSystemMsg(fmt.Sprintf("[ERR] Send file error: %v", err))
		} else {
			m.addSystemMsg(fmt.Sprintf("[SEND] Uploading '%s' (Expires: %s)...", filepath.Base(filePath), expiry))
		}

	case "/dir":
		if len(parts) < 2 {
			m.addSystemMsg(fmt.Sprintf("[DIR] Current downloads folder: %s\nChange with: /dir <path>", m.manager.DownloadDir))
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
		m.addSystemMsg(fmt.Sprintf("[DIR] Download directory updated to: %s", newDir))

	case "/peers", "/members", "/users", "/who":
		peers := m.manager.GetPeers()
		var sb strings.Builder
		roomInfo := "Direct LAN"
		if m.manager.RoomName != "" {
			roomInfo = "#" + m.manager.RoomName
		}
		sb.WriteString(fmt.Sprintf("[USERS] Room Members in %s (%d online):\n", roomInfo, len(peers)+1))
		sb.WriteString(fmt.Sprintf("   • %s (You) [ME]\n", m.manager.LocalName))
		for _, p := range peers {
			sb.WriteString(fmt.Sprintf("   • %s (%s) [ON]\n", p.Name, p.RemoteIP))
		}
		m.addSystemMsg(strings.TrimRight(sb.String(), "\n"))

	case "/ip":
		ips := network.GetLocalIPs()
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("[NET] Local IP Addresses (Port %d):\n", m.manager.TCPPort))
		for _, ip := range ips {
			sb.WriteString(fmt.Sprintf("   • %s:%d\n", ip, m.manager.TCPPort))
		}
		m.addSystemMsg(strings.TrimRight(sb.String(), "\n"))

	case "/quit", "/exit":
		m.manager.Stop()
		os.Exit(0)

	default:
		m.addSystemMsg(fmt.Sprintf("[?] Unknown command '%s'. Type /help for available commands.", command))
	}
}

func (m *Model) addSystemMsg(text string) {
	m.messages = append(m.messages, ChatMessage{
		SenderName: "SYSTEM",
		Content:    text,
		Timestamp:  time.Now(),
		IsSystem:   true,
	})
	m.trimMessagesBuffer()
	m.viewport.SetContent(m.renderMessages())
	m.viewport.GotoBottom()
}

func formatTimeDivider(t time.Time, now time.Time, width int) string {
	label := t.Format("03:04 PM")
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		label = fmt.Sprintf("Today, %s", t.Format("03:04 PM"))
	} else if t.Year() == now.Year() && t.YearDay() == now.YearDay()-1 {
		label = fmt.Sprintf("Yesterday, %s", t.Format("03:04 PM"))
	} else if t.Year() == now.Year() {
		label = t.Format("Jan 02, 03:04 PM")
	} else {
		label = t.Format("Jan 02 2006, 03:04 PM")
	}

	contentLen := len(label) + 2
	lineLen := (width - contentLen) / 2
	if lineLen < 3 {
		lineLen = 3
	}
	rule := strings.Repeat("─", lineLen)
	ruleStyle := lipgloss.NewStyle().Foreground(MutedColor)
	labelStyle := lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)

	dividerText := fmt.Sprintf("%s %s %s", ruleStyle.Render(rule), labelStyle.Render(label), ruleStyle.Render(rule))
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(dividerText)
}

func (m *Model) renderMessages() string {
	wrapWidth := m.viewport.Width - 16
	if wrapWidth < 20 {
		wrapWidth = 20
	}
	bodyStyle := MessageText.Width(wrapWidth)

	var sb strings.Builder

	if m.roomTopic != "" {
		topicBanner := lipgloss.NewStyle().
			Foreground(WarningColor).
			Background(BgLight).
			Bold(true).
			Padding(0, 1).
			Render(fmt.Sprintf("[TOPIC] %s", m.roomTopic))
		sb.WriteString(topicBanner + "\n\n")
	}

	msgIdx := 1
	var lastSender string
	var lastTime time.Time
	now := time.Now()

	for _, msg := range m.messages {
		// Insert centered time divider on natural conversation breaks (5+ mins) or day change
		if lastTime.IsZero() || msg.Timestamp.Sub(lastTime) >= 5*time.Minute || msg.Timestamp.YearDay() != lastTime.YearDay() || msg.Timestamp.Year() != lastTime.Year() {
			divider := formatTimeDivider(msg.Timestamp, now, wrapWidth)
			if msgIdx > 1 {
				sb.WriteString("\n")
			}
			sb.WriteString(divider + "\n\n")
			lastSender = ""
		}

		if msg.IsSystem {
			lastSender = ""
			if msg.IsFile {
				sb.WriteString(fmt.Sprintf("%s\n\n", FileNoticeStyle.Width(wrapWidth).Render(msg.Content)))
			} else {
				sb.WriteString(fmt.Sprintf("%s %s\n\n", SenderSystemStyle.Render("[SYS] >"), bodyStyle.Render(msg.Content)))
			}
		} else {
			isGrouped := false
			if lastSender != "" && msg.SenderName == lastSender && msg.ReplyToNum == 0 && msg.Timestamp.Sub(lastTime) < 90*time.Second {
				isGrouped = true
			}

			if msg.ReplyToNum > 0 {
				replyQuote := lipgloss.NewStyle().
					Foreground(MutedColor).
					Italic(true).
					Render(fmt.Sprintf("   |- Replying to #%d (%s: \"%s\")", msg.ReplyToNum, msg.ReplyToSender, msg.ReplyToText))
				sb.WriteString(replyQuote + "\n")
			}

			numBadge := lipgloss.NewStyle().Foreground(MutedColor).Render(fmt.Sprintf("#%d", msgIdx))
			var timerBadge string
			if !msg.ExpiresAt.IsZero() {
				rem := time.Until(msg.ExpiresAt)
				if rem > 0 {
					secs := int(rem.Seconds())
					timerBadge = " " + lipgloss.NewStyle().Foreground(WarningColor).Bold(true).Render(fmt.Sprintf("[⏱️ %02d:%02d]", secs/60, secs%60))
				}
			}

			if isGrouped {
				// Clean indented continuation: only message index and vertical guide
				firstLinePrefix := fmt.Sprintf("   %s%s %s", numBadge, timerBadge, lipgloss.NewStyle().Foreground(MutedColor).Render("│"))
				continuationPrefix := fmt.Sprintf("       %s", lipgloss.NewStyle().Foreground(MutedColor).Render("│"))
				formatted := FormatChatMessageWithFold(msg.Content, wrapWidth, firstLinePrefix, continuationPrefix, m.manager.LocalName, m.expandCodeBlocks)
				sb.WriteString(formatted)
			} else {
				if msgIdx > 1 && !lastTime.IsZero() && msg.Timestamp.Sub(lastTime) < 5*time.Minute {
					sb.WriteString("\n")
				}
				prefix := fmt.Sprintf("%s%s", numBadge, timerBadge)
				nameTag := getUserNameStyle(msg.SenderName, msg.IsMe).Render(fmt.Sprintf("[%s]:", msg.SenderName))
				firstLinePrefix := fmt.Sprintf("%s %s", prefix, nameTag)
				continuationPrefix := fmt.Sprintf("   %s", lipgloss.NewStyle().Foreground(MutedColor).Render("│"))
				formatted := FormatChatMessageWithFold(msg.Content, wrapWidth, firstLinePrefix, continuationPrefix, m.manager.LocalName, m.expandCodeBlocks)
				sb.WriteString(formatted)
			}

			lastSender = msg.SenderName
			lastTime = msg.Timestamp
			msgIdx++
		}
	}
	return sb.String()
}

var activeProgram *tea.Program

func SetupEventBridge(p *tea.Program) network.NetworkEvents {
	activeProgram = p
	return network.NetworkEvents{
		OnMessage: func(senderID, senderName, text string, ts time.Time, replyNum int, replySender, replyText string) {
			p.Send(incomingMsg{
				senderID:      senderID,
				senderName:    senderName,
				text:          text,
				ts:            ts,
				replyNum:      replyNum,
				replySender:   replySender,
				replyText:     replyText,
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
		OnStatus: func(senderName, statusText string) {
			p.Send(statusUpdateMsg{senderName: senderName, statusText: statusText})
		},
		OnTopic: func(senderName, topicText string) {
			p.Send(topicUpdateMsg{senderName: senderName, topicText: topicText})
		},
		OnPin: func(senderName, pinText string) {
			p.Send(pinUpdateMsg{senderName: senderName, pinText: pinText})
		},
	}
}

func (m *Model) refreshSharedFiles() {
	var list []SharedFileItem
	idx := 1
	for _, msg := range m.messages {
		if strings.Contains(msg.Content, "Shared file:") {
			lines := strings.Split(msg.Content, "\n")
			fileName := "Shared File"
			fileURL := ""
			for _, l := range lines {
				trimmed := strings.TrimSpace(l)
				if strings.Contains(l, "Shared file:") {
					parts := strings.Split(l, "Shared file:")
					if len(parts) >= 2 {
						fileName = strings.TrimSpace(parts[1])
					}
				} else if strings.HasPrefix(trimmed, "🔗 ") {
					fileURL = strings.TrimSpace(strings.TrimPrefix(trimmed, "🔗 "))
				} else if strings.HasPrefix(trimmed, ":: ") && strings.HasPrefix(strings.TrimPrefix(trimmed, ":: "), "http") {
					fileURL = strings.TrimSpace(strings.TrimPrefix(trimmed, ":: "))
				}
			}
			if fileURL != "" {
				sender := msg.SenderName
				if msg.IsMe {
					sender = "You"
				}
				list = append(list, SharedFileItem{
					Index:    idx,
					FileName: fileName,
					URL:      fileURL,
					Sender:   sender,
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

func (m *Model) recalculateViewport() {
	headerHeight := 4
	inputHeight := 3
	transferHeight := 0
	if len(m.transfers) > 0 {
		transferHeight = 2
	}
	updateHeight := 0
	if m.updateStatus != "" {
		updateHeight = 1
	}

	vpHeight := m.height - headerHeight - inputHeight - transferHeight - updateHeight - 2
	if vpHeight < 4 {
		vpHeight = 4
	}

	vpWidth := m.width - 2
	if m.width >= 70 {
		switch m.sidebarMode {
		case SidebarNormal:
			vpWidth = m.width - 24
		case SidebarWide:
			vpWidth = m.width - 36
		case SidebarHidden:
			vpWidth = m.width - 2
		}
	}

	if vpWidth < 20 {
		vpWidth = 20
	}

	if !m.ready {
		m.viewport = viewport.New(vpWidth, vpHeight)
		m.viewport.KeyMap = viewport.KeyMap{}
		m.viewport.SetContent(m.renderMessages())
		m.ready = true
	} else {
		m.viewport.Width = vpWidth
		m.viewport.Height = vpHeight
		m.viewport.KeyMap = viewport.KeyMap{}
		m.viewport.SetContent(m.renderMessages())
	}
}

