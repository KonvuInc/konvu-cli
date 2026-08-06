package output

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

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

// visibleLen returns the visible length of a string (ignoring ANSI codes),
// counted in runes so multibyte glyphs (e.g. "—") occupy one column, not one
// column per UTF-8 byte — otherwise cells holding them under-pad and the
// following columns drift left.
func visibleLen(s string) int {
	return utf8.RuneCountInString(stripAnsi(s))
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
