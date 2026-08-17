package network

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"termchat/pkg/system"

	"github.com/gorilla/websocket"
)

const (
	ChunkSize      = 32 * 1024 // 32KB per chunk
	DefaultTCPPort = 7332
)

type PeerConnection struct {
	ID        string
	Name      string
	RemoteIP  string
	Conn      net.Conn
	Writer    *bufio.Writer
	writeMu   sync.Mutex
	Connected time.Time
}

type FileTransferProgress struct {
	FileID     string
	FileName   string
	TotalBytes int64
	DoneBytes  int64
	IsIncoming bool
	IsDone     bool
	Error      string
	SavedPath  string
}

type NetworkEvents struct {
	OnMessage      func(senderID, senderName, text string, ts time.Time, replyNum int, replySender, replyText string)
	OnPeerJoin     func(id, name, addr string)
	OnPeerLeave    func(id, name string)
	OnSystemMsg    func(text string)
	OnFileProgress func(p FileTransferProgress)
	OnFileReceived func(fileName, savedPath string, size int64, senderName string)
	OnStatus       func(senderName, statusText string)
	OnTopic        func(senderName, topicText string)
	OnPin          func(senderName, pinText string)
}

type incomingFileState struct {
	fileID     string
	fileName   string
	fileSize   int64
	senderName string
	tempFile   *os.File
	written    int64
	targetPath string
}

type Manager struct {
	LocalID       string
	LocalName     string
	TCPPort       int
	DownloadDir   string
	EncryptionKey []byte
	RoomName      string
	RelayURL      string

	listener    net.Listener
	discovery   *DiscoveryService
	peers       map[string]*PeerConnection
	peersMu     sync.RWMutex
	events      NetworkEvents

	relayConn   *websocket.Conn
	relayMu     sync.Mutex

	cloudPeers   map[string]*PeerConnection
	cloudPeersMu sync.RWMutex

	incomingMu  sync.Mutex
	incomingMap map[string]*incomingFileState

	ctx    context.Context
	cancel context.CancelFunc
}

func GenerateID() string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func NewManager(name string, tcpPort, udpPort int, downloadDir string, events NetworkEvents) (*Manager, error) {
	if downloadDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			downloadDir = filepath.Join(home, "Downloads")
		} else {
			downloadDir = "./downloads"
		}
	}
	_ = os.MkdirAll(downloadDir, 0755)

	ctx, cancel := context.WithCancel(context.Background())
	id := GenerateID()

	m := &Manager{
		LocalID:     id,
		LocalName:   name,
		TCPPort:     tcpPort,
		DownloadDir: downloadDir,
		peers:       make(map[string]*PeerConnection),
		cloudPeers:  make(map[string]*PeerConnection),
		incomingMap: make(map[string]*incomingFileState),
		events:      events,
		ctx:         ctx,
		cancel:      cancel,
	}

	// Try listening on TCP port
	var ln net.Listener
	var err error
	for i := 0; i < 10; i++ {
		portToTry := tcpPort + i
		ln, err = net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", portToTry))
		if err == nil {
			m.TCPPort = portToTry
			m.listener = ln
			break
		}
	}
	if m.listener == nil {
		cancel()
		return nil, fmt.Errorf("failed to bind TCP port: %w", err)
	}

	// Initialize UDP Discovery
	m.discovery = NewDiscoveryService(
		m.LocalID,
		m.LocalName,
		m.TCPPort,
		udpPort,
		m.handleDiscoveredPeer,
		m.handleLostPeer,
	)

	return m, nil
}

func (m *Manager) SetEvents(events NetworkEvents) {
	m.events = events
}

func (m *Manager) SetEncryptionPassphrase(passphrase string) {
	if passphrase == "" {
		m.EncryptionKey = nil
	} else {
		m.EncryptionKey = system.DeriveKey(passphrase)
	}
}

func (m *Manager) Start() error {
	go m.acceptLoop()
	if err := m.discovery.Start(); err != nil {
		if m.events.OnSystemMsg != nil {
			m.events.OnSystemMsg(fmt.Sprintf("[WARN] UDP Discovery warning: %v (Direct connect still works)", err))
		}
	}
	return nil
}

func (m *Manager) Stop() {
	m.cancel()
	if m.discovery != nil {
		m.discovery.Stop()
	}
	if m.listener != nil {
		_ = m.listener.Close()
	}

	m.peersMu.Lock()
	for _, p := range m.peers {
		_ = p.Conn.Close()
	}
	m.peers = make(map[string]*PeerConnection)
	m.peersMu.Unlock()
}

