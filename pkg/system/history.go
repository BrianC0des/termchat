package system

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
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

func getHistoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "./history.jsonl"
	}
	dir := filepath.Join(home, ".local", "share", "termchat")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "history.jsonl")
}

func AppendHistory(entry HistoryEntry) {
	path := getHistoryPath()
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

func LoadHistory(limit int) []HistoryEntry {
	path := getHistoryPath()
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
			entries = append(entries, entry)
		}
	}

	if limit > 0 && len(entries) > limit {
		return entries[len(entries)-limit:]
	}
	return entries
}
