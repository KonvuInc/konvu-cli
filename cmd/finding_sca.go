package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/mapping"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var findingListCmd = &cobra.Command{
	Use:   "list",
	Short: "List security findings",
	Long: `List security findings with filtering and sorting.

Note: --since / --until filter by FIRST-SEEN date (when the finding first appeared),
not by when it changed state. To scope by when a finding was closed, use
--dismissed-since / --dismissed-before (dismissed findings only).

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed`,
	Example: `  # This week's exploitable findings (by first-seen date)
  konvu finding list --since 7d --assessment exploitable

  # Critical findings sorted by recency
  konvu finding list --severity critical --sort first_seen_at --output json

  # Just the count of not-assessed findings
  konvu finding list --assessment not-assessed --count

  # Findings with available fixes
  konvu finding list --has-fix fixed --assessment exploitable

  # Report: exploitable findings now fixed, with fix attribution (patcheus/unknown)
  konvu finding list --assessment exploitable --state fixed --output csv > report.csv

  # Group exploitable findings by repo to prioritize (counts cover the full result set)
  konvu finding list --assessment exploitable --group-by repository

  # Findings dismissed since mid-July (closed-in-window)
  konvu finding list --assessment exploitable --dismissed-since 2025-07-15

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
		dismissedSince, _ := cmd.Flags().GetString("dismissed-since")
		dismissedBefore, _ := cmd.Flags().GetString("dismissed-before")

		// Validate group-by
		if groupBy != "" && !validListGroupBy[groupBy] {
			fmt.Fprintf(os.Stderr, "Invalid group-by: %s. Valid: assessment, dependency, repository, severity\n", groupBy)
			os.Exit(clierrors.ExitUsageError)
		}

		client := api.NewClient("", "")
		defer client.Close()

		// Build filter params (no pagination / sort — those are added below).
		filterParams := map[string]any{}
		if since != "" {
			filterParams["first_seen_after"] = parseRelativeDate(since)
		}
		if until != "" && until != "now" {
			filterParams["first_seen_before"] = parseRelativeDate(until)
		}
		if len(severity) > 0 {
			upper := make([]string, len(severity))
			for i, s := range severity {
				upper[i] = strings.ToUpper(s)
			}
			filterParams["severity"] = upper
		}
		if len(assessment) > 0 {
			statuses, err := parseAssessments(assessment)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(clierrors.ExitUsageError)
			}
			var recs []string
			for _, s := range statuses {
				recs = append(recs, mapping.AssessmentToRecommendation(s)...)
			}
			filterParams["recommendation"] = recs
		}
		if len(state) > 0 {
			// Backend param is `source_state`; `any_source_state` is silently
			// ignored which used to make --state a no-op.
			filterParams["source_state"] = state
		}
		if hasFix != "" {
			filterParams["has_fix"] = hasFix
		}
		if repo != "" {
			filterParams["vcs_repository_url"] = []string{repo}
		}
		if cve != "" {
			filterParams["cve"] = []string{cve}
		}
		if dependency != "" {
			filterParams["dependency_name"] = []string{dependency}
		}
		if source != "" {
			filterParams["source"] = []string{source}
		}

		// Grouping and the client-side dismissed-date filter need the full result
		// set, not just one page, or their counts would reflect only the first page.
		dismissedFilter := dismissedSince != "" || dismissedBefore != ""
		needAll := groupBy != "" || dismissedFilter

		// --count without a client-side filter can use the cheap server count.
		// With --dismissed-since/-before we must fetch and filter first (below).
		if count && !dismissedFilter {
			total, err := countFindings(client, filterParams)
			if err != nil {
				handleFindingError(err, format)
				return nil
			}
			fmt.Println(total)
			return nil
		}

		var items []any
		var total int
		truncated := false

		if needAll {
			all, tr, err := fetchAllFindings(client, filterParams, sortFlag, order)
			if err != nil {
				handleFindingError(err, format)
				return nil
			}
			items = all
			truncated = tr
			total = len(items)
		} else {
			perPage := limit
			if perPage > 1000 {
				perPage = 1000
			}
			params := make(map[string]any, len(filterParams)+4)
			for k, v := range filterParams {
				params[k] = v
			}
			params["per_page"] = perPage
			params["page"] = (offset / maxInt(limit, 1)) + 1
			params["sort"] = sortFlag
			params["order"] = order

			data, err := client.Get("/sca_findings", params)
			if err != nil {
				handleFindingError(err, format)
				return nil
			}

			items = getSlice(data, "items")
			// The API doesn't return a total; derive it. If the page is full there
			// may be more, so paginate to get an accurate count for the summary.
			total = offset + len(items)
			if len(items) == perPage {
				if t, err := countFindings(client, filterParams); err == nil {
					total = t
				}
			}
		}

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

		// Client-side dismissed-date filter ("closed in window"). dismissed_at is
		// only populated for dismissed/muted findings, so this also restricts to them.
		if dismissedFilter {
			lo, hi, perr := parseDismissedWindow(dismissedSince, dismissedBefore)
			if perr != nil {
				fmt.Fprintf(os.Stderr, "Invalid dismissed date: %v\n", perr)
				os.Exit(clierrors.ExitUsageError)
			}
			kept := transformed[:0]
			for _, f := range transformed {
				da, _ := f["dismissed_at"].(string)
				t, err := time.Parse(time.RFC3339, da)
				if err != nil {
					continue // no/unparseable dismissed_at → not dismissed
				}
				if !lo.IsZero() && t.Before(lo) {
					continue
				}
				if !hi.IsZero() && !t.Before(hi) {
					continue
				}
				kept = append(kept, f)
			}
			transformed = kept
		}

		// When we fetched the full result set, the accurate total is what we hold.
		if needAll {
			total = len(transformed)
		}

		if truncated {
			fmt.Fprintf(os.Stderr, "Warning: result capped at %d findings; counts may be incomplete. Narrow with filters.\n", groupFetchCap)
		}

		// --count combined with the client-side dismissed filter: print the
		// post-filter total (the cheap server count above was skipped).
		if count {
			fmt.Println(total)
			return nil
		}

		if quiet {
			ids := transformed
			if needAll {
				ids = applyWindow(transformed, offset, limit)
			}
			fmt.Println(output.FormatQuiet(ids, "id"))
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

			// Emitted findings honor --offset/--limit; group counts stay full-set.
			var flatAll []map[string]any
			for _, g := range sortedGroups {
				flatAll = append(flatAll, g.Findings...)
			}
			displayFindings := applyWindow(flatAll, offset, limit)
			showing = len(displayFindings)

			// Regroup the windowed findings, preserving sorted-group order.
			displayByGroup := make(map[string][]map[string]any)
			for _, f := range displayFindings {
				k, _ := f[groupBy].(string)
				if k == "" {
					k = "unknown"
				}
				displayByGroup[k] = append(displayByGroup[k], f)
			}

			// Build grouped result for JSON: count is the full group size, findings
			// is the windowed subset.
			var groupedResult []map[string]any
			for _, g := range sortedGroups {
				groupFindings := displayByGroup[g.Key]
				if fieldList != nil {
					filtered := make([]map[string]any, len(groupFindings))
					for i, f := range groupFindings {
						filtered[i] = output.FilterFields(f, fieldList)
					}
					groupFindings = filtered
				}
				if groupFindings == nil {
					// Group outside the display window: keep its count, emit [] not null.
					groupFindings = []map[string]any{}
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
				cols := []string{groupBy, "id", "cve", "severity", "dependency", "assessment", "state", "fix_source"}
				if fieldList != nil {
					cols = append([]string{groupBy}, fieldList...)
				}
				fmt.Print(output.FormatCSV(csvData, cols, "findings"))
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
				// Findings table (windowed by --offset/--limit)
				var flatForTable []any
				for _, f := range displayFindings {
					flatForTable = append(flatForTable, f)
				}
				tableData := map[string]any{"findings": flatForTable}
				fmt.Print(output.FormatTable(tableData, defaultTableColumns, "findings", output.DefaultStyleCell))
			}
		} else {
			// When the full set was fetched (dismissed filter), honor --offset/--limit
			// for display; the server already paginated the normal path.
			display := transformed
			if needAll {
				display = applyWindow(transformed, offset, limit)
			}
			showing = len(display)

			if fieldList != nil {
				for i, f := range display {
					display[i] = output.FilterFields(f, fieldList)
				}
			}

			items := make([]any, len(display))
			for i, f := range display {
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
				cols := defaultCSVColumns
				if fieldList != nil {
					cols = fieldList
				}
				fmt.Print(output.FormatCSV(result, cols, "findings"))
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

func buildFindingResult(detail map[string]any, includeEvidence bool) map[string]any {
	vuln := getMap(detail, "vulnerability")
	ml := getMap(detail, "manifest_location")
	dep := getMap(detail, "dependency")
	assess := getMap(detail, "assessment")
	details := getMap(assess, "details")
	qual := getMap(details, "ai_assessment")

	assessmentResult := normalizeAssessmentResult(getStr(assess, "result"))
	qualSummary := getStr(assess, "summary")

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
		if includeEvidence {
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

	carto := getMap(getMap(details, "environment_analysis"), "evidence")
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
		checklistItems = append([]map[string]any{stackEntry}, checklistItems...)
	}

	assessmentSection := map[string]any{
		"status":    assessmentResult,
		"summary":   qualSummary,
		"checklist": checklistItems,
	}

	source := getMap(detail, "source")
	findingSection := map[string]any{
		"id":         getStr(detail, "id"),
		"dependency": getStr(dep, "name"),
		"repository": getStr(ml, "vcs_repository_url"),
		"manifest":   getStr(ml, "location"),
		"scanner":    scannerLabel(source),
		"source_id":  getStr(source, "identifier"),
		"state":      getStr(source, "state"),
		"first_seen": getStr(source, "remote_created_at"),
	}

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

	if includeEvidence {
		assessmentSection["reachability"] = getMap(details, "runtime_reachability")
	}

	return result
}

func runtimeReachabilityText(reach map[string]any) string {
	status := getStr(reach, "status")
	errMsg := getStr(reach, "error")
	if status == "" && errMsg == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n--- Runtime Reachability ---\n")

	switch status {
	case "completed":
		if observed, _ := getBool(reach, "has_findings"); observed {
			b.WriteString("Vulnerable dependency/function observed at runtime: yes\n")
		} else {
			b.WriteString("Vulnerable dependency/function observed at runtime: no\n")
		}
	case "not_installed":
		b.WriteString("Status: runtime sensor not installed\n")
	case "no_data":
		b.WriteString("Status: no runtime data collected yet\n")
	default:
		if status != "" {
			fmt.Fprintf(&b, "Status: %s\n", strings.ToUpper(status))
		}
	}

	if summary := getStr(reach, "summary"); summary != "" {
		fmt.Fprintf(&b, "%s\n", summary)
	}

	findings := getMap(reach, "findings")
	writeRuntimeObservation(&b, "Dependency", getMap(findings, "dependency"))
	writeRuntimeObservation(&b, "Function", getMap(findings, "function"))

	if errMsg != "" {
		fmt.Fprintf(&b, "Error: %s\n", errMsg)
	}
	return b.String()
}

func writeRuntimeObservation(b *strings.Builder, label string, observation map[string]any) {
	rec := getMap(observation, "last")
	if len(rec) == 0 {
		rec = getMap(observation, "first")
	}
	if len(rec) == 0 {
		return
	}
	line := getStr(rec, "name")
	if v := getStr(rec, "version"); v != "" {
		line += "@" + v
	}
	if callSite := getStr(rec, "call_site"); callSite != "" {
		line += " (" + callSite + ")"
	}
	fmt.Fprintf(b, "  %s observed: %s\n", label, line)
}

func checklistItemText(item map[string]any) string {
	var b strings.Builder
	status := strings.ToUpper(orDefault(getStr(item, "status"), "?"))
	fmt.Fprintf(&b, "\n  [%s] %s\n", status, getStr(item, "description"))
	if conclusion := getStr(item, "conclusion"); conclusion != "" {
		fmt.Fprintf(&b, "  Conclusion: %s\n", conclusion)
	}

	if steps := getSlice(item, "investigation_steps"); len(steps) > 0 {
		b.WriteString("\n  Investigation steps:\n")
		for _, raw := range steps {
			step, _ := raw.(string)
			fmt.Fprintf(&b, "    - %s\n", step)
		}
	}

	if proofs := getSlice(item, "proofs"); len(proofs) > 0 {
		b.WriteString("\n  Proofs:\n")
		for _, raw := range proofs {
			proof, _ := raw.(map[string]any)
			loc := getStr(proof, "file")
			if line := proof["line"]; line != nil {
				loc += fmt.Sprintf(":%v", line)
			}
			fmt.Fprintf(&b, "    %s\n", loc)
			if code := getStr(proof, "code"); code != "" {
				writeIndentedLines(&b, "        ", code)
			}
			if comment := getStr(proof, "comment"); comment != "" {
				writeIndentedLines(&b, "      ", "# "+comment)
			}
		}
	}
	return b.String()
}

func writeIndentedLines(b *strings.Builder, indent, text string) {
	for _, line := range strings.Split(text, "\n") {
		b.WriteString(indent)
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

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

		result := buildFindingResult(detail, includeSet["evidence"])

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
					fmt.Print(checklistItemText(item))
				}
			}

			if txt := runtimeReachabilityText(getMap(a, "reachability")); txt != "" {
				fmt.Print(txt)
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

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
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

		recID := recommendationID
		var rateAssessResult string

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

			latestRec := getMap(detail, "latest_recommendation")
			recID = getStr(latestRec, "id")
			if recID == "" {
				fmt.Fprintln(os.Stderr, "This finding has no recommendation to rate yet.")
				os.Exit(1)
			}
			rateAssessResult = normalizeAssessmentResult(getStr(getMap(detail, "assessment"), "result"))
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
			if rateAssessResult != "" && rateAssessResult != string(mapping.NotAssessed) {
				color := mapping.GetAssessmentColor(rateAssessResult)
				fmt.Fprintf(os.Stderr, "Rated assessment %s%s%s as: %s\n",
					color, strings.ToUpper(rateAssessResult), mapping.ColorReset(), rating)
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
		assessment, _ := cmd.Flags().GetStringSlice("assessment")
		state, _ := cmd.Flags().GetStringSlice("state")
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
		if len(state) > 0 {
			baseParams["source_state"] = state
		}
		if repo != "" {
			baseParams["vcs_repository_url"] = []string{repo}
		}
		if source != "" {
			baseParams["source"] = []string{source}
		}

		// Which assessment buckets to count. Default to all; --assessment narrows it
		// (e.g. only the exploitable column), which also speeds up the query.
		statuses := mapping.AllStatuses
		if len(assessment) > 0 {
			parsed, err := parseAssessments(assessment)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(clierrors.ExitUsageError)
			}
			statuses = parsed
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
				counts := computeAssessmentCounts(client, sevParams, statuses)
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
				for _, status := range statuses {
					header += fmt.Sprintf(" %15s", string(status))
				}
				header += fmt.Sprintf(" %8s", "total")
				fmt.Println(header)
				for _, row := range rows {
					line := fmt.Sprintf("  %-12s", row["severity"])
					for _, status := range statuses {
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
				counts := computeAssessmentCounts(client, periodParams, statuses)
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
				for _, status := range statuses {
					header += fmt.Sprintf(" %15s", string(status))
				}
				header += fmt.Sprintf(" %8s", "total")
				fmt.Println(header)
				for _, row := range rows {
					line := fmt.Sprintf("  %-20s", row["period"])
					for _, status := range statuses {
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
			counts := computeAssessmentCounts(client, baseParams, statuses)
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
				for _, status := range statuses {
					val := counts[string(status)]
					fmt.Printf("  %-20s %6d\n", string(status), val)
				}
				fmt.Printf("  %-20s %6d\n", "total", total)
			}
		}
		return nil
	},
}

func init() {
	// finding list — all 21 flags
	findingListCmd.Flags().String("since", "", "First-seen start date: '7d', '30d', or ISO date")
	findingListCmd.Flags().String("until", "", "First-seen end date: 'now' or ISO date")
	findingListCmd.Flags().String("dismissed-since", "", "Only findings dismissed on/after this date ('7d' or ISO); closed-in-window filter")
	findingListCmd.Flags().String("dismissed-before", "", "Only findings dismissed before this date ('now' or ISO)")
	findingListCmd.Flags().StringSliceP("severity", "s", nil, "Filter: critical,high,moderate,low")
	findingListCmd.Flags().StringSliceP("assessment", "a", nil, "Filter: exploitable,false-positive,inconclusive,not-assessed")
	findingListCmd.Flags().StringSlice("state", nil, "Filter: open,dismissed,fixed,muted")
	findingListCmd.Flags().String("has-fix", "", "Filter: fixed, no_fix")
	findingListCmd.Flags().StringP("repo", "r", "", "Filter by repository URL or name")
	findingListCmd.Flags().String("cve", "", "Filter by CVE ID")
	findingListCmd.Flags().StringP("dependency", "d", "", "Filter by dependency name")
	findingListCmd.Flags().String("source", "", "Filter by scanner source: snyk, dependabot, or a label submitted via 'finding submit'")
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
	findingCountsCmd.Flags().String("since", "", "First-seen start date: '7d', '30d', or ISO date")
	findingCountsCmd.Flags().String("until", "", "First-seen end date: 'now' or ISO date")
	findingCountsCmd.Flags().StringSliceP("severity", "s", nil, "Filter: critical,high,moderate,low")
	findingCountsCmd.Flags().StringSliceP("assessment", "a", nil, "Filter: exploitable,false-positive,inconclusive,not-assessed")
	findingCountsCmd.Flags().StringSlice("state", nil, "Filter: open,dismissed,fixed,muted")
	findingCountsCmd.Flags().StringP("repo", "r", "", "Filter by repository URL or name")
	findingCountsCmd.Flags().String("source", "", "Filter by scanner source, incl. a label submitted via 'finding submit'")
	findingCountsCmd.Flags().StringP("group-by", "g", "", "Break down by: severity, week, month")
	findingCountsCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	findingCmd.AddCommand(findingListCmd)
	findingCmd.AddCommand(findingGetCmd)
	findingCmd.AddCommand(findingRateCmd)
	findingCmd.AddCommand(findingCountsCmd)
	rootCmd.AddCommand(findingCmd)
}
