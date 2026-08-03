package cmd

import (
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
	guardrailsCmd.AddCommand(guardrailsConnectCmd)
	guardrailsCmd.AddCommand(guardrailsScanCmd)
	guardrailsCmd.AddCommand(guardrailsListCmd)
	guardrailsCmd.AddCommand(guardrailsShowCmd)
	guardrailsCmd.AddCommand(guardrailsApproveCmd)
	guardrailsCmd.AddCommand(guardrailsReviewCmd)
	guardrailsCmd.AddCommand(guardrailsExplainCmd)
	rootCmd.AddCommand(guardrailsCmd)
}

// requestedBranch is the branch a command acts on: what the caller asked for, else "" meaning
// "not stated, you decide".
//
// Nothing is inferred from the checkout, deliberately. The obvious local source is
// refs/remotes/origin/HEAD, and it is a cache with no invalidation: a clone records it once and a
// plain `git fetch` after the remote's default branch is renamed leaves the stale name in place
// (only `git fetch --prune` repairs it). Sending a branch is what makes it authoritative, so
// reading that ref would let a months-old clone address a branch no pull request targets --
// the same silent miss this command set exists to prevent, through a different door.
//
// So the rule is: say what you were told, and otherwise say nothing. The server holds the
// integration and asks the host for the current default, which cannot go stale.
func requestedBranch(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString("branch")
	return v
}

// branchParam carries the branch only when the caller named one. An omitted field asks the server
// to resolve the repository's default; sending a guess is indistinguishable from a deliberate
// choice, and stops it resolving.
func branchParam(branch string) map[string]any {
	if branch == "" {
		return map[string]any{}
	}
	return map[string]any{"branch": branch}
}
