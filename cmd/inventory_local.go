package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	"github.com/KonvuInc/konvu-cli/pkg/config"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const inventoryRepositoriesKey = "repositories"
const inventoryEmptyState = "No local Security Context Graphs yet. Run `konvu inventory map <local-path>` to create one.\n"

type inventoryListDependencies struct {
	listLocal        func() ([]baseline.RunEntry, error)
	hostedConfigured func() bool
	fetchHosted      func() (map[string]any, map[string]any, error)
}

type inventoryTUIDependencies struct {
	list       inventoryListDependencies
	pick       func([]output.InventoryRepositoryOption, int) (int, bool, error)
	openLocal  func(string) (output.BaselineWorkspaceOutcome, error)
	openHosted func(string, string) (output.BaselineWorkspaceOutcome, error)
}

func defaultInventoryListDependencies() (inventoryListDependencies, error) {
	store, err := baseline.DefaultStore()
	if err != nil {
		return inventoryListDependencies{}, err
	}
	return inventoryListDependencies{
		listLocal:        store.List,
		hostedConfigured: inventoryHostedConfigured,
		fetchHosted:      fetchInventoryHosted,
	}, nil
}

func runInventoryTUI(cmd *cobra.Command) error {
	store, err := baseline.DefaultStore()
	if err != nil {
		return inventoryLocalError(err)
	}
	list, err := defaultInventoryListDependencies()
	if err != nil {
		return inventoryLocalError(err)
	}
	return executeInventoryTUI(cmd, inventoryTUIDependencies{
		list: list,
		pick: output.PickInventoryRepository,
		openLocal: func(runID string) (output.BaselineWorkspaceOutcome, error) {
			run, err := store.Select(baseline.Selector{RunID: runID})
			if err != nil {
				return output.BaselineWorkspaceQuit, inventoryLocalError(err)
			}
			if run.Run.Status != baseline.StatusCompleted {
				return output.BrowseBaselineRunDiagnostics(guardrailsBaselineTUIOption(*run))
			}
			workspace, err := output.NewBaselineWorkspaceV1(run.Document)
			if err != nil {
				return output.BaselineWorkspaceQuit, inventoryLocalError(err)
			}
			return workspace.Browse()
		},
		openHosted: func(repositoryID, repository string) (output.BaselineWorkspaceOutcome, error) {
			client := api.NewClient("", "")
			defer client.Close()
			profile, err := client.Get(threatProfileRepoPath+repositoryID, nil)
			if err != nil {
				if inventoryIsNoThreatProfileError(err) {
					return output.BrowseInventoryRepositoryDetail(
						inventoryNoThreatProfileDetailText(repository),
					)
				}
				return output.BaselineWorkspaceQuit, inventoryHostedError(err)
			}
			return output.BrowseInventoryRepositoryDetail(inventoryHostedDetailText(profile))
		},
	})
}

