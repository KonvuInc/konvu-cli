package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

func TestFindingDetailTextPrioritizesAssessmentAndEvidence(t *testing.T) {
	result := map[string]any{
		"assessment": map[string]any{
			"status":  "exploitable",
			"summary": "Reachable from an unauthenticated request.",
			"checklist": []any{map[string]any{
				"description": "The vulnerable function is reachable",
				"conclusion":  "Observed in the request path",
				"proofs": []any{map[string]any{
					"file": "src/server.go", "line": float64(42), "comment": "Request handler calls the package",
				}},
			}},
		},
		"finding": map[string]any{
			"id": "finding-1", "dependency": "lodash", "repository": "github:acme/api", "state": "open",
		},
		"vulnerability": map[string]any{
			"cve": "CVE-2026-1234", "severity": "critical", "has_fix": "fixed", "summary": "Vulnerability summary.",
		},
	}
	text := findingDetailText(result)
	for _, want := range []string{
		"CVE-2026-1234 · lodash", "ASSESSMENT", "Exploitable", "WHY THIS ASSESSMENT",
		"Observed in the request path", "VULNERABILITY", "FINDING",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("finding detail missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "ASSESSMENT") > strings.Index(text, "VULNERABILITY") {
		t.Fatalf("assessment should precede vulnerability details:\n%s", text)
	}
	if strings.Contains(text, "src/server.go:42") || strings.Contains(text, "Request handler calls the package") {
		t.Fatalf("finding detail should omit proof-level evidence:\n%s", text)
	}
}

func TestExecuteFindingTUIReturnsToListFromDetail(t *testing.T) {
	options := []output.FindingOption{{Finding: "CVE-2026-1234 · example"}}
	var outputBuffer bytes.Buffer
	command := &cobra.Command{Use: "fixture"}
	command.SetOut(&outputBuffer)
	pickCalls := 0
	detailCalls := 0
	err := executeFindingOptionsTUI(options, findingTUIDependencies{
		pick: func(options []output.FindingOption, selected int) (int, bool, error) {
			pickCalls++
			if pickCalls == 1 {
				return 0, true, nil
			}
			return 0, false, nil
		},
		detail: func(index int) (string, error) {
			detailCalls++
			return "Finding detail", nil
		},
		open: func(content string) (output.BaselineWorkspaceOutcome, error) {
			return output.BaselineWorkspaceBack, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pickCalls != 2 || detailCalls != 1 {
		t.Fatalf("pick calls = %d, detail calls = %d", pickCalls, detailCalls)
	}
}

func TestTypedFindingDetailText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "sast",
			text: sastFindingDetailText(map[string]any{
				"title": "SQL Injection", "severity": "critical", "assessment": "exploitable",
				"repo": "github:acme/api", "location": "routes/search.ts:23", "cwe_ids": []any{"CWE-89"},
			}),
			want: []string{"SAST FINDING · HOSTED", "ASSESSMENT", "Exploitable", "SQL Injection", "routes/search.ts:23", "CWE-89"},
		},
		{
			name: "secret",
			text: secretFindingDetailText(map[string]any{
				"provider": "AWS", "assessment": "applicable", "verification_status": "verified", "id": "secret-1",
			}),
			want: []string{"SECRET FINDING · HOSTED", "ASSESSMENT", "Applicable", "SECRET", "Verified"},
		},
		{
			name: "container",
			text: containerFindingDetailText(map[string]any{
				"cve": "CVE-2026-1234", "package": "openssl", "severity": "high",
				"assessment": "false_positive", "image": "payments", "tag": "latest",
			}),
			want: []string{"CONTAINER FINDING · HOSTED", "ASSESSMENT", "False positive", "VULNERABILITY", "payments:latest"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, want := range test.want {
				if !strings.Contains(test.text, want) {
					t.Fatalf("detail missing %q:\n%s", want, test.text)
				}
			}
		})
	}
}

func TestFindingBrowserHelpDescribesInteractiveAndMachineOutputModes(t *testing.T) {
	for _, command := range []*cobra.Command{
		findingListCmd,
		scaListCmd,
		sastListCmd,
		secretsListCmd,
		containerListCmd,
	} {
		if !strings.Contains(command.Long, "interactively") ||
			!strings.Contains(command.Long, "non-interactive output") {
			t.Errorf("%s help does not describe both output modes:\n%s", command.CommandPath(), command.Long)
		}
	}
	if !strings.Contains(findingCmd.Long, "first 50 findings") {
		t.Errorf("finding help does not describe the bare-command limit:\n%s", findingCmd.Long)
	}
}
