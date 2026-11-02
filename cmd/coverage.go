package cmd

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const coverageConfigPath = "/repositories/assessment_config"
const coverageScopesPath = "/coverage/scopes"

// Canonical wire values; MODERATE is shown as "Medium" in the dashboard.
var validSeverities = map[string]bool{
	"CRITICAL": true,
	"HIGH":     true,
	"MODERATE": true,
	"LOW":      true,
}

// coverageTask is a task column of the dashboard's Coverage table: its flag,
// the scope step it is saved as, and whether it runs on picked severities.
type coverageTask struct {
	flag, step string
	severities bool
}

var coverageTasks = []coverageTask{
	{"analysis", "assessment", true},
	{"reproduction", "reproduction", true},
	{"remediation", "remediation", false},
}

// Finding types a scope covers.
var coverageProducts = []string{"sca", "sast"}

var coverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "Configure which repositories Konvu covers, and with which tasks",
	Long: `A scope runs tasks on a set of repositories for one finding type (sca or
sast), like a row of the dashboard's Coverage page:

  analysis      Exploitability analysis, on the severities picked
  reproduction  Reproduction, on the severities picked
  remediation   Remediation, on every severity

Scopes are named; pass --product when an SCA and a SAST scope share a name.
Repositories are identified by URL, id, or a unique URL substring.`,
}

var coverageListCmd = &cobra.Command{
	Use:   "list",
	Short: "List coverage scopes",
	RunE:  runCoverageList,
}

var coverageCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a scope",
	Long: `Create a scope running the tasks passed on the repositories passed.

Severities are CRITICAL, HIGH, MEDIUM, LOW, or all.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu coverage create "Production" --product sca --repo org/api,org/web --analysis CRITICAL,HIGH --remediation
  konvu coverage create "Everything" --product sast --repo org/api --analysis all`,
	Args: cobra.ExactArgs(1),
	RunE: runCoverageCreate,
}

var coverageAddCmd = &cobra.Command{
	Use:     "add <scope> <repo>...",
	Short:   "Add repositories to a scope",
	Example: `  konvu coverage add "Production" github:org/repo`,
	Args:    cobra.MinimumNArgs(2),
	RunE:    runCoverageMembers,
}

var coverageRemoveCmd = &cobra.Command{
	Use:     "remove <scope> <repo>...",
	Short:   "Remove repositories from a scope",
	Example: `  konvu coverage remove "Production" github:org/repo`,
	Args:    cobra.MinimumNArgs(2),
	RunE:    runCoverageMembers,
}

var coverageSeveritiesCmd = &cobra.Command{
	Use:   "severities <scope>",
	Short: "Change the severities a scope's tasks run on",
	Example: `  konvu coverage severities "Production" --analysis CRITICAL,HIGH
  konvu coverage severities "Production" --product sast --reproduction all`,
	Args: cobra.ExactArgs(1),
	RunE: runCoverageSeverities,
}

var coverageDeleteCmd = &cobra.Command{
	Use:     "delete <scope>",
	Short:   "Delete a scope; its tasks stop on its repositories",
	Example: `  konvu coverage delete "Production" --product sca`,
	Args:    cobra.ExactArgs(1),
	RunE:    runCoverageDelete,
}

// scopeRow is a row of the dashboard's Coverage table: one product's
// same-name scopes, one per task step.
type scopeRow struct {
	name, product string
	byStep        map[string]map[string]any
}

// scopeWrite is one API call of a change; a row spans several scopes.
type scopeWrite struct {
	Method string         `json:"method"`
	Path   string         `json:"path"`
	Body   map[string]any `json:"body,omitempty"`
}

func runCoverageList(cmd *cobra.Command, args []string) error {
	repoFilter, _ := cmd.Flags().GetString("repo")
	quiet, _ := cmd.Flags().GetBool("quiet")
	format := output.DetectOutputFormat(mustOutputFlag(cmd))

	client := api.NewClient("", "")
	defer client.Close()

	rows := fetchScopeRows(client, format)
	repos, _ := fetchCoverage(client, format)

	// Scopes can keep repositories the company no longer lists; like the
	// dashboard, show only the listed ones.
	known := map[string]bool{}
	for _, r := range repos {
		if m, ok := r.(map[string]any); ok {
			known[getStr(m, "id")] = true
		}
	}

	// Keep the scopes covering one repository.
	if repoFilter != "" {
		id := resolveRepoIDsOrExit(repos, []string{repoFilter}, format)[0]
		rows = rowsWithRepo(rows, id)
	}

	if quiet {
		for _, row := range rows {
			fmt.Println(row.name)
		}
		return nil
	}

	if format == output.JSON {
		items := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			items = append(items, row.json(known))
		}
		fmt.Println(output.FormatJSON(map[string]any{"scopes": items}))
		return nil
	}

	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.cells(known))
	}
	columns := []string{"scope", "product", "analysis", "reproduction", "remediation", "repositories"}
	if format == output.CSV {
		fmt.Print(output.FormatCSV(map[string]any{"scopes": items}, columns, "scopes"))
		return nil
	}
	fmt.Println(output.FormatTable(map[string]any{"scopes": items}, columns, "scopes", nil))
	return nil
}