func (m *Manager) SetName(newName string) {
	m.LocalName = newName
	if m.discovery != nil {
		m.discovery.UpdateName(newName)
	}
	// Broadcast new handshake to active peers
	m.peersMu.RLock()
	defer m.peersMu.RUnlock()
	p := &Packet{
		Type:      MsgTypeHandshake,
		SenderID:  m.LocalID,
		Sender:    newName,
		Timestamp: time.Now(),
	}
	for _, peer := range m.peers {
		_ = m.sendToPeer(peer, p)
	}
}

func (m *Manager) acceptLoop() {
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		conn, err := m.listener.Accept()
		if err != nil {
			continue
		}

		go m.handleIncomingConnection(conn)
	}
}

func (m *Manager) handleDiscoveredPeer(peer DiscoveredPeer) {
	if peer.ID == m.LocalID {
		return
	}

	m.peersMu.RLock()
	_, exists := m.peers[peer.ID]
	m.peersMu.RUnlock()

	if exists {
		return
	}

	// Tie-breaking: only smaller UUID initiates TCP connection to prevent dual connection races
	if strings.Compare(m.LocalID, peer.ID) < 0 {
		go m.ConnectTo(peer.Addr())
	}
}

func (m *Manager) handleLostPeer(peerID string) {
	// UDP lost does not close healthy TCP connections
}

func (m *Manager) ConnectTo(addr string) {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(15 * time.Second)
		_ = tcpConn.SetReadBuffer(64 * 1024)
		_ = tcpConn.SetWriteBuffer(64 * 1024)
	}

	peerConn := &PeerConnection{
		RemoteIP:  addr,
		Conn:      conn,
		Writer:    bufio.NewWriterSize(conn, 64*1024),
		Connected: time.Now(),
	}

	// Send handshake
	handshake := &Packet{
		Type:      MsgTypeHandshake,
		SenderID:  m.LocalID,
		Sender:    m.LocalName,
		Timestamp: time.Now(),
	}
	if err := m.sendToPeer(peerConn, handshake); err != nil {
		_ = conn.Close()
		return
	}

	go m.readLoop(peerConn)
}

func (m *Manager) handleIncomingConnection(conn net.Conn) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(15 * time.Second)
		_ = tcpConn.SetReadBuffer(64 * 1024)
		_ = tcpConn.SetWriteBuffer(64 * 1024)
	}

	peerConn := &PeerConnection{
		RemoteIP:  conn.RemoteAddr().String(),
		Conn:      conn,
		Writer:    bufio.NewWriterSize(conn, 64*1024),
		Connected: time.Now(),
	}

	// Send local handshake
	handshake := &Packet{
		Type:      MsgTypeHandshake,
		SenderID:  m.LocalID,
		Sender:    m.LocalName,
		Timestamp: time.Now(),
	}
	if err := m.sendToPeer(peerConn, handshake); err != nil {
		_ = conn.Close()
		return
	}

	m.readLoop(peerConn)
}

func (m *Manager) readLoop(p *PeerConnection) {
	reader := bufio.NewReader(p.Conn)
	defer func() {
		_ = p.Conn.Close()
		wasActive := false
		m.peersMu.Lock()
		if p.ID != "" {
			if curr, ok := m.peers[p.ID]; ok && curr == p {
				delete(m.peers, p.ID)
				wasActive = true
			}
		}
		m.peersMu.Unlock()
		if wasActive && m.events.OnPeerLeave != nil {
			m.events.OnPeerLeave(p.ID, p.Name)
		}
	}()

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		packet, err := DecodePacket(line)
		if err != nil {
			continue
		}

		m.handlePacket(p, packet)
	}
}

