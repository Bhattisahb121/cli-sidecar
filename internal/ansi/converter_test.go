package ansi

import (
	"strings"
	"testing"
)

func TestToMarkdownPlainText(t *testing.T) {
	result := ToMarkdown("Hello World")
	if strings.TrimSpace(result) != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", result)
	}
}

func TestToMarkdownBold(t *testing.T) {
	// Bold text inline (not a full-line heading)
	input := "Normal \x1b[1mBold\x1b[0m text \x1b[1mand more bold\x1b[0m end"
	result := ToMarkdown(input)
	if !strings.Contains(result, "**Bold**") {
		t.Errorf("expected **Bold** in output, got %q", result)
	}
	if !strings.Contains(result, "**and more bold**") {
		t.Errorf("expected **and more bold** in output, got %q", result)
	}
}

func TestToMarkdownItalic(t *testing.T) {
	input := "Normal \x1b[3mItalic\x1b[0m text"
	result := ToMarkdown(input)
	if !strings.Contains(result, "*Italic*") {
		t.Errorf("expected *Italic* in output, got %q", result)
	}
}

func TestToMarkdownBoldItalic(t *testing.T) {
	input := "\x1b[1;3mBoldItalic\x1b[0m"
	result := ToMarkdown(input)
	if !strings.Contains(result, "***BoldItalic***") {
		t.Errorf("expected ***BoldItalic*** in output, got %q", result)
	}
}

func TestToMarkdownStrikethrough(t *testing.T) {
	input := "\x1b[9mDeleted\x1b[0m"
	result := ToMarkdown(input)
	if !strings.Contains(result, "~~Deleted~~") {
		t.Errorf("expected ~~Deleted~~ in output, got %q", result)
	}
}

func TestToMarkdownHorizontalRule(t *testing.T) {
	input := "───────────────────"
	result := ToMarkdown(input)
	if strings.TrimSpace(result) != "---" {
		t.Errorf("expected '---', got %q", result)
	}
}

func TestToMarkdownInlineCode(t *testing.T) {
	// Text with background color should be detected as inline code
	input := "Use \x1b[44mfmt.Println\x1b[0m to print"
	result := ToMarkdown(input)
	if !strings.Contains(result, "`fmt.Println`") {
		t.Errorf("expected `fmt.Println` in output, got %q", result)
	}
}

func TestToMarkdownHeading(t *testing.T) {
	// Bold-only full line → heading
	input := "\x1b[1mIntroduction\x1b[0m\nSome body text"
	result := ToMarkdown(input)
	if !strings.Contains(result, "### Introduction") && !strings.Contains(result, "## Introduction") && !strings.Contains(result, "# Introduction") {
		t.Errorf("expected heading in output, got %q", result)
	}
	if !strings.Contains(result, "Some body text") {
		t.Errorf("expected body text in output, got %q", result)
	}
}

func TestToMarkdownMultiline(t *testing.T) {
	input := "Line 1\nLine 2\nLine 3"
	result := ToMarkdown(input)
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %q", len(lines), result)
	}
}

func TestToMarkdownExcessiveNewlines(t *testing.T) {
	input := "A\n\n\n\n\nB"
	result := ToMarkdown(input)
	if strings.Contains(result, "\n\n\n") {
		t.Errorf("should not have triple newlines, got %q", result)
	}
}

func TestToPlainText(t *testing.T) {
	input := "\x1b[1;31mHello\x1b[0m \x1b[32mWorld\x1b[0m"
	result := ToPlainText(input)
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", result)
	}
}

func TestToMarkdownHeadingWithColor(t *testing.T) {
	// Bold + bright color → H1
	input := "\x1b[1;91mMain Title\x1b[0m"
	result := ToMarkdown(input)
	if !strings.HasPrefix(strings.TrimSpace(result), "# ") {
		t.Errorf("expected H1 heading, got %q", result)
	}
}
