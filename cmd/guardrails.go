package cmd

import (
	"github.com/spf13/cobra"
)

var guardrailsNoSandbox bool

var guardrailsCmd = &cobra.Command{
	Use:   "guardrails",
	Short: "Scan and explore codebase security baselines",
	Long: `Scan and explore codebase security baselines.

All Guardrails operations live under 'konvu guardrails baseline'. Scans are
stored by run and can be listed or explored from any working directory.`,
}

func init() {
	rootCmd.AddCommand(guardrailsCmd)
}