func (m *Manager) handlePacket(p *PeerConnection, pkt *Packet) {
	// Handle encrypted wrapper packet
	if pkt.Type == MsgTypeEncrypted {
		if m.EncryptionKey == nil {
			if m.events.OnSystemMsg != nil {
				m.events.OnSystemMsg("[AES-256] Received encrypted packet but no passphrase is set. Use `/auth <passphrase>`")
			}
			return
		}
		decryptedJSON, err := system.Decrypt(pkt.Content, m.EncryptionKey)
		if err != nil {
			if m.events.OnSystemMsg != nil {
				m.events.OnSystemMsg("[ERR] Decryption failed: wrong passphrase or corrupted data")
			}
			return
		}
		var innerPkt Packet
		if err := json.Unmarshal([]byte(decryptedJSON), &innerPkt); err == nil {
			pkt = &innerPkt
		}
	}

	// Strict Room isolation filter:
	// If packet is from direct LAN:
	// - If we are in a Cloud Room (m.RoomName != ""): ignore LAN traffic so LAN devices don't leak into private room!
	// - If we are in LAN mode (m.RoomName == ""): ignore private cloud room traffic.
	if p.RemoteIP != "cloud-relay" && p.RemoteIP != "Cloud" {
		if m.RoomName != "" && pkt.Room != m.RoomName && pkt.Type != MsgTypeHandshake && pkt.Type != MsgTypePing && pkt.Type != MsgTypePong {
			return
		}
		if m.RoomName == "" && pkt.Room != "" && pkt.Type != MsgTypeHandshake && pkt.Type != MsgTypePing && pkt.Type != MsgTypePong {
			return
		}
	}

	// Auto-register peer in cloudPeers whenever any packet is received from them
	if m.RoomName != "" && pkt.SenderID != "" && pkt.Sender != "" && pkt.SenderID != m.LocalID {
		m.cloudPeersMu.Lock()
		m.cloudPeers[pkt.SenderID] = &PeerConnection{
			ID:       pkt.SenderID,
			Name:     pkt.Sender,
			RemoteIP: "Cloud",
		}
		m.cloudPeersMu.Unlock()
	}

	switch pkt.Type {
	case MsgTypeHandshake:
		if pkt.SenderID == m.LocalID {
			if p.Conn != nil {
				_ = p.Conn.Close()
			}
			return
		}

		p.ID = pkt.SenderID
		p.Name = pkt.Sender

		if m.RoomName != "" {
			m.cloudPeersMu.Lock()
			m.cloudPeers[p.ID] = p
			m.cloudPeersMu.Unlock()
		} else {
			m.peersMu.Lock()
			if old, exists := m.peers[p.ID]; exists && old != p {
				_ = old.Conn.Close()
			}
			m.peers[p.ID] = p
			m.peersMu.Unlock()
		}

		if m.events.OnPeerJoin != nil {
			m.events.OnPeerJoin(p.ID, p.Name, p.RemoteIP)
		}

	case MsgTypeChat:
		if m.events.OnMessage != nil {
			m.events.OnMessage(pkt.SenderID, pkt.Sender, pkt.Content, pkt.Timestamp, pkt.ReplyToNum, pkt.ReplyToSender, pkt.ReplyToText)
		}

	case MsgTypeStatus:
		if m.events.OnStatus != nil {
			m.events.OnStatus(pkt.Sender, pkt.Content)
		}

	case MsgTypeTopic:
		if m.events.OnTopic != nil {
			m.events.OnTopic(pkt.Sender, pkt.Content)
		}

	case MsgTypePin:
		if m.events.OnPin != nil {
			m.events.OnPin(pkt.Sender, pkt.Content)
		}

	case MsgTypeClipboard:
		_ = system.WriteClipboard(pkt.Content)
		if m.events.OnSystemMsg != nil {
			m.events.OnSystemMsg(fmt.Sprintf("[CLIP] Clipboard synced from %s (%d chars)", pkt.Sender, len(pkt.Content)))
		}

	case MsgTypeNotify:
		_ = system.SendNotification(fmt.Sprintf("TermChat from %s", pkt.Sender), pkt.Content)

	case MsgTypeRing:
		_ = system.TriggerRing()
		if m.events.OnSystemMsg != nil {
			m.events.OnSystemMsg(fmt.Sprintf("[NOTIFY] %s triggered your device alert/ring!", pkt.Sender))
		}

	case MsgTypeOpenUrl:
		_ = system.OpenURL(pkt.URL)
		if m.events.OnSystemMsg != nil {
			m.events.OnSystemMsg(fmt.Sprintf("[NET] %s opened URL: %s", pkt.Sender, pkt.URL))
		}

	case MsgTypeMedia:
		result, err := system.MediaControl(pkt.Action)
		msg := result
		if err != nil {
			msg = fmt.Sprintf("[ERR] Media control: %v", err)
		}
		if m.events.OnSystemMsg != nil {
			m.events.OnSystemMsg(fmt.Sprintf("%s (%s)", msg, pkt.Sender))
		}

	case MsgTypeFileOffer:
		m.handleFileOffer(p, pkt)

	case MsgTypeFileChunk:
		m.handleFileChunk(p, pkt)

	case MsgTypeFileDone:
		m.handleFileDone(p, pkt)

	case MsgTypeFileCancel:
		m.handleFileCancel(p, pkt)
	}
}