func runCoverageCreate(cmd *cobra.Command, args []string) error {
	product, _ := cmd.Flags().GetString("product")
	repoArgs, _ := cmd.Flags().GetStringSlice("repo")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	format := output.DetectOutputFormat(mustOutputFlag(cmd))

	if !contains(coverageProducts, product) {
		handleCoverageError(usageError("Pass --product sca or --product sast."), format)
	}
	if len(repoArgs) == 0 {
		handleCoverageError(usageError("Pass the repositories with --repo."), format)
	}

	client := api.NewClient("", "")
	defer client.Close()

	repos, _ := fetchCoverage(client, format)
	ids := resolveRepoIDsOrExit(repos, repoArgs, format)

	// One scope per task, all under the same name, as the dashboard saves them.
	var writes []scopeWrite
	for _, task := range coverageTasks {
		on, severities := taskFlag(cmd, task, format)
		if !on {
			continue
		}

		body := map[string]any{"step": task.step, "product": product, "name": args[0], "repository_ids": ids}
		if task.severities {
			body["severities"] = severities
		}
		writes = append(writes, scopeWrite{http.MethodPost, coverageScopesPath, body})
	}
	if len(writes) == 0 {
		handleCoverageError(usageError("Pass at least one task: --analysis, --reproduction or --remediation."), format)
	}

	applyScopeWrites(client, writes, dryRun, format, "create")
	return nil
}

// runCoverageMembers adds or removes repositories on every task of a scope.
func runCoverageMembers(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	format := output.DetectOutputFormat(mustOutputFlag(cmd))

	client := api.NewClient("", "")
	defer client.Close()

	row := findScopeRowOrExit(cmd, client, args[0], format)
	repos, _ := fetchCoverage(client, format)
	ids := resolveRepoIDsOrExit(repos, args[1:], format)

	key := "add_repository_ids"
	if cmd.Name() == "remove" {
		key = "remove_repository_ids"
	}

	var writes []scopeWrite
	for _, scope := range row.scopes() {
		writes = append(writes, scopeWrite{http.MethodPatch, scopePath(scope), map[string]any{key: ids}})
	}
	applyScopeWrites(client, writes, dryRun, format, cmd.Name())
	return nil
}

func runCoverageSeverities(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	format := output.DetectOutputFormat(mustOutputFlag(cmd))

	client := api.NewClient("", "")
	defer client.Close()

	row := findScopeRowOrExit(cmd, client, args[0], format)

	var writes []scopeWrite
	for _, task := range coverageTasks {
		if !task.severities {
			continue
		}

		on, severities := taskFlag(cmd, task, format)
		if !on {
			continue
		}

		scope := row.byStep[task.step]
		if scope == nil {
			handleCoverageError(usageError(fmt.Sprintf("Scope %q does not run %s.", row.name, task.flag)), format)
		}
		writes = append(writes, scopeWrite{http.MethodPatch, scopePath(scope), map[string]any{"severities": severities}})
	}
	if len(writes) == 0 {
		handleCoverageError(usageError("Pass --analysis or --reproduction."), format)
	}

	applyScopeWrites(client, writes, dryRun, format, "update")
	return nil
}

func runCoverageDelete(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	format := output.DetectOutputFormat(mustOutputFlag(cmd))

	client := api.NewClient("", "")
	defer client.Close()

	row := findScopeRowOrExit(cmd, client, args[0], format)

	var writes []scopeWrite
	for _, scope := range row.scopes() {
		writes = append(writes, scopeWrite{Method: http.MethodDelete, Path: scopePath(scope)})
	}
	applyScopeWrites(client, writes, dryRun, format, "delete")
	return nil
}

// taskFlag reads a task's flag: whether the task is on, and the severities
// to send (nil = all).
func taskFlag(cmd *cobra.Command, task coverageTask, format output.OutputFormat) (bool, any) {
	if !task.severities {
		on, _ := cmd.Flags().GetBool(task.flag)
		return on, nil
	}
	if !cmd.Flags().Changed(task.flag) {
		return false, nil
	}

	in, _ := cmd.Flags().GetStringSlice(task.flag)
	severities, err := parseSeverities(in)
	if err != nil {
		handleCoverageError(err, format)
	}
	return true, severities
}

