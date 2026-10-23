package cmd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/findings"
	"github.com/spf13/cobra"
)

// Every PR check call hangs off this prefix.
const prCheckBase = "/services/guardrails/v1/ci"

const (
	prCheckReviewedPath = prCheckBase + "/pull-requests/reviewed"
	prCheckRunsPath     = prCheckBase + "/pull-requests/%s/%d/runs"
	prCheckMaxLimit     = 100
)

var (
	// Outcomes a completed, non-failed PR check can have.
	prCheckOutcomes = []string{"violation", "pass"}
	prCheckStates   = []string{"open", "merged", "closed"}
)

// github.com/<owner>/<repo>/pull/<number>
var prURLRe = regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+)/pull/(\d+)/?$`)

// Finding fields that describe the flaw; rule and attribution internals are left out.
var prCheckFindingKeys = []string{
	"finding_id", "title", "summary", "change", "change_reason",
	"attack_path", "protections_checked", "fix", "evidence",
}

var prCheckCmd = &cobra.Command{
	Use:   "pr-check",
	Short: "Security flaws Konvu found on pull requests (read-only)",
	Long: `Review what Konvu's PR check found, in monitor or warn mode.

Each PR is judged on its latest completed run; failed runs are skipped.
Pair with 'gh pr diff <url>' to judge each finding against the code.`,
}

var prCheckCountsCmd = &cobra.Command{
	Use:   "counts",
	Short: "How many PRs were analyzed, by outcome and CI mode",
	Example: `  konvu finding pr-check counts
  konvu finding pr-check counts --repo acme/app --since 30d`,
	RunE: runPRCheckCounts,
}

var prCheckListCmd = &cobra.Command{
	Use:   "list",
	Short: "List analyzed PRs with their outcome and flaws",
	Example: `  konvu finding pr-check list --outcome violation
  konvu finding pr-check list --outcome pass --state merged --limit 30
  konvu finding pr-check list --offset 50 -q`,
	RunE: runPRCheckList,
}

var prCheckGetCmd = &cobra.Command{
	Use:     "get <pr-url>",
	Short:   "The security flaws found on one PR, with attack path and evidence",
	Example: `  konvu finding pr-check get https://github.com/acme/app/pull/42`,
	Args:    cobra.ExactArgs(1),
	RunE:    runPRCheckGet,
}

func prCheckRequestError(err error) error {
	if _, ok := err.(*api.AuthenticationError); ok {
		return clierrors.NewAuthError(err.Error())
	}
	return &clierrors.CLIError{
		Code:       "PR_CHECK_REQUEST_FAILED",
		Message:    fmt.Sprintf("could not read PR checks: %v", err),
		Suggestion: "Check your company access and that Guardrails CI is enabled on a repository.",
		Retryable:  true,
		ExitCode:   clierrors.ExitGeneralError,
	}
}

func prCheckUsageError(message, suggestion string) error {
	return &clierrors.CLIError{
		Code:       "INVALID_ARGUMENTS",
		Message:    message,
		Suggestion: suggestion,
		ExitCode:   clierrors.ExitUsageError,
	}
}

// prCheckFilters reads the filters shared by counts and list.
func prCheckFilters(cmd *cobra.Command) (map[string]any, error) {
	params := map[string]any{}
	if repo, _ := cmd.Flags().GetString("repo"); repo != "" {
		params["repository"] = repo
	}
	if since, _ := cmd.Flags().GetString("since"); since != "" {
		t, err := parseClientDate(since)
		if err != nil {
			return nil, prCheckUsageError(err.Error(), "Example: --since 30d")
		}
		params["since"] = t.Format("2006-01-02T15:04:05Z")
	}
	return params, nil
}

// validValues rejects the first value not in allowed, naming the flag.
func validValues(flag string, values, allowed []string) error {
	for _, v := range values {
		if !contains(allowed, v) {
			return invalidValue(flag, v, allowed)
		}
	}
	return nil
}

