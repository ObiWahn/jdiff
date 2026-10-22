package theme

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ansiNames are delta's names for the 16 ANSI colors.
var ansiNames = map[string]int{
	"black": 0, "red": 1, "green": 2, "yellow": 3,
	"blue": 4, "magenta": 5, "purple": 5, "cyan": 6, "white": 7,
}

// ParseStyle parses a delta style string: attributes, then an optional
// foreground and background color. Color words that are neither a known
// color nor "normal" are looked up as delta.<word> in cfg, like delta does.
// Words that only matter to delta (omit, raw, box, ...) are ignored.
func ParseStyle(s string, cfg Config) (lipgloss.Style, error) {
	style := lipgloss.NewStyle()
	colors := 0
	for _, word := range strings.Fields(strings.ToLower(s)) {
		word = strings.Trim(word, `"'`)
		switch word {
		case "bold":
			style = style.Bold(true)
		case "dim":
			style = style.Faint(true)
		case "italic":
			style = style.Italic(true)
		case "ul", "underline":
			style = style.Underline(true)
		case "blink":
			style = style.Blink(true)
		case "reverse":
			style = style.Reverse(true)
		case "strike":
			style = style.Strikethrough(true)
		case "hidden", "omit", "raw", "line-number", "file", "omit-code-fragment", "box", "ol", "none":
		default:
			c, err := parseColor(word, cfg)
			if err != nil {
				return lipgloss.NewStyle(), fmt.Errorf("style %q: %w", s, err)
			}
			switch colors {
			case 0:
				if c != "" {
					style = style.Foreground(c)
				}
			case 1:
				if c != "" {
					style = style.Background(c)
				}
			default:
				return lipgloss.NewStyle(), fmt.Errorf("style %q: more than two colors", s)
			}
			colors++
		}
	}
	return style, nil
}

// parseColor returns the color for word, or "" for no color. Colors beyond
// the 16 ANSI ones are passed on as-is; the 16 color profile maps them to
// the nearest ANSI color when rendering.
func parseColor(word string, cfg Config) (lipgloss.Color, error) {
	switch word {
	case "normal", "auto", "syntax":
		return "", nil
	}
	if n, ok := ansiNames[word]; ok {
		return lipgloss.Color(strconv.Itoa(n)), nil
	}
	if base, ok := strings.CutPrefix(word, "bright"); ok {
		if n, ok := ansiNames[strings.TrimPrefix(base, "-")]; ok {
			return lipgloss.Color(strconv.Itoa(n + 8)), nil
		}
	}
	if n, err := strconv.Atoi(word); err == nil && n >= 0 && n <= 255 {
		return lipgloss.Color(word), nil
	}
	if isHexColor(word) {
		return lipgloss.Color(word), nil
	}
	if v, ok := cfg["delta."+word]; ok && !strings.HasPrefix(word, "delta.") {
		// A named color defined elsewhere in the config; no further nesting.
		return parseColor(strings.ToLower(strings.TrimSpace(v)), nil)
	}
	return "", fmt.Errorf("unknown color %q", word)
}

func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(s[1:], 16, 32)
	return err == nil
}
