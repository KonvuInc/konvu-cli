package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/mapping"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var dismissCmd = &cobra.Command{
	Use:   "dismiss",
	Short: "Dismiss security issues",
}

var dismissRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Dismiss security issues",
	Long: `Dismiss security issues.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed`,
	Example: `  # Preview dismissals
  konvu dismiss run --assessment false-positive --dry-run

  # Dismiss specific issues
  konvu dismiss run --issues id1,id2 --reason "Not applicable"

  # Dismiss all false positives in a repo
  konvu dismiss run --assessment false-positive --repo github:org/repo`,
	RunE: runDismiss,
}

type dismissalSkip struct {
	FindingID string `json:"finding_id"`
	Reason    string `json:"reason"`
}

func findingDismissibility(finding map[string]any) (bool, string, bool) {
	source := getMap(finding, "source")
	if state := getStr(source, "state"); state != "open" {
		return false, "not_open", true
	}
	dismissible, present := getBool(source, "dismissible_from_konvu")
	if !present {
		return false, "", false
	}
	if !dismissible {
		return false, "not_dismissible_from_konvu", true
	}
	return true, "", true
}

func previewDismissals(client *api.Client, issueIDs []string, listed map[string]map[string]any) (int, []dismissalSkip, error) {
	dismissible := 0
	skipped := make([]dismissalSkip, 0)
	for _, findingID := range issueIDs {
		finding := listed[findingID]
		eligible, reason, known := findingDismissibility(finding)
		if finding == nil || !known {
			detail, err := client.Get(fmt.Sprintf("/sca_findings/%s", findingID), nil)
			if err != nil {
				if apiErr, ok := err.(*api.APIError); ok && apiErr.StatusCode == 404 {
					skipped = append(skipped, dismissalSkip{FindingID: findingID, Reason: "not_found"})
					continue
				}
				return 0, nil, err
			}
			eligible, reason, _ = findingDismissibility(detail)
		}
		if eligible {
			dismissible++
		} else {
			skipped = append(skipped, dismissalSkip{FindingID: findingID, Reason: reason})
		}
	}
	return dismissible, skipped, nil
}

func responseSkippedFindings(result map[string]any) []dismissalSkip {
	skipped := make([]dismissalSkip, 0)
	for _, raw := range getSlice(result, "skipped") {
		item, _ := raw.(map[string]any)
		skipped = append(skipped, dismissalSkip{
			FindingID: getStr(item, "finding_id"),
			Reason:    getStr(item, "reason"),
		})
	}
	return skipped
}

