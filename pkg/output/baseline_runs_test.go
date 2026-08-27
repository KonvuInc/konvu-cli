package output

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestBaselineRunTableContainsCatalogMetadata(t *testing.T) {
	table := RenderBaselineRunTable([]BaselineRunOption{{
		ID:              "payments--a17c2e99--000042",
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
		"Repository", "Commit", "Run", "Scanned", "Duration", "Assets", "Controls", "Implementations", "Status",
		"payments", "a17c2e99", "12.5s", "completed",
	} {
		if !strings.Contains(table, want) {
			t.Fatalf("run table is missing %q:\n%s", want, table)
		}
	}
}

func TestBaselineRunTableRespondsAtCommonTerminalWidths(t *testing.T) {
	option := BaselineRunOption{
		ID:              "payments-service-with-a-long-name--a17c2e99--000042",
		Repository:      "payments-service-with-a-long-name",
		Commit:          "a17c2e9987654321",
		Scanned:         "2026-08-27 10:00",
		Duration:        "12.5s",
		Assets:          7,
		Controls:        4,
		Implementations: 3,
		Status:          "completed",
	}
	for _, width := range []int{80, 100, 120} {
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			table := RenderBaselineRunTableWidth([]BaselineRunOption{option}, width)
			for _, line := range strings.Split(strings.TrimSuffix(table, "\n"), "\n") {
				if got := visibleLen(line); got > width {
					t.Fatalf("line width = %d, want <= %d:\n%s", got, width, table)
				}
			}
			for _, required := range []string{"Repository", "Commit", "Run", "Status", "a17c2e99"} {
				if !strings.Contains(table, required) {
					t.Fatalf("width %d missing %q:\n%s", width, required, table)
				}
			}
			if width >= 100 && (!strings.Contains(table, "Scanned") || !strings.Contains(table, "Duration")) {
				t.Fatalf("width %d omitted full metadata:\n%s", width, table)
			}
		})
	}
}

func TestBaselinePhysicalLineCountIncludesTerminalWraps(t *testing.T) {
	if got := baselinePhysicalLineCount("123456789\r\nx\r\n", 4); got != 4 {
		t.Fatalf("physical lines = %d, want 4", got)
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
		func() int { return 100 },
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
		func() int { return 100 },
	)
	if err != nil || opened || index != 2 {
		t.Fatalf("escape result = index %d, opened %v, error %v", index, opened, err)
	}
}

func TestBaselineRunDiagnosticsUsesActionableFallback(t *testing.T) {
	diagnostic := BaselineRunDiagnostics(BaselineRunOption{
		ID:         "payments--a17c2e99--000042",
		Repository: "payments",
		Status:     "failed",
	})
	for _, want := range []string{"payments", "failed", "run.log"} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("diagnostic is missing %q:\n%s", want, diagnostic)
		}
	}
}
