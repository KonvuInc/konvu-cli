package cmd

import (
	"github.com/spf13/cobra"
)

var (
	guardrailsExplainAPIKey string
	guardrailsExplainModel  string
)

var guardrailsExplainCmd = &cobra.Command{
	Use:   "explain <name> [profile-dir]",
	Short: "Explain a guard or Track-B weakness by name",
	Long: `Explain a guard or Track-B weakness by name.

Exits 1 if name doesn't resolve to either.

Thin wrapper over the cached guardrails-cli binary -- see 'konvu guardrails
scan --help' for the shared bootstrap/credentials behavior.`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runGuardrailsExec(append([]string{"explain"}, args...), guardrailsExplainAPIKey, guardrailsExplainModel)
	},
}

func init() {
	guardrailsExplainCmd.Flags().StringVar(&guardrailsExplainAPIKey, "openai-api-key", "", "OpenAI API key; writes ~/.config/guardrails/credentials")
	guardrailsExplainCmd.Flags().StringVar(&guardrailsExplainModel, "openai-model", "gpt-4o", "OpenAI model, written alongside --openai-api-key")
	guardrailsCmd.AddCommand(guardrailsExplainCmd)
}