func executeInventoryTUI(cmd *cobra.Command, deps inventoryTUIDependencies) error {
	entries, _, hostedErr, err := loadInventoryEntries(deps.list)
	if err != nil {
		return err
	}
	if hostedErr != nil {
		if err := output.WriteString(cmd.ErrOrStderr(), "Warning: Hosted Inventory is unavailable; showing local Security Context Graphs only.\n"); err != nil {
			return inventoryOutputError(err)
		}
	}
	if len(entries) == 0 {
		return output.WriteString(cmd.OutOrStdout(), inventoryEmptyState)
	}

	options := make([]output.InventoryRepositoryOption, 0, len(entries))
	for _, value := range entries {
		entry, _ := value.(map[string]any)
		identity := getMap(entry, "identity")
		option := output.InventoryRepositoryOption{
			Repository: orDefault(getStr(identity, "display_name"), getStr(identity, "selector")),
		}
		local := getMap(entry, "local")
		if len(local) > 0 {
			latestRun := getMap(local, "latest_run")
			option.Source = "local"
			option.Updated = inventoryRunTimestamp(latestRun)
		}
		if graph := getMap(local, "graph"); len(graph) > 0 {
			counts := getMap(graph, "counts")
			option.SecurityGraph = inventoryGraphDisplay(graph)
			option.Assets = fmt.Sprint(intOf(counts["assets"]))
			option.Controls = fmt.Sprint(intOf(counts["controls"]))
		} else if len(local) == 0 {
			option.Source = "hosted"
			option.SecurityGraph = "Not mapped"
		}
		if profile := getMap(getMap(entry, "hosted"), "threat_profile"); len(profile) > 0 {
			option.ThreatProfile = inventoryThreatProfileDisplay(profile)
			if option.Updated == "" {
				option.Updated = getStr(profile, "updated_at")
			}
		}
		options = append(options, option)
	}

	selected := 0
	for {
		index, opened, err := deps.pick(options, selected)
		if err != nil {
			if errors.Is(err, output.ErrBaselineCancelled) {
				return nil
			}
			return err
		}
		selected = index
		if !opened {
			return nil
		}
		entry, _ := entries[selected].(map[string]any)
		local := getMap(entry, "local")
		if len(local) > 0 {
			runID := getStr(getMap(local, "graph"), "run_id")
			if runID == "" {
				runID = getStr(getMap(local, "latest_run"), "run_id")
			}
			outcome, err := deps.openLocal(runID)
			if err != nil {
				return err
			}
			if outcome == output.BaselineWorkspaceBack {
				continue
			}
			return nil
		}
		identity := getMap(entry, "identity")
		if repositoryID := getStr(identity, "hosted_repository_id"); repositoryID != "" {
			outcome, err := deps.openHosted(
				repositoryID,
				orDefault(getStr(identity, "display_name"), getStr(identity, "selector")),
			)
			if err != nil {
				if errors.Is(err, output.ErrBaselineCancelled) {
					return nil
				}
				return err
			}
			if outcome == output.BaselineWorkspaceBack {
				continue
			}
			return nil
		}
		return inventoryLocalError(fmt.Errorf("repository has no readable local or hosted facet"))
	}
}

func newInventoryListCmd() *cobra.Command {
	var explicitFormat string
	var quiet bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List local and hosted repositories in Inventory",
		Long: `List repositories known from local Security Context Graphs and, when
authenticated, repositories and Threat Profiles hosted by Konvu.

Local results never require a Konvu account. If hosted data cannot be reached,
local results are still returned and the hosted source is marked unavailable.

Use -o json or -o table for deterministic output, or -q to print selectors that
can be passed directly to 'inventory show'.`,
		Example: `  konvu inventory list
  konvu inventory list -o json
  konvu inventory list -q | xargs -n1 konvu inventory show`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := inventoryOutputFormat(explicitFormat)
			if err != nil {
				return err
			}
			deps, err := defaultInventoryListDependencies()
			if err != nil {
				return inventoryLocalError(err)
			}
			return runInventoryList(cmd, deps, format, quiet)
		},
	}
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	command.Flags().BoolVarP(&quiet, "quiet", "q", false, "Print only repository selectors")
	return command
}

