package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var remediateBriefCmd = &cobra.Command{
	Use:   "brief [plan-id...]",
	Short: "Fetch remediation plan briefs ready to hand to a coding agent",
	Long: `Fetch one or more remediation plans with their packages, findings,
assessments, and a ready-to-use agent prompt.

By default prints the agent prompt(s) to stdout so you can pipe them straight
into a coding agent (e.g. konvu remediate brief <id> | claude -p). Use
--output json for the full structured payload.

Exit codes: 0 success, 1 general error, 3 not found, 4 auth failed`,
	Example: `  konvu remediate brief 01997d40-8253-7ab3-b813-f3caa73bf913
  konvu remediate brief id1 id2 id3
  konvu remediate brief id1 --output json`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		outputFlag, _ := cmd.Flags().GetString("output")

		client := api.NewClient("", "")
		defer client.Close()

		briefs := make([]map[string]any, 0, len(args))
		for _, planID := range args {
			fmt.Fprintf(os.Stderr, "Fetching plan %s...\n", planID)
			brief, err := client.Get("/remediations/plans/"+planID, nil)
			if err != nil {
				if _, ok := err.(*api.AuthenticationError); ok {
					fmt.Fprintf(os.Stderr, "Error: %s\n", err)
					os.Exit(clierrors.ExitAuthFailed)
				}
				if apiErr, ok := err.(*api.APIError); ok && apiErr.StatusCode == 404 {
					fmt.Fprintf(os.Stderr, "Plan %s not found.\n", planID)
					os.Exit(clierrors.ExitNotFound)
				}
				fmt.Fprintf(os.Stderr, "API Error: %s\n", err)
				os.Exit(1)
			}
			briefs = append(briefs, brief)
		}

		// The prompt is the deliverable, so it stays the default even when
		// piped — `--output json` is the explicit opt-in for raw data.
		if outputFlag == "json" {
			fmt.Println(output.FormatJSON(map[string]any{"items": briefs}))
			return nil
		}
		fmt.Println(strings.Join(agentPrompts(briefs), "\n\n---\n\n"))
		return nil
	},
}

// agentPrompts extracts the server-built agent prompt from each plan brief.
func agentPrompts(briefs []map[string]any) []string {
	prompts := make([]string, 0, len(briefs))
	for _, b := range briefs {
		if p, _ := b["agent_prompt"].(string); p != "" {
			prompts = append(prompts, p)
		}
	}
	return prompts
}

func init() {
	remediateBriefCmd.Flags().StringP("output", "o", "", "Output format: prompt (default), json")

	remediateCmd.AddCommand(remediateBriefCmd)
}
