package cmd

import (
	"github.com/spf13/cobra"
)

var guardrailsCmd = &cobra.Command{
	Use:   "guardrails",
	Short: "Scan your repo and see what's protected, and what isn't",
	Long: `Scan your repo and see what's protected, and what isn't.

'scan' writes a profile to .konvu/guardrails/: what's worth protecting, what
already protects it, and what doesn't. 'show', 'list', and 'explain' read
that profile back.`,
}

func init() {
	rootCmd.AddCommand(guardrailsCmd)
}