func inventoryOutputFormat(explicit string) (output.OutputFormat, error) {
	explicit = strings.ToLower(strings.TrimSpace(explicit))
	if explicit != "" && explicit != "json" && explicit != "table" && explicit != "text" {
		return output.JSON, &clierrors.CLIError{
			Code:       "INVALID_ARGUMENTS",
			Message:    fmt.Sprintf("unsupported output format %q", explicit),
			Suggestion: "Use --output table or --output json.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	return output.DetectOutputFormat(explicit), nil
}

func runInventoryList(
	cmd *cobra.Command,
	deps inventoryListDependencies,
	format output.OutputFormat,
	quiet bool,
) error {
	entries, sources, hostedErr, err := loadInventoryEntries(deps)
	if err != nil {
		return err
	}

	if quiet {
		selectors := make([]string, 0, len(entries))
		for _, entry := range entries {
			selectors = append(selectors, inventoryEntrySelector(entry))
		}
		value := strings.Join(selectors, "\n")
		if value != "" {
			value += "\n"
		}
		return output.WriteString(cmd.OutOrStdout(), value)
	}

	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(map[string]any{
			"sources":      sources,
			"repositories": entries,
		})+"\n")
	}

	if err := writeInventoryListTable(cmd.OutOrStdout(), entries); err != nil {
		return inventoryOutputError(err)
	}
	if hostedErr != nil {
		warning := "Warning: Hosted Inventory is unavailable; showing local Security Context Graphs only.\n"
		if err := output.WriteString(cmd.ErrOrStderr(), warning); err != nil {
			return inventoryOutputError(err)
		}
	}
	return nil
}

func loadInventoryEntries(deps inventoryListDependencies) ([]any, map[string]any, error, error) {
	runs, err := deps.listLocal()
	if err != nil {
		return nil, nil, nil, inventoryLocalError(err)
	}
	entries := inventoryLocalEntries(runs)
	sources := map[string]any{"local": "ok", "hosted": "not_configured"}
	var hostedErr error
	if deps.hostedConfigured() {
		coverage, summary, err := deps.fetchHosted()
		if err != nil {
			sources["hosted"] = "unavailable"
			hostedErr = err
		} else {
			sources["hosted"] = "ok"
			entries = append(entries, inventoryHostedEntries(coverage, summary)...)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToLower(inventoryEntrySelector(entries[i])) < strings.ToLower(inventoryEntrySelector(entries[j]))
	})
	return entries, sources, hostedErr, nil
}

