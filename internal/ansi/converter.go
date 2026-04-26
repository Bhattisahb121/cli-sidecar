package ansi

import (
	"strings"
)

// ToMarkdown converts ANSI-styled text back to Markdown.
// It detects bold, italic, strikethrough, code blocks, and headings
// from ANSI style attributes and reconstructs Markdown syntax.
func ToMarkdown(input string) string {
	spans := Parse(input)
	if len(spans) == 0 {
		return ""
	}

	var b strings.Builder
	lines := spansToLines(spans)

	for _, line := range lines {
		md := convertLine(line)
		b.WriteString(md)
		b.WriteByte('\n')
	}

	result := b.String()
	// Clean up excessive blank lines
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimRight(result, "\n") + "\n"
}

// lineSpan groups spans by line.
type lineSpan struct {
	spans []Span
}

func spansToLines(spans []Span) []lineSpan {
	var lines []lineSpan
	var currentLine []Span

	for _, s := range spans {
		parts := strings.Split(s.Text, "\n")
		for i, part := range parts {
			if i > 0 {
				lines = append(lines, lineSpan{spans: currentLine})
				currentLine = nil
			}
			if part != "" {
				currentLine = append(currentLine, Span{
					Text:  part,
					Style: s.Style,
					FgCol: s.FgCol,
					BgCol: s.BgCol,
				})
			}
		}
	}

	if len(currentLine) > 0 {
		lines = append(lines, lineSpan{spans: currentLine})
	}

	return lines
}

func convertLine(line lineSpan) string {
	if len(line.spans) == 0 {
		return ""
	}

	// Detect if entire line looks like a code block indicator
	plainText := lineText(line)
	trimmed := strings.TrimSpace(plainText)

	// Detect horizontal rules (glamour often renders these as ─── or ═══)
	if isHorizontalRule(trimmed) {
		return "---"
	}

	// Check if this is a heading (bold text that's a whole line, possibly with
	// specific color)
	if isHeading(line) {
		level := detectHeadingLevel(line)
		prefix := strings.Repeat("#", level)
		return prefix + " " + trimmed
	}

	// Convert inline styles
	var b strings.Builder
	inCodeBlock := false

	for _, s := range line.spans {
		text := s.Text

		// Detect inline code (often rendered with different bg color or specific fg)
		if isCodeStyle(s) && !inCodeBlock {
			b.WriteString("`")
			b.WriteString(text)
			b.WriteString("`")
			continue
		}

		// Apply Markdown formatting based on ANSI styles
		prefix, suffix := styleToMarkdown(s.Style)
		b.WriteString(prefix)
		b.WriteString(text)
		b.WriteString(suffix)

		_ = inCodeBlock
	}

	return b.String()
}

func lineText(line lineSpan) string {
	var b strings.Builder
	for _, s := range line.spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

func isHorizontalRule(text string) bool {
	if len(text) < 3 {
		return false
	}
	// Check for lines made of repeated box-drawing or dash characters
	for _, ch := range text {
		switch ch {
		case '─', '━', '═', '—', '-', '_', '▬':
			continue
		default:
			return false
		}
	}
	return true
}

func isHeading(line lineSpan) bool {
	if len(line.spans) == 0 {
		return false
	}

	// All spans must be bold-only (not bold+italic, not bold+strikethrough)
	// for the line to be treated as a heading.
	allBoldOnly := true
	for _, s := range line.spans {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		if s.Style&StyleBold == 0 {
			allBoldOnly = false
			break
		}
		// If other style flags are set besides bold, it's styled inline text
		if s.Style&(StyleItalic|StyleStrikethrough|StyleUnderline) != 0 {
			allBoldOnly = false
			break
		}
	}

	if !allBoldOnly {
		return false
	}

	text := strings.TrimSpace(lineText(line))
	return len(text) > 0 && len(text) < 200
}

func detectHeadingLevel(line lineSpan) int {
	// Try to determine heading level from color/style.
	// In most terminal themes:
	// - H1: bold + bright/specific color (often magenta, cyan)
	// - H2: bold + different color
	// - H3+: bold only or bold + dimmer color
	//
	// Without knowing the exact glamour theme, we use heuristics:
	// - If the text has a foreground color, it's likely H1 or H2
	// - If just bold with no color, it's likely H3+

	hasColor := false
	for _, s := range line.spans {
		if s.FgCol != nil && strings.TrimSpace(s.Text) != "" {
			hasColor = true
			break
		}
	}

	if hasColor {
		// Check for "bright" colors typically used for H1
		for _, s := range line.spans {
			if s.FgCol != nil && s.FgCol.Type == ColorBasic && s.FgCol.Value >= 8 {
				return 1 // Bright colors → H1
			}
			if s.FgCol != nil && (s.FgCol.Type == ColorRGB || s.FgCol.Type == Color256) {
				return 1 // Rich colors → H1
			}
		}
		return 2 // Standard colors → H2
	}

	return 3 // Bold only → H3
}

func isCodeStyle(s Span) bool {
	// Code is typically rendered with:
	// - A background color (highlighted block)
	// - Or a specific dim/faint style
	if s.BgCol != nil {
		return true
	}
	return false
}

func styleToMarkdown(style uint) (prefix, suffix string) {
	if style == StyleNone {
		return "", ""
	}

	var pre, suf strings.Builder

	// Order matters: outermost wrapper first
	if style&StyleBold != 0 && style&StyleItalic != 0 {
		pre.WriteString("***")
		suf.WriteString("***")
	} else {
		if style&StyleBold != 0 {
			pre.WriteString("**")
			suf.WriteString("**")
		}
		if style&StyleItalic != 0 {
			pre.WriteString("*")
			suf.WriteString("*")
		}
	}

	if style&StyleStrikethrough != 0 {
		pre.WriteString("~~")
		suf.WriteString("~~")
	}

	return pre.String(), suf.String()
}

// ToPlainText strips ANSI codes and returns clean plain text.
func ToPlainText(input string) string {
	return Strip(input)
}
