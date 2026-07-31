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
	guardrailsCmd.AddCommand(guardrailsInstallCmd)
	guardrailsCmd.AddCommand(guardrailsBaselineCmd)
	guardrailsCmd.AddCommand(guardrailsListCmd)
	guardrailsCmd.AddCommand(guardrailsShowCmd)
	guardrailsCmd.AddCommand(guardrailsRatifyCmd)
	guardrailsCmd.AddCommand(guardrailsReviewCmd)
	guardrailsCmd.AddCommand(guardrailsExplainCmd)
	rootCmd.AddCommand(guardrailsCmd)
}
