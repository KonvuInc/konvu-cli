package cmd

import (
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
// are standing on, else "main".
//
// The default used to be the constant "main" while `baseline` bundled HEAD, so on a repository
// whose default branch is `master` the two disagreed and nothing noticed: the baseline recorded
// under a branch no pull request has, `show` said it was there, and the gate -- which looks up the
// PR's base branch -- found nothing. `--repo` has always been inferred from the remote; this is
// the same idea for the other half of the address.
func branchOrCheckout(cmd *cobra.Command) string {
	if cmd.Flags().Changed("branch") {
		v, _ := cmd.Flags().GetString("branch")
		return v
	}
	if b := gitbundle.CurrentBranch("."); b != "" {
		return b
	}
	v, _ := cmd.Flags().GetString("branch")
	return v
}