func (m *Manager) handleFileOffer(p *PeerConnection, pkt *Packet) {
	safeName := filepath.Base(pkt.FileName)
	if safeName == "" || safeName == "." || safeName == "/" {
		safeName = "received_file"
	}

	targetPath := getUniqueFilePath(m.DownloadDir, safeName)
	tempPath := targetPath + ".part"

	tmpFile, err := os.Create(tempPath)
	if err != nil {
		if m.events.OnSystemMsg != nil {
			m.events.OnSystemMsg(fmt.Sprintf("[ERR] Error creating file: %v", err))
		}
		return
	}

	state := &incomingFileState{
		fileID:     pkt.FileID,
		fileName:   safeName,
		fileSize:   pkt.FileSize,
		senderName: pkt.Sender,
		tempFile:   tmpFile,
		written:    0,
		targetPath: targetPath,
	}

	m.incomingMu.Lock()
	m.incomingMap[pkt.FileID] = state
	m.incomingMu.Unlock()

	if m.events.OnFileProgress != nil {
		m.events.OnFileProgress(FileTransferProgress{
			FileID:     pkt.FileID,
			FileName:   safeName,
			TotalBytes: pkt.FileSize,
			DoneBytes:  0,
			IsIncoming: true,
			IsDone:     false,
		})
	}
}

func (m *Manager) handleFileChunk(p *PeerConnection, pkt *Packet) {
	m.incomingMu.Lock()
	state, exists := m.incomingMap[pkt.FileID]
	m.incomingMu.Unlock()

	if !exists || state.tempFile == nil {
		return
	}

	data, err := base64.StdEncoding.DecodeString(pkt.ChunkData)
	if err != nil {
		return
	}

	n, err := state.tempFile.Write(data)
	if err != nil {
		return
	}
	state.written += int64(n)

	if m.events.OnFileProgress != nil {
		m.events.OnFileProgress(FileTransferProgress{
			FileID:     pkt.FileID,
			FileName:   state.fileName,
			TotalBytes: state.fileSize,
			DoneBytes:  state.written,
			IsIncoming: true,
			IsDone:     false,
		})
	}
}

func (m *Manager) handleFileDone(p *PeerConnection, pkt *Packet) {
	m.incomingMu.Lock()
	state, exists := m.incomingMap[pkt.FileID]
	delete(m.incomingMap, pkt.FileID)
	m.incomingMu.Unlock()

	if !exists || state.tempFile == nil {
		return
	}

	_ = state.tempFile.Close()
	tempPath := state.tempFile.Name()
	_ = os.Rename(tempPath, state.targetPath)

	if m.events.OnFileProgress != nil {
		m.events.OnFileProgress(FileTransferProgress{
			FileID:     pkt.FileID,
			FileName:   state.fileName,
			TotalBytes: state.fileSize,
			DoneBytes:  state.written,
			IsIncoming: true,
			IsDone:     true,
			SavedPath:  state.targetPath,
		})
	}

	if m.events.OnFileReceived != nil {
		m.events.OnFileReceived(state.fileName, state.targetPath, state.written, state.senderName)
	}
}

func (m *Manager) handleFileCancel(p *PeerConnection, pkt *Packet) {
	m.incomingMu.Lock()
	state, exists := m.incomingMap[pkt.FileID]
	delete(m.incomingMap, pkt.FileID)
	m.incomingMu.Unlock()

	if exists && state.tempFile != nil {
		_ = state.tempFile.Close()
		_ = os.Remove(state.tempFile.Name())
	}
}

