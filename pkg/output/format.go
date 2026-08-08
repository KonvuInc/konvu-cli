package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

type OutputFormat int

const (
	JSON OutputFormat = iota
	Table
	CSV
)

func DetectOutputFormat(explicit string) OutputFormat {
	switch strings.ToLower(explicit) {
	case "json":
		return JSON
	case "table", "text":
		// "text" is an accepted alias for the human-readable format so an
		// explicit request never silently falls through to auto-detection
		// (which would emit JSON when stdout is piped).
		return Table
	case "csv":
		return CSV
	}
	// Auto-detect: table for TTY, JSON for pipe
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return Table
	}
	return JSON
}

func FormatJSON(data any) string {
	b, _ := json.MarshalIndent(data, "", "  ")
	return string(b)
}

func FormatCSV(data map[string]any, columns []string, listKey string) string {
	var sb strings.Builder
	w := csv.NewWriter(&sb)

	// Header
	w.Write(columns)

	// Rows
	items, _ := data[listKey].([]any)
	for _, item := range items {
		row, _ := item.(map[string]any)
		record := make([]string, len(columns))
		for i, col := range columns {
			record[i] = fmt.Sprintf("%v", row[col])
		}
		w.Write(record)
	}
	w.Flush()
	return sb.String()
}

func FormatQuiet(items []map[string]any, idField string) string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, fmt.Sprintf("%v", item[idField]))
	}
	return strings.Join(ids, "\n")
}

func FilterFields(data map[string]any, fields []string) map[string]any {
	if fields == nil {
		return data
	}
	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}
	result := make(map[string]any)
	for k, v := range data {
		if fieldSet[k] {
			result[k] = v
		}
	}
	return result
}
