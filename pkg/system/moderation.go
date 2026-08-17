package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// BanEntry records a banned user/device
type BanEntry struct {
	IdentityFingerprint string    `json:"fingerprint"`
	Name                string    `json:"name"`
	Reason              string    `json:"reason,omitempty"`
	BannedAt            time.Time `json:"banned_at"`
}

type BanList struct {
	mu      sync.RWMutex
	Entries map[string]BanEntry `json:"entries"`
}

var globalBanList = &BanList{
	Entries: make(map[string]BanEntry),
}

func getBanListPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".config", "termchat")
	_ = os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "banlist.json")
}

func LoadBanList() *BanList {
	path := getBanListPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return globalBanList
	}
	var entries map[string]BanEntry
	if err := json.Unmarshal(data, &entries); err == nil {
		globalBanList.mu.Lock()
		globalBanList.Entries = entries
		globalBanList.mu.Unlock()
	}
	return globalBanList
}

func SaveBanList() {
	path := getBanListPath()
	globalBanList.mu.RLock()
	data, err := json.MarshalIndent(globalBanList.Entries, "", "  ")
	globalBanList.mu.RUnlock()
	if err == nil {
		_ = os.WriteFile(path, data, 0600)
	}
}

func BanUser(fingerprint, name, reason string) {
	globalBanList.mu.Lock()
	globalBanList.Entries[fingerprint] = BanEntry{
		IdentityFingerprint: fingerprint,
		Name:                name,
		Reason:              reason,
		BannedAt:            time.Now(),
	}
	globalBanList.mu.Unlock()
	SaveBanList()
}

func UnbanUser(fingerprint string) bool {
	globalBanList.mu.Lock()
	_, exists := globalBanList.Entries[fingerprint]
	if exists {
		delete(globalBanList.Entries, fingerprint)
	}
	globalBanList.mu.Unlock()
	if exists {
		SaveBanList()
	}
	return exists
}

func IsBanned(fingerprint string) bool {
	globalBanList.mu.RLock()
	defer globalBanList.mu.RUnlock()
	_, exists := globalBanList.Entries[fingerprint]
	return exists
}

func GetBanList() []BanEntry {
	globalBanList.mu.RLock()
	defer globalBanList.mu.RUnlock()
	var list []BanEntry
	for _, entry := range globalBanList.Entries {
		list = append(list, entry)
	}
	return list
}
