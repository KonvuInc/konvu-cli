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

const remediationPlansPath = "/remediations/plans"

// validPlanKinds are the client-side --kind filter values. The API returns SCA
// and SAST plans together; we split them by each item's own "kind" field, the
// same way the dashboard does.
var validPlanKinds = map[string]bool{"sca": true, "sast": true, "all": true}

var remediateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List remediation plans waiting for you (SCA and SAST)",
	Long: `List the remediation plans available in your backlog — the same plans
the dashboard's remediate board shows — without needing a plan ID.

SCA and SAST plans are returned together; use --kind to see just one. Pipe the
ids into 'konvu remediate brief' to get the agent prompt for each:

  konvu remediate list --kind sca -q | while read id; do konvu remediate brief "$id"; done

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu remediate list
  konvu remediate list --kind sca
  konvu remediate list --repo github:org/repo
  konvu remediate list --kind sast --status ready
  konvu remediate list -o json
  konvu remediate list -q`,
	Args: cobra.NoArgs,
	RunE: runRemediateList,
}

func runRemediateList(cmd *cobra.Command, args []string) error {
	kind, _ := cmd.Flags().GetString("kind")
	status, _ := cmd.Flags().GetString("status")
	grouping, _ := cmd.Flags().GetString("grouping")
	repoScope, _ := cmd.Flags().GetString("repo-scope")
	repo, _ := cmd.Flags().GetString("repo")
	limit, _ := cmd.Flags().GetInt("limit")
	outputFlag, _ := cmd.Flags().GetString("output")
	quiet, _ := cmd.Flags().GetBool("quiet")
	format := output.DetectOutputFormat(outputFlag)

	kind = strings.ToLower(strings.TrimSpace(kind))
	repo = strings.TrimSpace(repo)
	if !validPlanKinds[kind] {
		handleRemediateListError(usageError(fmt.Sprintf("Invalid --kind %q. Use sca, sast, or all.", kind)), format, repo != "")
	}

	// grouping, repo-scope and repo are passthrough; the server resolves/validates them.
	params := map[string]any{"grouping": grouping, "repo_scope": repoScope, "limit": limit}
	if repo != "" {
		params["repo"] = repo
	}

	client := api.NewClient("", "")
	defer client.Close()

	data, err := client.Get(remediationPlansPath, params)
	if err != nil {
		handleRemediateListError(err, format, repo != "")
	}

	items := filterPlans(getSlice(data, "items"), kind, strings.ToLower(strings.TrimSpace(status)))

	if quiet {
		fmt.Println(output.FormatQuiet(asMaps(items), "id"))
		return nil
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(map[string]any{
			"items":    items,
			"grouping": data["grouping"],
			"total":    len(items),
		}))
		return nil
	}

	rows := make([]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			rows = append(rows, transformPlan(m))
		}
	}
	columns := []string{"id", "kind", "status", "repository", "target", "findings", "pr_url"}
	if format == output.CSV {
		fmt.Print(output.FormatCSV(map[string]any{"plans": rows}, columns, "plans"))
		return nil
	}
	fmt.Println(output.FormatTable(map[string]any{"plans": rows}, columns, "plans", nil))
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "No remediation plans waiting. You're all caught up.")
	}
	return nil
}

// filterPlans applies the client-side --kind and --status filters. An empty or
// "all" kind keeps both; an empty status keeps every returned (backlog) status.
func filterPlans(items []any, kind, status string) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if kind != "" && kind != "all" && getStr(m, "kind") != kind {
			continue
		}
		if status != "" && strings.ToLower(getStr(m, "status")) != status {
			continue
		}
		out = append(out, it)
	}
	return out
}

// transformPlan flattens a raw plan into a display row for the table/CSV.
func transformPlan(m map[string]any) map[string]any {
	return map[string]any{
		"id":         getStr(m, "id"),
		"kind":       orDefault(getStr(m, "kind"), "sca"),
		"status":     getStr(m, "status"),
		"repository": planRepository(m),
		"target":     planTarget(m),
		"findings":   fmt.Sprintf("%d", len(getSlice(m, "findings"))),
		"pr_url":     getStr(m, "pr_url"),
	}
}

