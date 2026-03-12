package output

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/KonvuTeam/konvu-cli/pkg/mapping"
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

func FormatTable(data map[string]any, columns []string, listKey string, styleCell StyleCellFunc) string {
	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 4, 2, ' ', 0)

	// Header
	headers := make([]string, len(columns))
	for i, col := range columns {
		headers[i] = titleCase(strings.ReplaceAll(col, "_", " "))
	}
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	// Rows
	items, _ := data[listKey].([]any)
	for _, item := range items {
		row, _ := item.(map[string]any)
		cells := make([]string, len(columns))
		for i, col := range columns {
			val := fmt.Sprintf("%v", row[col])
			if styleCell != nil {
				val = styleCell(col, val)
			}
			cells[i] = val
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	w.Flush()
	return sb.String()
}

// DefaultStyleCell applies color to assessment columns.
func DefaultStyleCell(column, value string) string {
	if column == "assessment" {
		return mapping.Colorize(value, mapping.AssessmentStatus(value))
	}
	return value
}

// PrintStderr prints a message to stderr.
func PrintStderr(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}
