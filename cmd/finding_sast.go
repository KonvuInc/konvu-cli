package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/findings"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var sastCmd = &cobra.Command{
	Use:   "sast",
	Short: "Application-code (SAST) findings from Semgrep, Arnica, and other scanners",
	Long: `Search and triage SAST findings.

The row 'id' returned by 'list' is the INVESTIGATION ID (Konvu's triage
record), while 'detection_id' is the stable Konvu finding ID. 'get' and
'rate' take investigation IDs; 'assess', 'dismiss', and 'reopen' take
detection IDs.

Untriaged detections are included in 'list' and 'counts' — their 'id' is
empty and 'triage_status' is "pending". 'sast list -q' pipes only rows
with a non-empty id, so 'sast list -q | xargs -I{} sast get {}' works
without the caller filtering explicitly.`,
}

var sastListCmd = &cobra.Command{
	Use:   "list",
	Short: "Browse or list SAST findings",
	Long: `Browse SAST findings interactively when stdin and stdout are terminals and
no machine-output flag is set.

Use -o json, -o table, -o csv, or -q for deterministic non-interactive output.
The browser and list include untriaged detections; -q prints only investigation
IDs that can be passed to get or rate.`,
	Example: `  konvu finding sast list
  konvu finding sast list --severity critical -o table
  konvu finding sast list -q | xargs -n1 konvu finding sast get`,
	RunE: runSastList,
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
	supportsDismissal, _ := getBool(raw, "supports_dismissal")
	return findings.Row{
		"id":                                invID,
		"detection_id":                      getStr(raw, "id"),
		"title":                             getStr(raw, "title"),
		"severity":                          getStr(raw, "severity"),
		"confidence":                        getStr(raw, "confidence"),
		"cwe_ids":                           raw["cwe_ids"],
		"location":                          getStr(raw, "location"),
		"repo":                              getStr(raw, "where"),
		"state":                             getStr(raw, "state"),
		"assessment":                        assessment,
		"triage_status":                     triageStatus,
		"triage_url":                        getStr(raw, "triage_url"),
		"dismissed_at":                      getStr(raw, "dismissed_at"),
		"dismissed_reason":                  getStr(raw, "dismissed_reason"),
		"dismissed_comment":                 getStr(raw, "dismissed_comment"),
		"dismissed_external_reference_code": getStr(raw, "dismissed_external_reference_code"),
		"supports_dismissal":                supportsDismissal,
	}
}

// sastDefaultColumns is the compact table set (URL-free so terminal tables
// don't wrap); sastCSVColumns adds triage_url for CSV export.
var sastDefaultColumns = []string{"id", "title", "severity", "location", "repo", "state", "assessment", "triage_status"}
var sastCSVColumns = []string{
	"id", "detection_id", "title", "severity", "location", "repo", "state", "assessment",
	"triage_status", "dismissed_at", "dismissed_reason", "dismissed_comment",
	"dismissed_external_reference_code", "triage_url",
}

