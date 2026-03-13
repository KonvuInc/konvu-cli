package cmd

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/KonvuTeam/konvu-cli/pkg/api"
	clierrors "github.com/KonvuTeam/konvu-cli/pkg/errors"
	"github.com/KonvuTeam/konvu-cli/pkg/mapping"
	"github.com/KonvuTeam/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var findingCmd = &cobra.Command{
	Use:   "finding",
	Short: "Security findings",
}

var defaultTableColumns = []string{"cve", "dependency", "repository", "assessment", "assessment_summary"}
var validCountsGroupBy = map[string]bool{"severity": true, "week": true, "month": true}
var validListGroupBy = map[string]bool{"repository": true, "dependency": true, "severity": true, "assessment": true}

var relDateRe = regexp.MustCompile(`^(\d+)d$`)

func parseRelativeDate(value string) string {
	m := relDateRe.FindStringSubmatch(value)
	if m != nil {
		days := 0
		fmt.Sscanf(m[1], "%d", &days)
		t := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
		return t.Format(time.RFC3339)
	}
	return value
}

func getStr(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func getMap(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}

func getSlice(m map[string]any, key string) []any {
	v, _ := m[key].([]any)
	return v
}

func getBool(m map[string]any, key string) (bool, bool) {
	v, ok := m[key].(bool)
	return v, ok
}

func getFloat(m map[string]any, key string) (float64, bool) {
	v, ok := m[key].(float64)
	return v, ok
}

func transformFinding(finding map[string]any) map[string]any {
	vuln := getMap(finding, "vulnerability")
	ml := getMap(finding, "manifest_location")
	dep := getMap(finding, "dependency")
	source := getMap(finding, "source")
	rec := getStr(finding, "calculated_recommendation")
	assessment := mapping.RecommendationToAssessment(rec)

	analyses := getMap(finding, "analyses")

	aliases := getSlice(vuln, "aliases")
	cve := ""
	if len(aliases) > 0 {
		cve, _ = aliases[0].(string)
	}
	if cve == "" {
		cve = getStr(vuln, "id")
	}

	qualSummary := getStr(analyses, "qualification_summary")
	if qualSummary == "" {
		qualSummary, _ = mapping.GetAssessmentSummary(assessment)
	}

	severity := strings.ToLower(getStr(vuln, "severity"))
	if severity == "" {
		severity = "unknown"
	}
	hasFix := strings.ToLower(getStr(vuln, "has_fix"))
	if hasFix == "" {
		hasFix = "unknown"
	}

	return map[string]any{
		"id":                 getStr(finding, "id"),
		"cve":                cve,
		"severity":           severity,
		"dependency":         getStr(dep, "name"),
		"repository":         getStr(ml, "vcs_repository_url"),
		"manifest":           getStr(ml, "location"),
		"assessment":         string(assessment),
		"assessment_summary": qualSummary,
		"has_fix":            hasFix,
		"first_seen":         getStr(source, "remote_created_at"),
		"state":              getStr(source, "state"),
		"source_id":          getStr(source, "identifier"),
		"scanner":            getStr(source, "source_name"),
	}
}

func computeAssessmentCounts(client *api.Client, baseParams map[string]any) map[string]int {
	counts := make(map[string]int)
	for _, status := range mapping.AllStatuses {
		recs := mapping.AssessmentToRecommendation(status)
		params := map[string]any{"per_page": 1, "recommendation": recs}
		for k, v := range baseParams {
			params[k] = v
		}
		params["recommendation"] = recs // always override
		data, err := client.Get("/sca_findings", params)
		if err != nil {
			continue
		}
		if total, ok := data["total"].(float64); ok {
			counts[string(status)] = int(total)
		}
	}
	return counts
}

func generateTimePeriods(groupBy string, since string) []map[string]any {
	now := time.Now().UTC()

	if groupBy == "week" {
		defaultPeriods := 4
		// Align to Monday of current week
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		weekday := int(today.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		currentMonday := today.AddDate(0, 0, -(weekday - 1))

		n := defaultPeriods
		if since != "" {
			parsed := parseRelativeDate(since)
			startDate, err := time.Parse(time.RFC3339, parsed)
			if err == nil {
				days := now.Sub(startDate).Hours() / 24
				n = int(days/7) + 1
				if n < 1 {
					n = 1
				}
			}
		}

		var periods []map[string]any
		for i := 0; i < n; i++ {
			weekStart := currentMonday.AddDate(0, 0, -7*i)
			weekEnd := weekStart.AddDate(0, 0, 7)
			label := weekStart.Format("2006-01-02")
			periods = append(periods, map[string]any{
				"label": "week of " + label,
				"start": weekStart.Format(time.RFC3339),
				"end":   weekEnd.Format(time.RFC3339),
			})
		}
		return periods
	}

	if groupBy == "month" {
		defaultPeriods := 3
		n := defaultPeriods
		if since != "" {
			parsed := parseRelativeDate(since)
			startDate, err := time.Parse(time.RFC3339, parsed)
			if err == nil {
				n = (now.Year()-startDate.Year())*12 + int(now.Month()) - int(startDate.Month()) + 1
				if n < 1 {
					n = 1
				}
			}
		}

		var periods []map[string]any
		for i := 0; i < n; i++ {
			monthStart := time.Date(now.Year(), now.Month()-time.Month(i), 1, 0, 0, 0, 0, time.UTC)
			nextMonth := monthStart.AddDate(0, 1, 0)
			label := monthStart.Format("2006-01")
			periods = append(periods, map[string]any{
				"label": label,
				"start": monthStart.Format(time.RFC3339),
				"end":   nextMonth.Format(time.RFC3339),
			})
		}
		return periods
	}

	return nil
}

func handleFindingError(err error, format output.OutputFormat) {
	var cliErr *clierrors.CLIError
	switch e := err.(type) {
	case *clierrors.CLIError:
		cliErr = e
	case *api.AuthenticationError:
		cliErr = clierrors.NewAuthError(e.Error())
	default:
		cliErr = clierrors.NewAPIError(e.Error())
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

// maxInt returns the larger of two ints.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// --- finding list ---

var findingListCmd = &cobra.Command{
	Use:   "list",
	Short: "List security findings",
	Long: `List security findings with filtering and sorting.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed`,
	Example: `  # This week's exploitable findings
  konvu finding list --since 7d --assessment exploitable

  # Critical findings sorted by recency
  konvu finding list --severity critical --sort first_seen_at --output json

  # Just the count of not-assessed findings
  konvu finding list --assessment not-assessed --count

  # Findings with available fixes
  konvu finding list --has-fix fixed --assessment exploitable

  # Group exploitable findings by repo to prioritize
  konvu finding list --assessment exploitable --group-by repository

  # Filter by scanner source
  konvu finding list --source snyk

  # Pipe finding IDs to detail
  konvu finding list --assessment exploitable -q | xargs -I {} konvu finding get {}`,
	RunE: func(cmd *cobra.Command, args []string) error {
		outputFlag, _ := cmd.Flags().GetString("output")
		format := output.DetectOutputFormat(outputFlag)

		since, _ := cmd.Flags().GetString("since")
		until, _ := cmd.Flags().GetString("until")
		severity, _ := cmd.Flags().GetStringSlice("severity")
		assessment, _ := cmd.Flags().GetStringSlice("assessment")
		state, _ := cmd.Flags().GetStringSlice("state")
		hasFix, _ := cmd.Flags().GetString("has-fix")
		repo, _ := cmd.Flags().GetString("repo")
		cve, _ := cmd.Flags().GetString("cve")
		dependency, _ := cmd.Flags().GetString("dependency")
		source, _ := cmd.Flags().GetString("source")
		sourceID, _ := cmd.Flags().GetString("source-id")
		sortFlag, _ := cmd.Flags().GetString("sort")
		order, _ := cmd.Flags().GetString("order")
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		quiet, _ := cmd.Flags().GetBool("quiet")
		count, _ := cmd.Flags().GetBool("count")
		groupBy, _ := cmd.Flags().GetString("group-by")
		fields, _ := cmd.Flags().GetString("fields")

		// Validate group-by
		if groupBy != "" && !validListGroupBy[groupBy] {
			fmt.Fprintf(os.Stderr, "Invalid group-by: %s. Valid: assessment, dependency, repository, severity\n", groupBy)
			os.Exit(clierrors.ExitUsageError)
		}

		client := api.NewClient("", "")
		defer client.Close()

		// Build params
		perPage := limit
		if perPage > 1000 {
			perPage = 1000
		}
		params := map[string]any{
			"per_page": perPage,
			"page":     (offset / maxInt(limit, 1)) + 1,
			"sort":     sortFlag,
			"order":    order,
		}
		if since != "" {
			params["first_seen_after"] = parseRelativeDate(since)
		}
		if until != "" && until != "now" {
			params["first_seen_before"] = parseRelativeDate(until)
		}
		if len(severity) > 0 {
			upper := make([]string, len(severity))
			for i, s := range severity {
				upper[i] = strings.ToUpper(s)
			}
			params["severity"] = upper
		}
		if len(assessment) > 0 {
			var recs []string
			for _, a := range assessment {
				normalized := strings.ToLower(strings.ReplaceAll(a, "_", "-"))
				r := mapping.AssessmentToRecommendation(mapping.AssessmentStatus(normalized))
				recs = append(recs, r...)
			}
			params["recommendation"] = recs
		}
		if len(state) > 0 {
			params["any_source_state"] = state
		}
		if hasFix != "" {
			params["has_fix"] = hasFix
		}
		if repo != "" {
			params["vcs_repository_url"] = []string{repo}
		}
		if cve != "" {
			params["cve"] = []string{cve}
		}
		if dependency != "" {
			params["dependency_name"] = []string{dependency}
		}
		if source != "" {
			params["source"] = []string{source}
		}

		data, err := client.Get("/sca_findings", params)
		if err != nil {
			handleFindingError(err, format)
			return nil
		}

		total := int(data["total"].(float64))

		if count {
			fmt.Println(total)
			return nil
		}

		items := getSlice(data, "items")

		// Client-side source_id filter
		if sourceID != "" {
			var filtered []any
			for _, item := range items {
				m, _ := item.(map[string]any)
				src := getMap(m, "source")
				if getStr(src, "identifier") == sourceID {
					filtered = append(filtered, item)
				}
			}
			items = filtered
			total = len(items)
		}

		// Transform findings
		var transformed []map[string]any
		for _, item := range items {
			m, _ := item.(map[string]any)
			transformed = append(transformed, transformFinding(m))
		}

		if quiet {
			fmt.Println(output.FormatQuiet(transformed, "id"))
			return nil
		}

		// Assessment breakdown
		breakdown := make(map[string]int)
		for _, f := range transformed {
			a, _ := f["assessment"].(string)
			breakdown[a]++
		}

		showing := len(transformed)

		// Field filtering
		var fieldList []string
		if fields != "" {
			for _, f := range strings.Split(fields, ",") {
				fieldList = append(fieldList, strings.TrimSpace(f))
			}
		}

		if groupBy != "" {
			// Group findings by field
			groups := make(map[string][]map[string]any)
			for _, f := range transformed {
				key, _ := f[groupBy].(string)
				if key == "" {
					key = "unknown"
				}
				groups[key] = append(groups[key], f)
			}

			// Sort groups by count desc, then key
			type groupEntry struct {
				Key      string
				Findings []map[string]any
			}
			var sortedGroups []groupEntry
			for k, v := range groups {
				sortedGroups = append(sortedGroups, groupEntry{k, v})
			}
			sort.Slice(sortedGroups, func(i, j int) bool {
				if len(sortedGroups[i].Findings) != len(sortedGroups[j].Findings) {
					return len(sortedGroups[i].Findings) > len(sortedGroups[j].Findings)
				}
				return sortedGroups[i].Key < sortedGroups[j].Key
			})

			// Build grouped result for JSON
			var groupedResult []map[string]any
			for _, g := range sortedGroups {
				groupFindings := g.Findings
				if fieldList != nil {
					filtered := make([]map[string]any, len(groupFindings))
					for i, f := range groupFindings {
						filtered[i] = output.FilterFields(f, fieldList)
					}
					groupFindings = filtered
				}
				groupedResult = append(groupedResult, map[string]any{
					"key":      g.Key,
					"count":    len(g.Findings),
					"findings": groupFindings,
				})
			}

			if format == output.JSON {
				result := map[string]any{
					"summary": map[string]any{
						"total":                total,
						"showing":              showing,
						"offset":               offset,
						"group_by":             groupBy,
						"groups":               len(sortedGroups),
						"assessment_breakdown": breakdown,
					},
					"groups": groupedResult,
				}
				fmt.Println(output.FormatJSON(result))
			} else if format == output.CSV {
				// Flatten groups for CSV
				var flat []any
				for _, g := range groupedResult {
					findings, _ := g["findings"].([]map[string]any)
					for _, f := range findings {
						row := map[string]any{groupBy: g["key"]}
						for k, v := range f {
							row[k] = v
						}
						flat = append(flat, row)
					}
				}
				csvData := map[string]any{"findings": flat}
				fmt.Print(output.FormatCSV(csvData, []string{groupBy, "id", "cve", "severity", "dependency", "assessment"}, "findings"))
			} else {
				// Table output
				fmt.Fprintf(os.Stderr, "\nShowing %d of %d findings\n", showing, total)
				fmt.Fprintf(os.Stderr, "  Grouped by %s: %d groups\n", groupBy, len(sortedGroups))
				if len(breakdown) > 0 {
					var parts []string
					keys := make([]string, 0, len(breakdown))
					for k := range breakdown {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						parts = append(parts, fmt.Sprintf("%s: %d", k, breakdown[k]))
					}
					fmt.Fprintf(os.Stderr, "  Assessment: %s\n", strings.Join(parts, ", "))
				}
				fmt.Fprintln(os.Stderr)
				for _, g := range sortedGroups {
					fmt.Printf("  %s (%d)\n", g.Key, len(g.Findings))
				}
				fmt.Println()
				// Also show the full table
				var flatForTable []any
				for _, g := range sortedGroups {
					for _, f := range g.Findings {
						flatForTable = append(flatForTable, f)
					}
				}
				tableData := map[string]any{"findings": flatForTable}
				fmt.Print(output.FormatTable(tableData, defaultTableColumns, "findings", output.DefaultStyleCell))
			}
		} else {
			if fieldList != nil {
				for i, f := range transformed {
					transformed[i] = output.FilterFields(f, fieldList)
				}
			}

			items := make([]any, len(transformed))
			for i, f := range transformed {
				items[i] = f
			}
			result := map[string]any{
				"summary": map[string]any{
					"total":                total,
					"showing":              showing,
					"offset":               offset,
					"assessment_breakdown": breakdown,
				},
				"findings": items,
			}

			if format == output.JSON {
				fmt.Println(output.FormatJSON(result))
			} else if format == output.CSV {
				fmt.Print(output.FormatCSV(result, []string{"id", "cve", "severity", "dependency", "repository", "assessment", "assessment_summary"}, "findings"))
			} else {
				// Table output with summary line
				fmt.Fprintf(os.Stderr, "\nShowing %d of %d findings  ", showing, total)
				if len(breakdown) > 0 {
					keys := make([]string, 0, len(breakdown))
					for k := range breakdown {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for i, k := range keys {
						if i > 0 {
							fmt.Fprintf(os.Stderr, " · ")
						}
						color := mapping.GetAssessmentColor(k)
						fmt.Fprintf(os.Stderr, "%s%d %s%s", color, breakdown[k], k, mapping.ColorReset())
					}
				}
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr)
				fmt.Print(output.FormatTable(result, defaultTableColumns, "findings", output.DefaultStyleCell))
			}
		}
		return nil
	},
}

// --- finding get ---

var findingGetCmd = &cobra.Command{
	Use:   "get [finding-id]",
	Short: "Get detailed information about a finding",
	Long: `Get detailed information about a finding.

Exit codes: 0 success, 1 general error, 3 not found, 4 auth failed`,
	Example: `  # Basic finding detail
  konvu finding get abc-123

  # Include evidence (exploitability checklist, reachability)
  konvu finding get abc-123 --include evidence

  # Include recommendation decision logs
  konvu finding get abc-123 --include logs

  # Both
  konvu finding get abc-123 --include evidence --include logs --output json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		findingID := args[0]
		include, _ := cmd.Flags().GetStringSlice("include")
		verbose, _ := cmd.Flags().GetBool("verbose")
		outputFlag, _ := cmd.Flags().GetString("output")
		fields, _ := cmd.Flags().GetString("fields")
		format := output.DetectOutputFormat(outputFlag)

		if verbose {
			hasEvidence := false
			for _, inc := range include {
				if inc == "evidence" {
					hasEvidence = true
					break
				}
			}
			if !hasEvidence {
				include = append(include, "evidence")
			}
		}

		includeSet := make(map[string]bool)
		for _, inc := range include {
			includeSet[inc] = true
		}

		client := api.NewClient("", "")
		defer client.Close()

		fmt.Fprintf(os.Stderr, "Fetching finding %s...\n", findingID)

		detail, err := client.Get(fmt.Sprintf("/sca_findings/%s", findingID), nil)
		if err != nil {
			if apiErr, ok := err.(*api.APIError); ok && apiErr.StatusCode == 404 {
				handleFindingError(&clierrors.CLIError{
					Code:       "FINDING_NOT_FOUND",
					Message:    fmt.Sprintf("Finding '%s' not found", findingID),
					Suggestion: "Run 'konvu finding list' to see available findings.",
					ExitCode:   clierrors.ExitNotFound,
				}, format)
				return nil
			}
			handleFindingError(err, format)
			return nil
		}

		vuln := getMap(detail, "vulnerability")
		ml := getMap(detail, "manifest_location")
		dep := getMap(detail, "dependency")
		analyses := getMap(detail, "analyses")
		qual := getMap(analyses, "qualification")
		_ = getMap(detail, "latest_recommendation") // available but not needed for output
		rec := getStr(detail, "calculated_recommendation")
		assessmentStatus := mapping.RecommendationToAssessment(rec)

		// --- Assessment section ---
		qualSummary := getStr(analyses, "qualification_summary")
		if qualSummary == "" {
			qualSummary = getStr(qual, "summary")
		}

		checklist := getMap(qual, "checklist")
		checklistRaw := getSlice(checklist, "items")
		checklistItems := make([]map[string]any, 0)
		for _, raw := range checklistRaw {
			item, _ := raw.(map[string]any)
			entry := map[string]any{
				"description": getStr(item, "description"),
				"status":      getStr(item, "status"),
				"conclusion":  getStr(item, "check_conclusion"),
			}
			if includeSet["evidence"] {
				entry["investigation_steps"] = getSlice(item, "investigation_steps")
				proofRaw := getSlice(item, "proofs")
				var proofs []map[string]any
				for _, pr := range proofRaw {
					p, _ := pr.(map[string]any)
					proofs = append(proofs, map[string]any{
						"file":    getStr(p, "file"),
						"line":    p["line"],
						"code":    getStr(p, "code"),
						"comment": getStr(p, "comment"),
					})
				}
				entry["proofs"] = proofs
			}
			checklistItems = append(checklistItems, entry)
		}

		// Carto evidence
		carto := getMap(analyses, "carto_evidence")
		cartoApplicable := carto["applicable"]
		cartoSummary := getStr(carto, "summary")
		if cartoApplicable != nil || cartoSummary != "" {
			conclusion := ""
			if cartoApplicable != nil {
				if applicable, ok := cartoApplicable.(bool); ok {
					if applicable {
						conclusion = "Applicable"
					} else {
						conclusion = "Not applicable"
					}
				} else if cartoApplicable != nil {
					conclusion = fmt.Sprintf("%v", cartoApplicable)
				}
			}
			if cartoSummary != "" {
				if conclusion != "" {
					conclusion = conclusion + " — " + cartoSummary
				} else {
					conclusion = cartoSummary
				}
			}
			stackEntry := map[string]any{
				"description": "Vulnerability applicable to dependency stack",
				"status":      "completed",
				"conclusion":  conclusion,
			}
			// Insert at beginning
			checklistItems = append([]map[string]any{stackEntry}, checklistItems...)
		}

		assessmentSection := map[string]any{
			"status":    string(assessmentStatus),
			"summary":   qualSummary,
			"checklist": checklistItems,
		}

		// --- Finding section ---
		source := getMap(detail, "source")
		findingSection := map[string]any{
			"id":         getStr(detail, "id"),
			"dependency": getStr(dep, "name"),
			"repository": getStr(ml, "vcs_repository_url"),
			"manifest":   getStr(ml, "location"),
			"scanner":    getStr(source, "source_name"),
			"source_id":  getStr(source, "identifier"),
			"state":      getStr(source, "state"),
			"first_seen": getStr(source, "remote_created_at"),
		}

		// --- Vulnerability section ---
		vulnSection := map[string]any{
			"cve":      getStr(vuln, "id"),
			"aliases":  getSlice(vuln, "aliases"),
			"severity": strings.ToLower(orDefault(getStr(vuln, "severity"), "unknown")),
			"summary":  getStr(vuln, "summary"),
			"has_fix":  strings.ToLower(orDefault(getStr(vuln, "has_fix"), "unknown")),
			"cvss":     vuln["cvss"],
			"epss":     vuln["epss"],
		}

		result := map[string]any{
			"assessment":    assessmentSection,
			"finding":       findingSection,
			"vulnerability": vulnSection,
		}

		if includeSet["evidence"] {
			reachability := getMap(analyses, "runtime_reachability")
			assessmentSection["reachability"] = reachability
		}

		if includeSet["logs"] {
			logsData, logErr := client.Get(fmt.Sprintf("/sca_findings/%s/logs", findingID), nil)
			if logErr == nil {
				decisions := getSlice(logsData, "recommendation_decisions")
				var decisionList []map[string]any
				for _, dRaw := range decisions {
					d, _ := dRaw.(map[string]any)
					recType := getStr(d, "recommendation_type")
					if recType == "" {
						recType = getStr(d, "raw_recommendation_type")
					}
					entry := map[string]any{
						"type":               recType,
						"reason":             d["recommendation_reason"],
						"confidence":         d["confidence_score"],
						"confidence_factors": d["confidence_factors"],
						"version":            d["decision_logic_version"],
						"created_at":         d["created_at"],
					}
					decisionList = append(decisionList, entry)
				}
				result["logs"] = map[string]any{
					"recommendation_decisions": decisionList,
					"analysis_events":          detail["analysis_events"],
				}
			}
		}

		// Field filtering
		if fields != "" {
			var fieldList []string
			for _, f := range strings.Split(fields, ",") {
				fieldList = append(fieldList, strings.TrimSpace(f))
			}
			result = output.FilterFields(result, fieldList)
		}

		if format == output.JSON {
			fmt.Println(output.FormatJSON(result))
		} else {
			// Table/text output
			a := getMap(result, "assessment")
			v := getMap(result, "vulnerability")
			f := getMap(result, "finding")

			// --- Assessment ---
			status := orDefault(getStr(a, "status"), "unknown")
			color := mapping.GetAssessmentColor(status)
			fmt.Fprintf(os.Stderr, "\nAssessment: %s%s%s\n", color, strings.ToUpper(status), mapping.ColorReset())

			if summary := getStr(a, "summary"); summary != "" {
				fmt.Printf("\n%s\n", summary)
			}

			checklistData := getSlice(a, "checklist")
			if len(checklistData) > 0 {
				fmt.Println("\n--- Checklist ---")
				for _, itemRaw := range checklistData {
					item, _ := itemRaw.(map[string]any)
					itemStatus := strings.ToUpper(orDefault(getStr(item, "status"), "?"))
					fmt.Printf("\n  [%s] %s\n", itemStatus, getStr(item, "description"))
					if conclusion := getStr(item, "conclusion"); conclusion != "" {
						fmt.Printf("  Conclusion: %s\n", conclusion)
					}
					for _, stepRaw := range getSlice(item, "investigation_steps") {
						step, _ := stepRaw.(string)
						fmt.Printf("    - %s\n", step)
					}
					for _, proofRaw := range getSlice(item, "proofs") {
						proof, _ := proofRaw.(map[string]any)
						loc := getStr(proof, "file")
						if line := proof["line"]; line != nil {
							loc += fmt.Sprintf(":%v", line)
						}
						fmt.Printf("    %s\n", loc)
						if code := getStr(proof, "code"); code != "" {
							fmt.Printf("      %s\n", code)
						}
						if comment := getStr(proof, "comment"); comment != "" {
							fmt.Printf("      # %s\n", comment)
						}
					}
				}
			}

			// --- Finding ---
			fmt.Println("\n--- Finding ---")
			fmt.Printf("ID:         %s\n", getStr(f, "id"))
			fmt.Printf("Dependency: %s\n", getStr(f, "dependency"))
			fmt.Printf("Repository: %s\n", getStr(f, "repository"))
			fmt.Printf("Manifest:   %s\n", getStr(f, "manifest"))
			if scanner := getStr(f, "scanner"); scanner != "" {
				fmt.Printf("Scanner:    %s\n", scanner)
			}
			if sourceIDVal := getStr(f, "source_id"); sourceIDVal != "" {
				fmt.Printf("Source ID:  %s\n", sourceIDVal)
			}

			// --- Vulnerability ---
			cveID := orDefault(getStr(v, "cve"), "Unknown")
			fmt.Println("\n--- Vulnerability ---")
			fmt.Println(cveID)
			fmt.Printf("Severity: %s\n", strings.ToUpper(orDefault(getStr(v, "severity"), "unknown")))
			epss := getMap(v, "epss")
			if score, ok := getFloat(epss, "score"); ok && score > 0 {
				percentile := "N/A"
				if p, pOk := getFloat(epss, "percentile"); pOk {
					percentile = fmt.Sprintf("%v", p)
				}
				fmt.Printf("EPSS: %v (percentile: %s)\n", score, percentile)
			}
			vulnSummary := orDefault(getStr(v, "summary"), "No summary available.")
			fmt.Printf("\n%s\n", vulnSummary)

			// --- Logs ---
			logs := getMap(result, "logs")
			if len(logs) > 0 {
				fmt.Println("\n--- Recommendation History ---")
				recDecisions := getSlice(logs, "recommendation_decisions")
				for _, dRaw := range recDecisions {
					d, _ := dRaw.(map[string]any)
					conf := ""
					if confScore, ok := getFloat(d, "confidence"); ok {
						conf = fmt.Sprintf(" (confidence: %.2f)", confScore)
					}
					ts := orDefault(fmt.Sprintf("%v", d["created_at"]), "?")
					dtype := orDefault(fmt.Sprintf("%v", d["type"]), "?")
					reason := orDefault(fmt.Sprintf("%v", d["reason"]), "?")
					fmt.Printf("  %s: %s -- %s%s\n", ts, dtype, reason, conf)
				}
			}
		}
		return nil
	},
}

// --- finding rate ---

var findingRateCmd = &cobra.Command{
	Use:   "rate [finding-id] [rating]",
	Short: "Rate Konvu's assessment of a finding",
	Long: `Rate Konvu's assessment of a finding.

Exit codes: 0 success, 1 general error, 3 not found, 4 auth failed`,
	Example: `  konvu finding rate abc-123 agree
  konvu finding rate abc-123 disagree --comment "Only used in tests"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		findingID := args[0]
		rating := args[1]
		comment, _ := cmd.Flags().GetString("comment")
		recommendationID, _ := cmd.Flags().GetString("recommendation-id")
		outputFlag, _ := cmd.Flags().GetString("output")
		format := output.DetectOutputFormat(outputFlag)

		if rating != "agree" && rating != "disagree" {
			fmt.Fprintln(os.Stderr, "Rating must be 'agree' or 'disagree'.")
			os.Exit(clierrors.ExitUsageError)
		}

		client := api.NewClient("", "")
		defer client.Close()

		var latestRec map[string]any
		recID := recommendationID

		if recID == "" {
			detail, err := client.Get(fmt.Sprintf("/sca_findings/%s", findingID), nil)
			if err != nil {
				errStr := err.Error()
				if strings.Contains(errStr, "404") || strings.Contains(strings.ToLower(errStr), "not found") {
					handleFindingError(&clierrors.CLIError{
						Code:       "FINDING_NOT_FOUND",
						Message:    fmt.Sprintf("Finding %s not found.", findingID),
						Suggestion: "Run 'konvu finding list' to see available findings.",
						ExitCode:   clierrors.ExitNotFound,
					}, format)
					return nil
				}
				handleFindingError(err, format)
				return nil
			}

			latestRec = getMap(detail, "latest_recommendation")
			recID = getStr(latestRec, "id")
			if recID == "" {
				fmt.Fprintln(os.Stderr, "This finding has no recommendation to rate yet.")
				os.Exit(1)
			}
		}

		helpful := rating == "agree"
		payload := map[string]any{
			"helpful":       helpful,
			"feedback_tags": []string{},
			"comment":       comment,
		}

		result, err := client.Post(
			fmt.Sprintf("/recommendation_decision_history/%s/integration_issue/%s/scoring", recID, findingID),
			payload,
		)
		if err != nil {
			handleFindingError(err, format)
			return nil
		}

		if format == output.JSON {
			if result == nil {
				result = map[string]any{"status": "ok"}
			}
			fmt.Println(output.FormatJSON(result))
		} else {
			if len(latestRec) > 0 {
				rawRecType := getStr(latestRec, "raw_recommendation_type")
				assessmentVal := mapping.RecommendationToAssessment(rawRecType)
				color := mapping.GetAssessmentColor(string(assessmentVal))
				fmt.Fprintf(os.Stderr, "Rated assessment %s%s%s as: %s\n",
					color, strings.ToUpper(string(assessmentVal)), mapping.ColorReset(), rating)
			} else {
				fmt.Fprintf(os.Stderr, "Rated as: %s\n", rating)
			}
			if comment != "" {
				fmt.Fprintf(os.Stderr, "Comment: %s\n", comment)
			}
		}
		return nil
	},
}

// --- finding counts ---

var findingCountsCmd = &cobra.Command{
	Use:   "counts",
	Short: "Show assessment counts",
	Long: `Show assessment counts (exploitable, false-positive, etc.).

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed`,
	Example: `  konvu finding counts
  konvu finding counts --since 7d
  konvu finding counts --severity critical --output json
  konvu finding counts --group-by severity
  konvu finding counts --group-by week
  konvu finding counts --group-by month --since 180d`,
	RunE: func(cmd *cobra.Command, args []string) error {
		outputFlag, _ := cmd.Flags().GetString("output")
		format := output.DetectOutputFormat(outputFlag)

		since, _ := cmd.Flags().GetString("since")
		until, _ := cmd.Flags().GetString("until")
		severity, _ := cmd.Flags().GetStringSlice("severity")
		repo, _ := cmd.Flags().GetString("repo")
		source, _ := cmd.Flags().GetString("source")
		groupBy, _ := cmd.Flags().GetString("group-by")

		if groupBy != "" && !validCountsGroupBy[groupBy] {
			fmt.Fprintf(os.Stderr, "Invalid group-by: %s. Valid: month, severity, week\n", groupBy)
			os.Exit(clierrors.ExitUsageError)
		}

		client := api.NewClient("", "")
		defer client.Close()

		baseParams := map[string]any{}
		if since != "" {
			baseParams["first_seen_after"] = parseRelativeDate(since)
		}
		if until != "" && until != "now" {
			baseParams["first_seen_before"] = parseRelativeDate(until)
		}
		if len(severity) > 0 {
			upper := make([]string, len(severity))
			for i, s := range severity {
				upper[i] = strings.ToUpper(s)
			}
			baseParams["severity"] = upper
		}
		if repo != "" {
			baseParams["vcs_repository_url"] = []string{repo}
		}
		if source != "" {
			baseParams["source"] = []string{source}
		}

		if groupBy == "severity" {
			severityLevels := []string{"CRITICAL", "HIGH", "MODERATE", "LOW"}
			var rows []map[string]any
			for _, sev := range severityLevels {
				sevParams := make(map[string]any)
				for k, v := range baseParams {
					sevParams[k] = v
				}
				sevParams["severity"] = []string{sev}
				counts := computeAssessmentCounts(client, sevParams)
				rowTotal := 0
				for _, v := range counts {
					rowTotal += v
				}
				if rowTotal > 0 {
					row := map[string]any{"severity": strings.ToLower(sev), "total": rowTotal}
					for k, v := range counts {
						row[k] = v
					}
					rows = append(rows, row)
				}
			}

			grandTotal := 0
			for _, r := range rows {
				grandTotal += r["total"].(int)
			}
			result := map[string]any{
				"total":    grandTotal,
				"group_by": "severity",
				"rows":     rows,
			}

			if format == output.JSON {
				fmt.Println(output.FormatJSON(result))
			} else {
				fmt.Println("\nAssessment Counts by Severity")
				fmt.Println(strings.Repeat("=", 60))
				header := fmt.Sprintf("  %-12s", "severity")
				for _, status := range mapping.AllStatuses {
					header += fmt.Sprintf(" %15s", string(status))
				}
				header += fmt.Sprintf(" %8s", "total")
				fmt.Println(header)
				for _, row := range rows {
					line := fmt.Sprintf("  %-12s", row["severity"])
					for _, status := range mapping.AllStatuses {
						val := 0
						if v, ok := row[string(status)]; ok {
							val = v.(int)
						}
						line += fmt.Sprintf(" %15d", val)
					}
					line += fmt.Sprintf(" %8d", row["total"])
					fmt.Println(line)
				}
			}
		} else if groupBy == "week" || groupBy == "month" {
			periods := generateTimePeriods(groupBy, since)
			var rows []map[string]any
			for _, period := range periods {
				periodParams := make(map[string]any)
				for k, v := range baseParams {
					periodParams[k] = v
				}
				periodParams["first_seen_after"] = period["start"]
				periodParams["first_seen_before"] = period["end"]
				counts := computeAssessmentCounts(client, periodParams)
				rowTotal := 0
				for _, v := range counts {
					rowTotal += v
				}
				row := map[string]any{"period": period["label"], "total": rowTotal}
				for k, v := range counts {
					row[k] = v
				}
				rows = append(rows, row)
			}

			grandTotal := 0
			for _, r := range rows {
				grandTotal += r["total"].(int)
			}
			result := map[string]any{
				"total":    grandTotal,
				"group_by": groupBy,
				"rows":     rows,
			}

			if format == output.JSON {
				fmt.Println(output.FormatJSON(result))
			} else {
				label := "Week"
				if groupBy == "month" {
					label = "Month"
				}
				fmt.Printf("\nAssessment Counts by %s\n", label)
				fmt.Println(strings.Repeat("=", 70))
				header := fmt.Sprintf("  %-20s", "period")
				for _, status := range mapping.AllStatuses {
					header += fmt.Sprintf(" %15s", string(status))
				}
				header += fmt.Sprintf(" %8s", "total")
				fmt.Println(header)
				for _, row := range rows {
					line := fmt.Sprintf("  %-20s", row["period"])
					for _, status := range mapping.AllStatuses {
						val := 0
						if v, ok := row[string(status)]; ok {
							val = v.(int)
						}
						line += fmt.Sprintf(" %15d", val)
					}
					line += fmt.Sprintf(" %8d", row["total"])
					fmt.Println(line)
				}
			}
		} else {
			// No grouping — simple counts
			counts := computeAssessmentCounts(client, baseParams)
			total := 0
			for _, v := range counts {
				total += v
			}
			result := map[string]any{"total": total}
			for k, v := range counts {
				result[k] = v
			}

			if format == output.JSON {
				fmt.Println(output.FormatJSON(result))
			} else {
				fmt.Println("\nAssessment Counts")
				fmt.Println(strings.Repeat("=", 25))
				for _, status := range mapping.AllStatuses {
					val := counts[string(status)]
					fmt.Printf("  %-20s %6d\n", string(status), val)
				}
				fmt.Printf("  %-20s %6d\n", "total", total)
			}
		}
		return nil
	},
}

// orDefault returns s if non-empty, else fallback.
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func init() {
	// finding list — all 21 flags
	findingListCmd.Flags().String("since", "", "Start date: '7d', '30d', or ISO date")
	findingListCmd.Flags().String("until", "", "End date: 'now' or ISO date")
	findingListCmd.Flags().StringSliceP("severity", "s", nil, "Filter: critical,high,moderate,low")
	findingListCmd.Flags().StringSliceP("assessment", "a", nil, "Filter: exploitable,false-positive,inconclusive,not-assessed")
	findingListCmd.Flags().StringSlice("state", nil, "Filter: open,dismissed,fixed,muted")
	findingListCmd.Flags().String("has-fix", "", "Filter: fixed, no_fix")
	findingListCmd.Flags().StringP("repo", "r", "", "Filter by repository URL or name")
	findingListCmd.Flags().String("cve", "", "Filter by CVE ID")
	findingListCmd.Flags().StringP("dependency", "d", "", "Filter by dependency name")
	findingListCmd.Flags().String("source", "", "Filter by scanner source: snyk, dependabot, etc.")
	findingListCmd.Flags().String("source-id", "", "Filter by external source identifier")
	findingListCmd.Flags().String("sort", "recommendation", "Sort: severity,recommendation,first_seen_at,updated_at,dependency_name,cve")
	findingListCmd.Flags().String("order", "desc", "Order: asc,desc")
	findingListCmd.Flags().IntP("limit", "n", 50, "Maximum findings to return")
	findingListCmd.Flags().Int("offset", 0, "Skip N results")
	findingListCmd.Flags().StringP("output", "o", "", "Output format: json, table, csv")
	findingListCmd.Flags().BoolP("quiet", "q", false, "Output bare finding IDs only")
	findingListCmd.Flags().Bool("count", false, "Output only the total count")
	findingListCmd.Flags().StringP("group-by", "g", "", "Group by: repository, dependency, severity, assessment")
	findingListCmd.Flags().String("fields", "", "Comma-separated fields to include in JSON output")

	// finding get
	findingGetCmd.Flags().StringSliceP("include", "i", nil, "Include: evidence, logs")
	findingGetCmd.Flags().BoolP("verbose", "v", false, "Show all details for each check")
	findingGetCmd.Flags().StringP("output", "o", "", "Output format: json, table")
	findingGetCmd.Flags().String("fields", "", "Comma-separated fields to include")

	// finding rate
	findingRateCmd.Flags().StringP("comment", "c", "", "Optional feedback comment")
	findingRateCmd.Flags().String("recommendation-id", "", "Recommendation ID (skips extra API call)")
	findingRateCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	// finding counts
	findingCountsCmd.Flags().String("since", "", "Start date: '7d', '30d', or ISO date")
	findingCountsCmd.Flags().String("until", "", "End date: 'now' or ISO date")
	findingCountsCmd.Flags().StringSliceP("severity", "s", nil, "Filter: critical,high,moderate,low")
	findingCountsCmd.Flags().StringP("repo", "r", "", "Filter by repository URL or name")
	findingCountsCmd.Flags().String("source", "", "Filter by scanner source")
	findingCountsCmd.Flags().StringP("group-by", "g", "", "Break down by: severity, week, month")
	findingCountsCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	findingCmd.AddCommand(findingListCmd)
	findingCmd.AddCommand(findingGetCmd)
	findingCmd.AddCommand(findingRateCmd)
	findingCmd.AddCommand(findingCountsCmd)
	rootCmd.AddCommand(findingCmd)
}