func runPRCheckCounts(cmd *cobra.Command, args []string) error {
	params, err := prCheckFilters(cmd)
	if err != nil {
		return err
	}
	params["limit"] = 1

	client := api.NewClient("", "")
	defer client.Close()
	resp, err := client.Get(prCheckReviewedPath, params)
	if err != nil {
		return prCheckRequestError(err)
	}

	counts := getMap(resp, "counts")
	outcomes := getMap(counts, "outcomes")
	modes := getMap(counts, "modes")
	row := findings.Row{
		"pull_requests": counts["pull_requests"],
		"pass":          outcomes["pass"],
		"violation":     outcomes["violation"],
		"findings":      counts["findings"],
		"monitor":       modes["monitor"],
		"warn":          modes["warn"],
	}
	return findings.Render(cmd, []findings.Row{row}, []string{"pull_requests", "pass", "violation", "findings", "monitor", "warn"})
}

func transformReviewedPR(raw map[string]any) findings.Row {
	titles := []string{}
	for _, t := range getSlice(raw, "findings") {
		if s, ok := t.(string); ok && s != "" {
			titles = append(titles, s)
		}
	}
	return findings.Row{
		"url":           getStr(raw, "url"),
		"repository":    getStr(raw, "repository"),
		"number":        raw["number"],
		"title":         getStr(raw, "title"),
		"author":        getStr(raw, "author"),
		"state":         getStr(raw, "state"),
		"mode":          getStr(raw, "ci_mode"),
		"outcome":       getStr(raw, "outcome"),
		"finding_count": raw["finding_count"],
		"findings":      strings.Join(titles, "; "),
		"run_id":        getStr(raw, "run_id"),
		"head_sha":      getStr(raw, "head_sha"),
		"reviewed_at":   getStr(raw, "reviewed_at"),
	}
}

var prCheckTableColumns = []string{"url", "state", "outcome", "finding_count", "findings"}
var prCheckCSVColumns = []string{"url", "repository", "number", "title", "author", "state", "mode", "outcome", "finding_count", "findings", "run_id", "reviewed_at"}

func runPRCheckList(cmd *cobra.Command, args []string) error {
	params, err := prCheckFilters(cmd)
	if err != nil {
		return err
	}
	outcomes, _ := cmd.Flags().GetStringSlice("outcome")
	if err := validValues("outcome", outcomes, prCheckOutcomes); err != nil {
		return err
	}
	states, _ := cmd.Flags().GetStringSlice("state")
	if err := validValues("state", states, prCheckStates); err != nil {
		return err
	}
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 || limit > prCheckMaxLimit {
		return prCheckUsageError(fmt.Sprintf("--limit must be between 1 and %d", prCheckMaxLimit), "Use --offset to read further.")
	}
	offset, _ := cmd.Flags().GetInt("offset")
	if offset < 0 {
		return prCheckUsageError("--offset cannot be negative", "Pass 0 or more.")
	}
	params["outcome"] = outcomes
	params["state"] = states
	params["limit"] = limit
	params["offset"] = offset

	client := api.NewClient("", "")
	defer client.Close()
	resp, err := client.Get(prCheckReviewedPath, params)
	if err != nil {
		return prCheckRequestError(err)
	}

	rows := []findings.Row{}
	for _, it := range getSlice(resp, "items") {
		if m, ok := it.(map[string]any); ok {
			rows = append(rows, transformReviewedPR(m))
		}
	}
	if quiet, _ := cmd.Flags().GetBool("quiet"); quiet {
		return findings.RenderBareIDs(cmd, rows, "url")
	}
	return findings.RenderColumns(cmd, rows, prCheckTableColumns, prCheckCSVColumns)
}

