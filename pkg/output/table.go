package output

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/KonvuInc/konvu-cli/pkg/mapping"
	"golang.org/x/term"
)

// StyleCellFunc optionally styles a cell value. Return value is printed as-is.
type StyleCellFunc func(column, value string) string

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// termWidth returns the terminal width, defaulting to 120 if unavailable.
func termWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 120
	}
	return w
}

// stripAnsi removes ANSI escape sequences to get the visible length.
func stripAnsi(s string) string {
	result := strings.Builder{}
	inEsc := false
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

// visibleLen returns the number of terminal cells a string occupies, ignoring
// ANSI codes. Measuring in bytes (or even runes) misaligns columns: a 3-byte
// glyph like "—" is one cell, a CJK ideograph or emoji is two, and a combining
// mark is zero. This is a pragmatic subset of wcwidth — enough to keep
// backend-provided repo names/summaries aligned — not a full East Asian Width
// implementation (that would need a dependency).
func visibleLen(s string) int {
	w := 0
	for _, r := range stripAnsi(s) {
		w += runeCells(r)
	}
	return w
}

// runeCells returns the terminal-cell width of a single rune: 0 for combining
// marks and zero-width joiners, 2 for East Asian wide ranges and most emoji,
// 1 otherwise.
func runeCells(r rune) int {
	switch {
	case r == 0:
		return 0
	case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r),
		r == '\u200b', r == '\u200c', r == '\u200d', r == '\ufeff':
		return 0
	case isWideRune(r):
		return 2
	default:
		return 1
	}
}

// isWideRune reports whether r renders in two terminal cells. The ranges cover
// the common East Asian Wide / Fullwidth blocks and emoji; it is an
// approximation, not the full Unicode EastAsianWidth table.
func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2329 && r <= 0x232A,   // angle brackets
		r >= 0x2E80 && r <= 0x303E,   // CJK radicals, Kangxi, punctuation
		r >= 0x3041 && r <= 0x33FF,   // Hiragana .. CJK compatibility
		r >= 0x3400 && r <= 0x4DBF,   // CJK Extension A
		r >= 0x4E00 && r <= 0x9FFF,   // CJK Unified Ideographs
		r >= 0xA000 && r <= 0xA4CF,   // Yi
		r >= 0xAC00 && r <= 0xD7A3,   // Hangul Syllables
		r >= 0xF900 && r <= 0xFAFF,   // CJK Compatibility Ideographs
		r >= 0xFE10 && r <= 0xFE19,   // Vertical forms
		r >= 0xFE30 && r <= 0xFE6F,   // CJK Compatibility Forms
		r >= 0xFF00 && r <= 0xFF60,   // Fullwidth Forms
		r >= 0xFFE0 && r <= 0xFFE6,   // Fullwidth signs
		r >= 0x1F000 && r <= 0x1F02F, // Mahjong
		r >= 0x1F300 && r <= 0x1FAFF, // Emoji & pictographs
		r >= 0x20000 && r <= 0x3FFFD: // CJK Extension B and beyond
		return true
	default:
		return false
	}
}

// truncate shortens s to maxLen visible characters, adding "…" if truncated.
// Preserves trailing ANSI reset sequences.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 || visibleLen(s) <= maxLen {
		return s
	}
	visible := 0
	inEsc := false
	var result strings.Builder
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			result.WriteRune(r)
			continue
		}
		if inEsc {
			result.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if visible >= maxLen-1 {
			result.WriteRune('…')
			// Only close a sequence we opened: an unconditional reset writes a bare escape into
			// plain text, which renders as "[0m" wherever output is not a terminal.
			if strings.ContainsRune(s, '\033') {
				result.WriteString("\033[0m")
			}
			return result.String()
		}
		result.WriteRune(r)
		visible++
	}
	return result.String()
}

const colGap = 2

// wordWrap splits text into lines of at most width characters, breaking at word boundaries.
func wordWrap(text string, width int) []string {
	if width <= 0 || len(text) <= width {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) <= width {
			line += " " + w
		} else {
			lines = append(lines, line)
			line = w
		}
	}
	lines = append(lines, line)
	return lines
}

