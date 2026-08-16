package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type RelayPacket struct {
	Type      string `json:"type"`
	SenderID  string `json:"sender_id"`
	Sender    string `json:"sender"`
	Timestamp int64  `json:"timestamp"`
	ExtraData string `json:"extra_data,omitempty"`
}

type PeerInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Client struct {
	ID       string
	Name     string
	Room     string
	Conn     *websocket.Conn
	SendChan chan []byte
	mu       sync.Mutex
}

type Room struct {
	Name    string
	Clients map[string]*Client
	mu      sync.RWMutex
}

type Server struct {
	rooms       map[string]*Room
	roomsMu     sync.RWMutex
	uploadDir   string
	s3Client    *s3.Client
	r2Bucket    string
	r2PublicURL string
	useR2       bool
}

func generateID() string {
	bytes := make([]byte, 6)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func NewServer() *Server {
	uploadDir := "/tmp/termchat_uploads"
	_ = os.MkdirAll(uploadDir, 0755)

	s := &Server{
		rooms:     make(map[string]*Room),
		uploadDir: uploadDir,
	}

	// Initialize Cloudflare R2 / S3 if env vars provided
	r2AccountID := os.Getenv("R2_ACCOUNT_ID")
	r2AccessKey := os.Getenv("R2_ACCESS_KEY_ID")
	r2SecretKey := os.Getenv("R2_SECRET_ACCESS_KEY")
	r2Bucket := os.Getenv("R2_BUCKET_NAME")
	r2PublicURL := os.Getenv("R2_PUBLIC_URL")

	if r2AccountID != "" && r2AccessKey != "" && r2SecretKey != "" && r2Bucket != "" {
		customEndpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", r2AccountID)
		cfg, err := config.LoadDefaultConfig(context.TODO(),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(r2AccessKey, r2SecretKey, "")),
			config.WithRegion("auto"),
		)
		if err == nil {
			s.s3Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
				o.BaseEndpoint = aws.String(customEndpoint)
			})
			s.r2Bucket = r2Bucket
			s.r2PublicURL = r2PublicURL
			s.useR2 = true
			log.Printf("☁️ Cloudflare R2 Object Storage active (Bucket: %s)", r2Bucket)
		} else {
			log.Printf("⚠️ Failed to initialize R2 S3 client: %v", err)
		}
	} else {
		log.Printf("💾 Using local filesystem storage (%s)", uploadDir)
	}

	// Auto-cleanup local files older than 24h (if not using R2)
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			files, err := os.ReadDir(uploadDir)
			if err == nil {
				now := time.Now()
				for _, f := range files {
					info, err := f.Info()
					if err == nil && now.Sub(info.ModTime()) > 24*time.Hour {
						_ = os.RemoveAll(filepath.Join(uploadDir, f.Name()))
					}
				}
			}
		}
	}()

	return s
}

func (s *Server) getOrCreateRoom(name string) *Room {
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()

	room, exists := s.rooms[name]
	if !exists {
		room = &Room{
			Name:    name,
			Clients: make(map[string]*Client),
		}
		s.rooms[name] = room
	}
	return room
}

func (s *Server) removeClient(c *Client) {
	s.roomsMu.Lock()
	room, exists := s.rooms[c.Room]
	if exists {
		room.mu.Lock()
		delete(room.Clients, c.ID)
		count := len(room.Clients)
		room.mu.Unlock()

		if count == 0 {
			delete(s.rooms, c.Room)
		}
	}
	s.roomsMu.Unlock()

	// Broadcast leave to room
	leavePkt := RelayPacket{
		Type:      "peer_left",
		SenderID:  c.ID,
		Sender:    c.Name,
		Timestamp: time.Now().Unix(),
	}
	data, _ := json.Marshal(leavePkt)
	s.broadcastToRoom(c.Room, c.ID, data)
}

