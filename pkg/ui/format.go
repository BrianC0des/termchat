package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	header3Regex  = regexp.MustCompile(`(?i)(?:^|\s{2,})(#{1,4}\s+)`)
	bulletRegex   = regexp.MustCompile(`(?i)(?:^|\s{2,})([•\-\*]\s+)`)
	numListRegex  = regexp.MustCompile(`(?i)(?:^|\s{2,})(\d+\.\s+)`)
	dividerRegex  = regexp.MustCompile(`(?i)(?:^|\s{2,})([─\-]{3,}|={3,})`)
	codeTickRegex = regexp.MustCompile("`([^`]+)`")
)

// normalizeFlattenedPaste detects if a pasted snippet had its newlines flattened into spaces,
// and automatically restores clean newlines before headers, bullets, and dividers.
func normalizeFlattenedPaste(text string) string {
	// If it already has multiple real newlines, just standardize CRLF
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// If there are few/no newlines, but contains markdown structure markers separated by 2+ spaces, auto-split
	if strings.Count(text, "\n") < 2 {
		// Insert newlines before markdown headings (###, ##, #)
		text = header3Regex.ReplaceAllString(text, "\n$1")
		// Insert newlines before bullets (•, -, *)
		text = bulletRegex.ReplaceAllString(text, "\n$1")
		// Insert newlines before numbered lists (1., 2.)
		text = numListRegex.ReplaceAllString(text, "\n$1")
		// Insert newlines before dividers (───, ---)
		text = dividerRegex.ReplaceAllString(text, "\n$1")
	}

	return strings.TrimSpace(text)
}

// FormatChatMessage formats a message string (with markdown, code blocks, headers, bullet points,
// and continuation lines) into beautifully aligned terminal output matching Discord/Slack/GitHub.
func FormatChatMessage(rawContent string, wrapWidth int, firstLinePrefix string, continuationPrefix string, myName string) string {
	if wrapWidth < 30 {
		wrapWidth = 30
	}

	content := normalizeFlattenedPaste(rawContent)
	rawLines := strings.Split(content, "\n")

	var formattedLines []string
	inCodeBlock := false
	codeLang := ""
	var codeBlockLines []string

	mentionStyle := lipgloss.NewStyle().Bold(true).Foreground(WarningColor)
	codeBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3B4261")).
		Background(lipgloss.Color("#16161E")).
		Padding(0, 1)

	for lineIdx, line := range rawLines {
		trimmed := strings.TrimSpace(line)

		// 1. Code Block Fence (```)
		if strings.HasPrefix(trimmed, "```") {
			if !inCodeBlock {
				inCodeBlock = true
				codeLang = strings.TrimPrefix(trimmed, "```")
				codeBlockLines = []string{}
				continue
			} else {
				inCodeBlock = false
				// Flush accumulated code block
				headerTitle := " Code "
				if codeLang != "" {
					headerTitle = fmt.Sprintf(" Code [%s] ", codeLang)
				}

				maxCodeWidth := wrapWidth - lipgloss.Width(continuationPrefix) - 6
				if maxCodeWidth < 20 {
					maxCodeWidth = 20
				}

				codeContent := strings.Join(codeBlockLines, "\n")
				renderedCodeBox := codeBoxStyle.
					Width(maxCodeWidth).
					Render(fmt.Sprintf("%s\n%s", lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render(headerTitle), codeContent))

				boxLines := strings.Split(renderedCodeBox, "\n")
				for _, bl := range boxLines {
					formattedLines = append(formattedLines, bl)
				}
				continue
			}
		}

		if inCodeBlock {
			// Inside Code Block: preserve raw formatting with syntax-colored mono text
			codeLine := lipgloss.NewStyle().Foreground(lipgloss.Color("#7AA2F7")).Render(line)
			codeBlockLines = append(codeBlockLines, codeLine)
			continue
		}

		// Empty line separator
		if trimmed == "" {
			if lineIdx > 0 && lineIdx < len(rawLines)-1 {
				formattedLines = append(formattedLines, "")
			}
			continue
		}

		// 2. Horizontal Divider (───, ---, ===)
		if (strings.HasPrefix(trimmed, "─") || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "=")) && len(trimmed) >= 3 {
			dividerLen := wrapWidth - lipgloss.Width(continuationPrefix) - 4
			if dividerLen < 6 {
				dividerLen = 6
			}
			rule := lipgloss.NewStyle().Foreground(lipgloss.Color("#3B4261")).Render(strings.Repeat("─", dividerLen))
			formattedLines = append(formattedLines, rule)
			continue
		}

		// 3. Markdown Headers (#, ##, ###, ####)
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			headerText := strings.TrimSpace(trimmed[level:])
			var headerStyle lipgloss.Style
			switch level {
			case 1:
				headerStyle = lipgloss.NewStyle().Bold(true).Foreground(PrimaryColor).Underline(true)
			case 2:
				headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#BB9AF7"))
			case 3:
				headerStyle = lipgloss.NewStyle().Bold(true).Foreground(WarningColor)
			default:
				headerStyle = lipgloss.NewStyle().Bold(true).Foreground(SecondaryColor)
			}
			formattedLines = append(formattedLines, headerStyle.Render(headerText))
			continue
		}

		// 4. Bullet Lists (•, -, *)
		if strings.HasPrefix(trimmed, "•") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			bulletBody := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(trimmed, "•"), "-"), "*")
			bulletBody = strings.TrimSpace(bulletBody)

			bulletTag := lipgloss.NewStyle().Foreground(SecondaryColor).Bold(true).Render("• ")
			renderedBullet := bulletTag + highlightInlineFormatting(bulletBody, myName, mentionStyle)

			wrapped := wrapIndentedText(renderedBullet, wrapWidth-lipgloss.Width(continuationPrefix), "  ")
			formattedLines = append(formattedLines, wrapped...)
			continue
		}

		// 5. Numbered Lists (1., 2., etc.)
		if numListRegex.MatchString(trimmed) {
			matches := numListRegex.FindStringSubmatch(trimmed)
			if len(matches) > 1 {
				numPrefix := matches[1]
				body := strings.TrimPrefix(trimmed, numPrefix)
				renderedNum := lipgloss.NewStyle().Foreground(AccentColor).Bold(true).Render(numPrefix) + highlightInlineFormatting(body, myName, mentionStyle)

				wrapped := wrapIndentedText(renderedNum, wrapWidth-lipgloss.Width(continuationPrefix), "   ")
				formattedLines = append(formattedLines, wrapped...)
				continue
			}
		}

		// 6. Standard Text with inline formatting (mentions, inline code, bold)
		renderedLine := highlightInlineFormatting(trimmed, myName, mentionStyle)
		wrapped := wrapIndentedText(renderedLine, wrapWidth-lipgloss.Width(continuationPrefix), "")
		formattedLines = append(formattedLines, wrapped...)
	}

	// Flush unclosed code block if any
	if inCodeBlock && len(codeBlockLines) > 0 {
		maxCodeWidth := wrapWidth - lipgloss.Width(continuationPrefix) - 6
		codeContent := strings.Join(codeBlockLines, "\n")
		renderedCodeBox := codeBoxStyle.Width(maxCodeWidth).Render(codeContent)
		for _, bl := range strings.Split(renderedCodeBox, "\n") {
			formattedLines = append(formattedLines, bl)
		}
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
