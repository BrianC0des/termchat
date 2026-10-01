package ui

import (
	"strings"
	"testing"

	"termchat/pkg/network"

	tea "github.com/charmbracelet/bubbletea"
)

func TestOutputModal_OpenAndDismiss(t *testing.T) {
	m := &Model{
		width:      100,
		height:     30,
		manager:    &network.Manager{LocalID: "test-id", LocalName: "Tester"},
		filePicker: NewFilePicker(),
	}
	m.recalculateViewport()

	if m.showModal {
		t.Fatalf("expected modal initially closed")
	}

	testTitle := "◆ TEST MODAL"
	testContent := "Line 1: Hello World\nLine 2: Test Content\nLine 3: More Details"
	m.openModal(testTitle, testContent)

	if !m.showModal {
		t.Fatalf("expected showModal to be true")
	}
	if m.modalTitle != testTitle {
		t.Fatalf("expected title %q, got %q", testTitle, m.modalTitle)
	}

	viewOutput := m.View()
	if !strings.Contains(viewOutput, "TEST MODAL") {
		t.Errorf("expected View() to contain modal title, got:\n%s", viewOutput)
	}
	if !strings.Contains(viewOutput, "Hello World") {
		t.Errorf("expected View() to contain modal content, got:\n%s", viewOutput)
	}

	// Test navigation key: 'j' or Down arrow
	downKey := tea.KeyMsg{Type: tea.KeyDown}
	resModel, _ := m.Update(downKey)
	m = resModel.(*Model)
	if !m.showModal {
		t.Fatalf("expected modal to remain open after down key")
	}

	// Test dismiss with 'q'
	qKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	resModel, _ = m.Update(qKey)
	m = resModel.(*Model)

	if m.showModal {
		t.Fatalf("expected modal to close on 'q'")
	}

	// Reopen and test dismiss with Esc
	m.openModal(testTitle, testContent)
	escKey := tea.KeyMsg{Type: tea.KeyEsc}
	resModel, _ = m.Update(escKey)
	m = resModel.(*Model)

	if m.showModal {
		t.Fatalf("expected modal to close on Esc")
	}
}

func TestOutputModal_Resize(t *testing.T) {
	m := &Model{
		width:      100,
		height:     30,
		manager:    &network.Manager{LocalID: "test-id", LocalName: "Tester"},
		filePicker: NewFilePicker(),
	}
	m.openModal("◆ RESIZE TEST", "Lots of text...")

	initialVpWidth := m.modalViewport.Width
	initialVpHeight := m.modalViewport.Height

	// Send WindowSizeMsg with larger size
	resizeMsg := tea.WindowSizeMsg{Width: 140, Height: 50}
	resModel, _ := m.Update(resizeMsg)
	m = resModel.(*Model)

	if m.modalViewport.Width <= initialVpWidth && m.modalViewport.Width < 80 {
		t.Errorf("expected modal viewport width to adapt on resize, got %d", m.modalViewport.Width)
	}
	if m.modalViewport.Height <= initialVpHeight {
		t.Errorf("expected modal viewport height to adapt on resize, got %d", m.modalViewport.Height)
	}
}