func (s *Server) broadcastToRoom(roomName string, senderID string, msg []byte) {
	s.roomsMu.RLock()
	room, exists := s.rooms[roomName]
	s.roomsMu.RUnlock()

	if !exists {
		return
	}

	room.mu.RLock()
	defer room.mu.RUnlock()

	for id, client := range room.Clients {
		if id != senderID {
			select {
			case client.SendChan <- msg:
			default:
			}
		}
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 100MB max memory/file
	err := r.ParseMultipartForm(100 << 20)
	if err != nil {
		http.Error(w, "File too large (max 100MB)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Invalid file form", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileID := generateID()
	cleanName := filepath.Base(header.Filename)

	proto := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
		proto = "http"
	}
	host := r.Host
	if r.Header.Get("X-Forwarded-Host") != "" {
		host = r.Header.Get("X-Forwarded-Host")
	}

	if s.useR2 {
		r2Key := fmt.Sprintf("files/%s/%s", fileID, cleanName)
		_, err := s.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
			Bucket:      aws.String(s.r2Bucket),
			Key:         aws.String(r2Key),
			Body:        file,
			ContentType: aws.String(header.Header.Get("Content-Type")),
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to upload to Cloudflare R2: %v", err), http.StatusInternalServerError)
			return
		}

		downloadURL := fmt.Sprintf("%s://%s/files/%s/%s", proto, host, fileID, url.PathEscape(cleanName))
		if s.r2PublicURL != "" {
			downloadURL = fmt.Sprintf("%s/%s", strings.TrimSuffix(s.r2PublicURL, "/"), r2Key)
		}

		resp := map[string]interface{}{
			"id":       fileID,
			"filename": cleanName,
			"size":     header.Size,
			"url":      downloadURL,
			"storage":  "cloudflare_r2",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Local filesystem storage fallback
	dirPath := filepath.Join(s.uploadDir, fileID)
	_ = os.MkdirAll(dirPath, 0755)

	destPath := filepath.Join(dirPath, cleanName)
	destFile, err := os.Create(destPath)
	if err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer destFile.Close()

	written, err := io.Copy(destFile, file)
	if err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}

	downloadURL := fmt.Sprintf("%s://%s/files/%s/%s", proto, host, fileID, url.PathEscape(cleanName))

	resp := map[string]interface{}{
		"id":       fileID,
		"filename": cleanName,
		"size":     written,
		"url":      downloadURL,
		"storage":  "local_disk",
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	unescapedPath, err := url.PathUnescape(r.URL.Path)
	if err != nil {
		unescapedPath = r.URL.Path
	}
	parts := strings.Split(strings.TrimPrefix(unescapedPath, "/files/"), "/")
	if len(parts) < 1 {
		http.NotFound(w, r)
		return
	}

	fileID := parts[0]
	fileName := ""
	if len(parts) >= 2 {
		fileName = parts[1]
	}

	// 1. If using Cloudflare R2
	if s.useR2 {
		r2Key := fmt.Sprintf("files/%s/%s", fileID, fileName)
		obj, err := s.s3Client.GetObject(r.Context(), &s3.GetObjectInput{
			Bucket: aws.String(s.r2Bucket),
			Key:    aws.String(r2Key),
		})
		if err != nil {
			// Fallback: search key with prefix
			prefix := fmt.Sprintf("files/%s/", fileID)
			listOut, lErr := s.s3Client.ListObjectsV2(r.Context(), &s3.ListObjectsV2Input{
				Bucket: aws.String(s.r2Bucket),
				Prefix: aws.String(prefix),
			})
			if lErr == nil && len(listOut.Contents) > 0 {
				r2Key = *listOut.Contents[0].Key
				parts := strings.Split(r2Key, "/")
				if len(parts) > 0 {
					fileName = parts[len(parts)-1]
				}
				obj, err = s.s3Client.GetObject(r.Context(), &s3.GetObjectInput{
					Bucket: aws.String(s.r2Bucket),
					Key:    aws.String(r2Key),
				})
			}
		}

		if err != nil || obj == nil {
			http.NotFound(w, r)
			return
		}
		defer obj.Body.Close()

		if obj.ContentType != nil {
			w.Header().Set("Content-Type", *obj.ContentType)
		}
		if obj.ContentLength != nil {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", *obj.ContentLength))
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
		_, _ = io.Copy(w, obj.Body)
		return
	}

	// 2. Local filesystem storage
	dirPath := filepath.Join(s.uploadDir, fileID)
	var targetFile string

	if fileName != "" {
		filePath := filepath.Join(dirPath, fileName)
		if _, err := os.Stat(filePath); err == nil {
			targetFile = filePath
		}
	}

	// Fallback: If not matched by exact name, find whatever file was uploaded in that fileID folder
	if targetFile == "" {
		entries, err := os.ReadDir(dirPath)
		if err == nil && len(entries) > 0 {
			targetFile = filepath.Join(dirPath, entries[0].Name())
			fileName = entries[0].Name()
		}
	}

	if targetFile == "" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	http.ServeFile(w, r, targetFile)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	roomName := r.URL.Query().Get("room")
	if roomName == "" {
		roomName = "general"
	}
	clientID := r.URL.Query().Get("id")
	if clientID == "" {
		clientID = fmt.Sprintf("peer-%d", time.Now().UnixNano())
	}
	clientName := r.URL.Query().Get("name")
	if clientName == "" {
		clientName = "Anonymous"
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}

	client := &Client{
		ID:       clientID,
		Name:     clientName,
		Room:     roomName,
		Conn:     conn,
		SendChan: make(chan []byte, 256),
	}

	room := s.getOrCreateRoom(roomName)
	room.mu.Lock()

	// Collect current peers in room
	var currentPeers []PeerInfo
	for _, c := range room.Clients {
		currentPeers = append(currentPeers, PeerInfo{ID: c.ID, Name: c.Name})
	}

	room.Clients[client.ID] = client
	room.mu.Unlock()

	log.Printf("🟢 [%s] %s (%s) connected. Total in room: %d", roomName, clientName, clientID, len(room.Clients))

	// Send current peers list to new client
	peersJSON, _ := json.Marshal(currentPeers)
	listPkt := RelayPacket{
		Type:      "peer_list",
		SenderID:  "server",
		Sender:    "Relay",
		Timestamp: time.Now().Unix(),
		ExtraData: string(peersJSON),
	}
	listData, _ := json.Marshal(listPkt)
	client.SendChan <- listData

	// Broadcast join to existing clients in room
	joinPkt := RelayPacket{
		Type:      "peer_joined",
		SenderID:  client.ID,
		Sender:    client.Name,
		Timestamp: time.Now().Unix(),
	}
	joinData, _ := json.Marshal(joinPkt)
	s.broadcastToRoom(roomName, client.ID, joinData)

	// Writer pump
	go func() {
		defer func() {
			_ = conn.Close()
		}()
		for msg := range client.SendChan {
			client.mu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err := conn.WriteMessage(websocket.TextMessage, msg)
			client.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	// Reader pump
	defer func() {
		s.removeClient(client)
		close(client.SendChan)
		_ = conn.Close()
		log.Printf("🔴 [%s] %s disconnected", roomName, clientName)
	}()

	conn.SetReadLimit(10 * 1024 * 1024)
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		s.broadcastToRoom(roomName, client.ID, msg)
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := NewServer()

	http.HandleFunc("/ws", server.handleWS)
	http.HandleFunc("/api/upload", server.handleUpload)
	http.HandleFunc("/files/", server.handleDownload)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		server.roomsMu.RLock()
		totalRooms := len(server.rooms)
		server.roomsMu.RUnlock()

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>TermChat Relay Server</title>
<style>
body { font-family: monospace; background: #1a1b26; color: #a9b1d6; padding: 40px; }
h1 { color: #7aa2f7; }
.card { background: #24283b; border-radius: 8px; padding: 20px; max-width: 500px; border: 1px solid #3b4261; }
.badge { color: #9ece6a; font-weight: bold; }
</style>
</head>
<body>
<div class="card">
  <h1>⚡ TermChat Relay Server</h1>
  <p>Status: <span class="badge">● ONLINE (24/7)</span></p>
  <p>Active Rooms: <b>%d</b></p>
  <p>WebSocket: <code>/ws?room=&lt;room_name&gt;&name=&lt;nickname&gt;&id=&lt;id&gt;</code></p>
  <p>File Storage: <code>/api/upload</code> & <code>/files/&lt;id&gt;/&lt;name&gt;</code></p>
</div>
</body>
</html>`, totalRooms)
	})

	log.Printf("⚡ TermChat Relay listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
