package cmd

import (
	"fmt"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/findings"
	"github.com/spf13/cobra"
)

var sastCmd = &cobra.Command{
	Use:   "sast",
	Short: "Application-code (SAST) findings from Semgrep, Arnica, and other scanners",
	Long: `Search and triage SAST findings.

The row 'id' returned by 'list' is the INVESTIGATION ID (Konvu's triage
record), not the raw scanner detection ID. 'get' and 'rate' both take
investigation IDs — the raw detection ID is available as 'detection_id'.

Detections without a Konvu investigation are hidden by default; pass
--include-untriaged to see them (their 'id' will be empty).`,
}

var sastListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SAST findings",
	RunE:  runSastList,
}

var sastGetCmd = &cobra.Command{
	Use:   "get [investigation-id]",
	Short: "Get a SAST investigation (use the 'id' from 'sast list')",
	Args:  cobra.ExactArgs(1),
	RunE:  runSastGet,
}

var sastRateCmd = &cobra.Command{
	Use:   "rate [investigation-id] [helpful|not-helpful]",
	Short: "Rate a SAST investigation",
	Args:  cobra.ExactArgs(2),
	RunE:  runSastRate,
}

var sastCountsCmd = &cobra.Command{
	Use:   "counts",
	Short: "Count SAST findings",
	RunE:  runSastCounts,
}

func transformDetection(raw map[string]any) findings.Row {
	// `investigations` is ordered latest-first by the backend
	// (see dashboard_backend/routes/detections.py _latest_investigation_subquery).
	// We take index 0 — the most recent triage — and expose it as the row's `id`.
	investigations := getSlice(raw, "investigations")
	var invID string
	var assessment string
	triageStatus := "pending"
	if len(investigations) > 0 {
		if inv, ok := investigations[0].(map[string]any); ok {
			invID = getStr(inv, "id")
			assessment = getStr(inv, "assessment_result")
			if invID != "" {
				triageStatus = "triaged"
			}
		}
	}
	return findings.Row{
		"id":            invID, // primary key = investigation ID; empty when untriaged
		"detection_id":  getStr(raw, "id"),
		"title":         getStr(raw, "title"),
		"severity":      getStr(raw, "severity"),
		"confidence":    getStr(raw, "confidence"),
		"cwe_ids":       raw["cwe_ids"],
		"location":      getStr(raw, "location"),
		"repo":          getStr(raw, "where"),
		"state":         getStr(raw, "state"),
		"assessment":    assessment,
		"triage_status": triageStatus,
	}
}

var sastDefaultColumns = []string{"id", "title", "severity", "location", "repo", "state", "assessment", "triage_status"}

func runSastList(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	f, err := findings.ReadCommonFilters(cmd)
	if err != nil {
		return err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 30
	}
	kind, _ := cmd.Flags().GetString("kind")
	if kind == "" {
		kind = "sast_app"
	}
	params := map[string]any{
		"kind":     kind,
		"per_page": limit,
		"page":     1,
	}
	if len(f.Severity) > 0 {
		params["severity"] = f.Severity
	}
	if len(f.Repository) > 0 {
		params["where"] = f.Repository
	}
	if len(f.Assessment) > 0 {
		params["assessment_result"] = f.Assessment
	}
	if f.Since != "" {
		params["created_after"] = parseRelativeDate(f.Since)
	}
	if cwe, _ := cmd.Flags().GetStringSlice("cwe"); len(cwe) > 0 {
		params["cwe_ids"] = cwe
	}
	if conf, _ := cmd.Flags().GetStringSlice("confidence"); len(conf) > 0 {
		params["confidence"] = conf
	}
	if title, _ := cmd.Flags().GetString("title"); title != "" {
		params["title"] = []string{title}
	}

	resp, err := client.Get("/detections", params)
	if err != nil {
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("list SAST findings: %v", err),
			Suggestion: "Check auth and permissions.",
		}
	}

	items := getSlice(resp, "items")
	includeUntriaged, _ := cmd.Flags().GetBool("include-untriaged")
	rows := make([]findings.Row, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		row := transformDetection(m)
		if !includeUntriaged && row["id"] == "" {
			continue
		}
		rows = append(rows, row)
	}
	if f.QuietIDs {
		return findings.RenderBareIDs(cmd, rows, "id")
	}
	return findings.Render(cmd, rows, sastDefaultColumns)
}

