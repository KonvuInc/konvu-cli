package cmd

import (
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/api"
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

func TestDetectSCM(t *testing.T) {
	cases := []struct {
		name string
		ml   map[string]any
		want scmType
	}{
		{"github via vcs_source", map[string]any{"vcs_source": "github"}, scmGitHub},
		{"github_autofix_pr via vcs_source", map[string]any{"vcs_source": "github_autofix_pr"}, scmGitHub},
		{"github_on_prem via vcs_source", map[string]any{"vcs_source": "github_on_prem"}, scmGitHub},
		{"gitlab via vcs_source", map[string]any{"vcs_source": "gitlab"}, scmGitLab},
		{"gitlab_autofix_pr via vcs_source", map[string]any{"vcs_source": "gitlab_autofix_pr"}, scmGitLab},
		{"github via url", map[string]any{"vcs_base_url": "https://github.com/acme/web-app"}, scmGitHub},
		{"gitlab via url", map[string]any{"vcs_repository_url": "https://gitlab.com/acme/web-app.git"}, scmGitLab},
		{"unknown source", map[string]any{"vcs_source": "snyk"}, scmUnknown},
		{"empty", map[string]any{}, scmUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectSCM(tc.ml); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMapRemediateAPIErrorInstallLink(t *testing.T) {
	apiErr := &api.APIError{
		Message:    `API error: {"detail":"autofix_integration_missing"}`,
		StatusCode: 422,
	}
	cases := []struct {
		name      string
		scm       scmType
		wantInMsg string
		wantInSug string
	}{
		{"github scm", scmGitHub, "GitHub", "Konvu Autofix GitHub App"},
		{"gitlab scm", scmGitLab, "GitLab", "Konvu GitLab remediation integration"},
		{"unknown scm", scmUnknown, "your SCM", "GitHub Autofix App or GitLab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cliErr := mapRemediateAPIError(apiErr, "fid", tc.scm)
			if !strings.Contains(cliErr.Message, tc.wantInMsg) {
				t.Errorf("Message = %q, want contains %q", cliErr.Message, tc.wantInMsg)
			}
			if !strings.Contains(cliErr.Suggestion, tc.wantInSug) {
				t.Errorf("Suggestion = %q, want contains %q", cliErr.Suggestion, tc.wantInSug)
			}
			if !strings.Contains(cliErr.Suggestion, "http") {
				t.Errorf("Suggestion = %q, want an install URL", cliErr.Suggestion)
			}
		})
	}
}

func TestMapRemediateAPIErrorRepoNotCoveredGitLab(t *testing.T) {
	// The gitlab-specific detail code must force scmGitLab even when the
	// caller didn't detect the SCM from the finding.
	apiErr := &api.APIError{
		Message:    `API error: {"detail":"autofix_repo_not_covered_gitlab"}`,
		StatusCode: 422,
	}
	cliErr := mapRemediateAPIError(apiErr, "fid", scmUnknown)
	if !strings.Contains(cliErr.Message, "GitLab") {
		t.Errorf("expected GitLab in message, got: %s", cliErr.Message)
	}
	if !strings.Contains(cliErr.Suggestion, "GitLab") {
		t.Errorf("expected GitLab in suggestion, got: %s", cliErr.Suggestion)
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