func runDismiss(cmd *cobra.Command, args []string) error {
	issuesList, _ := cmd.Flags().GetString("issues")
	assessments, _ := cmd.Flags().GetStringArray("assessment")
	severities, _ := cmd.Flags().GetStringArray("severity")
	repo, _ := cmd.Flags().GetString("repo")
	reason, _ := cmd.Flags().GetString("reason")
	comment, _ := cmd.Flags().GetString("comment")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	outputFlag, _ := cmd.Flags().GetString("output")

	if issuesList == "" && len(assessments) == 0 {
		fmt.Fprintln(os.Stderr, "Error: Must specify --issues or --assessment filter")
		os.Exit(clierrors.ExitUsageError)
	}

	client := api.NewClient("", "")
	defer client.Close()

	// Collect open integration issue IDs to dismiss.
	var issueIDs []string
	listedFindings := make(map[string]map[string]any)

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
					os.Exit(clierrors.ExitUsageError)
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

		params["source_state"] = []string{"open"}
		params["has_code_sensor"] = "true"

		// Paginate through all pages
		page := 1
		for {
			params["page"] = fmt.Sprintf("%d", page)

			data, err := client.Get("/sca_findings", params)
			if err != nil {
				if _, ok := err.(*api.AuthenticationError); ok {
					fmt.Fprintln(os.Stderr, "Error:", err)
					os.Exit(clierrors.ExitAuthFailed)
				}
				fmt.Fprintln(os.Stderr, "API Error:", err)
				os.Exit(1)
			}

			items, _ := data["items"].([]any)
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				id, _ := item["id"].(string)
				issueIDs = append(issueIDs, id)
				listedFindings[id] = item
			}

			if len(items) < 500 {
				break
			}
			page++
		}
	} else if issuesList != "" {
		ids := strings.Split(issuesList, ",")
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id != "" {
				issueIDs = append(issueIDs, id)
			}
		}
	}

	if len(issueIDs) == 0 {
		fmt.Println("No issues found matching criteria.")
		return nil
	}

	outputFormat := output.DetectOutputFormat(outputFlag)

	if dryRun {
		wouldDismiss, skipped, err := previewDismissals(client, issueIDs, listedFindings)
		if err != nil {
			if _, ok := err.(*api.AuthenticationError); ok {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(clierrors.ExitAuthFailed)
			}
			fmt.Fprintln(os.Stderr, "API Error:", err)
			os.Exit(1)
		}
		msg := fmt.Sprintf(
			"Would dismiss %d of %d issues and skip %d. Use without --dry-run to execute.",
			wouldDismiss, len(issueIDs), len(skipped),
		)
		if outputFormat == output.JSON {
			jsonOut := map[string]any{
				"action":           "dismiss",
				"dry_run":          true,
				"reason":           reason,
				"total":            len(issueIDs),
				"would_dismiss":    wouldDismiss,
				"skipped":          len(skipped),
				"skipped_findings": skipped,
				"message":          msg,
			}
			fmt.Println(output.FormatJSON(jsonOut))
		} else {
			fmt.Printf("\nDry run: would dismiss %d of %d issues\n", wouldDismiss, len(issueIDs))
			for _, item := range skipped {
				fmt.Printf("Skipped %s: %s\n", item.FindingID, item.Reason)
			}
			fmt.Printf("Reason: %s\n", reason)
		}
		return nil
	}

	// Bulk dismiss in chunks of 500 (API limit)
	totalDismissed := 0
	totalSkipped := 0
	totalSkippedFindings := make([]dismissalSkip, 0)

	for i := 0; i < len(issueIDs); i += 500 {
		end := i + 500
		if end > len(issueIDs) {
			end = len(issueIDs)
		}
		chunk := issueIDs[i:end]

		body := map[string]any{
			"finding_ids":       chunk,
			"dismissed_reason":  reason,
			"dismissed_comment": comment,
		}

		result, err := client.Post("/sca_findings/bulk_dismiss", body)
		if err != nil {
			if _, ok := err.(*api.AuthenticationError); ok {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(clierrors.ExitAuthFailed)
			}
			// 404 means no open findings in this chunk
			if apiErr, ok := err.(*api.APIError); ok && apiErr.StatusCode == 404 {
				totalSkipped += len(chunk)
				continue
			}
			fmt.Fprintln(os.Stderr, "API Error:", err)
			os.Exit(1)
		}

		dismissed, _ := result["dismissed_count"].(float64)
		totalDismissed += int(dismissed)
		if skipped, ok := result["skipped_count"].(float64); ok {
			totalSkipped += int(skipped)
		} else {
			totalSkipped += len(chunk) - int(dismissed)
		}
		totalSkippedFindings = append(totalSkippedFindings, responseSkippedFindings(result)...)
	}

	if outputFormat == output.JSON {
		jsonOut := map[string]any{
			"action":           "dismiss",
			"dry_run":          false,
			"reason":           reason,
			"dismissed":        totalDismissed,
			"skipped":          totalSkipped,
			"skipped_findings": totalSkippedFindings,
		}
		fmt.Println(output.FormatJSON(jsonOut))
	} else {
		fmt.Printf("\nDismissed %d issues\n", totalDismissed)
		for _, item := range totalSkippedFindings {
			fmt.Printf("Skipped %s: %s\n", item.FindingID, item.Reason)
		}
		if totalSkipped > len(totalSkippedFindings) {
			fmt.Printf("Skipped %d additional findings (reason unavailable)\n", totalSkipped-len(totalSkippedFindings))
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
	cmd.Flags().String("comment", "", "Comment for dismissal (auto-generated if empty)")
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
