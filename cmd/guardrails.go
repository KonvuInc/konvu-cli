package cmd

import (
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/gitbundle"
	"github.com/spf13/cobra"
)

// Guardrails endpoints are reached through the Konvu API gateway, so they use the same
// login and the same base URL as every other command — there is no second credential.
const guardrailsAPI = "/services/guardrails/v1"

var guardrailsCmd = &cobra.Command{
	Use:   "guardrails",
	Short: "Authorization baselines and checks",
	Long: `Authorization baselines and checks.

Guardrails records the authorization your code enforces as a baseline, then reports
when a change drifts from it. These commands run against the repo you are in and
use your 'konvu login' session.`,
}

func init() {
	guardrailsCmd.AddCommand(guardrailsInstallCmd)
	guardrailsCmd.AddCommand(guardrailsBaselineCmd)
	guardrailsCmd.AddCommand(guardrailsListCmd)
	guardrailsCmd.AddCommand(guardrailsShowCmd)
	guardrailsCmd.AddCommand(guardrailsRatifyCmd)
	guardrailsCmd.AddCommand(guardrailsReviewCmd)
	guardrailsCmd.AddCommand(guardrailsExplainCmd)
	rootCmd.AddCommand(guardrailsCmd)
}

// resolveBranch is the branch a command acts on: what was asked for, else the default branch of
// the repository being named, else "" meaning "I do not know, you decide".
//
// The DEFAULT branch, not the checked-out one: a baseline describes what pull requests are
// measured against, so a developer on `feature/x` still means `master`.
//
// Read from the checkout only when that checkout IS the repository named, since `show` and
// `ratify` take it as an argument and would otherwise borrow an unrelated repo's branch. `dir` is
// the checkout to read: `baseline ../web` records somewhere else.
//
// Empty must travel as an omitted field, never as "main" -- only the server can ask GitHub for a
// repository's default, and a client-side guess overrides an answer it would have got right.
func resolveBranch(cmd *cobra.Command, repo, dir string) string {
	if v, _ := cmd.Flags().GetString("branch"); v != "" {
		return v
	}
	if sameRepo(repo, gitbundle.RepoSlug(dir)) {
		return gitbundle.DefaultBranch(dir)
	}
	return ""
}

// sameRepo compares two "owner/name" ids. GitHub treats them case-insensitively and so must this,
// or standing in `acmekonvu/pygoat` while naming `AcmeKonvu/pygoat` silently stops inferring.
func sameRepo(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(a, b)
}

// branchParam carries the branch only when we know it. An omitted field asks the server to resolve
// the repository's default; sending a guess instead would override an answer it can look up.
func branchParam(branch string) map[string]any {
	if branch == "" {
		return map[string]any{}
	}
	return map[string]any{"branch": branch}
}