// parseSeverities turns a severity flag into the value to send: nil (JSON
// null = every severity) for "all", else the normalized list.
func parseSeverities(in []string) (any, *clierrors.CLIError) {
	if len(in) == 1 && strings.EqualFold(strings.TrimSpace(in[0]), "all") {
		return nil, nil
	}

	norm, err := normalizeSeverities(in)
	if err != nil {
		return nil, err
	}
	return norm, nil
}

func fetchScopeRows(client *api.Client, format output.OutputFormat) []scopeRow {
	data, err := client.Get(coverageScopesPath, nil)
	if err != nil {
		handleCoverageError(err, format)
	}
	return groupScopes(getSlice(data, "scopes"))
}

// groupScopes folds scopes into dashboard rows, in the order they come.
func groupScopes(scopes []any) []scopeRow {
	var rows []scopeRow
	index := map[[2]string]int{}
	for _, s := range scopes {
		m, ok := s.(map[string]any)
		if !ok || !contains(coverageProducts, getStr(m, "product")) {
			continue
		}

		key := [2]string{getStr(m, "product"), getStr(m, "name")}
		i, seen := index[key]
		if !seen {
			i = len(rows)
			index[key] = i
			rows = append(rows, scopeRow{name: key[1], product: key[0], byStep: map[string]map[string]any{}})
		}
		rows[i].byStep[getStr(m, "step")] = m
	}
	return rows
}

func rowsWithRepo(rows []scopeRow, id string) []scopeRow {
	var kept []scopeRow
	for _, row := range rows {
		if contains(row.repositoryIDs(), id) {
			kept = append(kept, row)
		}
	}
	return kept
}

func findScopeRowOrExit(cmd *cobra.Command, client *api.Client, name string, format output.OutputFormat) scopeRow {
	product, _ := cmd.Flags().GetString("product")
	row, err := findScopeRow(fetchScopeRows(client, format), name, product)
	if err != nil {
		handleCoverageError(err, format)
	}
	return row
}

// findScopeRow picks the row named name, narrowed to product when given.
func findScopeRow(rows []scopeRow, name, product string) (scopeRow, *clierrors.CLIError) {
	var matches []scopeRow
	for _, row := range rows {
		if !strings.EqualFold(row.name, name) {
			continue
		}
		if product != "" && row.product != product {
			continue
		}
		matches = append(matches, row)
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return scopeRow{}, &clierrors.CLIError{
			Code:       "SCOPE_NOT_FOUND",
			Message:    fmt.Sprintf("No scope named %q.", name),
			Suggestion: "Run 'konvu coverage list' to see scopes.",
			ExitCode:   clierrors.ExitNotFound,
		}
	}
	return scopeRow{}, &clierrors.CLIError{
		Code:       "AMBIGUOUS_SCOPE",
		Message:    fmt.Sprintf("An SCA and a SAST scope are both named %q.", name),
		Suggestion: "Pass --product sca or --product sast.",
		ExitCode:   clierrors.ExitUsageError,
	}
}

// scopes returns the row's scopes in task order.
func (r scopeRow) scopes() []map[string]any {
	var out []map[string]any
	for _, task := range coverageTasks {
		if scope := r.byStep[task.step]; scope != nil {
			out = append(out, scope)
		}
	}
	return out
}