func (m *Manager) ConnectRelay(relayURL, roomName string) {
	if relayURL == "" {
		return
	}
	m.RelayURL = relayURL
	m.RoomName = roomName

	go func() {
		for {
			select {
			case <-m.ctx.Done():
				return
			default:
			}

			u := fmt.Sprintf("%s?room=%s&name=%s&id=%s", relayURL, roomName, m.LocalName, m.LocalID)
			if !strings.HasPrefix(u, "ws://") && !strings.HasPrefix(u, "wss://") {
				u = "wss://" + u
			}

			dialer := websocket.Dialer{
				NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					var d net.Dialer
					c, err := d.DialContext(ctx, network, addr)
					if err != nil {
						return nil, err
					}
					if tcpConn, ok := c.(*net.TCPConn); ok {
						_ = tcpConn.SetNoDelay(true)
						_ = tcpConn.SetKeepAlive(true)
						_ = tcpConn.SetKeepAlivePeriod(15 * time.Second)
						_ = tcpConn.SetReadBuffer(64 * 1024)
						_ = tcpConn.SetWriteBuffer(64 * 1024)
					}
					return c, nil
				},
				HandshakeTimeout: 8 * time.Second,
			}

			conn, _, err := dialer.Dial(u, nil)
			if err != nil {
				time.Sleep(1 * time.Second)
				continue
			}

			m.relayMu.Lock()
			m.relayConn = conn
			m.relayMu.Unlock()

			if m.events.OnSystemMsg != nil {
				m.events.OnSystemMsg(fmt.Sprintf("[ROOM] Connected to Cloud Room #%s (24/7 Global)", roomName))
			}

			// Broadcast presence to all room peers
			_ = m.SendPacket(&Packet{
				Type:      MsgTypeHandshake,
				SenderID:  m.LocalID,
				Sender:    m.LocalName,
				Timestamp: time.Now(),
			})

			// Keep-alive heartbeat pinger
			stopHeartbeat := make(chan struct{})
			go func() {
				ticker := time.NewTicker(15 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-stopHeartbeat:
						return
					case <-m.ctx.Done():
						return
					case <-ticker.C:
						m.relayMu.Lock()
						rc := m.relayConn
						m.relayMu.Unlock()
						if rc != nil {
							_ = rc.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
						}
					}
				}
			}()

			// Read loop from relay
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					break
				}
				pkt, err := DecodePacket(data)
				if err != nil {
					continue
				}
				if pkt.SenderID == m.LocalID {
					continue
				}

				if pkt.Type == "peer_list" {
					var peers []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					}
					if err := json.Unmarshal([]byte(pkt.ExtraData), &peers); err == nil {
						m.cloudPeersMu.Lock()
						m.cloudPeers = make(map[string]*PeerConnection)
						for _, p := range peers {
							if p.ID != m.LocalID && p.Name != "" {
								m.cloudPeers[p.ID] = &PeerConnection{
									ID:       p.ID,
									Name:     p.Name,
									RemoteIP: "Cloud",
								}
							}
						}
						m.cloudPeersMu.Unlock()
						if m.events.OnPeerJoin != nil {
							m.events.OnPeerJoin("", "", "")
						}
					}
					continue
				}

				if pkt.Type == "peer_joined" {
					m.cloudPeersMu.Lock()
					m.cloudPeers[pkt.SenderID] = &PeerConnection{
						ID:       pkt.SenderID,
						Name:     pkt.Sender,
						RemoteIP: "Cloud",
					}
					m.cloudPeersMu.Unlock()
					if m.events.OnPeerJoin != nil {
						m.events.OnPeerJoin(pkt.SenderID, pkt.Sender, "Cloud")
					}
					continue
				}

				if pkt.Type == "peer_left" {
					m.cloudPeersMu.Lock()
					delete(m.cloudPeers, pkt.SenderID)
					m.cloudPeersMu.Unlock()
					if m.events.OnPeerLeave != nil {
						m.events.OnPeerLeave(pkt.SenderID, pkt.Sender)
					}
					continue
				}

				m.handlePacket(&PeerConnection{ID: pkt.SenderID, Name: pkt.Sender, RemoteIP: "cloud-relay"}, pkt)
			}

			close(stopHeartbeat)

			m.relayMu.Lock()
			m.relayConn = nil
			m.relayMu.Unlock()

			m.cloudPeersMu.Lock()
			m.cloudPeers = make(map[string]*PeerConnection)
			m.cloudPeersMu.Unlock()

			time.Sleep(1 * time.Second)
		}
	}()
}

func (m *Manager) LeaveRoom() {
	m.relayMu.Lock()
	if m.relayConn != nil {
		_ = m.relayConn.Close()
		m.relayConn = nil
	}
	m.relayMu.Unlock()

	m.RoomName = ""
	m.cloudPeersMu.Lock()
	m.cloudPeers = make(map[string]*PeerConnection)
	m.cloudPeersMu.Unlock()
}

func (m *Manager) SendPacket(p *Packet) error {
	p.Room = m.RoomName
	data, err := EncodePacket(p)
	if err != nil {
		return err
	}

	// 1. If in Cloud Room mode: send ONLY to Cloud Relay
	if m.RoomName != "" {
		m.relayMu.Lock()
		if m.relayConn != nil {
			_ = m.relayConn.WriteMessage(websocket.TextMessage, data)
		}
		m.relayMu.Unlock()
	} else {
		// 2. If in pure Offline LAN mode: send ONLY to direct LAN peers
		m.peersMu.RLock()
		for _, peer := range m.peers {
			_ = m.sendToPeer(peer, p)
		}
		m.peersMu.RUnlock()
	}

	return nil
}

func (m *Manager) SendChat(text string) error {
	p := &Packet{
		Type:      MsgTypeChat,
		SenderID:  m.LocalID,
		Sender:    m.LocalName,
		Timestamp: time.Now(),
		Content:   text,
	}

	// Encrypt if passphrase is configured
	if m.EncryptionKey != nil {
		rawJSON, _ := json.Marshal(p)
		encryptedText, err := system.Encrypt(string(rawJSON), m.EncryptionKey)
		if err == nil {
			p = &Packet{
				Type:      MsgTypeEncrypted,
				SenderID:  m.LocalID,
				Sender:    m.LocalName,
				Timestamp: time.Now(),
				Content:   encryptedText,
			}
		}
	}

	return m.SendPacket(p)
}

