package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var (
	header3Regex     = regexp.MustCompile(`(?i)(?:^|\s{2,})(#{1,4}\s+)`)
	bulletRegex      = regexp.MustCompile(`(?i)(?:^|\s{2,})([•\-\*]\s+)`)
	numListRegex     = regexp.MustCompile(`(?i)(?:^|\s{2,})(\d+\.\s+)`)
	dividerRegex     = regexp.MustCompile(`(?i)(?:^|\s{2,})([─\-]{3,}|={3,})`)
	codeFenceRegex   = regexp.MustCompile("(?i)```([a-zA-Z0-9_-]*)")
	codeTickRegex    = regexp.MustCompile("`([^`]+)`")
	sqlKeywordRegex  = regexp.MustCompile(`(?i)\s{2,}(SELECT|FROM|WHERE|ORDER BY|GROUP BY|HAVING|LIMIT|JOIN|INNER JOIN|LEFT JOIN|RIGHT JOIN|INSERT INTO|VALUES|UPDATE|SET|DELETE FROM|CREATE TABLE|ALTER TABLE|DROP TABLE|DESCRIBE|DESC|--|/\*)`)
	progKeywordRegex = regexp.MustCompile(`(?i)\s{2,}(func\s|def\s|class\s|return\s|import\s|package\s|const\s|let\s|var\s|if\s|else\s|for\s|while\s|try\s|catch\s|//|/\*)`)
)

func normalizeCodeBlockContent(inner string) string {
	inner = strings.ReplaceAll(inner, "\r\n", "\n")
	inner = strings.ReplaceAll(inner, "\r", "\n")

	// If the code block was flattened into a single line or has few newlines, restore individual lines!
	if strings.Count(inner, "\n") < 2 {
		inner = sqlKeywordRegex.ReplaceAllString(inner, "\n$1")
		inner = progKeywordRegex.ReplaceAllString(inner, "\n$1")
		inner = regexp.MustCompile(`;\s{2,}`).ReplaceAllString(inner, ";\n")
		inner = regexp.MustCompile(`\s{2,}(--|//)`).ReplaceAllString(inner, "\n$1")
		inner = regexp.MustCompile(`\s{3,}`).ReplaceAllString(inner, "\n")
	}
	return inner
}

// detectCodeLanguage inspects raw un-fenced text and determines if it is source code.
func detectCodeLanguage(text string) (bool, string) {
	lower := strings.ToLower(text)

	// SQL Detection
	if strings.Contains(lower, "select ") && (strings.Contains(lower, "from ") || strings.Contains(lower, "where ") || strings.Contains(lower, "join ") || strings.Contains(lower, "limit ")) {
		return true, "sql"
	}
	if strings.Contains(lower, "create table ") || strings.Contains(lower, "insert into ") || strings.Contains(lower, "alter table ") {
		return true, "sql"
	}

	// Python Detection
	if (strings.Contains(text, "def ") || strings.Contains(text, "async def ") || strings.Contains(text, "import ") || strings.Contains(text, "from ")) &&
		(strings.Contains(text, ":\n") || strings.Contains(text, "print(") || strings.Contains(text, "__main__") || strings.Contains(text, "return ") || strings.Contains(text, "self.")) {
		return true, "python"
	}

	// Go Detection
	if (strings.Contains(text, "func ") || strings.Contains(text, "package ")) &&
		(strings.Contains(text, "import (") || strings.Contains(text, "struct {") || strings.Contains(text, ":=") || strings.Contains(text, "fmt.")) {
		return true, "go"
	}

	// JavaScript / TypeScript Detection
	if (strings.Contains(text, "const ") || strings.Contains(text, "let ") || strings.Contains(text, "function ") || strings.Contains(text, "async function ")) &&
		(strings.Contains(text, "console.log") || strings.Contains(text, "=>") || strings.Contains(text, "export ") || strings.Contains(text, "require(")) {
		return true, "javascript"
	}

	// Rust / C / C++
	if strings.Contains(text, "#include <") || strings.Contains(text, "int main(") {
		return true, "c"
	}
	if strings.Contains(text, "fn main()") || (strings.Contains(text, "impl ") && strings.Contains(text, "struct ")) {
		return true, "rust"
	}

	return false, ""
}