func runSastGet(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	resp, err := client.Get(fmt.Sprintf("/investigations/%s", args[0]), nil)
	if err != nil {
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("get SAST investigation: %v", err),
			Suggestion: "Pass the investigation ID (the 'id' from 'konvu finding sast list'), not the raw detection ID.",
		}
	}
	// Detail responses are richly nested; force JSON.
	if format, _ := cmd.Flags().GetString("output"); format == "table" || format == "csv" {
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("`%s` output is not supported for sast get", format),
			Suggestion: "Use -o json (default). Investigation details are too nested for a flat table.",
		}
	}
	return findings.Render(cmd, []findings.Row{resp}, nil)
}

func runSastRate(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	invID, verdict := args[0], args[1]
	var helpful bool
	switch verdict {
	case "helpful", "yes", "agree":
		helpful = true
	case "not-helpful", "no", "disagree":
		helpful = false
	default:
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("invalid verdict %q", verdict),
			Suggestion: "Use 'helpful' or 'not-helpful' (aliases: agree/disagree, yes/no).",
		}
	}
	comment, _ := cmd.Flags().GetString("comment")
	tags, _ := cmd.Flags().GetStringSlice("feedback-tag")

	payload := map[string]any{
		"helpful":       helpful,
		"feedback_tags": tags,
		"comment":       comment,
	}
	if tags == nil {
		payload["feedback_tags"] = []string{}
	}
	resp, err := client.Post(fmt.Sprintf("/investigations/%s/scoring", invID), payload)
	if err != nil {
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("rate SAST investigation: %v", err),
			Suggestion: "Verify the investigation ID.",
		}
	}
	if format, _ := cmd.Flags().GetString("output"); format == "table" || format == "csv" {
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("`%s` output is not supported for sast rate", format),
			Suggestion: "Use -o json (default).",
		}
	}
	return findings.Render(cmd, []findings.Row{resp}, nil)
}

func runSastCounts(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	f, err := findings.ReadCommonFilters(cmd)
	if err != nil {
		return err
	}
	params := map[string]any{"kind": "sast_app"}
	if len(f.Severity) > 0 {
		params["severity"] = f.Severity
	}
	if len(f.Repository) > 0 {
		params["where"] = f.Repository
	}
	n, err := findings.CountByPagination(client, "/detections", params)
	if err != nil {
		return err
	}
	return findings.Render(cmd, []findings.Row{{"count": n}}, []string{"count"})
}

func init() {
	// list — full common set + SAST-specific
	sastListCmd.Flags().String("since", "", "Filter by created-after date (e.g. 7d, 2025-01-01)")
	sastListCmd.Flags().StringSlice("severity", nil, "Filter by severity: critical, high, medium, low")
	sastListCmd.Flags().StringSlice("repo", nil, "Filter by repository URL (repeatable)")
	sastListCmd.Flags().StringSlice("assessment", nil, "Filter by assessment (repeatable): exploitable, false_positive, inconclusive, not_assessed")
	sastListCmd.Flags().Int("limit", 30, "Maximum rows to return (per_page)")
	sastListCmd.Flags().StringP("output", "o", "", "Output format: json, table, csv")
	sastListCmd.Flags().BoolP("quiet", "q", false, "Print bare IDs (investigation IDs)")
	sastListCmd.Flags().StringSlice("cwe", nil, "Filter by CWE identifier (repeatable; e.g. CWE-89)")
	sastListCmd.Flags().StringSlice("confidence", nil, "Filter by confidence: high, medium, low")
	sastListCmd.Flags().String("kind", "sast_app", "Detection kind (default sast_app)")
	sastListCmd.Flags().String("title", "", "Filter by detection title (exact match)")
	sastListCmd.Flags().Bool("include-untriaged", false, "Include detections without a Konvu investigation (row 'id' will be empty)")

	sastGetCmd.Flags().StringP("output", "o", "", "Output format: json (default)")

	sastRateCmd.Flags().StringP("comment", "c", "", "Free-text comment attached to the rating")
	sastRateCmd.Flags().StringSlice("feedback-tag", nil, "Feedback tag (repeatable)")
	sastRateCmd.Flags().StringP("output", "o", "", "Output format: json")

	sastCountsCmd.Flags().StringSlice("severity", nil, "Filter by severity")
	sastCountsCmd.Flags().StringSlice("repo", nil, "Filter by repository (repeatable)")
	sastCountsCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	sastCmd.AddCommand(sastListCmd, sastGetCmd, sastRateCmd, sastCountsCmd)
	findingCmd.AddCommand(sastCmd)
}