func (m *Manager) SendBotChat(botName, text string) error {
	p := &Packet{
		Type:      MsgTypeChat,
		SenderID:  "agy-bot",
		Sender:    botName,
		Timestamp: time.Now(),
		Content:   text,
	}
	return m.SendPacket(p)
}

func (m *Manager) UploadFileToRelay(filePath string, expiry string) (string, string, int64, string, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return "", "", 0, "", err
	}
	file, err := os.Open(filePath)
	if err != nil {
		return "", "", 0, "", err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", "", 0, "", err
	}

	_, err = io.Copy(part, file)
	if err != nil {
		return "", "", 0, "", err
	}
	if expiry == "" {
		expiry = "24h"
	}
	_ = writer.WriteField("expiry", expiry)
	_ = writer.Close()

	uploadURL := "https://termchat-o51d.onrender.com/api/upload"
	if m.RelayURL != "" {
		u := m.RelayURL
		u = strings.Replace(u, "wss://", "https://", 1)
		u = strings.Replace(u, "ws://", "http://", 1)
		if idx := strings.Index(u, "/ws"); idx != -1 {
			u = u[:idx]
		}
		uploadURL = u + "/api/upload"
	}

	req, err := http.NewRequest("POST", uploadURL, body)
	if err != nil {
		return "", "", 0, "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", "", 0, "", fmt.Errorf("upload failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var res struct {
		ID        string `json:"id"`
		FileName  string `json:"filename"`
		Size      int64  `json:"size"`
		URL       string `json:"url"`
		ExpiresIn string `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", "", 0, "", err
	}
	if res.ExpiresIn == "" {
		res.ExpiresIn = expiry
	}

	return res.URL, res.FileName, fileInfo.Size(), res.ExpiresIn, nil
}

func (m *Manager) UploadFileToRelayLegacy(filePath string) (string, string, int64, error) {
	u, fn, sz, _, err := m.UploadFileToRelay(filePath, "24h")
	return u, fn, sz, err
}

func (m *Manager) DownloadFileFromURL(fileURL string) (string, error) {
	// Clean and parse URL
	u, err := url.Parse(fileURL)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "TermChat-Client/1.1")

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("file expired or was cleared during server restart (please ask sender to re-share)")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	fileName := filepath.Base(u.Path)
	if unescaped, err := url.PathUnescape(fileName); err == nil && unescaped != "" {
		fileName = unescaped
	}
	if idx := strings.Index(fileName, "?"); idx != -1 {
		fileName = fileName[:idx]
	}
	if fileName == "" || fileName == "." || fileName == "/" {
		fileName = "downloaded_file"
	}

	targetPath := getUniqueFilePath(m.DownloadDir, fileName)
	out, err := os.Create(targetPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	totalSize := resp.ContentLength
	fileID := GenerateID()

	if m.events.OnFileProgress != nil {
		m.events.OnFileProgress(FileTransferProgress{
			FileID:     fileID,
			FileName:   fileName,
			TotalBytes: totalSize,
			DoneBytes:  0,
			IsIncoming: true,
			IsDone:     false,
		})
	}

	buf := make([]byte, 32*1024)
	var written int64

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			wN, wErr := out.Write(buf[:n])
			if wErr != nil {
				return "", wErr
			}
			written += int64(wN)
			if m.events.OnFileProgress != nil {
				m.events.OnFileProgress(FileTransferProgress{
					FileID:     fileID,
					FileName:   fileName,
					TotalBytes: totalSize,
					DoneBytes:  written,
					IsIncoming: true,
					IsDone:     false,
				})
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return "", readErr
		}
	}

	if m.events.OnFileProgress != nil {
		m.events.OnFileProgress(FileTransferProgress{
			FileID:     fileID,
			FileName:   fileName,
			TotalBytes: totalSize,
			DoneBytes:  written,
			IsIncoming: true,
			IsDone:     true,
		})
	}

	if m.events.OnFileReceived != nil {
		m.events.OnFileReceived(fileName, targetPath, written, "Cloud")
	}

	return targetPath, nil
}

func zipDirectory(sourceDir, targetZip string) error {
	zipFile, err := os.Create(targetZip)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	archive := zip.NewWriter(zipFile)
	defer archive.Close()

	sourceDir = filepath.Clean(sourceDir)

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		header.Name = filepath.ToSlash(relPath)
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(writer, file)
		return err
	})
}

func (m *Manager) SendFile(filePath string) error {
	return m.SendFileWithExpiry(filePath, "24h")
}

func (m *Manager) SendFileWithExpiry(filePath, expiry string) error {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("cannot access file: %w", err)
	}

	fileName := filepath.Base(filePath)
	fileSize := fileInfo.Size()

	var isTempZip bool
	var toCleanZip string

	if fileInfo.IsDir() {
		zipName := filepath.Base(filePath) + ".zip"
		tempZip := filepath.Join(os.TempDir(), fmt.Sprintf("termchat_%d_%s", time.Now().UnixNano(), zipName))
		if m.events.OnSystemMsg != nil {
			m.events.OnSystemMsg(fmt.Sprintf("[ZIP] Auto-compressing folder '%s' into ZIP archive...", filepath.Base(filePath)))
		}
		if err := zipDirectory(filePath, tempZip); err != nil {
			return fmt.Errorf("failed to compress directory: %w", err)
		}
		filePath = tempZip
		fileName = zipName
		if zInfo, err := os.Stat(tempZip); err == nil {
			fileSize = zInfo.Size()
		}
		isTempZip = true
		toCleanZip = tempZip
	}

	badge := GetFileIcon(fileName)

	// 1. If in Cloud Room mode: upload to cloud and share interactive download card
	if m.RoomName != "" {
		go func() {
			if isTempZip {
				defer func() {
					if toCleanZip != "" {
						_ = os.Remove(toCleanZip)
					}
				}()
			}
			if m.events.OnSystemMsg != nil {
				m.events.OnSystemMsg(fmt.Sprintf("[NET] Uploading %s '%s' (%s) [Expires: %s]...", badge, fileName, FormatBytes(fileSize), expiry))
			}
			dlURL, _, _, expiresIn, err := m.UploadFileToRelay(filePath, expiry)
			if err != nil {
				if m.events.OnSystemMsg != nil {
					m.events.OnSystemMsg(fmt.Sprintf("[ERR] Upload failed: %v", err))
				}
				return
			}
			shareMsg := fmt.Sprintf("%s Shared file: %s (%s)\n:: %s\n[TTL] Auto-Expires in: %s\n:: Type `/get %s` or click the link to download", badge, fileName, FormatBytes(fileSize), dlURL, expiresIn, dlURL)
			_ = m.SendChat(shareMsg)
			if m.events.OnMessage != nil {
				m.events.OnMessage(m.LocalID, m.LocalName, shareMsg, time.Now(), 0, "", "")
			}
			if m.events.OnSystemMsg != nil {
				m.events.OnSystemMsg(fmt.Sprintf("[OK] Uploaded '%s' to room #%s (Expires in %s)!", fileName, m.RoomName, expiresIn))
			}
		}()
		return nil
	}

	// 2. If in pure LAN mode: direct TCP peer streaming
	m.peersMu.RLock()
	peersCount := len(m.peers)
	peersList := make([]*PeerConnection, 0, peersCount)
	for _, p := range m.peers {
		peersList = append(peersList, p)
	}
	m.peersMu.RUnlock()

	if peersCount == 0 {
		return fmt.Errorf("no connected peers to send file to")
	}

	fileID := GenerateID()

	go func() {
		file, err := os.Open(filePath)
		if err != nil {
			if m.events.OnSystemMsg != nil {
				m.events.OnSystemMsg(fmt.Sprintf("[ERR] Error opening file: %v", err))
			}
			return
		}
		defer file.Close()

		offer := &Packet{
			Type:      MsgTypeFileOffer,
			SenderID:  m.LocalID,
			Sender:    m.LocalName,
			Timestamp: time.Now(),
			FileID:    fileID,
			FileName:  fileName,
			FileSize:  fileSize,
		}
		for _, peer := range peersList {
			_ = m.sendToPeer(peer, offer)
		}

		if m.events.OnFileProgress != nil {
			m.events.OnFileProgress(FileTransferProgress{
				FileID:     fileID,
				FileName:   fileName,
				TotalBytes: fileSize,
				DoneBytes:  0,
				IsIncoming: false,
				IsDone:     false,
			})
		}

		buf := make([]byte, ChunkSize)
		var totalSent int64
		chunkIdx := 0

		for {
			select {
			case <-m.ctx.Done():
				return
			default:
			}

			n, readErr := file.Read(buf)
			if n > 0 {
				chunkBase64 := base64.StdEncoding.EncodeToString(buf[:n])
				totalSent += int64(n)
				isLast := (readErr == io.EOF)

				chunkPkt := &Packet{
					Type:       MsgTypeFileChunk,
					SenderID:   m.LocalID,
					Sender:     m.LocalName,
					FileID:     fileID,
					ChunkIndex: chunkIdx,
					ChunkData:  chunkBase64,
					IsLast:     isLast,
				}

				for _, peer := range peersList {
					_ = m.sendToPeer(peer, chunkPkt)
				}

				chunkIdx++

				if m.events.OnFileProgress != nil {
					m.events.OnFileProgress(FileTransferProgress{
						FileID:     fileID,
						FileName:   fileName,
						TotalBytes: fileSize,
						DoneBytes:  totalSent,
						IsIncoming: false,
						IsDone:     false,
					})
				}

				time.Sleep(2 * time.Millisecond)
			}

			if readErr != nil {
				break
			}
		}

		donePkt := &Packet{
			Type:      MsgTypeFileDone,
			SenderID:  m.LocalID,
			Sender:    m.LocalName,
			FileID:    fileID,
			FileName:  fileName,
			FileSize:  fileSize,
		}
		for _, peer := range peersList {
			_ = m.sendToPeer(peer, donePkt)
		}

		if m.events.OnFileProgress != nil {
			m.events.OnFileProgress(FileTransferProgress{
				FileID:     fileID,
				FileName:   fileName,
				TotalBytes: fileSize,
				DoneBytes:  fileSize,
				IsIncoming: false,
				IsDone:     true,
			})
		}

		if m.events.OnSystemMsg != nil {
			m.events.OnSystemMsg(fmt.Sprintf("[SEND] Finished sending '%s' (%s)", fileName, FormatBytes(fileSize)))
		}
	}()

	return nil
}

func (m *Manager) sendToPeer(peer *PeerConnection, p *Packet) error {
	data, err := EncodePacket(p)
	if err != nil {
		return err
	}

	peer.writeMu.Lock()
	defer peer.writeMu.Unlock()

	_, err = peer.Writer.Write(data)
	if err != nil {
		return err
	}
	return peer.Writer.Flush()
}

func (m *Manager) GetPeers() []PeerConnection {
	if m.RoomName != "" {
		// In Cloud Room mode: show only members connected to this room
		m.cloudPeersMu.RLock()
		list := make([]PeerConnection, 0, len(m.cloudPeers))
		for _, p := range m.cloudPeers {
			list = append(list, *p)
		}
		m.cloudPeersMu.RUnlock()
		return list
	}

	// In Offline LAN mode: show only local LAN peers
	m.peersMu.RLock()
	list := make([]PeerConnection, 0, len(m.peers))
	for _, p := range m.peers {
		list = append(list, *p)
	}
	m.peersMu.RUnlock()
	return list
}

func getUniqueFilePath(dir, baseName string) string {
	target := filepath.Join(dir, baseName)
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return target
	}

	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)
	counter := 1

	for {
		newTarget := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", nameWithoutExt, counter, ext))
		if _, err := os.Stat(newTarget); os.IsNotExist(err) {
			return newTarget
		}
		counter++
	}
}

func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func (m *Manager) SendReply(replyToNum int, replyToSender, replyToText, content string) error {
	p := &Packet{
		Type:          MsgTypeChat,
		SenderID:      m.LocalID,
		Sender:        m.LocalName,
		Timestamp:     time.Now(),
		Content:       content,
		ReplyToNum:    replyToNum,
		ReplyToSender: replyToSender,
		ReplyToText:   replyToText,
	}

	if m.EncryptionKey != nil {
		rawJSON, _ := json.Marshal(p)
		encryptedText, err := system.Encrypt(string(rawJSON), m.EncryptionKey)
		if err == nil {
			p = &Packet{
				Type:          MsgTypeEncrypted,
				SenderID:      m.LocalID,
				Sender:        m.LocalName,
				Timestamp:     time.Now(),
				Content:       encryptedText,
				ReplyToNum:    replyToNum,
				ReplyToSender: replyToSender,
				ReplyToText:   replyToText,
			}
		}
	}

	return m.SendPacket(p)
}

func (m *Manager) SendStatus(statusText string) error {
	p := &Packet{
		Type:      MsgTypeStatus,
		SenderID:  m.LocalID,
		Sender:    m.LocalName,
		Timestamp: time.Now(),
		Content:   statusText,
	}
	return m.SendPacket(p)
}

func (m *Manager) SendTopic(topicText string) error {
	p := &Packet{
		Type:      MsgTypeTopic,
		SenderID:  m.LocalID,
		Sender:    m.LocalName,
		Timestamp: time.Now(),
		Content:   topicText,
	}
	return m.SendPacket(p)
}

func (m *Manager) SendPin(pinText string) error {
	p := &Packet{
		Type:      MsgTypePin,
		SenderID:  m.LocalID,
		Sender:    m.LocalName,
		Timestamp: time.Now(),
		Content:   pinText,
	}
	return m.SendPacket(p)
}