func FormatTable(data map[string]any, columns []string, listKey string, styleCell StyleCellFunc) string {
	items, _ := data[listKey].([]any)

	// Build headers
	headers := make([]string, len(columns))
	for i, col := range columns {
		headers[i] = titleCase(strings.ReplaceAll(col, "_", " "))
	}

	// Build raw cell values (styled)
	rows := make([][]string, len(items))
	for r, item := range items {
		row, _ := item.(map[string]any)
		cells := make([]string, len(columns))
		for c, col := range columns {
			val := fmt.Sprintf("%v", row[col])
			if styleCell != nil {
				val = styleCell(col, val)
			}
			cells[c] = val
		}
		rows[r] = cells
	}

	// Compute natural widths (visible chars, ignoring ANSI)
	colWidths := make([]int, len(columns))
	for i, h := range headers {
		colWidths[i] = len(h)
	}
	for _, cells := range rows {
		for i, cell := range cells {
			vl := visibleLen(cell)
			if vl > colWidths[i] {
				colWidths[i] = vl
			}
		}
	}

	// Fit columns to terminal width.
	// Strategy: shrink non-last columns proportionally to free space
	// for the last column (typically assessment_summary).
	tw := termWidth()
	totalGap := colGap * (len(columns) - 1)
	lastCol := len(columns) - 1

	// Reserve at least 40% of terminal width for the last column
	lastColMin := tw * 40 / 100
	if lastColMin < 30 {
		lastColMin = 30
	}
	budget := tw - totalGap - lastColMin

	// Shrink non-last columns to fit within budget
	usedByOthers := 0
	for i := 0; i < lastCol; i++ {
		usedByOthers += colWidths[i]
	}
	if usedByOthers > budget && budget > 0 {
		for i := 0; i < lastCol; i++ {
			colWidths[i] = colWidths[i] * budget / usedByOthers
			if colWidths[i] < len(headers[i]) {
				colWidths[i] = len(headers[i])
			}
		}
	}

	// Give last column all remaining space
	usedByOthers = 0
	for i := 0; i < lastCol; i++ {
		usedByOthers += colWidths[i]
	}
	remaining := tw - totalGap - usedByOthers
	if remaining < 20 {
		remaining = 20
	}
	colWidths[lastCol] = remaining

	// Render
	var sb strings.Builder
	// Header
	for i, h := range headers {
		if i > 0 {
			sb.WriteString(strings.Repeat(" ", colGap))
		}
		sb.WriteString(h)
		if i < len(headers)-1 {
			pad := colWidths[i] - len(h)
			if pad > 0 {
				sb.WriteString(strings.Repeat(" ", pad))
			}
		}
	}
	sb.WriteString("\n")

	// Separator
	for i, w := range colWidths {
		if i > 0 {
			sb.WriteString(strings.Repeat(" ", colGap))
		}
		if i == len(colWidths)-1 {
			// Last column separator matches header text width
			sb.WriteString(strings.Repeat("─", len(headers[i])))
		} else {
			sb.WriteString(strings.Repeat("─", w))
		}
	}
	sb.WriteString("\n")

	// Compute indent for continuation lines (width of all non-last columns + gaps)
	indent := 0
	for i := 0; i < lastCol; i++ {
		indent += colWidths[i] + colGap
	}

	// Rows
	for _, cells := range rows {
		// Render non-last columns on the first line
		for i := 0; i < lastCol; i++ {
			if i > 0 {
				sb.WriteString(strings.Repeat(" ", colGap))
			}
			truncated := truncate(cells[i], colWidths[i])
			sb.WriteString(truncated)
			pad := colWidths[i] - visibleLen(truncated)
			if pad > 0 {
				sb.WriteString(strings.Repeat(" ", pad))
			}
		}

		// Word-wrap the last column
		lastVal := stripAnsi(cells[lastCol])
		wrapped := wordWrap(lastVal, colWidths[lastCol])
		for li, line := range wrapped {
			if li == 0 {
				sb.WriteString(strings.Repeat(" ", colGap))
			} else {
				sb.WriteString(strings.Repeat(" ", indent))
			}
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// DefaultStyleCell applies color to the assessment column and strips the konvu VCS URL prefix from repositories.
func DefaultStyleCell(column, value string) string {
	switch column {
	case "assessment":
		return mapping.Colorize(value, mapping.AssessmentStatus(value))
	case "repository":
		for _, prefix := range []string{"github:", "gitlab:"} {
			if strings.HasPrefix(value, prefix) {
				return strings.TrimPrefix(value, prefix)
			}
		}
	}
	return value
}

// PrintStderr prints a message to stderr.
func PrintStderr(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}
