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

const coverageConfigPath = "/repositories/assessment_config"
const coverageDefaultPath = "/repositories/assessment_config/default_severities"

// Canonical wire values; MODERATE is shown as "Medium" in the dashboard.
var validSeverities = map[string]bool{
	"CRITICAL": true,
	"HIGH":     true,
	"MODERATE": true,
	"LOW":      true,
}

var coverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "Configure where AI Assessment runs (repositories and severities)",
}

var coverageListCmd = &cobra.Command{
	Use:   "list",
	Short: "List repositories and their assessment coverage",
	RunE:  runCoverageList,
}

var coverageEnableCmd = &cobra.Command{
	Use:   "enable <repo>...",
	Short: "Enable AI Assessment on one or more repositories",
	Long: `Enable AI Assessment on repositories (identified by URL or id).

Without --severities the repository starts on the company default severities.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu coverage enable github:org/repo
  konvu coverage enable org/repo --severities CRITICAL,HIGH`,
	RunE: runCoverageEnable,
}

var coverageDisableCmd = &cobra.Command{
	Use:     "disable <repo>...",
	Short:   "Disable AI Assessment on one or more repositories",
	Example: `  konvu coverage disable github:org/repo`,
	RunE:    runCoverageDisable,
}

var coverageSeveritiesCmd = &cobra.Command{
	Use:   "severities <repo>...",
	Short: "Set the severities assessed for one or more repositories",
	Example: `  konvu coverage severities org/repo --set CRITICAL,HIGH
  konvu coverage severities org/repo --all`,
	RunE: runCoverageSeverities,
}

var coverageDefaultCmd = &cobra.Command{
	Use:   "default",
	Short: "Show or set the company-wide default severities new repositories inherit",
	Example: `  konvu coverage default
  konvu coverage default --set CRITICAL,HIGH
  konvu coverage default --all`,
	RunE: runCoverageDefault,
}

func runCoverageList(cmd *cobra.Command, args []string) error {
	repoFilter, _ := cmd.Flags().GetString("repo")
	outputFlag, _ := cmd.Flags().GetString("output")
	quiet, _ := cmd.Flags().GetBool("quiet")
	format := output.DetectOutputFormat(outputFlag)

	client := api.NewClient("", "")
	defer client.Close()

	data, err := client.Get(coverageConfigPath, nil)
	if err != nil {
		handleCoverageError(err, format)
	}
	repos := getSlice(data, "repositories")
	defaultSevs := data["default_severities"]

	if repoFilter != "" {
		filtered := repos[:0:0]
		needle := strings.ToLower(repoFilter)
		for _, r := range repos {
			m, ok := r.(map[string]any)
			if ok && strings.Contains(strings.ToLower(getStr(m, "url")), needle) {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
	}

	if quiet {
		items := make([]map[string]any, 0, len(repos))
		for _, r := range repos {
			if m, ok := r.(map[string]any); ok {
				items = append(items, m)
			}
		}
		fmt.Println(output.FormatQuiet(items, "id"))
		return nil
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(map[string]any{
			"repositories":       repos,
			"default_severities": defaultSevs,
		}))
		return nil
	}

	rows := make([]any, 0, len(repos))
	for _, r := range repos {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		enabled, _ := getBool(m, "assessment_enabled")
		rows = append(rows, map[string]any{
			"repository": getStr(m, "url"),
			"assessment": yesNo(enabled),
			"severities": severitiesDisplay(m["assessment_severities"]),
		})
	}
	columns := []string{"repository", "assessment", "severities"}
	if format == output.CSV {
		fmt.Print(output.FormatCSV(map[string]any{"repositories": rows}, columns, "repositories"))
		return nil
	}
	fmt.Println(output.FormatTable(map[string]any{"repositories": rows}, columns, "repositories", nil))
	fmt.Printf("Default severities: %s\n", severitiesDisplay(defaultSevs))
	return nil
}

// normalizeDefaultSeverities coerces an empty severity array to nil so callers
// never forward the forbidden empty list (the API 422s on []); nil/null and a
// non-empty list pass through unchanged.
func normalizeDefaultSeverities(v any) any {
	switch t := v.(type) {
	case []any:
		if len(t) == 0 {
			return nil
		}
	case []string:
		if len(t) == 0 {
			return nil
		}
	}
	return v
}

func runCoverageEnable(cmd *cobra.Command, args []string) error {
	severities, _ := cmd.Flags().GetStringSlice("severities")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	format := output.DetectOutputFormat(mustOutputFlag(cmd))

	if len(args) == 0 {
		handleCoverageError(usageError("Specify at least one repository (URL or id)."), format)
	}

	client := api.NewClient("", "")
	defer client.Close()

	repos, defaultSevs := fetchCoverage(client, format)
	ids := resolveRepoIDsOrExit(repos, args, format)

	// Without explicit --severities, start the repo on the company default
	// (mirrors the dashboard). The default may be null (= all) or, if an admin
	// stored one, an empty array — coerce that to null so we never PATCH the
	// forbidden empty list (server 422).
	var sevVal any = normalizeDefaultSeverities(defaultSevs)
	if len(severities) > 0 {
		sevVal = normalizeSeveritiesOrExit(severities, format)
	}

	items := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		items = append(items, map[string]any{
			"repository_id":         id,
			"assessment_enabled":    true,
			"assessment_severities": sevVal,
		})
	}
	applyCoverage(client, items, dryRun, format, "enable")
	return nil
}

