package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for terminal clients
	},
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
	rooms   map[string]*Room
	roomsMu sync.RWMutex
}

func NewServer() *Server {
	return &Server{
		rooms: make(map[string]*Room),
	}
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
				// Channel full, drop or close
			}
		}
	}
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
	room.Clients[client.ID] = client
	room.mu.Unlock()

	log.Printf("🟢 [%s] %s (%s) connected. Total in room: %d", roomName, clientName, clientID, len(room.Clients))

	// Start writer pump
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

	conn.SetReadLimit(10 * 1024 * 1024) // 10MB per packet max
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
  <p>WebSocket Endpoint: <code>/ws?room=&lt;room_name&gt;&name=&lt;nickname&gt;&id=&lt;id&gt;</code></p>
</div>
</body>
</html>`, totalRooms)
	})

	log.Printf("⚡ TermChat Relay listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
