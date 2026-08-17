package system

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type BatteryInfo struct {
	Percentage int    `json:"percentage"`
	Status     string `json:"status"`
	Plugged    string `json:"plugged"`
	Health     string `json:"health,omitempty"`
}

// GetBatteryInfo queries battery on Android (via termux-battery-status) or Linux (/sys/class/power_supply)
func GetBatteryInfo() (*BatteryInfo, error) {
	if isCommandAvailable("termux-battery-status") {
		cmd := exec.Command("termux-battery-status")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			var raw struct {
				Percentage int    `json:"percentage"`
				Status     string `json:"status"`
				Plugged    string `json:"plugged"`
				Health     string `json:"health"`
			}
			if err := json.Unmarshal(out.Bytes(), &raw); err == nil {
				return &BatteryInfo{
					Percentage: raw.Percentage,
					Status:     raw.Status,
					Plugged:    raw.Plugged,
					Health:     raw.Health,
				}, nil
			}
		}
	}

	// Fallback to upower or sysfs on Linux PC
	if isCommandAvailable("upower") {
		cmd := exec.Command("upower", "-i", "/org/freedesktop/UPower/devices/battery_BAT0")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			lines := strings.Split(out.String(), "\n")
			info := &BatteryInfo{Status: "discharging"}
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "percentage:") {
					pctStr := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "percentage:"), "%"))
					_, _ = fmt.Sscanf(pctStr, "%d", &info.Percentage)
				}
				if strings.HasPrefix(line, "state:") {
					info.Status = strings.TrimSpace(strings.TrimPrefix(line, "state:"))
				}
			}
			if info.Percentage > 0 {
				return info, nil
			}
		}
	}

	return nil, fmt.Errorf("battery status not available")
}

// SendNotification triggers a native OS notification
func SendNotification(title, message string) error {
	if isCommandAvailable("termux-notification") {
		cmd := exec.Command("termux-notification", "--title", title, "--content", message, "--priority", "high")
		return cmd.Run()
	}
	if isCommandAvailable("notify-send") {
		cmd := exec.Command("notify-send", title, message)
		return cmd.Run()
	}
	return nil
}

// TriggerRing triggers a vibration / alert on the device
func TriggerRing() error {
	if isCommandAvailable("termux-vibrate") {
		cmd := exec.Command("termux-vibrate", "-d", "1500", "-f")
		_ = cmd.Run()
	}
	if isCommandAvailable("termux-media-player") {
		// Play system alert tone if available
		_ = exec.Command("termux-media-player", "play").Run()
	}
	// Terminal bell fallback
	fmt.Print("\a")
	return nil
}

// OpenURL opens a URL in the default browser
func OpenURL(url string) error {
	if isCommandAvailable("termux-open-url") {
		return exec.Command("termux-open-url", url).Run()
	}
	if isCommandAvailable("xdg-open") {
		return exec.Command("xdg-open", url).Run()
	}
	return fmt.Errorf("no browser opener available")
}

// MediaControl controls media playback (play-pause, next, previous)
func MediaControl(action string) (string, error) {
	if isCommandAvailable("playerctl") {
		cmd := exec.Command("playerctl", action)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		if err == nil {
			return fmt.Sprintf("[AUDIO] Playerctl: %s executed", action), nil
		}
		return "", fmt.Errorf("playerctl error: %s", out.String())
	}
	if isCommandAvailable("termux-media-player") {
		cmd := exec.Command("termux-media-player", action)
		_ = cmd.Run()
		return fmt.Sprintf("[AUDIO] Termux Media: %s", action), nil
	}
	return "", fmt.Errorf("media control (playerctl) not found on this system")
}