func runCoverageDisable(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	format := output.DetectOutputFormat(mustOutputFlag(cmd))

	if len(args) == 0 {
		handleCoverageError(usageError("Specify at least one repository (URL or id)."), format)
	}

	client := api.NewClient("", "")
	defer client.Close()

	repos, _ := fetchCoverage(client, format)
	ids := resolveRepoIDsOrExit(repos, args, format)

	items := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		items = append(items, map[string]any{
			"repository_id":      id,
			"assessment_enabled": false,
		})
	}
	applyCoverage(client, items, dryRun, format, "disable")
	return nil
}

func runCoverageSeverities(cmd *cobra.Command, args []string) error {
	set, _ := cmd.Flags().GetStringSlice("set")
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	format := output.DetectOutputFormat(mustOutputFlag(cmd))

	if len(args) == 0 {
		handleCoverageError(usageError("Specify at least one repository (URL or id)."), format)
	}
	sevVal := resolveSeverityValueOrExit(set, all, format)

	client := api.NewClient("", "")
	defer client.Close()

	repos, _ := fetchCoverage(client, format)
	ids := resolveRepoIDsOrExit(repos, args, format)

	items := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		items = append(items, map[string]any{
			"repository_id":         id,
			"assessment_severities": sevVal,
		})
	}
	applyCoverage(client, items, dryRun, format, "update")
	return nil
}

func runCoverageDefault(cmd *cobra.Command, args []string) error {
	set, _ := cmd.Flags().GetStringSlice("set")
	all, _ := cmd.Flags().GetBool("all")
	format := output.DetectOutputFormat(mustOutputFlag(cmd))

	client := api.NewClient("", "")
	defer client.Close()

	// No flags: read and show the current default.
	if len(set) == 0 && !all {
		data, err := client.Get(coverageConfigPath, nil)
		if err != nil {
			handleCoverageError(err, format)
		}
		defaultSevs := data["default_severities"]
		if format == output.JSON {
			fmt.Println(output.FormatJSON(map[string]any{"default_severities": defaultSevs}))
		} else {
			fmt.Printf("Default severities: %s\n", severitiesDisplay(defaultSevs))
		}
		return nil
	}

	sevVal := resolveSeverityValueOrExit(set, all, format)
	if _, err := client.Put(coverageDefaultPath, map[string]any{"assessment_severities": sevVal}); err != nil {
		handleCoverageError(err, format)
	}
	if format == output.JSON {
		fmt.Println(output.FormatJSON(map[string]any{"default_severities": sevVal}))
	} else {
		fmt.Printf("Default severities set to: %s\n", severitiesDisplay(sevVal))
	}
	return nil
}

// fetchCoverage GETs the config and returns (repositories, default_severities).
func fetchCoverage(client *api.Client, format output.OutputFormat) ([]any, any) {
	data, err := client.Get(coverageConfigPath, nil)
	if err != nil {
		handleCoverageError(err, format)
	}
	return getSlice(data, "repositories"), data["default_severities"]
}

