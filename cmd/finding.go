package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/findings"
	"github.com/KonvuInc/konvu-cli/pkg/mapping"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var findingCmd = &cobra.Command{
	Use:   "finding",
	Short: "Security findings",
}

var defaultTableColumns = []string{"cve", "dependency", "repository", "assessment", "assessment_summary"}

// defaultCSVColumns is a report-oriented column set: it includes state and dates so
// a CSV export is usable on its own (the table view stays compact).
var defaultCSVColumns = []string{"id", "cve", "severity", "dependency", "repository", "state", "first_seen", "has_fix", "assessment", "dismissed_at", "autofix_status", "fix_source", "triage_url"}
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

// parseClientDate resolves a --since-style value ('7d', ISO date, or RFC3339) to a
// concrete UTC time for client-side comparisons.
func parseClientDate(value string) (time.Time, error) {
	v := parseRelativeDate(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date %q (use '7d', 'now', or an ISO date)", value)
}

// parseDismissedWindow parses the optional --dismissed-since / --dismissed-before
// bounds. Zero times mean the bound is open.
func parseDismissedWindow(since, before string) (lo, hi time.Time, err error) {
	if since != "" {
		if lo, err = parseClientDate(since); err != nil {
			return lo, hi, err
		}
	}
	if before != "" && before != "now" {
		if hi, err = parseClientDate(before); err != nil {
			return lo, hi, err
		}
	}
	return lo, hi, nil
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
	switch v := m[key].(type) {
	case []any:
		return v
	case []map[string]any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out
	}
	return nil
}

func getBool(m map[string]any, key string) (bool, bool) {
	v, ok := m[key].(bool)
	return v, ok
}

func getFloat(m map[string]any, key string) (float64, bool) {
	v, ok := m[key].(float64)
	return v, ok
}

// scannerLabel reads the submitted scanner label, falling back to source_name
// (the ingestion channel) for backends that don't send scanner yet.
func scannerLabel(source map[string]any) string {
	return orDefault(getStr(source, "scanner"), getStr(source, "source_name"))
}

func normalizeAssessmentResult(result string) string {
	if result == "" {
		return string(mapping.NotAssessed)
	}
	// API uses underscores ("false_positive"), CLI uses hyphens ("false-positive")
	return strings.ReplaceAll(result, "_", "-")
}

// parseAssessments normalizes and validates --assessment values. It rejects
// unknown statuses so they don't silently fall through AssessmentToRecommendation's
// default branch and count the wrong bucket.
func parseAssessments(values []string) ([]mapping.AssessmentStatus, error) {
	var out []mapping.AssessmentStatus
	for _, a := range values {
		s := mapping.AssessmentStatus(strings.ToLower(strings.ReplaceAll(a, "_", "-")))
		if !mapping.IsValidStatus(s) {
			return nil, fmt.Errorf("invalid assessment %q (valid: exploitable, false-positive, needs-input, inconclusive, not-assessed)", a)
		}
		out = append(out, s)
	}
	return out, nil
}

func transformFinding(finding map[string]any) map[string]any {
	vuln := getMap(finding, "vulnerability")
	ml := getMap(finding, "manifest_location")
	dep := getMap(finding, "dependency")
	source := getMap(finding, "source")
	assess := getMap(finding, "assessment")
	autofix := getMap(finding, "autofix_pr")
	risk := getMap(finding, "risk_score")

	cve := getStr(vuln, "display_id")
	if cve == "" {
		cve = getStr(vuln, "id")
	}

	assessmentResult := normalizeAssessmentResult(getStr(assess, "result"))
	assessmentSummary := getStr(assess, "summary")

	severity := strings.ToLower(getStr(vuln, "severity"))
	if severity == "" {
		severity = "unknown"
	}
	hasFix := strings.ToLower(getStr(vuln, "has_fix"))
	if hasFix == "" {
		hasFix = "unknown"
	}

	state := getStr(source, "state")
	autofixStatus := getStr(autofix, "status")

	return map[string]any{
		"id":                 getStr(finding, "id"),
		"cve":                cve,
		"severity":           severity,
		"dependency":         getStr(dep, "name"),
		"repository":         getStr(ml, "vcs_repository_url"),
		"manifest":           getStr(ml, "location"),
		"assessment":         assessmentResult,
		"assessment_summary": assessmentSummary,
		"has_fix":            hasFix,
		"first_seen":         getStr(source, "remote_created_at"),
		"state":              state,
		"source_id":          getStr(source, "identifier"),
		"scanner":            scannerLabel(source),
		"triage_url":         getStr(finding, "triage_url"),
		// Fields already present in the /sca_findings payload, surfaced here for reporting.
		"dismissed_at":     getStr(source, "dismissed_at"),
		"dismissed_reason": getStr(source, "dismissed_reason"),
		"last_assessed_at": getStr(assess, "last_assessed_at"),
		"risk_tier":        getStr(risk, "tier"),
		"autofix_status":   autofixStatus,
		"autofix_pr_url":   getStr(autofix, "pr_url"),
		// Heuristic fix attribution over fields already in the payload. Empty unless fixed.
		"fix_source": deriveFixSource(state, autofixStatus),
	}
}

// deriveFixSource labels how a fixed finding was remediated, using only fields
// already in the response. A merged autofix PR is a reliable "fixed by patcheus"
// signal. Without it we cannot distinguish an external fix from missing autofix
// data client-side, so we report "unknown" rather than asserting "external".
// Returns "" for findings that are not in the fixed state.
func deriveFixSource(state, autofixStatus string) string {
	if state != "fixed" {
		return ""
	}
	if autofixStatus == "merged" {
		return "patcheus"
	}
	return "unknown"
}

// groupFetchCap bounds how many findings are pulled when an operation needs the
// full result set (--group-by, --dismissed-since/-before). At 500/page this is at
// most ~20 requests. Beyond it, results are flagged as truncated rather than
// silently undercounted.
const groupFetchCap = 10000

// fetchAllFindings paginates /sca_findings, collecting up to groupFetchCap items so
// client-side grouping and filtering see the whole result set instead of one page.
// The bool return is true when the cap was hit (results truncated).
func fetchAllFindings(client *api.Client, filterParams map[string]any, sortFlag, order string) ([]any, bool, error) {
	const pageSize = 500
	p := make(map[string]any, len(filterParams)+4)
	for k, v := range filterParams {
		p[k] = v
	}
	p["per_page"] = pageSize
	p["sort"] = sortFlag
	p["order"] = order
	var all []any
	for page := 1; ; page++ {
		p["page"] = page
		data, err := client.Get("/sca_findings", p)
		if err != nil {
			return nil, false, err
		}
		items := getSlice(data, "items")
		all = append(all, items...)
		// A short page means we've reached the end: truncated only if we still
		// overflowed the cap (never for an exactly-cap result set).
		if len(items) < pageSize {
			if len(all) > groupFetchCap {
				return all[:groupFetchCap], true, nil
			}
			return all, false, nil
		}
		// Full page and strictly over the cap: more remain, so truncate. At exactly
		// the cap we keep going to confirm exhaustion on the next (short) page.
		if len(all) > groupFetchCap {
			return all[:groupFetchCap], true, nil
		}
	}
}

// dependabotAlertURLRe matches a GitHub Dependabot alert URL, capturing the
// repository URL and the per-repo alert number, e.g.
// https://github.com/octo-org/octo-repo/security/dependabot/42
var dependabotAlertURLRe = regexp.MustCompile(`(?i)^(https?://[^/]+/[^/]+/[^/]+)/security/dependabot/(\d+)/?$`)

// dependabotAlertShortRe matches the OWNER/REPO#NUMBER shorthand, e.g.
// octo-org/octo-repo#42
var dependabotAlertShortRe = regexp.MustCompile(`^([^\s/]+/[^\s/#]+)#(\d+)$`)

// parseDependabotRef recognizes a GitHub Dependabot alert reference and returns a
// server-side repo filter (param name + value) that scopes it to a single repo,
// plus the alert number. It accepts the full alert URL
// (https://github.com/OWNER/REPO/security/dependabot/N) or the OWNER/REPO#N
// shorthand. ok is false for anything else, e.g. a Konvu finding UUID.
func parseDependabotRef(arg string) (repoParam, repoValue, alertNumber string, ok bool) {
	if m := dependabotAlertURLRe.FindStringSubmatch(arg); m != nil {
		return "vcs_repository_url", m[1], m[2], true
	}
	if m := dependabotAlertShortRe.FindStringSubmatch(arg); m != nil {
		return "repo_glob", m[1], m[2], true
	}
	return "", "", "", false
}

// resolveDependabotFinding maps a Dependabot alert reference to its Konvu finding
// UUID by querying /sca_findings scoped to the alert number and repo. Alert numbers
// are per-repo, so the repo filter is what makes the match unique. It returns a
// clear CLIError when nothing matches or the reference is ambiguous.
func resolveDependabotFinding(client *api.Client, repoParam, repoValue, alertNumber string) (string, error) {
	data, err := client.Get("/sca_findings", map[string]any{
		"dependabot_id": []string{alertNumber},
		repoParam:       []string{repoValue},
		"per_page":      2,
	})
	if err != nil {
		return "", err
	}
	items := getSlice(data, "items")
	switch {
	case len(items) == 0:
		return "", &clierrors.CLIError{
			Code:       "FINDING_NOT_FOUND",
			Message:    fmt.Sprintf("No Konvu finding matches Dependabot alert %s in %s", alertNumber, repoValue),
			Suggestion: "Confirm the repository is onboarded to Konvu and the alert number is correct.",
			ExitCode:   clierrors.ExitNotFound,
		}
	case len(items) > 1:
		// A full URL already pins one repo, so re-passing it can't disambiguate;
		// only the OWNER/REPO#N shorthand (a glob) is narrowed by giving the URL.
		suggestion := "Pass the full alert URL (…/security/dependabot/N) to disambiguate."
		if repoParam == "vcs_repository_url" {
			suggestion = fmt.Sprintf("Run `konvu finding list --repo %s` and pass the Konvu finding ID directly.", repoValue)
		}
		return "", &clierrors.CLIError{
			Code:       "FINDING_AMBIGUOUS",
			Message:    fmt.Sprintf("Dependabot alert %s matches %d findings in %s", alertNumber, len(items), repoValue),
			Suggestion: suggestion,
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	m, _ := items[0].(map[string]any)
	id := getStr(m, "id")
	if id == "" {
		return "", clierrors.NewAPIError("matched finding is missing an id")
	}
	return id, nil
}

func computeAssessmentCounts(client *api.Client, baseParams map[string]any, statuses []mapping.AssessmentStatus) map[string]int {
	counts := make(map[string]int)
	for _, status := range statuses {
		params := make(map[string]any, len(baseParams)+1)
		for k, v := range baseParams {
			params[k] = v
		}
		params["recommendation"] = mapping.AssessmentToRecommendation(status)
		n, err := findings.CountByPagination(client, "/sca_findings", params)
		if err != nil {
			continue
		}
		counts[string(status)] = n
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

// applyWindow returns the [offset, offset+limit) slice of list, clamped to bounds.
// A non-positive limit means "no limit" (return everything from offset). Used to
// apply --offset/--limit to findings we fetched in full (group-by / dismissed filter)
// so the display honors the requested page while counts still reflect the full set.
func applyWindow(list []map[string]any, offset, limit int) []map[string]any {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(list) {
		return nil
	}
	end := len(list)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return list[offset:end]
}

// --- finding list ---

// orDefault returns s if non-empty, else fallback.
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// copyFlagsFrom copies every non-inherited flag from src to dst.
// Used so BC aliases (konvu finding list) expose the same flag surface as
// their canonical form (konvu finding sca list) without duplicated code.
//
// AddFlag reuses the SAME *pflag.Flag pointer that src holds, so writes
// through the alias command flow to the canonical RunE without divergence.
// If a flag with the same name is already on dst, we leave the existing
// one alone.
func copyFlagsFrom(dst, src *cobra.Command) {
	src.Flags().VisitAll(func(f *pflag.Flag) {
		if dst.Flags().Lookup(f.Name) != nil {
			return
		}
		dst.Flags().AddFlag(f)
	})
}

// --- BC aliases: bare `konvu finding <op>` delegates to `konvu finding sca <op>` ---
// See cmd/finding_sca.go init() for flag registration + AddCommand wiring.

var findingListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SCA findings (alias for `finding sca list`)",
	Long:  "Backward-compatible alias for `konvu finding sca list`. See that command for full documentation.",
	RunE:  func(cmd *cobra.Command, args []string) error { return scaListCmd.RunE(cmd, args) },
}

var findingGetCmd = &cobra.Command{
	Use:   "get [finding-id | dependabot-alert-url]",
	Short: "Get an SCA finding by Konvu ID or Dependabot alert (alias for `finding sca get`)",
	Long:  "Backward-compatible alias for `konvu finding sca get`. See that command for full documentation.",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return scaGetCmd.RunE(cmd, args) },
}

var findingRateCmd = &cobra.Command{
	Use:   "rate [finding-id] [rating]",
	Short: "Rate an SCA finding (alias for `finding sca rate`)",
	Args:  cobra.ExactArgs(2),
	RunE:  func(cmd *cobra.Command, args []string) error { return scaRateCmd.RunE(cmd, args) },
}

var findingCountsCmd = &cobra.Command{
	Use:   "counts",
	Short: "Count SCA findings (alias for `finding sca counts`)",
	RunE:  func(cmd *cobra.Command, args []string) error { return scaCountsCmd.RunE(cmd, args) },
}

var findingSubmitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit SCA findings (alias for `finding sca submit`)",
	RunE:  func(cmd *cobra.Command, args []string) error { return scaSubmitCmd.RunE(cmd, args) },
}

func init() {
	rootCmd.AddCommand(findingCmd)
}
