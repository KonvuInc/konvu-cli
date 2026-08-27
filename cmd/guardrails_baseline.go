package cmd

import (
	"os"
	"strings"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const guardrailsBaselineModel = "gpt-5.6-luna"

var (
	guardrailsBaselineRepo   string
	guardrailsBaselineAPIKey string
	guardrailsBaselineYes    bool
)

type guardrailsRunner func(args []string, apiKey, model string)
type guardrailsConfirmer func(prompt string, defaultYes bool) bool

var guardrailsBaselineCmd = &cobra.Command{
	Use:   "baseline",
	Short: "Create a normalized security baseline for a repository",
}

var guardrailsBaselineScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Index, estimate, and run a baseline scan",
	Long: `Index a repository and estimate the remaining baseline scan before asking
whether to continue. The accepted run uses public OpenAI with gpt-5.6-luna and
writes its final normalized graph to protections.json.

Set OPENAI_API_KEY in the environment or pass --openai-api-key. The key is
provided only to the guardrails child process and is not written to disk.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		apiKey := resolveGuardrailsAPIKey(guardrailsBaselineAPIKey, os.Getenv("OPENAI_API_KEY"))
		if apiKey == "" {
			reportGuardrailsError(&clierrors.CLIError{
				Code:       "MISSING_OPENAI_API_KEY",
				Message:    "an OpenAI API key is required for a baseline scan",
				Suggestion: "Set OPENAI_API_KEY or pass --openai-api-key.",
				ExitCode:   clierrors.ExitUsageError,
			})
		}

		runGuardrailsBaselineScan(
			guardrailsBaselineRepo,
			apiKey,
			guardrailsBaselineYes,
			runGuardrailsExec,
			output.Confirm,
		)
	},
}

func resolveGuardrailsAPIKey(flagValue, envValue string) string {
	if value := strings.TrimSpace(flagValue); value != "" {
		return value
	}
	return strings.TrimSpace(envValue)
}

func runGuardrailsBaselineScan(
	repo, apiKey string,
	yes bool,
	run guardrailsRunner,
	confirm guardrailsConfirmer,
) {
	run([]string{"baseline", "prepare", repo}, "", "")
	if !yes && !confirm("Continue with the baseline scan?", false) {
		return
	}
	run([]string{"baseline", "continue", repo}, apiKey, guardrailsBaselineModel)
}

func init() {
	guardrailsBaselineScanCmd.Flags().StringVar(&guardrailsBaselineRepo, "repo", ".", "repository path to scan")
	guardrailsBaselineScanCmd.Flags().StringVar(&guardrailsBaselineAPIKey, "openai-api-key", "", "OpenAI API key (prefer OPENAI_API_KEY to avoid shell history)")
	guardrailsBaselineScanCmd.Flags().BoolVarP(&guardrailsBaselineYes, "yes", "y", false, "continue without prompting")
	guardrailsBaselineCmd.AddCommand(guardrailsBaselineScanCmd)
	guardrailsCmd.AddCommand(guardrailsBaselineCmd)
}