func resolveRepoIDsOrExit(repos []any, args []string, format output.OutputFormat) []string {
	ids, err := resolveRepoIDs(repos, args)
	if err != nil {
		handleCoverageError(err, format)
	}
	return ids
}

// resolveRepoIDs maps each arg (an id, a url, or a unique url substring) to its
// repository_id, returning a friendly error on miss/ambiguity.
func resolveRepoIDs(repos []any, args []string) ([]string, *clierrors.CLIError) {
	byID := map[string]bool{}
	byURL := map[string]string{}
	type entry struct{ id, url string }
	var all []entry
	for _, r := range repos {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		id, url := getStr(m, "id"), getStr(m, "url")
		byID[id] = true
		byURL[url] = id
		all = append(all, entry{id, url})
	}

	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	for _, arg := range args {
		if byID[arg] {
			add(arg)
			continue
		}
		if id, ok := byURL[arg]; ok {
			add(id)
			continue
		}
		needle := strings.ToLower(arg)
		var matches []entry
		for _, e := range all {
			if strings.Contains(strings.ToLower(e.url), needle) {
				matches = append(matches, e)
			}
		}
		switch len(matches) {
		case 1:
			add(matches[0].id)
		case 0:
			return nil, &clierrors.CLIError{
				Code:       "REPOSITORY_NOT_FOUND",
				Message:    fmt.Sprintf("No repository matches %q.", arg),
				Suggestion: "Run 'konvu coverage list' to see repository URLs and ids.",
				ExitCode:   clierrors.ExitNotFound,
			}
		default:
			urls := make([]string, len(matches))
			for i, e := range matches {
				urls[i] = e.url
			}
			return nil, &clierrors.CLIError{
				Code:       "AMBIGUOUS_REPOSITORY",
				Message:    fmt.Sprintf("%q matches multiple repositories: %s", arg, strings.Join(urls, ", ")),
				Suggestion: "Use the full repository URL or id.",
				ExitCode:   clierrors.ExitUsageError,
			}
		}
	}
	return ids, nil
}

func resolveSeverityValueOrExit(set []string, all bool, format output.OutputFormat) any {
	v, err := resolveSeverityValue(set, all)
	if err != nil {
		handleCoverageError(err, format)
	}
	return v
}

// resolveSeverityValue turns --set/--all into the value to send: nil (JSON
// null = all severities) for --all, or the normalized list. On a nil error,
// a nil value means "all".
func resolveSeverityValue(set []string, all bool) (any, *clierrors.CLIError) {
	if all && len(set) > 0 {
		return nil, usageError("Pass either --all or --set, not both.")
	}
	if all {
		return nil, nil
	}
	if len(set) == 0 {
		return nil, usageError("Pass --set CRITICAL,HIGH or --all (every severity).")
	}
	norm, err := normalizeSeverities(set)
	if err != nil {
		return nil, err
	}
	return norm, nil
}

func normalizeSeveritiesOrExit(in []string, format output.OutputFormat) []string {
	out, err := normalizeSeverities(in)
	if err != nil {
		handleCoverageError(err, format)
	}
	return out
}

// normalizeSeverities upper-cases, maps MEDIUM->MODERATE, validates and
// de-duplicates; never returns an empty slice (empty would 422 server-side).
func normalizeSeverities(in []string) ([]string, *clierrors.CLIError) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		u := strings.ToUpper(strings.TrimSpace(s))
		if u == "MEDIUM" {
			u = "MODERATE"
		}
		if !validSeverities[u] {
			return nil, usageError(fmt.Sprintf("Invalid severity %q. Use CRITICAL, HIGH, MEDIUM, or LOW.", s))
		}
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		return nil, usageError("No severities given; use --all to assess every severity.")
	}
	return out, nil
}