// repositoryIDs returns every repository any of the row's tasks runs on.
func (r scopeRow) repositoryIDs() []string {
	var ids []string
	for _, scope := range r.scopes() {
		for _, v := range getSlice(scope, "repository_ids") {
			if id, ok := v.(string); ok && !contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// listedIDs returns the row's repositories the company still lists.
func (r scopeRow) listedIDs(known map[string]bool) []string {
	ids := []string{}
	for _, id := range r.repositoryIDs() {
		if known[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

// json shows each task on, with its severities (null = all), e.g.
// {"analysis": {"severities": ["CRITICAL"]}, "remediation": {"severities": null}}.
func (r scopeRow) json(known map[string]bool) map[string]any {
	tasks := map[string]any{}
	for _, task := range coverageTasks {
		if scope := r.byStep[task.step]; scope != nil {
			tasks[task.flag] = map[string]any{"severities": scope["severities"]}
		}
	}
	return map[string]any{"name": r.name, "product": r.product, "tasks": tasks, "repository_ids": r.listedIDs(known)}
}

// cells renders the row as the dashboard's table does.
func (r scopeRow) cells(known map[string]bool) map[string]any {
	cells := map[string]any{
		"scope":        r.name,
		"product":      strings.ToUpper(r.product),
		"repositories": len(r.listedIDs(known)),
	}
	for _, task := range coverageTasks {
		scope := r.byStep[task.step]
		switch {
		case scope == nil:
			cells[task.flag] = "off"
		case task.severities:
			cells[task.flag] = severitiesDisplay(scope["severities"])
		default:
			cells[task.flag] = "on"
		}
	}
	return cells
}

func scopePath(scope map[string]any) string {
	return coverageScopesPath + "/" + getStr(scope, "id")
}

// applyScopeWrites sends the calls in order (or previews them under --dry-run).
func applyScopeWrites(client *api.Client, writes []scopeWrite, dryRun bool, format output.OutputFormat, action string) {
	if dryRun {
		if format == output.JSON {
			fmt.Println(output.FormatJSON(map[string]any{"action": action, "dry_run": true, "requests": writes}))
		} else {
			fmt.Printf("Dry run: would %s with %d request(s).\n", action, len(writes))
		}
		return
	}

	for i, w := range writes {
		var err error
		switch w.Method {
		case http.MethodPost:
			_, err = client.Post(w.Path, w.Body)
		case http.MethodPatch:
			_, err = client.Patch(w.Path, w.Body)
		case http.MethodDelete:
			_, err = client.Delete(w.Path, nil)
		}
		if err == nil {
			continue
		}

		// Earlier requests already applied: say so, so the caller checks.
		if i > 0 {
			err = &clierrors.CLIError{
				Code:       "PARTIALLY_APPLIED",
				Message:    fmt.Sprintf("%s (%d of %d requests applied)", err.Error(), i, len(writes)),
				Suggestion: "Run 'konvu coverage list' to see what changed.",
				ExitCode:   clierrors.ExitGeneralError,
			}
		}
		handleCoverageError(err, format)
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(map[string]any{"action": action, "dry_run": false, "requests": len(writes)}))
	} else {
		fmt.Printf("Applied %s (%d request(s)).\n", action, len(writes))
	}
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
			return nil, usageError(fmt.Sprintf("Invalid severity %q. Use CRITICAL, HIGH, MEDIUM, LOW, or all.", s))
		}
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		return nil, usageError("No severities given; use all for every severity.")
	}
	return out, nil
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
				Suggestion: "Severities are CRITICAL, HIGH, MEDIUM, LOW, or all.",
				ExitCode:   clierrors.ExitUsageError,
			}
		case 409:
			cliErr = &clierrors.CLIError{
				Code:       "CONFLICT",
				Message:    e.Error(),
				Suggestion: "Run 'konvu coverage list' to see scopes.",
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
	coverageListCmd.Flags().StringP("repo", "r", "", "Only scopes covering this repository (URL, id or unique substring)")
	coverageListCmd.Flags().StringP("output", "o", "", "Output format: json, table, csv")
	coverageListCmd.Flags().BoolP("quiet", "q", false, "Print only scope names")

	coverageCreateCmd.Flags().String("product", "", "Finding type: sca or sast")
	coverageCreateCmd.Flags().StringSlice("repo", nil, "Repositories to cover")
	coverageCreateCmd.Flags().StringSlice("analysis", nil, "Run Exploitability analysis on these severities (or all)")
	coverageCreateCmd.Flags().StringSlice("reproduction", nil, "Run Reproduction on these severities (or all)")
	coverageCreateCmd.Flags().Bool("remediation", false, "Run Remediation")

	coverageSeveritiesCmd.Flags().StringSlice("analysis", nil, "Exploitability analysis severities (or all)")
	coverageSeveritiesCmd.Flags().StringSlice("reproduction", nil, "Reproduction severities (or all)")

	for _, c := range []*cobra.Command{coverageAddCmd, coverageRemoveCmd, coverageSeveritiesCmd, coverageDeleteCmd} {
		c.Flags().String("product", "", "sca or sast, when both have a scope with this name")
	}
	for _, c := range []*cobra.Command{coverageCreateCmd, coverageAddCmd, coverageRemoveCmd, coverageSeveritiesCmd, coverageDeleteCmd} {
		c.Flags().Bool("dry-run", false, "Preview without applying")
		c.Flags().StringP("output", "o", "", "Output format: json, table")
	}

	coverageCmd.AddCommand(coverageListCmd, coverageCreateCmd, coverageAddCmd, coverageRemoveCmd, coverageSeveritiesCmd, coverageDeleteCmd)

	// Bare `konvu coverage` lists, matching the dismiss alias pattern.
	coverageCmd.RunE = runCoverageList
	coverageCmd.Flags().StringP("repo", "r", "", "Only scopes covering this repository (URL, id or unique substring)")
	coverageCmd.Flags().StringP("output", "o", "", "Output format: json, table, csv")
	coverageCmd.Flags().BoolP("quiet", "q", false, "Print only scope names")

	rootCmd.AddCommand(coverageCmd)
}
