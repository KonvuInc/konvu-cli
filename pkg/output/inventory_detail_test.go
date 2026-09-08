package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderInventoryRepositoryDetailShowsNavigation(t *testing.T) {
	var writer bytes.Buffer
	lines := []string{"Repository", "", "Threat Profile · Hosted", "Score: 92"}
	if err := renderInventoryRepositoryDetail(&writer, lines, 0, 4, 80, baselineStyle{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Repository", "Threat Profile · Hosted", "↑↓ scroll", "←/Esc repositories", "Q quit"} {
		if !strings.Contains(writer.String(), want) {
			t.Errorf("detail missing %q:\n%s", want, writer.String())
		}
	}
}

func TestRenderInventoryRepositoryDetailScrolls(t *testing.T) {
	var writer bytes.Buffer
	lines := []string{"zero", "one", "two", "three"}
	if err := renderInventoryRepositoryDetail(&writer, lines, 2, 2, 80, baselineStyle{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(writer.String(), "zero") || !strings.Contains(writer.String(), "two") || !strings.Contains(writer.String(), "three") {
		t.Errorf("unexpected scrolled detail:\n%s", writer.String())
	}
}

func TestInventoryDetailWrapsSummaryAndIndentedEvidence(t *testing.T) {
	lines := []string{
		"This is a long summary that should wrap into multiple readable lines rather than being truncated at the terminal edge.",
		"    • This is long evidence that should wrap while preserving indentation for its continuation.",
	}
	wrapped := inventoryDetailWrapLines(lines, 60)
	if len(wrapped) < 4 {
		t.Fatalf("wrapped lines = %v", wrapped)
	}
	for _, line := range wrapped {
		if visibleLen(line) > 58 {
			t.Errorf("line width = %d, want <= 58: %q", visibleLen(line), line)
		}
	}
	if !strings.HasPrefix(wrapped[len(wrapped)-1], "      ") {
		t.Errorf("evidence continuation lost indentation: %q", wrapped[len(wrapped)-1])
	}
}

func TestFindingDetailColorsAssessmentStatus(t *testing.T) {
	got := inventoryDetailStyledLine("Status           Exploitable", baselineStyle{enabled: true})
	if !strings.Contains(got, "\033[1;31mExploitable\033[0m") {
		t.Fatalf("assessment status is not colored red: %q", got)
	}
}
