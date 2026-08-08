package output

import (
	"strings"
	"testing"
)

func TestDetectOutputFormat_Explicit(t *testing.T) {
	if f := DetectOutputFormat("json"); f != JSON {
		t.Errorf("got %v, want JSON", f)
	}
	if f := DetectOutputFormat("TABLE"); f != Table {
		t.Errorf("got %v, want Table", f)
	}
	if f := DetectOutputFormat("csv"); f != CSV {
		t.Errorf("got %v, want CSV", f)
	}
	// "text" is an explicit alias for the human-readable (table) format and
	// must not fall through to auto-detection.
	if f := DetectOutputFormat("text"); f != Table {
		t.Errorf("got %v, want Table for \"text\"", f)
	}
}

func TestDetectOutputFormat_Empty(t *testing.T) {
	// When no explicit format, behavior depends on isatty.
	// In test context stdout is not a TTY, so default is JSON.
	f := DetectOutputFormat("")
	if f != JSON {
		t.Errorf("got %v, want JSON (non-TTY default)", f)
	}
}

func TestFormatJSON(t *testing.T) {
	data := map[string]any{"key": "value", "num": 42}
	got := FormatJSON(data)
	if !strings.Contains(got, `"key": "value"`) {
		t.Errorf("FormatJSON missing expected content: %s", got)
	}
}

func TestFormatCSV(t *testing.T) {
	data := map[string]any{
		"items": []any{
			map[string]any{"id": "1", "name": "foo"},
			map[string]any{"id": "2", "name": "bar"},
		},
	}
	got := FormatCSV(data, []string{"id", "name"}, "items")
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 { // header + 2 rows
		t.Errorf("FormatCSV got %d lines, want 3", len(lines))
	}
}

func TestFormatQuiet(t *testing.T) {
	items := []map[string]any{
		{"id": "abc-1"},
		{"id": "abc-2"},
	}
	got := FormatQuiet(items, "id")
	if got != "abc-1\nabc-2" {
		t.Errorf("FormatQuiet = %q, want %q", got, "abc-1\nabc-2")
	}
}

func TestFilterFields(t *testing.T) {
	data := map[string]any{"a": 1, "b": 2, "c": 3}
	got := FilterFields(data, []string{"a", "c"})
	if len(got) != 2 {
		t.Errorf("FilterFields returned %d fields, want 2", len(got))
	}
}
