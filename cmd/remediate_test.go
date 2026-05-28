package cmd

import (
	"testing"
)

func TestRemediateSubcommands(t *testing.T) {
	subs := map[string]bool{}
	for _, c := range remediateCmd.Commands() {
		subs[c.Name()] = true
	}
	for _, name := range []string{"run", "status"} {
		if !subs[name] {
			t.Errorf("remediate missing subcommand: %s", name)
		}
	}
}

func TestRemediateRunFlags(t *testing.T) {
	flags := []string{"source-url", "wait", "timeout", "poll-interval", "output"}
	for _, flag := range flags {
		if remediateRunCmd.Flags().Lookup(flag) == nil {
			t.Errorf("remediate run missing flag: --%s", flag)
		}
	}
}

func TestRemediateStatusFlags(t *testing.T) {
	flags := []string{"wait", "timeout", "poll-interval", "output"}
	for _, flag := range flags {
		if remediateStatusCmd.Flags().Lookup(flag) == nil {
			t.Errorf("remediate status missing flag: --%s", flag)
		}
	}
}

func TestExtractAPIErrorDetail(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"integration missing", `API error: {"detail":"autofix_integration_missing"}`, "autofix_integration_missing"},
		{"repo not covered", `API error: {"detail":"autofix_repo_not_covered"}`, "autofix_repo_not_covered"},
		{"gitlab variant", `API error: {"detail":"autofix_repo_not_covered_gitlab"}`, "autofix_repo_not_covered_gitlab"},
		{"no body", `connection refused`, ""},
		{"malformed json", `API error: {detail:`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractAPIErrorDetail(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRemediateTerminalStatuses(t *testing.T) {
	terminal := []string{"succeeded", "failed", "merged", "closed"}
	nonTerminal := []string{"pending", "running"}
	for _, s := range terminal {
		if !remediateTerminalStatuses[s] {
			t.Errorf("expected %s to be terminal", s)
		}
	}
	for _, s := range nonTerminal {
		if remediateTerminalStatuses[s] {
			t.Errorf("expected %s to NOT be terminal", s)
		}
	}
}
