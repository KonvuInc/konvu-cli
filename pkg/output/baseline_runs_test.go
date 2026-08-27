package output

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestBaselineRunTableContainsCatalogMetadata(t *testing.T) {
	table := RenderBaselineRunTable([]BaselineRunOption{{
		ID:              "payments--a17c2e9--000042",
		Repository:      "payments",
		Commit:          "a17c2e9987654321",
		Scanned:         "2026-08-27 10:00",
		Duration:        "12.5s",
		Assets:          7,
		Controls:        4,
		Implementations: 3,
		Status:          "completed",
	}})
	for _, want := range []string{
		"Repository", "Commit", "Run", "Scanned", "Time", "Assets", "Controls", "Implementations", "Status",
		"payments", "a17c2e9", "12.5s", "completed",
	} {
		if !strings.Contains(table, want) {
			t.Fatalf("run table is missing %q:\n%s", want, table)
		}
	}
}

func TestBaselineRunPickerNavigationAndEscape(t *testing.T) {
	options := []BaselineRunOption{{ID: "one"}, {ID: "two"}, {ID: "three"}}
	var writer bytes.Buffer
	index, opened, err := pickBaselineRunIO(
		bufio.NewReader(strings.NewReader("\x1b[B\r")),
		&writer,
		options,
		0,
		false,
	)
	if err != nil || !opened || index != 1 {
		t.Fatalf("picker result = index %d, opened %v, error %v", index, opened, err)
	}

	writer.Reset()
	index, opened, err = pickBaselineRunIO(
		bufio.NewReader(strings.NewReader("\x1b")),
		&writer,
		options,
		2,
		false,
	)
	if err != nil || opened || index != 2 {
		t.Fatalf("escape result = index %d, opened %v, error %v", index, opened, err)
	}
}

func TestBaselineRunDiagnosticsUsesActionableFallback(t *testing.T) {
	diagnostic := BaselineRunDiagnostics(BaselineRunOption{
		ID:         "payments--a17c2e9--000042",
		Repository: "payments",
		Status:     "failed",
	})
	for _, want := range []string{"payments", "failed", "run.log"} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("diagnostic is missing %q:\n%s", want, diagnostic)
		}
	}
}