// normalizeFlattenedPaste detects if a pasted snippet had its newlines flattened into spaces,
// and automatically restores clean newlines before headers, bullets, dividers, and code lines.
func normalizeFlattenedPaste(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// If the user pasted raw code without markdown backticks, auto-wrap it!
	if !strings.Contains(text, "```") {
		if isCode, lang := detectCodeLanguage(text); isCode {
			normalizedCode := normalizeCodeBlockContent(text)
			return fmt.Sprintf("```%s\n%s\n```", lang, strings.TrimSpace(normalizedCode))
		}
	}

	if strings.Contains(text, "```") {
		matches := codeFenceRegex.FindAllStringIndex(text, -1)
		if len(matches) > 0 {
			var sb strings.Builder
			cursor := 0
			for _, m := range matches {
				sb.WriteString(text[cursor:m[0]])
				sb.WriteString("\n" + text[m[0]:m[1]] + "\n")
				cursor = m[1]
			}
			sb.WriteString(text[cursor:])
			text = sb.String()
		}
	}

	// Auto-split markdown headers, bullets, and dividers if flattened
	if strings.Count(text, "\n") < 2 {
		text = header3Regex.ReplaceAllString(text, "\n$1")
		text = bulletRegex.ReplaceAllString(text, "\n$1")
		text = numListRegex.ReplaceAllString(text, "\n$1")
		text = dividerRegex.ReplaceAllString(text, "\n$1")
	}

	lines := strings.Split(text, "\n")
	var result []string
	inBlock := false
	var blockContent []string
	var fenceHeader string

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "```") {
			if !inBlock {
				inBlock = true
				fenceHeader = trimmed
				blockContent = []string{}
			} else {
				inBlock = false
				result = append(result, fenceHeader)
				innerRaw := strings.Join(blockContent, "\n")
				normalizedInner := normalizeCodeBlockContent(innerRaw)
				for _, inl := range strings.Split(normalizedInner, "\n") {
					if strings.TrimSpace(inl) != "" {
						result = append(result, inl)
					}
				}
				result = append(result, "```")
			}
			continue
		}

		if inBlock {
			blockContent = append(blockContent, l)
		} else {
			result = append(result, l)
		}
	}

	if inBlock && len(blockContent) > 0 {
		result = append(result, fenceHeader)
		innerRaw := strings.Join(blockContent, "\n")
		normalizedInner := normalizeCodeBlockContent(innerRaw)
		for _, inl := range strings.Split(normalizedInner, "\n") {
			if strings.TrimSpace(inl) != "" {
				result = append(result, inl)
			}
		}
		result = append(result, "```")
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}

// foldLongCodeBlocks checks if markdown code blocks exceed maxLines, and collapses them with a preview.
func foldLongCodeBlocks(text string, maxLines int) string {
	lines := strings.Split(text, "\n")
	var result []string
	inBlock := false
	var blockLines []string
	var fenceHeader string

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "```") {
			if !inBlock {
				inBlock = true
				fenceHeader = trimmed
				blockLines = []string{}
			} else {
				inBlock = false
				result = append(result, fenceHeader)
				if len(blockLines) > maxLines {
					previewCount := 5
					if previewCount > len(blockLines) {
						previewCount = len(blockLines)
					}
					for i := 0; i < previewCount; i++ {
						result = append(result, blockLines[i])
					}
					hiddenCount := len(blockLines) - previewCount
					result = append(result, fmt.Sprintf("... +%d lines folded (Press Ctrl+E or /expand to show)", hiddenCount))
				} else {
					result = append(result, blockLines...)
				}
				result = append(result, "```")
			}
			continue
		}

		if inBlock {
			blockLines = append(blockLines, l)
		} else {
			result = append(result, l)
		}
	}

	if inBlock && len(blockLines) > 0 {
		result = append(result, fenceHeader)
		if len(blockLines) > maxLines {
			previewCount := 5
			if previewCount > len(blockLines) {
				previewCount = len(blockLines)
			}
			for i := 0; i < previewCount; i++ {
				result = append(result, blockLines[i])
			}
			hiddenCount := len(blockLines) - previewCount
			result = append(result, fmt.Sprintf("... +%d lines folded (Press Ctrl+E or /expand to show)", hiddenCount))
		} else {
			result = append(result, blockLines...)
		}
		result = append(result, "```")
	}

	return strings.Join(result, "\n")
}

