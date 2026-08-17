package ui

import (
	"regexp"
	"strings"
	"testing"
)

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

func TestFormatChatMessage_SQLCodeBlock(t *testing.T) {
	raw := "```sql\nSELECT actor_id, first_name, last_name\nFROM sakila.actor\nWHERE last_name LIKE 'G%'\nORDER BY first_name ASC\nLIMIT 5;\n```"
	res := stripANSI(FormatChatMessage(raw, 80, "#1 [alice]:", "   ", "bob"))
	
	if !strings.Contains(res, "SELECT") || !strings.Contains(res, "LIMIT 5") {
		t.Fatalf("Expected SQL content to be formatted properly, got: %s", res)
	}
}

func TestFormatChatMessage_MarkdownLists(t *testing.T) {
	raw := "# Security Overview\n• Zero-knowledge encryption\n• AES-256-GCM\n• Verification code"
	res := stripANSI(FormatChatMessage(raw, 80, "#2 [alice]:", "   ", "bob"))

	if !strings.Contains(res, "Security Overview") || !strings.Contains(res, "Zero-knowledge") {
		t.Fatalf("Expected markdown list to be formatted, got: %s", res)
	}
}

func TestFormatChatMessage_RawPythonPaste(t *testing.T) {
	raw := "import asyncio\nimport aiohttp\nasync def fetch_user_data(user_id: int) -> dict:\n    print(f'Fetching {user_id}')\n    return {'id': user_id}"
	res := FormatChatMessage(raw, 80, "#3 [alice]:", "   ", "bob")

	if !strings.Contains(res, "fetch_user_data") || !strings.Contains(res, "asyncio") {
		t.Fatalf("Expected raw python code to be formatted into syntax highlighted block, got: %s", res)
	}
}

func TestFormatChatMessage_CodeFolding(t *testing.T) {
	longCode := "```python\nline1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12\n```"
	folded := FormatChatMessageWithFold(longCode, 80, "#4 [alice]:", "   ", "bob", false)
	if !strings.Contains(folded, "folded") {
		t.Fatalf("Expected long code block to be folded when expandCodeBlocks=false, got: %s", folded)
	}

	expanded := FormatChatMessageWithFold(longCode, 80, "#4 [alice]:", "   ", "bob", true)
	if strings.Contains(expanded, "folded") || !strings.Contains(expanded, "line12") {
		t.Fatalf("Expected long code block to be fully expanded when expandCodeBlocks=true, got: %s", expanded)
	}
}


