package cmd

import (
	"github.com/spf13/cobra"
)

var (
	guardrailsScanAPIKey string
	guardrailsScanModel  string
)

var guardrailsScanCmd = &cobra.Command{
	Use:   "scan [repo]",
	Short: "Scan a repo and write a profile to .konvu/guardrails/",
	Long: `Scan a repo and write a profile to .konvu/guardrails/.

This is a thin wrapper: konvu fetches and caches the guardrails-cli binary on
first use, then runs it directly. All scanning, profile generation, and
output rendering happen in that binary -- konvu only streams its stdout/stderr
through live and exits with its real exit code.

Set OPENAI_API_KEY or pass --openai-api-key to provide public OpenAI
credentials for this run. Explicit credentials are passed to the child process
only and are not written to disk.`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runGuardrailsExec(append([]string{"scan"}, args...), guardrailsScanAPIKey, guardrailsScanModel)
	},
}

func init() {
	guardrailsScanCmd.Flags().StringVar(&guardrailsScanAPIKey, "openai-api-key", "", "OpenAI API key (prefer OPENAI_API_KEY to avoid shell history)")
	guardrailsScanCmd.Flags().StringVar(&guardrailsScanModel, "openai-model", "gpt-4o", "OpenAI model used with --openai-api-key")
	guardrailsCmd.AddCommand(guardrailsScanCmd)
}