// applyCoverage sends the PATCH (or previews it under --dry-run).
func applyCoverage(client *api.Client, items []map[string]any, dryRun bool, format output.OutputFormat, action string) {
	noun := "repository"
	if len(items) != 1 {
		noun = "repositories"
	}
	if dryRun {
		if format == output.JSON {
			fmt.Println(output.FormatJSON(map[string]any{
				"action": action, "dry_run": true, "total": len(items), "items": items,
			}))
		} else {
			fmt.Printf("Dry run: would %s %d %s.\n", action, len(items), noun)
		}
		return
	}
	if _, err := client.Patch(coverageConfigPath, items); err != nil {
		handleCoverageError(err, format)
	}
	if format == output.JSON {
		fmt.Println(output.FormatJSON(map[string]any{"action": action, "dry_run": false, "updated": len(items)}))
	} else {
		fmt.Printf("Updated %d %s.\n", len(items), noun)
	}
}

func severitiesDisplay(v any) string {
	var raw []any
	switch t := v.(type) {
	case nil:
		return "all"
	case []any:
		raw = t
	case []string:
		for _, s := range t {
			raw = append(raw, s)
		}
	default:
		return "all"
	}
	if len(raw) == 0 {
		return "all"
	}
	parts := make([]string, 0, len(raw))
	for _, s := range raw {
		str, _ := s.(string)
		parts = append(parts, severityTitle(str))
	}
	return strings.Join(parts, ", ")
}

func severityTitle(s string) string {
	switch strings.ToUpper(s) {
	case "CRITICAL":
		return "Critical"
	case "HIGH":
		return "High"
	case "MODERATE":
		return "Medium"
	case "LOW":
		return "Low"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func usageError(msg string) *clierrors.CLIError {
	return &clierrors.CLIError{
		Code:     "INVALID_ARGUMENTS",
		Message:  msg,
		ExitCode: clierrors.ExitUsageError,
	}
}

func mustOutputFlag(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString("output")
	return v
}

func handleCoverageError(err error, format output.OutputFormat) {
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
				Code:       "INVALID_SEVERITIES",
				Message:    e.Error(),
				Suggestion: "Pass --all to assess every severity, or list them like --set CRITICAL,HIGH.",
				ExitCode:   clierrors.ExitUsageError,
			}
		case 404:
			cliErr = &clierrors.CLIError{
				Code:       "NOT_FOUND",
				Message:    e.Error(),
				Suggestion: "Run 'konvu coverage list' to see repositories.",
				ExitCode:   clierrors.ExitNotFound,
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
	coverageListCmd.Flags().StringP("repo", "r", "", "Filter by repository URL/name (substring)")
	coverageListCmd.Flags().StringP("output", "o", "", "Output format: json, table, csv")
	coverageListCmd.Flags().BoolP("quiet", "q", false, "Print only repository ids")

	coverageEnableCmd.Flags().StringSlice("severities", nil, "Severities to assess (default: company default)")
	coverageEnableCmd.Flags().Bool("dry-run", false, "Preview without applying")
	coverageEnableCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	coverageDisableCmd.Flags().Bool("dry-run", false, "Preview without applying")
	coverageDisableCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	coverageSeveritiesCmd.Flags().StringSlice("set", nil, "Severities to assess (e.g. CRITICAL,HIGH)")
	coverageSeveritiesCmd.Flags().Bool("all", false, "Assess every severity")
	coverageSeveritiesCmd.Flags().Bool("dry-run", false, "Preview without applying")
	coverageSeveritiesCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	coverageDefaultCmd.Flags().StringSlice("set", nil, "Severities to assess (e.g. CRITICAL,HIGH)")
	coverageDefaultCmd.Flags().Bool("all", false, "Assess every severity")
	coverageDefaultCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	coverageCmd.AddCommand(coverageListCmd, coverageEnableCmd, coverageDisableCmd, coverageSeveritiesCmd, coverageDefaultCmd)

	// Bare `konvu coverage` lists, matching the dismiss alias pattern.
	coverageCmd.RunE = runCoverageList
	coverageCmd.Flags().StringP("repo", "r", "", "Filter by repository URL/name (substring)")
	coverageCmd.Flags().StringP("output", "o", "", "Output format: json, table, csv")
	coverageCmd.Flags().BoolP("quiet", "q", false, "Print only repository ids")

	rootCmd.AddCommand(coverageCmd)
}
