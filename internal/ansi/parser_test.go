package ansi

import (
	"testing"
)

func TestParseEmpty(t *testing.T) {
	spans := Parse("")
	if len(spans) != 0 {
		t.Errorf("expected 0 spans, got %d", len(spans))
	}
}

func TestParsePlainText(t *testing.T) {
	spans := Parse("Hello World")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Text != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", spans[0].Text)
	}
	if spans[0].Style != StyleNone {
		t.Errorf("expected StyleNone, got %d", spans[0].Style)
	}
}

func TestParseBold(t *testing.T) {
	// ESC[1m Hello ESC[0m
	input := "\x1b[1mHello\x1b[0m World"
	spans := Parse(input)
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	if spans[0].Text != "Hello" || spans[0].Style&StyleBold == 0 {
		t.Errorf("span 0: expected bold 'Hello', got style=%d text=%q", spans[0].Style, spans[0].Text)
	}
	if spans[1].Text != " World" || spans[1].Style != StyleNone {
		t.Errorf("span 1: expected plain ' World', got style=%d text=%q", spans[1].Style, spans[1].Text)
	}
}

func TestParseForegroundColor(t *testing.T) {
	// ESC[31m Red text ESC[0m
	input := "\x1b[31mRed\x1b[0m"
	spans := Parse(input)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].FgCol == nil {
		t.Fatal("expected foreground color")
	}
	if spans[0].FgCol.Type != ColorBasic || spans[0].FgCol.Value != 1 {
		t.Errorf("expected basic color 1 (red), got type=%d value=%d", spans[0].FgCol.Type, spans[0].FgCol.Value)
	}
}

func TestParse256Color(t *testing.T) {
	// ESC[38;5;208m Orange ESC[0m
	input := "\x1b[38;5;208mOrange\x1b[0m"
	spans := Parse(input)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].FgCol == nil {
		t.Fatal("expected foreground color")
	}
	if spans[0].FgCol.Type != Color256 || spans[0].FgCol.Value != 208 {
		t.Errorf("expected 256-color 208, got type=%d value=%d", spans[0].FgCol.Type, spans[0].FgCol.Value)
	}
}

func TestParseTrueColor(t *testing.T) {
	// ESC[38;2;255;128;0m TrueColor ESC[0m
	input := "\x1b[38;2;255;128;0mTrueColor\x1b[0m"
	spans := Parse(input)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].FgCol == nil {
		t.Fatal("expected foreground color")
	}
	if spans[0].FgCol.Type != ColorRGB {
		t.Fatalf("expected RGB color, got %d", spans[0].FgCol.Type)
	}
	if spans[0].FgCol.R != 255 || spans[0].FgCol.G != 128 || spans[0].FgCol.B != 0 {
		t.Errorf("expected RGB(255,128,0), got (%d,%d,%d)", spans[0].FgCol.R, spans[0].FgCol.G, spans[0].FgCol.B)
	}
}

func TestParseMultipleStyles(t *testing.T) {
	// Bold + Italic
	input := "\x1b[1;3mBoldItalic\x1b[0m"
	spans := Parse(input)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Style&StyleBold == 0 || spans[0].Style&StyleItalic == 0 {
		t.Errorf("expected bold+italic, got style=%d", spans[0].Style)
	}
}

func TestParseBrightColors(t *testing.T) {
	// ESC[91m Bright Red ESC[0m
	input := "\x1b[91mBright\x1b[0m"
	spans := Parse(input)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].FgCol == nil {
		t.Fatal("expected foreground color")
	}
	if spans[0].FgCol.Value != 9 { // 91 - 90 + 8 = 9
		t.Errorf("expected bright red (9), got %d", spans[0].FgCol.Value)
	}
}

func TestParseStrikethrough(t *testing.T) {
	input := "\x1b[9mDeleted\x1b[0m"
	spans := Parse(input)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Style&StyleStrikethrough == 0 {
		t.Errorf("expected strikethrough, got %d", spans[0].Style)
	}
}

func TestStripANSI(t *testing.T) {
	input := "\x1b[1;31mHello\x1b[0m \x1b[32mWorld\x1b[0m"
	result := Strip(input)
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", result)
	}
}

func TestParseBackgroundColor(t *testing.T) {
	input := "\x1b[44mBlueBG\x1b[0m"
	spans := Parse(input)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].BgCol == nil {
		t.Fatal("expected background color")
	}
	if spans[0].BgCol.Value != 4 {
		t.Errorf("expected blue (4), got %d", spans[0].BgCol.Value)
	}
}

func TestParseOSCSequence(t *testing.T) {
	// OSC with BEL terminator
	input := "Before\x1b]0;Title\x07After"
	spans := Parse(input)
	text := ""
	for _, s := range spans {
		text += s.Text
	}
	if text != "BeforeAfter" {
		t.Errorf("expected 'BeforeAfter', got %q", text)
	}
}
