package system

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	LastRemotePeer string `json:"last_remote_peer,omitempty"`
	Nickname       string `json:"nickname,omitempty"`
	DownloadDir    string `json:"download_dir,omitempty"`
	AuthPassphrase string `json:"auth_passphrase,omitempty"`
	Theme          string `json:"theme,omitempty"`
}

func getConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "./config.json"
	}
	dir := filepath.Join(home, ".config", "termchat")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "config.json")
}

func LoadConfig() Config {
	path := getConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}
	}
	var cfg Config
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

func SaveConfig(cfg Config) {
	path := getConfigPath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err == nil {
		_ = os.WriteFile(path, data, 0644)
	}
}
