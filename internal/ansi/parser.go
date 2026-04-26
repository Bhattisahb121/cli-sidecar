package ansi

import (
	"strconv"
	"strings"
)

// Style flags for text formatting.
const (
	StyleNone          uint = 0
	StyleBold          uint = 1 << 0
	StyleFaint         uint = 1 << 1
	StyleItalic        uint = 1 << 2
	StyleUnderline     uint = 1 << 3
	StyleBlink         uint = 1 << 4
	StyleInverse       uint = 1 << 5
	StyleHidden        uint = 1 << 6
	StyleStrikethrough uint = 1 << 7
)

// Color represents a parsed ANSI color.
type Color struct {
	Type  ColorType
	Value int // 0-15 for basic, 0-255 for 256-color
	R, G, B int // for true color
}

// ColorType distinguishes color encoding.
type ColorType int

const (
	ColorNone  ColorType = iota
	ColorBasic           // 0-7 standard, 8-15 bright
	Color256             // 0-255
	ColorRGB             // 24-bit true color
)

// Span represents a piece of styled text.
type Span struct {
	Text  string
	Style uint
	FgCol *Color
	BgCol *Color
}

// Parse takes a string with ANSI escape codes and returns structured spans.
func Parse(input string) []Span {
	var spans []Span
	var currentStyle uint
	var currentFg, currentBg *Color
	var textBuf strings.Builder

	i := 0
	n := len(input)

	flushText := func() {
		if textBuf.Len() > 0 {
			spans = append(spans, Span{
				Text:  textBuf.String(),
				Style: currentStyle,
				FgCol: currentFg,
				BgCol: currentBg,
			})
			textBuf.Reset()
		}
	}

	for i < n {
		// Check for ESC (0x1B)
		if input[i] == 0x1B && i+1 < n && input[i+1] == '[' {
			flushText()

			// Find end of CSI sequence
			j := i + 2
			for j < n && !isCSITerminator(input[j]) {
				j++
			}
			if j >= n {
				// Incomplete sequence, skip ESC
				i++
				continue
			}

			terminator := input[j]
			paramStr := input[i+2 : j]

			if terminator == 'm' {
				// SGR (Select Graphic Rendition)
				currentStyle, currentFg, currentBg = applySGR(paramStr, currentStyle, currentFg, currentBg)
			}
			// Skip other CSI sequences (cursor movement, etc.)

			i = j + 1
			continue
		}

		// Check for OSC sequences: ESC ]
		if input[i] == 0x1B && i+1 < n && input[i+1] == ']' {
			flushText()
			// Skip until ST (ESC \) or BEL (0x07)
			j := i + 2
			for j < n {
				if input[j] == 0x07 {
					j++
					break
				}
				if input[j] == 0x1B && j+1 < n && input[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
			continue
		}

		textBuf.WriteByte(input[i])
		i++
	}

	flushText()
	return spans
}

func isCSITerminator(b byte) bool {
	return b >= 0x40 && b <= 0x7E
}

func applySGR(params string, style uint, fg, bg *Color) (uint, *Color, *Color) {
	if params == "" || params == "0" {
		return StyleNone, nil, nil
	}

	codes := splitSGRParams(params)
	i := 0
	for i < len(codes) {
		code := codes[i]
		switch {
		case code == 0:
			style = StyleNone
			fg = nil
			bg = nil
		case code == 1:
			style |= StyleBold
		case code == 2:
			style |= StyleFaint
		case code == 3:
			style |= StyleItalic
		case code == 4:
			style |= StyleUnderline
		case code == 5 || code == 6:
			style |= StyleBlink
		case code == 7:
			style |= StyleInverse
		case code == 8:
			style |= StyleHidden
		case code == 9:
			style |= StyleStrikethrough

		// Reset individual styles
		case code == 21 || code == 22:
			style &^= StyleBold | StyleFaint
		case code == 23:
			style &^= StyleItalic
		case code == 24:
			style &^= StyleUnderline
		case code == 25:
			style &^= StyleBlink
		case code == 27:
			style &^= StyleInverse
		case code == 28:
			style &^= StyleHidden
		case code == 29:
			style &^= StyleStrikethrough

		// Foreground colors (30-37)
		case code >= 30 && code <= 37:
			fg = &Color{Type: ColorBasic, Value: code - 30}

		// Extended foreground: 38;5;n or 38;2;r;g;b
		case code == 38:
			fg, i = parseExtendedColor(codes, i)

		// Default foreground
		case code == 39:
			fg = nil

		// Background colors (40-47)
		case code >= 40 && code <= 47:
			bg = &Color{Type: ColorBasic, Value: code - 40}

		// Extended background: 48;5;n or 48;2;r;g;b
		case code == 48:
			bg, i = parseExtendedColor(codes, i)

		// Default background
		case code == 49:
			bg = nil

		// Bright foreground (90-97)
		case code >= 90 && code <= 97:
			fg = &Color{Type: ColorBasic, Value: code - 90 + 8}

		// Bright background (100-107)
		case code >= 100 && code <= 107:
			bg = &Color{Type: ColorBasic, Value: code - 100 + 8}
		}
		i++
	}

	return style, fg, bg
}

func parseExtendedColor(codes []int, i int) (*Color, int) {
	if i+1 >= len(codes) {
		return nil, i
	}

	switch codes[i+1] {
	case 5: // 256-color
		if i+2 < len(codes) {
			return &Color{Type: Color256, Value: codes[i+2]}, i + 2
		}
	case 2: // True color
		if i+4 < len(codes) {
			return &Color{
				Type: ColorRGB,
				R:    codes[i+2],
				G:    codes[i+3],
				B:    codes[i+4],
			}, i + 4
		}
	}

	return nil, i + 1
}

func splitSGRParams(s string) []int {
	parts := strings.Split(s, ";")
	result := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			result = append(result, 0)
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		result = append(result, n)
	}
	return result
}

// Strip removes all ANSI escape codes and returns plain text.
func Strip(input string) string {
	spans := Parse(input)
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}
