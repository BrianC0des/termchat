package system

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type HistoryEntry struct {
	SenderID   string    `json:"sender_id"`
	SenderName string    `json:"sender_name"`
	Content    string    `json:"content"`
	Timestamp  time.Time `json:"timestamp"`
	IsMe       bool      `json:"is_me"`
	IsSystem   bool      `json:"is_system"`
	IsFile     bool      `json:"is_file"`
}

var safeRoomRegex = regexp.MustCompile(`[^a-zA-Z0-9_\-]+`)

func sanitizeRoomName(room string) string {
	room = strings.TrimSpace(room)
	if room == "" {
		return "direct-lan"
	}
	safe := safeRoomRegex.ReplaceAllString(room, "_")
	if safe == "" {
		return "room"
	}
	return safe
}

func getRoomHistoryPath(room string) string {
	safeName := sanitizeRoomName(room)
	home, err := os.UserHomeDir()
	var dir string
	if err != nil {
		dir = "./termchat_data/rooms"
	} else {
		dir = filepath.Join(home, ".local", "share", "termchat", "rooms")
	}
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, safeName+".jsonl")
}

func AppendHistory(room string, entry HistoryEntry) {
	if entry.IsSystem {
		return // Never save system messages to chat history logs
	}
	path := getRoomHistoryPath(room)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer file.Close()

	data, err := json.Marshal(entry)
	if err == nil {
		_, _ = file.Write(append(data, '\n'))
	}
}

func LoadHistory(room string, limit int) []HistoryEntry {
	path := getRoomHistoryPath(room)
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var entries []HistoryEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err == nil {
			if !entry.IsSystem {
				entries = append(entries, entry)
			}
		}
	}

	if limit > 0 && len(entries) > limit {
		return entries[len(entries)-limit:]
	}
	return entries
}

// PurgeHistory securely removes all saved chat logs for a given room
func PurgeHistory(room string) {
	path := getRoomHistoryPath(room)
	_ = os.Remove(path)
}
