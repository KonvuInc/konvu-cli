package cmd

import (
	"github.com/spf13/cobra"
)

var guardrailsExplainCmd = &cobra.Command{
	Use:   "explain <name> [profile-dir]",
	Short: "Explain one entry from the profile by name",
	Long: `Explain one entry from the profile by name -- a guard, a weakness, a risk
category, an asset, or a file.

Exits 1 if name doesn't resolve to any of them.

Thin wrapper over the cached guardrails-cli binary -- see 'konvu guardrails
scan --help' for the shared bootstrap behavior.`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runGuardrailsReadOnly(append([]string{"explain"}, args...))
	},
}

func init() {
	guardrailsCmd.AddCommand(guardrailsExplainCmd)
}