func runPRCheckGet(cmd *cobra.Command, args []string) error {
	if err := findings.RequireJSON(cmd, "pr-check get"); err != nil {
		return err
	}
	m := prURLRe.FindStringSubmatch(strings.TrimSpace(args[0]))
	if m == nil {
		// shortcut: GitHub PR URLs only; add GitLab merge-request URLs when a GitLab tenant enables PR checks.
		return prCheckUsageError(
			fmt.Sprintf("not a GitHub pull request URL: %q", args[0]),
			"Example: konvu finding pr-check get https://github.com/acme/app/pull/42",
		)
	}
	number, _ := strconv.Atoi(m[2])

	client := api.NewClient("", "")
	defer client.Close()

	// The reviewed list resolves the repository id and the run the PR is judged on.
	resp, err := client.Get(prCheckReviewedPath, map[string]any{"repository": m[1], "number": number, "limit": 1})
	if err != nil {
		return prCheckRequestError(err)
	}
	items := getSlice(resp, "items")
	if len(items) == 0 {
		return &clierrors.CLIError{
			Code:       "PR_CHECK_NOT_FOUND",
			Message:    fmt.Sprintf("no completed PR check for %s", args[0]),
			Suggestion: "List analyzed PRs with 'konvu finding pr-check list'.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	pr, _ := items[0].(map[string]any)

	runs, err := client.GetList(fmt.Sprintf(prCheckRunsPath, getStr(pr, "repository_id"), number), nil)
	if err != nil {
		return prCheckRequestError(err)
	}
	return findings.Render(cmd, []findings.Row{prCheckReview(pr, runs)}, nil)
}

// prCheckReview shapes one PR's review: its judged run's flaws plus every
// completed, non-failed run so the reader sees findings come and go per push.
func prCheckReview(pr map[string]any, runs []any) findings.Row {
	judged := getStr(pr, "run_id")
	flaws := []any{}
	history := []any{}
	baseSHA := ""
	for _, r := range runs {
		run, ok := r.(map[string]any)
		if !ok || !contains(prCheckOutcomes, getStr(run, "outcome")) {
			continue
		}
		history = append(history, map[string]any{
			"run_id":        getStr(run, "id"),
			"head_sha":      getStr(run, "head_sha"),
			"created_at":    getStr(run, "created_at"),
			"outcome":       getStr(run, "outcome"),
			"finding_count": run["finding_count"],
		})
		if getStr(run, "id") != judged {
			continue
		}
		baseSHA = getStr(run, "target_base_sha")
		for _, f := range getSlice(run, "findings") {
			finding, ok := f.(map[string]any)
			if !ok {
				continue
			}
			flaw := map[string]any{}
			for _, key := range prCheckFindingKeys {
				flaw[key] = finding[key]
			}
			flaws = append(flaws, flaw)
		}
	}
	return findings.Row{
		"pr": map[string]any{
			"url":        getStr(pr, "url"),
			"repository": getStr(pr, "repository"),
			"number":     pr["number"],
			"title":      getStr(pr, "title"),
			"author":     getStr(pr, "author"),
			"state":      getStr(pr, "state"),
			"head_sha":   getStr(pr, "head_sha"),
			"base_sha":   baseSHA,
		},
		"mode":     getStr(pr, "ci_mode"),
		"outcome":  getStr(pr, "outcome"),
		"run_id":   judged,
		"findings": flaws,
		"runs":     history,
	}
}

func addPRCheckFilterFlags(c *cobra.Command) {
	c.Flags().StringP("repo", "r", "", "Filter by repository full name (owner/repo)")
	c.Flags().String("since", "", "Runs since: '7d', '30d', or ISO date")
	c.Flags().StringP("output", "o", "", "Output format: json, table, csv")
}

func addPRCheckListFlags(c *cobra.Command) {
	addPRCheckFilterFlags(c)
	c.Flags().StringSlice("outcome", nil, "Filter by outcome (repeatable): "+strings.Join(prCheckOutcomes, ", "))
	c.Flags().StringSlice("state", nil, "Filter by PR state (repeatable): "+strings.Join(prCheckStates, ", "))
	c.Flags().IntP("limit", "n", 50, fmt.Sprintf("Maximum PRs to return (max %d)", prCheckMaxLimit))
	c.Flags().Int("offset", 0, "Skip N results")
	c.Flags().BoolP("quiet", "q", false, "Print bare PR URLs, one per line")
}

func addPRCheckGetFlags(c *cobra.Command) {
	c.Flags().StringP("output", "o", "", "Output format: json (default)")
}

func init() {
	addPRCheckFilterFlags(prCheckCountsCmd)
	addPRCheckListFlags(prCheckListCmd)
	addPRCheckGetFlags(prCheckGetCmd)

	prCheckCmd.AddCommand(prCheckCountsCmd, prCheckListCmd, prCheckGetCmd)
	findingCmd.AddCommand(prCheckCmd)
}
