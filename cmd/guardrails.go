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
// the repository being named, else "main".
//
// The default branch, not the one you are standing on. A baseline describes what pull requests are
// measured against, and they target the default branch -- so a developer on `feature/x` still means
// `master`, and recording a baseline for the feature branch would file it at an address no pull
// request queries and that disappears at merge.
//
// It is read from the checkout only when that checkout IS the repository being named: `show` and
// `ratify` take the repository as an argument, so run from an unrelated directory they would
// otherwise borrow a stranger's branch. `dir` is the checkout to read, which is not always "." --
// `baseline ../web` records somewhere else, and that is the repository whose default branch labels
// the result.
func resolveBranch(cmd *cobra.Command, repo, dir string) string {
	if v, _ := cmd.Flags().GetString("branch"); v != "" {
		return v
	}
	if sameRepo(repo, gitbundle.RepoSlug(dir)) {
		if b := gitbundle.DefaultBranch(dir); b != "" {
			return b
		}
	}
	return "main"
}

// sameRepo compares two "owner/name" ids. GitHub treats them case-insensitively and so must this,
// or standing in `acmekonvu/pygoat` while naming `AcmeKonvu/pygoat` silently stops inferring.
func sameRepo(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(a, b)
}