func inventoryLocalEntries(runs []baseline.RunEntry) []any {
	type repositoryRuns struct {
		latest *baseline.RunEntry
		graph  *baseline.RunEntry
	}
	grouped := make(map[string]*repositoryRuns)
	for _, run := range runs {
		if !run.Valid || strings.TrimSpace(run.Codebase.Path) == "" {
			continue
		}
		path := filepath.Clean(run.Codebase.Path)
		group := grouped[path]
		if group == nil {
			group = &repositoryRuns{}
			grouped[path] = group
		}
		runCopy := run
		if group.latest == nil {
			group.latest = &runCopy
		}
		if group.graph == nil && run.Run.Status == baseline.StatusCompleted {
			group.graph = &runCopy
		}
	}

	entries := make([]any, 0, len(grouped))
	for path := range grouped {
		group := grouped[path]
		latest := group.latest
		entry := map[string]any{
			"identity": map[string]any{
				"selector":     path,
				"display_name": orDefault(latest.Codebase.Name, path),
				"local_paths":  []any{path},
			},
			"local": map[string]any{
				"latest_run": inventoryMappingRunValue(*latest),
			},
		}
		if group.graph != nil {
			graph := group.graph
			getMap(entry, "local")["graph"] = map[string]any{
				"run_id":    graph.ID,
				"status":    "ready",
				"commit":    graph.Codebase.Git.Commit,
				"branch":    graph.Codebase.Git.Branch,
				"dirty":     graph.Codebase.Git.Dirty,
				"mapped_at": inventoryRunTimestamp(inventoryMappingRunValue(*graph)),
				"counts":    guardrailsBaselineCountsValue(graph.Counts),
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

func inventoryMappingRunValue(run baseline.RunEntry) map[string]any {
	return map[string]any{
		"run_id":       run.ID,
		"status":       inventoryGraphStatus(run.Run.Status),
		"started_at":   run.Run.StartedAt,
		"completed_at": run.Run.CompletedAt,
		"problem":      inventoryRunProblem(run),
	}
}

func inventoryRunProblem(run baseline.RunEntry) string {
	if run.Problem != "" {
		return run.Problem
	}
	if run.Document == nil {
		return ""
	}
	raw := run.Document.Raw()
	metadata, _ := raw["run"].(map[string]any)
	problem, _ := metadata["error"].(string)
	return problem
}

func inventoryRunTimestamp(run map[string]any) string {
	if completed := getStr(run, "completed_at"); completed != "" {
		return completed
	}
	return getStr(run, "started_at")
}

func inventoryMappingStatusDisplay(run map[string]any) string {
	return orDefault(getStr(run, "status"), "—")
}

func inventoryHostedEntries(coverage, summary map[string]any) []any {
	profiles := make(map[string]map[string]any)
	for _, value := range getSlice(summary, "top_repos") {
		profile, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if id := getStr(profile, "vcs_repository_id"); id != "" {
			profiles[id] = profile
		}
	}

	entries := make([]any, 0)
	for _, value := range getSlice(coverage, inventoryRepositoriesKey) {
		repository, ok := value.(map[string]any)
		if !ok {
			continue
		}
		id, repositoryURL := getStr(repository, "id"), getStr(repository, "url")
		selector := orDefault(repositoryURL, id)
		if selector == "" {
			continue
		}
		entry := map[string]any{
			"identity": map[string]any{
				"selector":             selector,
				"display_name":         selector,
				"repository_url":       repositoryURL,
				"hosted_repository_id": id,
			},
			"hosted": map[string]any{},
		}
		if profile := profiles[id]; profile != nil {
			entry["hosted"] = map[string]any{"threat_profile": inventoryThreatProfileSummary(profile)}
		}
		entries = append(entries, entry)
	}
	return entries
}

func inventoryThreatProfileSummary(profile map[string]any) map[string]any {
	return map[string]any{
		"score":      profile["threat_profile_score"],
		"tier":       profile["threat_profile_tier"],
		"tier_label": profile["threat_profile_tier_label"],
		"updated_at": profile["updated_at"],
	}
}

func writeInventoryListTable(writer io.Writer, entries []any) error {
	rows := make([]any, 0, len(entries))
	for _, value := range entries {
		entry, _ := value.(map[string]any)
		identity := getMap(entry, "identity")
		row := map[string]any{
			"repository":     orDefault(getStr(identity, "display_name"), getStr(identity, "selector")),
			"threat_profile": "—",
			"security_graph": "Not mapped",
			"updated":        "—",
		}
		if hosted := getMap(entry, "hosted"); len(hosted) > 0 {
			if profile := getMap(hosted, "threat_profile"); len(profile) > 0 {
				row["threat_profile"] = inventoryThreatProfileDisplay(profile)
				if updated := getStr(profile, "updated_at"); updated != "" {
					row["updated"] = updated
				}
			}
		}
		if local := getMap(entry, "local"); len(local) > 0 {
			latestRun := getMap(local, "latest_run")
			if updated := inventoryRunTimestamp(latestRun); updated != "" {
				row["updated"] = updated
			}
			if graph := getMap(local, "graph"); len(graph) > 0 {
				row["security_graph"] = inventoryGraphDisplay(graph)
			}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return output.WriteString(writer, "No repositories in Inventory. Run 'konvu inventory map .' to create a local Security Context Graph.\n")
	}
	return output.WriteString(writer, output.FormatTable(
		map[string]any{inventoryRepositoriesKey: rows},
		[]string{"repository", "threat_profile", "security_graph", "updated"},
		inventoryRepositoriesKey,
		nil,
	))
}

func inventoryEntrySelector(entry any) string {
	value, _ := entry.(map[string]any)
	return getStr(getMap(value, "identity"), "selector")
}

func inventoryThreatProfileDisplay(profile map[string]any) string {
	score := scoreDisplay(profile["score"])
	tier := getStr(profile, "tier_label")
	if tier == "" {
		tier = tierLabel(getStr(profile, "tier"))
	}
	if score == "—" {
		return orDefault(tier, "—")
	}
	if tier == "" {
		return score
	}
	return score + " · " + tier
}

func inventoryGraphDisplay(graph map[string]any) string {
	status := getStr(graph, "status")
	commit := getStr(graph, "commit")
	if len(commit) > 7 {
		commit = commit[:7]
	}
	if dirty, _ := getBool(graph, "dirty"); dirty {
		commit += "*"
	}
	if commit == "" {
		return orDefault(status, "Not mapped")
	}
	if status == "" || status == "ready" {
		return commit + " · ready"
	}
	return commit + " · " + status
}

func inventoryGraphStatus(status baseline.Status) string {
	if status == baseline.StatusCompleted {
		return "ready"
	}
	return string(status)
}

func inventoryHostedConfigured() bool {
	if strings.TrimSpace(os.Getenv("KONVU_ACCESS_TOKEN")) != "" {
		return true
	}
	info, err := os.Stat(config.GetCredentialsPath())
	return err == nil && info.Mode().IsRegular()
}

func fetchInventoryHosted() (map[string]any, map[string]any, error) {
	client := api.NewClient("", "")
	defer client.Close()
	coverage, err := client.Get(coverageConfigPath, nil)
	if err != nil {
		return nil, nil, err
	}
	summary, err := client.Get(threatProfileSummaryPath, nil)
	if err != nil {
		return nil, nil, err
	}
	return coverage, summary, nil
}

func inventoryLocalError(err error) error {
	return &clierrors.CLIError{
		Code:       "LOCAL_INVENTORY_UNAVAILABLE",
		Message:    fmt.Sprintf("could not read local Security Context Graphs: %v", err),
		Suggestion: "Check the local Konvu data directory permissions and try again.",
		ExitCode:   clierrors.ExitGeneralError,
	}
}

func inventoryOutputError(err error) error {
	return &clierrors.CLIError{
		Code:       "OUTPUT_FAILED",
		Message:    fmt.Sprintf("could not write Inventory output: %v", err),
		Suggestion: "Check that the output destination is writable and try again.",
		ExitCode:   clierrors.ExitGeneralError,
	}
}

func inventoryHostedError(err error) error {
	var authenticationError *api.AuthenticationError
	if errors.As(err, &authenticationError) {
		return clierrors.NewAuthError(authenticationError.Error())
	}
	return clierrors.NewAPIError(err.Error())
}

func inventoryIsNoThreatProfileError(err error) bool {
	var apiError *api.APIError
	if !errors.As(err, &apiError) {
		return false
	}
	return strings.Contains(
		strings.ToLower(apiError.Message),
		"no threat profile for this repository",
	)
}

func inventoryNoThreatProfileError() *clierrors.CLIError {
	return &clierrors.CLIError{
		Code:       "REPOSITORY_NOT_MAPPED",
		Message:    "This repository hasn't been mapped in Konvu yet.",
		Suggestion: "Its Threat Profile will appear after Konvu maps it. To inspect a local checkout in the meantime, run 'konvu inventory map <local-path>'.",
		ExitCode:   clierrors.ExitNotFound,
	}
}

func inventoryNoThreatProfileDetailText(repository string) string {
	var value strings.Builder
	value.WriteString(repository)
	value.WriteString("\n")
	value.WriteString(strings.Repeat("─", 64))
	value.WriteString("\n\nTHREAT PROFILE · HOSTED\n\n")
	value.WriteString("This repository hasn't been mapped in Konvu yet.\n\n")
	value.WriteString("Its Threat Profile will appear after Konvu maps it.\n")
	value.WriteString("To inspect a local checkout in the meantime:\n\n")
	value.WriteString("  konvu inventory map <local-path>\n")
	return value.String()
}

func writeInventoryHostedDetail(writer io.Writer, profile map[string]any) error {
	if err := output.WriteString(writer, inventoryHostedDetailText(profile)); err != nil {
		return inventoryOutputError(err)
	}
	return nil
}

func inventoryHostedDetailText(profile map[string]any) string {
	name := orDefault(getStr(profile, "repo_name"), getStr(profile, "repo_url"))
	var value strings.Builder
	value.WriteString(name)
	value.WriteString("\n")
	if repositoryURL := getStr(profile, "repo_url"); repositoryURL != "" && repositoryURL != name {
		value.WriteString(repositoryURL)
		value.WriteString("\n")
	}
	value.WriteString(strings.Repeat("─", 64))
	value.WriteString("\n\nTHREAT PROFILE · HOSTED\n\n")
	score := intOf(profile["threat_profile_score"])
	fmt.Fprintf(
		&value,
		"%-18s %s  %d / 100\n%-18s %s\n",
		"Score",
		inventoryScoreBar(score, 20),
		score,
		"Tier",
		repoTierText(profile),
	)
	classification := inventoryDetailLabel(orDefault(getStr(profile, "classification_category"), "—"))
	if confidence, ok := getFloat(profile, "classification_confidence"); ok {
		classification += " · " + inventoryConfidenceDisplay(confidence)
	}
	fmt.Fprintf(&value, "%-18s %s\n", "Classification", classification)
	if production, ok := getBool(profile, "is_production"); ok {
		fmt.Fprintf(&value, "%-18s %s\n", "Production", inventoryYesNo(production))
	}
	if surface := getStr(profile, "surface"); surface != "" {
		fmt.Fprintf(&value, "%-18s %s\n", "Surface", inventoryDetailLabel(surface))
	}
	updated := getStr(profile, "updated_at")
	if updated != "" {
		updated = formatGuardrailsBaselineScanned(updated)
	}
	fmt.Fprintf(&value, "%-18s %s\n", "Updated", orDefault(updated, "—"))

	if summary := getStr(profile, "threat_profile_summary"); summary != "" {
		value.WriteString("\nSUMMARY\n\n")
		value.WriteString(summary)
		value.WriteString("\n")
	}
	if domains := getSlice(profile, "domains"); len(domains) > 0 {
		value.WriteString("\nDOMAINS\n\n")
		for _, domain := range stringsOf(domains) {
			value.WriteString("  • ")
			value.WriteString(domain)
			value.WriteString("\n")
		}
	}
	inventoryWriteScoreFactors(&value, getMap(profile, "threat_profile_factors"))
	if attributes := getMap(profile, "attributes"); len(attributes) > 0 {
		value.WriteString("\nSECURITY SIGNALS\n\n")
		for _, key := range sortedAnyKeys(attributes) {
			attribute, ok := attributes[key].(map[string]any)
			if !ok {
				continue
			}
			value.WriteString(inventoryDetailLabel(key))
			value.WriteString("\n")
			fmt.Fprintf(&value, "  %-16s %s\n", "Value", inventoryPrettyDisplayValue(attribute["value"]))
			var metadata []string
			if provenance := getStr(attribute, "provenance"); provenance != "" {
				metadata = append(metadata, inventoryDetailLabel(provenance))
			}
			if confidence, ok := getFloat(attribute, "confidence"); ok {
				metadata = append(metadata, inventoryConfidenceDisplay(confidence))
			}
			if len(metadata) > 0 {
				fmt.Fprintf(&value, "  %-16s %s\n", "Assessment", strings.Join(metadata, " · "))
			}
			evidence := getSlice(attribute, "evidence")
			if len(evidence) > 0 {
				value.WriteString("  Evidence\n")
			}
			for _, evidence := range evidence {
				if text, ok := evidence.(string); ok && text != "" {
					value.WriteString("    • ")
					value.WriteString(text)
					value.WriteString("\n")
				}
			}
			value.WriteString("\n")
		}
	}
	if source := getStr(profile, "source"); source != "" {
		value.WriteString("\n")
		fmt.Fprintf(&value, "%-18s %s\n", "Source", inventoryPrettyValue(source))
	}
	return value.String()
}

func inventoryWriteScoreFactors(value *strings.Builder, factors map[string]any) {
	if len(factors) == 0 {
		return
	}
	value.WriteString("\nSCORE FACTORS\n\n")
	for _, key := range sortedAnyKeys(factors) {
		factor, ok := getFloat(factors, key)
		if ok && factor >= 0 && factor <= 1 {
			fmt.Fprintf(
				value,
				"%-20s %s  %s\n",
				inventoryDetailLabel(key),
				inventoryScoreBar(int(factor*100+0.5), 12),
				confidenceDisplay(factor),
			)
			continue
		}
		fmt.Fprintf(value, "%-20s %s\n", inventoryDetailLabel(key), valueDisplay(factors[key]))
	}
}

func inventoryDetailLabel(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "_", " ")
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func inventoryPrettyValue(value string) string {
	return strings.ReplaceAll(value, "_", " ")
}

func inventoryPrettyDisplayValue(value any) string {
	switch typed := value.(type) {
	case bool:
		return inventoryYesNo(typed)
	case string:
		return inventoryPrettyValue(typed)
	case []any:
		values := stringsOf(typed)
		for index := range values {
			values[index] = inventoryPrettyValue(values[index])
		}
		return strings.Join(values, ", ")
	default:
		return valueDisplay(value)
	}
}

func inventoryYesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

func inventoryConfidenceDisplay(value float64) string {
	percent := int(value*100 + 0.5)
	return fmt.Sprintf("%d%% confidence", percent)
}

func inventoryScoreBar(score, width int) string {
	score = max(0, min(100, score))
	filled := (score*width + 50) / 100
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func newInventoryMapCmd() *cobra.Command {
	var apiKey string
	var yes bool
	var noSandbox bool
	command := &cobra.Command{
		Use:   "map <local-path>",
		Short: "Map a local repository into a Security Context Graph",
		Long: `Create a Security Context Graph from a local repository.

Mapping runs locally and does not require a Konvu account. Hosted repository
selectors are not supported yet; clone the repository and pass its local path.
The mapper uses OPENAI_API_KEY unless --openai-api-key is provided.`,
		Example: `  konvu inventory map .
  konvu inventory map /path/to/repository --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path, err := inventoryLocalDirectory(args[0])
			if err != nil {
				return err
			}
			guardrailsNoSandbox = noSandbox
			runGuardrailsBaselineScan(
				path,
				resolveGuardrailsAPIKey(apiKey, os.Getenv("OPENAI_API_KEY")),
				yes,
				runGuardrailsExec,
			)
			return nil
		},
	}
	command.Flags().StringVar(&apiKey, "openai-api-key", "", "OpenAI API key (prefer OPENAI_API_KEY to avoid shell history)")
	command.Flags().BoolVarP(&yes, "yes", "y", false, "continue without prompting")
	command.Flags().BoolVar(&noSandbox, "no-sandbox", false, "run the mapping runtime without OS filesystem isolation")
	return command
}

func inventoryLocalDirectory(value string) (string, error) {
	path := strings.TrimSpace(value)
	if path == "" {
		return "", inventoryMapTargetError(value)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", inventoryMapTargetError(value)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return "", inventoryMapTargetError(value)
	}
	return filepath.Clean(absolute), nil
}

func inventoryMapTargetError(value string) error {
	return &clierrors.CLIError{
		Code:       "LOCAL_REPOSITORY_NOT_FOUND",
		Message:    fmt.Sprintf("%q is not an existing local directory", strings.TrimSpace(value)),
		Suggestion: "Hosted mapping is not available yet. Clone the repository and run 'konvu inventory map <local-path>'.",
		ExitCode:   clierrors.ExitNotFound,
	}
}

func inventoryLooksLocalTarget(target string) bool {
	target = strings.TrimSpace(target)
	if filepath.IsAbs(target) {
		return true
	}
	info, err := os.Stat(target)
	return err == nil && info.IsDir()
}

func runInventoryShowLocal(
	cmd *cobra.Command,
	target string,
	fields string,
	format output.OutputFormat,
) error {
	store, err := baseline.DefaultStore()
	if err != nil {
		return inventoryLocalError(err)
	}
	path := strings.TrimSpace(target)
	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(path)
		if err != nil {
			return inventoryLocalError(err)
		}
	}
	runs, err := store.List()
	if err != nil {
		return inventoryLocalError(err)
	}
	var entry map[string]any
	for _, value := range inventoryLocalEntries(runs) {
		candidate, _ := value.(map[string]any)
		if getStr(getMap(candidate, "identity"), "selector") == filepath.Clean(path) {
			entry = candidate
			break
		}
	}
	if entry == nil {
		return &clierrors.CLIError{
			Code:       "SECURITY_CONTEXT_GRAPH_NOT_FOUND",
			Message:    fmt.Sprintf("no local Security Context Graph was found for %q", target),
			Suggestion: "Run 'konvu inventory map <local-path>' to create one.",
			ExitCode:   clierrors.ExitNotFound,
		}
	}
	if format == output.JSON {
		if fields != "" {
			entry = output.FilterFields(entry, splitFields(fields))
		}
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(entry)+"\n")
	}
	return writeInventoryLocalDetail(cmd.OutOrStdout(), entry)
}

func writeInventoryLocalDetail(writer io.Writer, entry map[string]any) error {
	identity := getMap(entry, "identity")
	local := getMap(entry, "local")
	graph := getMap(local, "graph")
	latestRun := getMap(local, "latest_run")
	if len(graph) == 0 {
		value := fmt.Sprintf(
			"%s\n\nSecurity Context Graph · Local\nNo successful graph is available.\nLatest mapping attempt: %s\nRun: %s\nUpdated: %s\n",
			orDefault(getStr(identity, "display_name"), getStr(identity, "selector")),
			inventoryMappingStatusDisplay(latestRun),
			getStr(latestRun, "run_id"),
			orDefault(inventoryRunTimestamp(latestRun), "—"),
		)
		if problem := getStr(latestRun, "problem"); problem != "" {
			value += "Error: " + problem + "\n"
		}
		if err := output.WriteString(writer, value); err != nil {
			return inventoryOutputError(err)
		}
		return nil
	}
	counts := getMap(graph, "counts")
	commit := getStr(graph, "commit")
	if commit == "" {
		commit = "no commit"
	}
	if dirty, _ := getBool(graph, "dirty"); dirty {
		commit += " (dirty)"
	}
	value := fmt.Sprintf(
		"%s\n\nSecurity Context Graph · Local\nRun: %s\nStatus: %s\nCommit: %s\nBranch: %s\nMapped: %s\nAssets: %d  Controls: %d  Relationships: %d\n",
		orDefault(getStr(identity, "display_name"), getStr(identity, "selector")),
		getStr(graph, "run_id"),
		getStr(graph, "status"),
		commit,
		orDefault(getStr(graph, "branch"), "—"),
		orDefault(getStr(graph, "mapped_at"), "—"),
		intOf(counts["assets"]),
		intOf(counts["controls"]),
		intOf(counts["implementations"]),
	)
	if getStr(latestRun, "run_id") != getStr(graph, "run_id") {
		value += fmt.Sprintf(
			"\nLatest mapping attempt: %s (%s)\n",
			inventoryMappingStatusDisplay(latestRun),
			getStr(latestRun, "run_id"),
		)
		if problem := getStr(latestRun, "problem"); problem != "" {
			value += "Error: " + problem + "\n"
		}
	}
	if err := output.WriteString(writer, value); err != nil {
		return inventoryOutputError(err)
	}
	return nil
}
