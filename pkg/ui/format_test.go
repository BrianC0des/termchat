package ui

import (
	"strings"
	"testing"
)

func TestFormatChatMessage_SQLCodeBlock(t *testing.T) {
	raw := "```sql\nSELECT actor_id, first_name, last_name\nFROM sakila.actor\nWHERE last_name LIKE 'G%'\nORDER BY first_name ASC\nLIMIT 5;\n```"
	res := FormatChatMessage(raw, 80, "#1 [alice]:", "   ", "bob")
	
	if !strings.Contains(res, "SELECT") || !strings.Contains(res, "LIMIT 5") {
		t.Fatalf("Expected SQL content to be formatted properly, got: %s", res)
	}
}

func TestFormatChatMessage_MarkdownLists(t *testing.T) {
	raw := "# Security Overview\n• Zero-knowledge encryption\n• AES-256-GCM\n• Verification code"
	res := FormatChatMessage(raw, 80, "#2 [alice]:", "   ", "bob")

	if !strings.Contains(res, "Security Overview") || !strings.Contains(res, "Zero-knowledge") {
		t.Fatalf("Expected markdown list to be formatted, got: %s", res)
	}
}
