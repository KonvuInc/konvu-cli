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

// branchOrCheckout is the branch a command should act on: what was asked for, else the branch you
// are standing on WHEN that checkout is the repo being named, else "main".
//
// The default used to be the constant "main" while `baseline` bundled HEAD, so on a repository
// whose default branch is `master` the two disagreed and nothing noticed: the baseline recorded
// under a branch no pull request has, `show` said it was there, and the gate -- which looks up the
// PR's base branch -- found nothing.
//
// `repo` is what makes the inference safe. For `baseline` the checkout IS the subject, but `show`
// and `ratify` take the repository as an argument and the directory is incidental: run
// `ratify AcmeKonvu/pygoat` from an unrelated checkout sitting on `main` and an unconditional
// inference hands you `main` -- another repository's branch name, addressing a baseline that may
// well exist and be the wrong one. Pass "" for repo when there is nothing to compare against.
//
// `dir` is the checkout to read, which is not always ".": `baseline ../web` bundles somewhere else,
// and that is the checkout whose branch labels the result.
func branchOrCheckout(cmd *cobra.Command, repo, dir string) string {
	fallback, _ := cmd.Flags().GetString("branch")
	if cmd.Flags().Changed("branch") {
		return fallback
	}
	if repo == "" || !sameRepo(repo, gitbundle.RepoSlug(dir)) {
		return fallback
	}
	if b := gitbundle.CurrentBranch(dir); b != "" {
		return b
	}
	return fallback
}

// sameRepo compares two "owner/name" ids. GitHub treats them case-insensitively and so must this,
// or standing in `acmekonvu/pygoat` while naming `AcmeKonvu/pygoat` silently stops inferring.
func sameRepo(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(a, b)
}