// planRepository resolves the repo URL for either kind: SAST plans carry
// detection_repository_url; SCA plans anchor on the manifest location.
func planRepository(m map[string]any) string {
	if url := getStr(m, "detection_repository_url"); url != "" {
		return url
	}
	return getStr(getMap(m, "manifest_location"), "vcs_repository_url")
}

// planTarget is the human anchor for a plan: the vulnerable package(s) for SCA,
// the detection title/location for SAST.
func planTarget(m map[string]any) string {
	if getStr(m, "kind") == "sast" {
		return orDefault(getStr(m, "detection_title"), getStr(m, "detection_location"))
	}
	pkgs := getSlice(m, "packages")
	if len(pkgs) == 0 {
		return getStr(getMap(m, "manifest_location"), "location")
	}
	first, _ := pkgs[0].(map[string]any)
	label := getStr(first, "name")
	if v := getStr(first, "version"); v != "" {
		label += "@" + v
	}
	if len(pkgs) > 1 {
		label += fmt.Sprintf(" (+%d)", len(pkgs)-1)
	}
	return label
}

// asMaps keeps only the map items, for helpers that take []map[string]any.
func asMaps(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// handleRemediateListError renders the error and exits. repoFiltered gates the
// 404 → REPOSITORY_NOT_FOUND mapping: only a --repo request treats a 404 as an
// unknown repository; without it a 404 is just a generic (route/proxy) API error.
func handleRemediateListError(err error, format output.OutputFormat, repoFiltered bool) {
	var cliErr *clierrors.CLIError
	switch e := err.(type) {
	case *clierrors.CLIError:
		cliErr = e
	case *api.AuthenticationError:
		cliErr = clierrors.NewAuthError(e.Error())
	case *api.APIError:
		switch e.StatusCode {
		case 422:
			cliErr = &clierrors.CLIError{
				Code:       "INVALID_ARGUMENTS",
				Message:    e.Error(),
				Suggestion: "Check --grouping (recommended|by_dependency|most_cve_cleared|most_at_risk), --repo-scope (tier_1_2|all), and --limit (1-50).",
				ExitCode:   clierrors.ExitUsageError,
			}
		case 404:
			if repoFiltered {
				cliErr = &clierrors.CLIError{
					Code:       "REPOSITORY_NOT_FOUND",
					Message:    e.Error(),
					Suggestion: "Pass --repo as a Konvu slug (github:org/repo), a full repo URL, or a repository id.",
					ExitCode:   clierrors.ExitNotFound,
				}
			} else {
				cliErr = clierrors.NewAPIError(e.Error())
			}
		default:
			cliErr = clierrors.NewAPIError(e.Error())
		}
	default:
		cliErr = clierrors.NewAPIError(err.Error())
	}

	if format == output.JSON {
		fmt.Println(clierrors.FormatErrorJSON(cliErr))
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", cliErr.Message)
		if cliErr.Suggestion != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", cliErr.Suggestion)
		}
	}
	os.Exit(cliErr.ExitCode)
}

func init() {
	remediateListCmd.Flags().String("kind", "all", "Filter by plan kind: sca, sast, or all")
	remediateListCmd.Flags().String("status", "", "Filter by status (e.g. ready); default shows the whole backlog")
	remediateListCmd.Flags().String("grouping", "most_cve_cleared", "Plan ranking: recommended, by_dependency, most_cve_cleared, most_at_risk")
	remediateListCmd.Flags().String("repo-scope", "all", "Repository scope: tier_1_2 or all")
	remediateListCmd.Flags().String("repo", "", "Filter to one repository (github:org/repo, full URL, or repo id)")
	remediateListCmd.Flags().Int("limit", 15, "Max plans per kind (1-50)")
	remediateListCmd.Flags().StringP("output", "o", "", "Output format: json, table, csv")
	remediateListCmd.Flags().BoolP("quiet", "q", false, "Print only plan ids")

	remediateCmd.AddCommand(remediateListCmd)
}
