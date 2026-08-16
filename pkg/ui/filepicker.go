package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"termchat/pkg/network"

	"github.com/charmbracelet/lipgloss"
)

type FileItem struct {
	Name  string
	Path  string
	IsDir bool
	Size  int64
}

type FilePicker struct {
	CurrentDir string
	Items      []FileItem
	Cursor     int
	Active     bool
	Width      int
	Height     int
}

func NewFilePicker() *FilePicker {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	fp := &FilePicker{
		CurrentDir: home,
		Cursor:     0,
		Active:     false,
	}
	fp.Refresh()
	return fp
}

func (fp *FilePicker) Open() {
	fp.Active = true
	fp.Refresh()
}

func (fp *FilePicker) Close() {
	fp.Active = false
}

func (fp *FilePicker) Refresh() {
	entries, err := os.ReadDir(fp.CurrentDir)
	if err != nil {
		return
	}

	var items []FileItem
	// Parent dir entry if not root
	if fp.CurrentDir != "/" && fp.CurrentDir != "" {
		items = append(items, FileItem{
			Name:  "..",
			Path:  filepath.Dir(fp.CurrentDir),
			IsDir: true,
		})
	}

	for _, entry := range entries {
		// Ignore hidden files by default unless needed
		if strings.HasPrefix(entry.Name(), ".") && entry.Name() != ".." {
			continue
		}
		info, err := entry.Info()
		size := int64(0)
		if err == nil {
			size = info.Size()
		}
		items = append(items, FileItem{
			Name:  entry.Name(),
			Path:  filepath.Join(fp.CurrentDir, entry.Name()),
			IsDir: entry.IsDir(),
			Size:  size,
		})
	}

	// Sort directories first, then alphabetical
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	fp.Items = items
	if fp.Cursor >= len(fp.Items) {
		fp.Cursor = max(0, len(fp.Items)-1)
	}
}

func (fp *FilePicker) MoveUp() {
	if fp.Cursor > 0 {
		fp.Cursor--
	}
}

func (fp *FilePicker) MoveDown() {
	if fp.Cursor < len(fp.Items)-1 {
		fp.Cursor++
	}
}

func (fp *FilePicker) Select() (selectedFile string, isSelected bool) {
	if len(fp.Items) == 0 || fp.Cursor >= len(fp.Items) {
		return "", false
	}

	item := fp.Items[fp.Cursor]
	if item.IsDir {
		fp.CurrentDir = item.Path
		fp.Cursor = 0
		fp.Refresh()
		return "", false
	}

	// Selected a regular file to send!
	fp.Close()
	return item.Path, true
}

func (fp *FilePicker) View(width, height int) string {
	if !fp.Active {
		return ""
	}

	boxWidth := min(width-4, 72)
	boxHeight := min(height-4, 22)

	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(PrimaryColor).Padding(0, 1).Render("📂 SELECT FILE TO SEND")
	currentDirStr := lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7")).Bold(true).Render(fp.CurrentDir)

	var listLines []string
	visibleCount := boxHeight - 7
	if visibleCount < 3 {
		visibleCount = 3
	}

	startIdx := 0
	if fp.Cursor >= visibleCount {
		startIdx = fp.Cursor - visibleCount + 1
	}
	endIdx := min(len(fp.Items), startIdx+visibleCount)

	for i := startIdx; i < endIdx; i++ {
		item := fp.Items[i]
		icon := "📄 "
		if item.IsDir {
			icon = "📁 "
		} else {
			ext := strings.ToLower(filepath.Ext(item.Name))
			switch ext {
			case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
				icon = "🖼️ "
			case ".zip", ".tar", ".gz", ".7z", ".apk":
				icon = "📦 "
			case ".mp3", ".wav", ".flac", ".ogg":
				icon = "🎵 "
			case ".mp4", ".mkv", ".webm", ".avi":
				icon = "🎬 "
			case ".pdf", ".doc", ".docx", ".txt", ".md":
				icon = "📝 "
			}
		}

		nameStr := item.Name
		if len(nameStr) > 38 {
			nameStr = nameStr[:35] + "..."
		}

		sizeStr := ""
		if !item.IsDir {
			sizeStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Render(network.FormatBytes(item.Size))
		}

		gap := boxWidth - lipgloss.Width(nameStr) - lipgloss.Width(sizeStr) - 10
		if gap < 1 {
			gap = 1
		}

		lineContent := fmt.Sprintf("%s%-38s%s%s", icon, nameStr, strings.Repeat(" ", gap), sizeStr)

		if i == fp.Cursor {
			listLines = append(listLines, lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#3B4261")).
				Width(boxWidth-4).
				Render(" ❯ "+lineContent))
		} else {
			listLines = append(listLines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("#C0CAF5")).
				Width(boxWidth-4).
				Render("   "+lineContent))
		}
	}

	controls := lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89")).Render(
		"↑/↓: Navigate | Enter: Open/Send | Backspace: Up | Esc: Cancel",
	)

	body := strings.Join(listLines, "\n")
	content := fmt.Sprintf("%s\n\n%s\n\n%s\n\n%s", title, currentDirStr, body, controls)

	return lipgloss.Place(
		width,
		height,
		lipgloss.Center,
		lipgloss.Center,
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(PrimaryColor).
			Padding(1, 2).
			Width(boxWidth).
			Height(boxHeight).
			Render(content),
	)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
