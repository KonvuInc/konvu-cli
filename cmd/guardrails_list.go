package cmd

import (
	"github.com/spf13/cobra"
)

var (
	guardrailsListAPIKey string
	guardrailsListModel  string
)

var guardrailsListCmd = &cobra.Command{
	Use:   "list [registry.json]",
	Short: "List past guardrails scans",
	Long: `List past guardrails scans, read from ~/.cache/guardrails/scans.json by
default, or from the given registry file.

Thin wrapper over the cached guardrails-cli binary -- see 'konvu guardrails
scan --help' for the shared bootstrap behavior.`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runGuardrailsReadOnly(append([]string{"list"}, args...))
	},
}

func init() {
	guardrailsListCmd.Flags().StringVar(&guardrailsListAPIKey, "openai-api-key", "", "OpenAI API key (prefer OPENAI_API_KEY to avoid shell history)")
	guardrailsListCmd.Flags().StringVar(&guardrailsListModel, "openai-model", "gpt-4o", "OpenAI model used with --openai-api-key")
	guardrailsCmd.AddCommand(guardrailsListCmd)
}