// FormatChatMessage formats a message string (with markdown, code blocks, headers, bullet points,
// and continuation lines) into beautifully aligned terminal output matching Discord/Slack/GitHub.
func FormatChatMessage(rawContent string, wrapWidth int, firstLinePrefix string, continuationPrefix string, myName string) string {
	return FormatChatMessageWithFold(rawContent, wrapWidth, firstLinePrefix, continuationPrefix, myName, false)
}

// FormatChatMessageWithFold formats a message string with option to expand or fold long code blocks.
func FormatChatMessageWithFold(rawContent string, wrapWidth int, firstLinePrefix string, continuationPrefix string, myName string, expandCodeBlocks bool) string {
	if wrapWidth < 30 {
		wrapWidth = 30
	}

	content := normalizeFlattenedPaste(rawContent)

	if !expandCodeBlocks {
		content = foldLongCodeBlocks(content, 8)
	}

	// Check if message has rich markdown content (code block, heading, list, quote, table)
	hasMarkdown := strings.Contains(content, "```") ||
		strings.HasPrefix(content, "#") ||
		strings.Contains(content, "\n#") ||
		strings.Contains(content, "\n•") ||
		strings.Contains(content, "\n- ") ||
		strings.Contains(content, "\n* ") ||
		strings.Contains(content, "\n1.") ||
		strings.Contains(content, "---") ||
		strings.Contains(content, "───")

	if hasMarkdown {
		glamourWidth := wrapWidth - lipgloss.Width(continuationPrefix) - 4
		if glamourWidth < 25 {
			glamourWidth = 25
		}

		r, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(glamourWidth),
		)
		if err == nil {
			rendered, rErr := r.Render(content)
			if rErr == nil {
				rendered = strings.Trim(rendered, "\n")
				lines := strings.Split(rendered, "\n")
				var sb strings.Builder
				for i, l := range lines {
					if i == 0 {
						sb.WriteString(fmt.Sprintf("%s %s\n", firstLinePrefix, l))
					} else {
						sb.WriteString(fmt.Sprintf("%s %s\n", continuationPrefix, l))
					}
				}
				return sb.String()
			}
		}
	}

	// Fallback / Lightweight single-line & simple message rendering
	rawLines := strings.Split(content, "\n")
	var formattedLines []string
	mentionStyle := lipgloss.NewStyle().Bold(true).Foreground(WarningColor)

	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			formattedLines = append(formattedLines, "")
			continue
		}
		renderedLine := highlightInlineFormatting(trimmed, myName, mentionStyle)
		wrapped := wrapIndentedText(renderedLine, wrapWidth-lipgloss.Width(continuationPrefix), "")
		formattedLines = append(formattedLines, wrapped...)
	}

	if len(formattedLines) == 0 {
		return firstLinePrefix
	}

	var sb strings.Builder
	for i, fl := range formattedLines {
		if i == 0 {
			sb.WriteString(fmt.Sprintf("%s %s\n", firstLinePrefix, fl))
		} else {
			sb.WriteString(fmt.Sprintf("%s %s\n", continuationPrefix, fl))
		}
	}

	return sb.String()
}

// highlightInlineFormatting highlights inline code (`foo`), mentions (@user), and URLs
func highlightInlineFormatting(text string, myName string, mentionStyle lipgloss.Style) string {
	// 1. Highlight inline backticks: `some_cmd` -> chip
	text = codeTickRegex.ReplaceAllStringFunc(text, func(m string) string {
		inner := strings.Trim(m, "`")
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7AA2F7")).
			Background(lipgloss.Color("#1F2335")).
			Padding(0, 1).
			Render(inner)
	})

	// 2. Highlight user mentions
	if myName != "" {
		myMention := "@" + myName
		if strings.Contains(strings.ToLower(text), strings.ToLower(myMention)) || strings.Contains(strings.ToLower(text), "@all") {
			text = mentionStyle.Render(text)
		}
	}

	return text
}

// wrapIndentedText wraps long text across terminal width preserving clean continuation
func wrapIndentedText(text string, maxWidth int, indent string) []string {
	if maxWidth < 20 {
		maxWidth = 20
	}

	// Use lipgloss soft-wrap
	wrappedStr := lipgloss.NewStyle().Width(maxWidth).Render(text)
	lines := strings.Split(wrappedStr, "\n")

	var result []string
	for i, l := range lines {
		if i > 0 && indent != "" {
			result = append(result, indent+l)
		} else {
			result = append(result, l)
		}
	}
	return result
}
