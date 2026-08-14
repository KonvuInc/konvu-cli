package cmd

import (
	"fmt"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/findings"
	"github.com/spf13/cobra"
)

var containerCmd = &cobra.Command{
	Use:   "container",
	Short: "Container image vulnerability findings",
	Long:  "Search and inspect container findings from AWS Inspector and other scanners.",
}

var containerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List container findings",
	RunE:  runContainerList,
}

var containerGetCmd = &cobra.Command{
	Use:   "get [finding-id]",
	Short: "Get a container finding",
	Args:  cobra.ExactArgs(1),
	RunE:  runContainerGet,
}

var containerCountsCmd = &cobra.Command{
	Use:   "counts",
	Short: "Count container findings",
	RunE:  runContainerCounts,
}

var validContainerAssessments = map[string]bool{
	"exploitable":    true,
	"false_positive": true,
	"failed":         true,
	"not_assessed":   true,
}

func transformContainerFinding(raw map[string]any) findings.Row {
	assessment := getMap(raw, "assessment")
	return findings.Row{
		"id":          getStr(raw, "id"),
		"cve":         getStr(raw, "cve_id"),
		"severity":    getStr(raw, "severity"),
		"package":     getStr(raw, "package_name"),
		"version":     getStr(raw, "package_version"),
		"ecosystem":   getStr(raw, "package_ecosystem"),
		"image":       getStr(raw, "image_name"),
		"tag":         getStr(raw, "tag"),
		"state":       getStr(raw, "state"),
		"source":      getStr(raw, "source"),
		"observed_at": getStr(raw, "observed_at"),
		"updated_at":  getStr(raw, "updated_at"),
		"assessment":  getStr(assessment, "result"),
		"triage_url":  getStr(raw, "triage_url"),
	}
}

// containerDefaultColumns is the compact table set (URL-free so terminal tables
// don't wrap); containerCSVColumns adds triage_url for CSV export.
var containerDefaultColumns = []string{"id", "cve", "severity", "package", "image", "state", "assessment"}
var containerCSVColumns = append(append([]string{}, containerDefaultColumns...), "triage_url")

func runContainerList(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	f := findings.ReadCommonFilters(cmd)
	params := map[string]any{"per_page": f.LimitOr(30), "page": 1}
	if len(f.Severity) > 0 {
		params["severity"] = f.Severity
	}
	if len(f.Assessment) > 0 {
		for _, a := range f.Assessment {
			if !validContainerAssessments[a] {
				return &clierrors.CLIError{
					Message:    fmt.Sprintf("invalid --assessment value %q for container", a),
					Suggestion: "Valid: exploitable, false_positive, failed, not_assessed.",
				}
			}
		}
		params["assessment"] = f.Assessment
	}
	if img, _ := cmd.Flags().GetString("image"); img != "" {
		params["repository"] = []string{img}
	}
	if state, _ := cmd.Flags().GetStringSlice("state"); len(state) > 0 {
		params["source_state"] = state
	}
	if cve, _ := cmd.Flags().GetString("cve"); cve != "" {
		params["cve"] = []string{cve}
	}
	if src, _ := cmd.Flags().GetString("source"); src != "" {
		params["source"] = []string{src}
	}

	resp, err := client.Get("/container_findings", params)
	if err != nil {
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("list container findings: %v", err),
			Suggestion: "Check auth and permissions.",
		}
	}

	items := getSlice(resp, "items")
	rows := make([]findings.Row, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, transformContainerFinding(m))
	}

	if f.QuietIDs {
		return findings.RenderBareIDs(cmd, rows, "id")
	}
	return findings.RenderColumns(cmd, rows, containerDefaultColumns, containerCSVColumns)
}

func runContainerGet(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	if err := findings.RequireJSON(cmd, "container get"); err != nil {
		return err
	}
	resp, err := client.Get(fmt.Sprintf("/container_findings/%s", args[0]), nil)
	if err != nil {
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("get container finding: %v", err),
			Suggestion: "Verify the ID is a container finding ID.",
		}
	}
	return findings.Render(cmd, []findings.Row{resp}, nil)
}

func runContainerCounts(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	f := findings.ReadCommonFilters(cmd)
	params := map[string]any{}
	if len(f.Severity) > 0 {
		params["severity"] = f.Severity
	}
	if state, _ := cmd.Flags().GetStringSlice("state"); len(state) > 0 {
		params["source_state"] = state
	}

	n, err := findings.CountByPagination(client, "/container_findings", params)
	if err != nil {
		return err
	}
	return findings.Render(cmd, []findings.Row{{"count": n}}, []string{"count"})
}

func init() {
	// list — common subset (severity, assessment, limit, -o, -q) + container-specific
	containerListCmd.Flags().StringSlice("severity", nil, "Filter by severity: critical, high, medium, low")
	containerListCmd.Flags().StringSlice("assessment", nil, "Filter by assessment (repeatable): exploitable, false_positive, failed, not_assessed")
	containerListCmd.Flags().Int("limit", 30, "Maximum rows to return (per_page)")
	containerListCmd.Flags().StringP("output", "o", "", "Output format: json, table, csv")
	containerListCmd.Flags().BoolP("quiet", "q", false, "Print bare IDs, one per line")
	containerListCmd.Flags().String("image", "", "Filter by container image name")
	containerListCmd.Flags().StringSlice("state", nil, "Filter by state (repeatable): open, fixed, dismissed, muted, auto_dismissed")
	containerListCmd.Flags().String("cve", "", "Filter by CVE ID (exact match)")
	containerListCmd.Flags().String("source", "", "Filter by scanner source (e.g. aws_inspector)")

	// get — output only
	containerGetCmd.Flags().StringP("output", "o", "", "Output format: json (default)")

	// counts — severity + state
	containerCountsCmd.Flags().StringSlice("severity", nil, "Filter by severity")
	containerCountsCmd.Flags().StringSlice("state", nil, "Filter by state")
	containerCountsCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	containerCmd.AddCommand(containerListCmd, containerGetCmd, containerCountsCmd)
	findingCmd.AddCommand(containerCmd)
}
