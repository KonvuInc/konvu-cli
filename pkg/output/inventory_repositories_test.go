package output

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestInventoryRepositoryTableKeepsRichMetadata(t *testing.T) {
	options := []InventoryRepositoryOption{{
		Repository:    "payments",
		Source:        "local",
		ThreatProfile: "92 · Crown jewel",
		SecurityGraph: "a17c2e99 · ready",
		Updated:       "2026-09-07 12:00",
		Assets:        "7",
		Controls:      "4",
	}}
	table := RenderInventoryRepositoryTableWidth(options, 180)
	for _, want := range []string{
		"Inventory", "Repository", "Source", "Threat profile", "Security graph",
		"Updated", "Assets", "Controls", "payments", "Crown jewel", "a17c2e99",
	} {
		if !strings.Contains(table, want) {
			t.Fatalf("inventory table missing %q:\n%s", want, table)
		}
	}
	for _, removed := range []string{"Duration", "Cost", "Latest map", "Implementations"} {
		if strings.Contains(table, removed) {
			t.Fatalf("inventory table still contains removed column %q:\n%s", removed, table)
		}
	}
}

func TestInventoryRepositoryPickerUsesBaselineNavigation(t *testing.T) {
	options := []InventoryRepositoryOption{{Repository: "one"}, {Repository: "two"}, {Repository: "three"}}
	var writer bytes.Buffer
	index, opened, err := pickInventoryRepositoryIO(
		bufio.NewReader(strings.NewReader("\x1b[B\r")),
		&writer,
		options,
		0,
		false,
		func() int { return 120 },
	)
	if err != nil || !opened || index != 1 {
		t.Fatalf("picker result = index %d, opened %v, error %v", index, opened, err)
	}
}
