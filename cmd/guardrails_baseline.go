package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const guardrailsBaselineModel = "gpt-5.6-luna"

var (
	guardrailsBaselineAPIKey string
	guardrailsBaselineYes    bool
)

type guardrailsRunner func(args []string, apiKey, model string)

var guardrailsBaselineCmd = &cobra.Command{
	Use:   "baseline",
	Short: "Codebase security baselines",
	Long: `Codebase security baselines

A baseline is a run-scoped graph of assets, controls, implementations,
resources, routes, classes, roles, and supporting observations.`,
	Example: `  # List recent baseline runs
  konvu guardrails baseline list

  # Filter runs by repository name or path
  konvu guardrails baseline list --repo <repository>

  # Get a run with selected details
  konvu guardrails baseline get <run-id> --include architecture,counts,stages

  # Search records within a run
  konvu guardrails baseline records search "manual override" --run <run-id>

  # Explain a record and its relationships
  konvu guardrails baseline records explain <record-id> --run <run-id> --depth 2`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

var guardrailsBaselineScanCmd = &cobra.Command{
	Use:   "scan <codebase>",
	Short: "Scan a codebase and create a baseline",
	Long: `Index a repository, estimate the remaining scan, and ask whether to
continue. The Guardrails runtime owns the complete workflow and records the run
as baseline.json and run.log under ~/.konvu/guardrails/baselines.

An OpenAI API key is required only when continuing into model-backed steps.
Set OPENAI_API_KEY or pass --openai-api-key. The key is provided only to the
Guardrails child process and is not written to disk. Use --yes to continue
without prompting.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		apiKey := resolveGuardrailsAPIKey(guardrailsBaselineAPIKey, os.Getenv("OPENAI_API_KEY"))
		runGuardrailsBaselineScan(
			args[0],
			apiKey,
			guardrailsBaselineYes,
			runGuardrailsExec,
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
) {
	args := []string{"baseline", "scan", repo}
	if yes {
		args = append(args, "--yes")
	}
	run(args, apiKey, guardrailsBaselineModel)
}

func init() {
	guardrailsBaselineScanCmd.Flags().StringVar(&guardrailsBaselineAPIKey, "openai-api-key", "", "OpenAI API key (prefer OPENAI_API_KEY to avoid shell history)")
	guardrailsBaselineScanCmd.Flags().BoolVarP(&guardrailsBaselineYes, "yes", "y", false, "continue without prompting")
	guardrailsBaselineScanCmd.Flags().BoolVar(
		&guardrailsNoSandbox,
		"no-sandbox",
		false,
		"run the Guardrails runtime without OS filesystem isolation",
	)
	guardrailsBaselineCmd.AddCommand(guardrailsBaselineScanCmd)
	guardrailsCmd.AddCommand(guardrailsBaselineCmd)
}