func parseSastListFields(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	valid := transformDetection(map[string]any{})
	validNames := make([]string, 0, len(valid))
	for name := range valid {
		validNames = append(validNames, name)
	}
	sort.Strings(validNames)

	fields := make([]string, 0)
	for _, field := range strings.Split(value, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := valid[field]; !ok {
			return nil, &clierrors.CLIError{
				Code:       "INVALID_ARGUMENTS",
				Message:    fmt.Sprintf("unknown SAST field %q", field),
				Suggestion: "Valid fields: " + strings.Join(validNames, ", ") + ".",
				ExitCode:   clierrors.ExitUsageError,
			}
		}
		fields = append(fields, field)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return fields, nil
}

func runSastList(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()
	browse := shouldBrowseFindings(cmd)

	f := findings.ReadCommonFilters(cmd)
	fieldsValue, _ := cmd.Flags().GetString("fields")
	fieldList, err := parseSastListFields(fieldsValue)
	if err != nil {
		return err
	}
	kind, _ := cmd.Flags().GetString("kind")
	if kind == "" {
		kind = "sast_app"
	}
	params := map[string]any{
		"kind":     kind,
		"per_page": f.LimitOr(30),
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
	if state, _ := cmd.Flags().GetStringSlice("state"); len(state) > 0 {
		params["state"] = state
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

	clearLoading := findingLoading(cmd, browse, "SAST")
	resp, err := client.Get("/detections", params)
	clearLoading()
	if err != nil {
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("list SAST findings: %v", err),
			Suggestion: "Check auth and permissions.",
		}
	}

	// Every detection is emitted, triaged or not. Untriaged rows carry
	// triage_status="pending" and an empty investigation id, so downstream
	// consumers can filter them explicitly (e.g. `jq '.[] | select(.id!="")'`)
	// instead of relying on a client-side default that would silently drop
	// rows returned within the page window.
	items := getSlice(resp, "items")
	rows := make([]findings.Row, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, transformDetection(m))
	}
	if browse {
		return browseSASTFindings(cmd, rows)
	}
	if f.QuietIDs {
		return findings.RenderBareIDs(cmd, rows, "id")
	}
	tableColumns, csvColumns := sastDefaultColumns, sastCSVColumns
	if fieldList != nil {
		filtered := make([]findings.Row, len(rows))
		for i, row := range rows {
			filtered[i] = output.FilterFields(row, fieldList)
		}
		rows, tableColumns, csvColumns = filtered, fieldList, fieldList
	}
	return findings.RenderColumns(cmd, rows, tableColumns, csvColumns)
}

func runSastGet(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	if err := findings.RequireJSON(cmd, "sast get"); err != nil {
		return err
	}
	resp, err := client.Get(fmt.Sprintf("/investigations/%s", args[0]), nil)
	if err != nil {
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("get SAST investigation: %v", err),
			Suggestion: "Pass the investigation ID (the 'id' from 'konvu finding sast list'), not the raw detection ID.",
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

	if err := findings.RequireJSON(cmd, "sast rate"); err != nil {
		return err
	}
	payload := map[string]any{
		"helpful":       helpful,
		"feedback_tags": tags,
		"comment":       comment,
	}
	resp, err := client.Post(fmt.Sprintf("/investigations/%s/scoring", invID), payload)
	if err != nil {
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("rate SAST investigation: %v", err),
			Suggestion: "Verify the investigation ID.",
		}
	}
	return findings.Render(cmd, []findings.Row{resp}, nil)
}

func runSastCounts(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	f := findings.ReadCommonFilters(cmd)
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

func addSastListFlags(cmd *cobra.Command) {
	cmd.Flags().String("since", "", "Filter by created-after date (e.g. 7d, 2025-01-01)")
	cmd.Flags().StringSlice("severity", nil, "Filter by severity: critical, high, medium, low")
	cmd.Flags().StringSlice("repo", nil, "Filter by repository URL (repeatable)")
	cmd.Flags().StringSlice("assessment", nil, "Filter by assessment (repeatable): exploitable, false_positive, inconclusive, not_assessed")
	cmd.Flags().StringSlice("state", nil, "Filter by state: open, fixed, dismissed, auto_dismissed, muted")
	cmd.Flags().Int("limit", 30, "Maximum rows to return (per_page)")
	cmd.Flags().StringP("output", "o", "", "Output format: json, table, csv")
	cmd.Flags().BoolP("quiet", "q", false, "Print bare IDs (investigation IDs)")
	cmd.Flags().String("fields", "", "Comma-separated fields to include")
	cmd.Flags().StringSlice("cwe", nil, "Filter by CWE identifier (repeatable; e.g. CWE-89)")
	cmd.Flags().StringSlice("confidence", nil, "Filter by confidence: high, medium, low")
	cmd.Flags().String("kind", "sast_app", "Detection kind (default sast_app)")
	cmd.Flags().String("title", "", "Filter by detection title (exact match)")
}

func init() {
	addSastListFlags(sastListCmd)

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
