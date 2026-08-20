package cmd

import (
	"github.com/spf13/cobra"
)

var guardrailsCmd = &cobra.Command{
	Use:   "guardrails",
	Short: "Access rules for your code, checked on every pull request",
	Long: `Access rules for your code, checked on every pull request.

Konvu scans your repo, drafts the access rules your code already enforces — who
may do what — and you approve them. From then on every pull request is checked
against them, and anything that breaks a rule is flagged down to the line that
did it.

These commands run against the repo you are in and use your 'konvu login' session.`,
}

func init() {
	rootCmd.AddCommand(guardrailsCmd)
}
