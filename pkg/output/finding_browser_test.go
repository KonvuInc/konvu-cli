package output

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestFindingTableShowsPrioritizationColumns(t *testing.T) {
	options := []FindingOption{{
		Severity:   "CRITICAL",
		Assessment: "Exploitable",
		Finding:    "CVE-2026-1234 · lodash",
		Summary:    "Reachable from an unauthenticated route",
		Repository: "github:acme/payments",
		State:      "open",
	}}
	table := RenderFindingTableWidth(options, 140)
	for _, want := range []string{
		"Findings", "Severity", "Assessment", "Finding", "Assessment summary", "Repository", "State",
		"CRITICAL", "Exploitable", "CVE-2026-1234", "Reachable from an unauthenticated", "github:acme/payments",
	} {
		if !strings.Contains(table, want) {
			t.Fatalf("finding table missing %q:\n%s", want, table)
		}
	}
}

func TestFindingTableColorsAssessmentResults(t *testing.T) {
	options := []FindingOption{
		{Assessment: "Exploitable", Finding: "first"},
		{Assessment: "False positive", Finding: "second"},
	}
	table := renderFindingTable(options, -1, baselineStyle{enabled: true}, "\n", 120)
	for _, want := range []string{"\033[1;31mExploitable", "\033[32mFalse positive"} {
		if !strings.Contains(table, want) {
			t.Fatalf("finding table missing assessment color %q:\n%s", want, table)
		}
	}
}

func TestFindingTableLabelsTypedBrowser(t *testing.T) {
	table := RenderFindingTableWidth([]FindingOption{{Kind: "SAST", Finding: "SQL Injection"}}, 100)
	if !strings.Contains(table, "SAST findings") || !strings.Contains(table, "Repository / asset") {
		t.Fatalf("typed finding browser labels are missing:\n%s", table)
	}
}

func TestFindingPickerUsesInventoryNavigation(t *testing.T) {
	options := []FindingOption{{Finding: "one"}, {Finding: "two"}, {Finding: "three"}}
	var writer bytes.Buffer
	index, opened, err := pickFindingIO(
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
