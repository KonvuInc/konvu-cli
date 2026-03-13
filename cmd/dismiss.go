package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/KonvuTeam/konvu-cli/pkg/api"
	"github.com/KonvuTeam/konvu-cli/pkg/mapping"
	"github.com/KonvuTeam/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var dismissCmd = &cobra.Command{
	Use:   "dismiss",
	Short: "Dismiss security issues",
}

var dismissRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Dismiss security issues",
	Long: "Dismiss security issues.",
	Example: `  # Preview dismissals
  konvu dismiss run --assessment false-positive --dry-run

  # Dismiss specific issues
  konvu dismiss run --issues id1,id2 --reason "Not applicable"

  # Dismiss all false positives in a repo
  konvu dismiss run --assessment false-positive --repo github:org/repo`,
	RunE: runDismiss,
}

func runDismiss(cmd *cobra.Command, args []string) error {
	issuesList, _ := cmd.Flags().GetString("issues")
	assessments, _ := cmd.Flags().GetStringArray("assessment")
	severities, _ := cmd.Flags().GetStringArray("severity")
	repo, _ := cmd.Flags().GetString("repo")
	reason, _ := cmd.Flags().GetString("reason")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	outputFlag, _ := cmd.Flags().GetString("output")

	if issuesList == "" && len(assessments) == 0 {
		fmt.Fprintln(os.Stderr, "Error: Must specify --issues or --assessment filter")
		os.Exit(1)
	}

	client := api.NewClient("", "")
	defer client.Close()

	type dismissItem struct {
		IntegrationID string `json:"integration_id,omitempty"`
		IssueID       string `json:"issue_id,omitempty"`
		CVE           string `json:"cve,omitempty"`
		Repository    string `json:"repository,omitempty"`
	}

	var toDismiss []dismissItem

	// If using filters, first query matching issues
	if len(assessments) > 0 || len(severities) > 0 || repo != "" {
		params := map[string]any{
			"per_page": "500",
		}

		if len(assessments) > 0 {
			var recommendations []string
			for _, a := range assessments {
				normalized := strings.ToLower(strings.ReplaceAll(a, "_", "-"))
				status := mapping.AssessmentStatus(normalized)
				valid := false
				for _, s := range mapping.AllStatuses {
					if s == status {
						valid = true
						break
					}
				}
				if !valid {
					fmt.Fprintf(os.Stderr, "Invalid assessment: %s\n", a)
					os.Exit(1)
				}
				recommendations = append(recommendations, mapping.AssessmentToRecommendation(status)...)
			}
			params["recommendation"] = recommendations
		}

		if len(severities) > 0 {
			upper := make([]string, len(severities))
			for i, s := range severities {
				upper[i] = strings.ToUpper(s)
			}
			params["severity"] = upper
		}

		if repo != "" {
			params["vcs_repository_url"] = []string{repo}
		}

		params["any_source_state"] = []string{"open"}

		data, err := client.Get("/sca_issues", params)
		if err != nil {
			if _, ok := err.(*api.AuthenticationError); ok {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "API Error:", err)
			os.Exit(1)
		}

		items, _ := data["items"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			sources, _ := item["sources"].([]any)

			vuln, _ := item["vulnerability"].(map[string]any)
			cve, _ := vuln["cve_number"].(string)

			manifestLoc, _ := item["manifest_location"].(map[string]any)
			repoURL, _ := manifestLoc["repository_url"].(string)

			for _, rawSrc := range sources {
				src, _ := rawSrc.(map[string]any)
				integrationID, _ := src["integration_id"].(string)
				issueID, _ := src["id"].(string)
				toDismiss = append(toDismiss, dismissItem{
					IntegrationID: integrationID,
					IssueID:       issueID,
					CVE:           cve,
					Repository:    repoURL,
				})
			}
		}
	} else if issuesList != "" {
		// Parse comma-separated issue IDs
		ids := strings.Split(issuesList, ",")
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id != "" {
				toDismiss = append(toDismiss, dismissItem{IssueID: id})
			}
		}
	}

	if len(toDismiss) == 0 {
		fmt.Println("No issues found matching criteria.")
		return nil
	}

	// Build items for output (first 50)
	limit := 50
	if len(toDismiss) < limit {
		limit = len(toDismiss)
	}

	type outputItemJSON struct {
		IntegrationID string `json:"integration_id,omitempty"`
		IssueID       string `json:"issue_id,omitempty"`
		CVE           string `json:"cve,omitempty"`
		Repository    string `json:"repository,omitempty"`
	}
	previewItems := make([]outputItemJSON, limit)
	for i := 0; i < limit; i++ {
		previewItems[i] = outputItemJSON{
			IntegrationID: toDismiss[i].IntegrationID,
			IssueID:       toDismiss[i].IssueID,
			CVE:           toDismiss[i].CVE,
			Repository:    toDismiss[i].Repository,
		}
	}

	outputFormat := output.DetectOutputFormat(outputFlag)

	if dryRun {
		msg := fmt.Sprintf("Would dismiss %d issues. Use without --dry-run to execute.", len(toDismiss))
		if outputFormat == output.JSON {
			jsonOut := map[string]any{
				"action":  "dismiss",
				"dry_run": true,
				"reason":  reason,
				"total":   len(toDismiss),
				"items":   previewItems,
				"message": msg,
			}
			fmt.Println(output.FormatJSON(jsonOut))
		} else {
			fmt.Printf("\nDry run: would dismiss %d issues\n", len(toDismiss))
			fmt.Printf("Reason: %s\n\n", reason)
			showLimit := 10
			if len(toDismiss) < showLimit {
				showLimit = len(toDismiss)
			}
			for i := 0; i < showLimit; i++ {
				item := toDismiss[i]
				label := item.CVE
				if label == "" {
					label = item.IssueID
				}
				repo := item.Repository
				if repo == "" {
					repo = "unknown"
				}
				fmt.Printf("  - %s in %s\n", label, repo)
			}
			if len(toDismiss) > 10 {
				fmt.Printf("  ... and %d more\n", len(toDismiss)-10)
			}
		}
		return nil
	}

	// Execute dismissals
	type failedItem struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	var succeeded []outputItemJSON
	var failed []failedItem

	for _, item := range toDismiss {
		integrationID := item.IntegrationID
		issueID := item.IssueID

		if integrationID == "" || issueID == "" {
			failed = append(failed, failedItem{
				ID:    issueID,
				Error: "Missing integration_id or issue_id",
			})
			continue
		}

		_, err := client.Post(
			fmt.Sprintf("/integrations/%s/issue/%s/dismiss", integrationID, issueID),
			map[string]any{"reason": reason},
		)
		if err != nil {
			if _, ok := err.(*api.AuthenticationError); ok {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			failed = append(failed, failedItem{ID: issueID, Error: err.Error()})
		} else {
			succeeded = append(succeeded, outputItemJSON{
				IntegrationID: integrationID,
				IssueID:       issueID,
				CVE:           item.CVE,
				Repository:    item.Repository,
			})
		}
	}

	if outputFormat == output.JSON {
		jsonOut := map[string]any{
			"action":  "dismiss",
			"dry_run": false,
			"reason":  reason,
			"total":   len(toDismiss),
			"items":   previewItems,
			"results": map[string]any{
				"succeeded": len(succeeded),
				"failed":    len(failed),
			},
			"succeeded": succeeded,
			"failed":    failed,
		}
		fmt.Println(output.FormatJSON(jsonOut))
	} else {
		fmt.Printf("\nDismissed %d issues\n", len(succeeded))
		if len(failed) > 0 {
			fmt.Printf("Failed: %d\n", len(failed))
			showLimit := 5
			if len(failed) < showLimit {
				showLimit = len(failed)
			}
			for i := 0; i < showLimit; i++ {
				f := failed[i]
				fmt.Printf("  - %s: %s\n", f.ID, f.Error)
			}
		}
	}

	return nil
}

func addDismissFlags(cmd *cobra.Command) {
	cmd.Flags().String("issues", "", "Comma-separated list of issue IDs to dismiss")
	cmd.Flags().StringArrayP("assessment", "a", nil, "Filter: dismiss all with this assessment (e.g., false-positive)")
	cmd.Flags().StringArrayP("severity", "s", nil, "Filter by severity")
	cmd.Flags().StringP("repo", "r", "", "Filter by repository")
	cmd.Flags().String("reason", "Dismissed via Konvu CLI", "Reason for dismissal")
	cmd.Flags().Bool("dry-run", false, "Preview what would be dismissed without executing")
	cmd.Flags().StringP("output", "o", "", "Output format: json, table")
}

func init() {
	addDismissFlags(dismissRunCmd)
	dismissCmd.AddCommand(dismissRunCmd)

	// Top-level `konvu dismiss` convenience alias: runs directly without requiring `run` subcommand
	dismissCmd.RunE = runDismiss
	addDismissFlags(dismissCmd)

	rootCmd.AddCommand(dismissCmd)
}
