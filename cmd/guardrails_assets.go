package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const guardrailsAssetsScanSuggestion = "Run 'konvu guardrails baseline scan --repo <path>' to create a stored baseline."

type guardrailsAssetsEntry struct {
	raw         map[string]any
	repoPath    string
	runDir      string
	lastScanned string
	id          string
}

type guardrailsAssetsBaseline struct {
	entry       guardrailsAssetsEntry
	run         []map[string]any
	blueprint   map[string]any
	protections map[string]any
}

type guardrailsAssetsCompletionMarker struct {
	file *os.File
	info os.FileInfo
	raw  []byte
}

type guardrailsAssetsRepositoryOption struct {
	ID          string
	DisplayName string
	Commit      string
}

type guardrailsAssetsBrowseOutcome int

const (
	guardrailsAssetsBrowseQuit guardrailsAssetsBrowseOutcome = iota
	guardrailsAssetsBrowseBack
	guardrailsAssetsBrowseCancelled
)

var errGuardrailsAssetsCancelled = errors.New("guardrails assets cancelled")

type guardrailsAssetsDependencies struct {
	interactive func() bool
	pick        func([]guardrailsAssetsRepositoryOption, int) (int, bool, error)
	browse      func(*guardrailsAssetsBaseline) (guardrailsAssetsBrowseOutcome, error)
	static      func(*guardrailsAssetsBaseline) (string, error)
}

var guardrailsAssetsCmd = &cobra.Command{
	Use:   "assets",
	Short: "Browse the assets and security controls in a stored baseline",
	Run: func(cmd *cobra.Command, args []string) {
		exitCode := executeGuardrailsAssetsCommand(
			cmd,
			args,
			guardrailsAssetsStoreRoot,
			defaultGuardrailsAssetsDependencies(),
		)
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	},
}

func executeGuardrailsAssetsCommand(
	cmd *cobra.Command,
	args []string,
	storeRoot func() (string, error),
	deps guardrailsAssetsDependencies,
) int {
	explicitFormat, _ := cmd.Flags().GetString("output")
	var err error
	if len(args) != 0 {
		err = guardrailsAssetsError(
			"INVALID_ARGUMENTS",
			"guardrails assets does not accept positional arguments",
			clierrors.ExitUsageError,
		)
	} else {
		var root string
		root, err = storeRoot()
		if err == nil {
			repository, _ := cmd.Flags().GetString("repo")
			err = runGuardrailsAssets(cmd, root, repository, explicitFormat, deps)
		}
	}
	if err == nil {
		return 0
	}
	return reportGuardrailsAssetsCommandError(cmd, err, guardrailsAssetsErrorOutputFormat(explicitFormat))
}

func guardrailsAssetsErrorOutputFormat(explicit string) output.OutputFormat {
	switch strings.ToLower(strings.TrimSpace(explicit)) {
	case "json":
		return output.JSON
	case "table":
		return output.Table
	default:
		return output.DetectOutputFormat("")
	}
}

func reportGuardrailsAssetsCommandError(
	cmd *cobra.Command,
	err error,
	format output.OutputFormat,
) int {
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		cliErr = guardrailsAssetsError(
			"GUARDRAILS_BASELINE_FAILED",
			fmt.Sprintf("could not browse the stored baseline: %v", err),
			clierrors.ExitGeneralError,
		)
	}

	destination := cmd.ErrOrStderr()
	rendered := "Error: " + cliErr.Message + "\n"
	if cliErr.Suggestion != "" {
		rendered += "  " + guardrailsAssetsSafeText(cliErr.Suggestion) + "\n"
	}
	if format == output.JSON {
		destination = cmd.OutOrStdout()
		rendered = clierrors.FormatErrorJSON(cliErr) + "\n"
	}
	if err := output.WriteString(destination, rendered); err != nil {
		return clierrors.ExitGeneralError
	}
	if cliErr.ExitCode == 0 {
		return clierrors.ExitGeneralError
	}
	return cliErr.ExitCode
}

func guardrailsAssetsStoreRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", guardrailsAssetsError(
			"GUARDRAILS_BASELINE_UNAVAILABLE",
			fmt.Sprintf("could not determine the guardrails baseline store: %v", err),
			clierrors.ExitGeneralError,
		)
	}
	return filepath.Join(home, ".cache", "guardrails", "posture"), nil
}

func guardrailsAssetsError(code, message string, exitCode int) *clierrors.CLIError {
	suggestion := guardrailsAssetsScanSuggestion
	switch code {
	case "INVALID_ARGUMENTS":
		suggestion = "Run 'konvu guardrails assets --help' to see valid arguments and output formats."
	case "GUARDRAILS_BASELINE_REPOSITORY_REQUIRED":
		suggestion = "Pass one of the available repository IDs with --repo."
	case "GUARDRAILS_BASELINE_SELECTION_FAILED":
		suggestion = "Choose a listed repository, or pass its ID with --repo."
	case "GUARDRAILS_BASELINE_WORKSPACE_FAILED":
		suggestion = "Re-run with --output table for a non-interactive summary."
	case "GUARDRAILS_BASELINE_OUTPUT_FAILED":
		suggestion = "Check that the output destination is writable, then try again."
	case "GUARDRAILS_BASELINE_UNAVAILABLE":
		suggestion = "Set HOME to the user directory containing the guardrails baseline store, then try again."
	}
	return &clierrors.CLIError{
		Code:       code,
		Message:    guardrailsAssetsSafeText(message),
		Suggestion: suggestion,
		ExitCode:   exitCode,
	}
}

func guardrailsAssetsSafeText(value string) string {
	var cleaned strings.Builder
	cleaned.Grow(len(value))
	for _, character := range value {
		if unicode.IsControl(character) {
			cleaned.WriteByte(' ')
			continue
		}
		cleaned.WriteRune(character)
	}
	return strings.Join(strings.Fields(cleaned.String()), " ")
}

func wrapGuardrailsAssetsError(code, message string, err error) error {
	if err == nil {
		return nil
	}
	var cliErr *clierrors.CLIError
	if errors.As(err, &cliErr) {
		return cliErr
	}
	return guardrailsAssetsError(
		code,
		fmt.Sprintf("%s: %v", message, err),
		clierrors.ExitGeneralError,
	)
}

func loadGuardrailsAssetsRegistry(root string) ([]guardrailsAssetsEntry, error) {
	path := filepath.Join(root, "registry.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, guardrailsAssetsError(
				"GUARDRAILS_BASELINE_NOT_FOUND",
				"no stored guardrails baselines were found",
				clierrors.ExitNotFound,
			)
		}
		return nil, guardrailsAssetsError(
			"GUARDRAILS_BASELINE_UNREADABLE",
			fmt.Sprintf("could not read the stored baseline registry: %v", err),
			clierrors.ExitGeneralError,
		)
	}

	var records []map[string]any
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, guardrailsAssetsError(
			"GUARDRAILS_BASELINE_INVALID",
			fmt.Sprintf("the stored baseline registry is not valid JSON: %v", err),
			clierrors.ExitGeneralError,
		)
	}
	if len(records) == 0 {
		return nil, guardrailsAssetsError(
			"GUARDRAILS_BASELINE_NOT_FOUND",
			"no stored guardrails baselines were found",
			clierrors.ExitNotFound,
		)
	}

	entries := make([]guardrailsAssetsEntry, 0, len(records))
	seenPaths := make(map[string]bool, len(records))
	for _, record := range records {
		repoPath, _ := record["repo_path"].(string)
		runDir, _ := record["run_dir"].(string)
		lastScanned, _ := record["last_scanned"].(string)
		repoPath = strings.TrimSpace(repoPath)
		runDir = strings.TrimSpace(runDir)
		lastScanned = strings.TrimSpace(lastScanned)
		if repoPath == "" || runDir == "" || lastScanned == "" ||
			!guardrailsAssetsNonNegativeInteger(record["resources_count"]) ||
			!guardrailsAssetsNonNegativeInteger(record["mechanisms_count"]) {
			return nil, guardrailsAssetsError(
				"GUARDRAILS_BASELINE_INVALID",
				"the stored baseline registry contains an entry that does not match the baseline registry schema",
				clierrors.ExitGeneralError,
			)
		}
		if seenPaths[repoPath] {
			return nil, guardrailsAssetsError(
				"GUARDRAILS_BASELINE_INVALID",
				fmt.Sprintf("the stored baseline registry contains duplicate entries for %q", repoPath),
				clierrors.ExitGeneralError,
			)
		}
		seenPaths[repoPath] = true
		entries = append(entries, guardrailsAssetsEntry{
			raw:         record,
			repoPath:    repoPath,
			runDir:      runDir,
			lastScanned: lastScanned,
		})
	}

	if err := assignGuardrailsAssetsRepositoryIDs(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func guardrailsAssetsNonNegativeInteger(value any) bool {
	number, ok := value.(float64)
	return ok && number >= 0 && number == float64(int64(number))
}

func assignGuardrailsAssetsRepositoryIDs(entries []guardrailsAssetsEntry) error {
	parts := make([][]string, len(entries))
	depths := make([]int, len(entries))
	for i := range entries {
		clean := filepath.ToSlash(filepath.Clean(entries[i].repoPath))
		for _, part := range strings.Split(clean, "/") {
			if part != "" && part != "." {
				parts[i] = append(parts[i], part)
			}
		}
		if len(parts[i]) == 0 {
			return guardrailsAssetsError(
				"GUARDRAILS_BASELINE_INVALID",
				"the stored baseline registry contains an invalid repo_path",
				clierrors.ExitGeneralError,
			)
		}
		depths[i] = min(2, len(parts[i]))
	}

	for {
		ids := make([]string, len(entries))
		groups := make(map[string][]int, len(entries))
		for i := range entries {
			ids[i] = strings.Join(parts[i][len(parts[i])-depths[i]:], "/")
			groups[ids[i]] = append(groups[ids[i]], i)
		}

		changed := false
		duplicates := false
		for _, indexes := range groups {
			if len(indexes) < 2 {
				continue
			}
			duplicates = true
			for _, index := range indexes {
				if depths[index] < len(parts[index]) {
					depths[index]++
					changed = true
				}
			}
		}
		if !duplicates {
			for i := range entries {
				entries[i].id = ids[i]
			}
			return nil
		}
		if !changed {
			return guardrailsAssetsError(
				"GUARDRAILS_BASELINE_INVALID",
				"the stored baseline registry cannot produce unique repository IDs",
				clierrors.ExitGeneralError,
			)
		}
	}
}

func loadGuardrailsAssetsBaseline(entry guardrailsAssetsEntry) (*guardrailsAssetsBaseline, error) {
	runPath := filepath.Join(entry.runDir, "run.json")
	marker, err := openGuardrailsAssetsCompletionMarker(runPath)
	if err != nil {
		return nil, completedGuardrailsAssetsError(entry.id, "run.json", err)
	}
	defer func() { _ = marker.file.Close() }()
	var run []map[string]any
	if err := json.Unmarshal(marker.raw, &run); err != nil {
		return nil, completedGuardrailsAssetsError(entry.id, "run.json", err)
	}
	if run == nil {
		return nil, completedGuardrailsAssetsError(
			entry.id,
			"run.json",
			fmt.Errorf("artifact must be a JSON array"),
		)
	}

	blueprintPath := filepath.Join(entry.runDir, "blueprint.json")
	blueprint := map[string]any{}
	if err := readGuardrailsAssetsJSON(blueprintPath, &blueprint); err != nil || len(blueprint) == 0 {
		if err == nil {
			err = fmt.Errorf("artifact is empty")
		}
		return nil, completedGuardrailsAssetsError(entry.id, "blueprint.json", err)
	}
	if err := validateGuardrailsAssetsBlueprint(blueprint); err != nil {
		return nil, completedGuardrailsAssetsError(entry.id, "blueprint.json", err)
	}

	protectionsPath := filepath.Join(entry.runDir, "protections.json")
	protections := map[string]any{}
	if err := readGuardrailsAssetsJSON(protectionsPath, &protections); err != nil || len(protections) == 0 {
		if err == nil {
			err = fmt.Errorf("artifact is empty")
		}
		return nil, completedGuardrailsAssetsError(entry.id, "protections.json", err)
	}
	for _, key := range []string{"assets", "controls", "implementations", "protections"} {
		if _, ok := protections[key].([]any); !ok {
			return nil, completedGuardrailsAssetsError(
				entry.id,
				"protections.json",
				fmt.Errorf("field %q is missing or is not an array", key),
			)
		}
	}
	if _, err := output.ParseBaselineCatalog(protections); err != nil {
		return nil, completedGuardrailsAssetsError(entry.id, "protections.json", err)
	}
	if err := marker.verifyUnchanged(runPath); err != nil {
		return nil, completedGuardrailsAssetsError(entry.id, "run.json", err)
	}

	return &guardrailsAssetsBaseline{
		entry:       entry,
		run:         run,
		blueprint:   blueprint,
		protections: protections,
	}, nil
}

func openGuardrailsAssetsCompletionMarker(path string) (*guardrailsAssetsCompletionMarker, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	raw, err := io.ReadAll(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &guardrailsAssetsCompletionMarker{file: file, info: info, raw: raw}, nil
}

func (marker *guardrailsAssetsCompletionMarker) verifyUnchanged(path string) error {
	current, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("completion marker changed while artifacts were being read: %w", err)
	}
	defer func() { _ = current.Close() }()

	currentInfo, err := current.Stat()
	if err != nil {
		return fmt.Errorf("completion marker changed while artifacts were being read: %w", err)
	}
	currentRaw, err := io.ReadAll(current)
	if err != nil {
		return fmt.Errorf("completion marker changed while artifacts were being read: %w", err)
	}
	if !os.SameFile(marker.info, currentInfo) || !bytes.Equal(marker.raw, currentRaw) {
		return fmt.Errorf("completion marker changed while artifacts were being read")
	}
	return nil
}

func readGuardrailsAssetsJSON(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	return nil
}

func validateGuardrailsAssetsBlueprint(blueprint map[string]any) error {
	if err := rejectGuardrailsAssetsUnknownFields(
		blueprint,
		"blueprint",
		"format_version", "repo", "metrics", "languages", "components",
		"frameworks", "databases", "orms", "unknowns",
	); err != nil {
		return err
	}
	version, ok := blueprint["format_version"].(float64)
	if !ok || version != 1 {
		return fmt.Errorf("field %q must be the supported integer version 1", "format_version")
	}

	repo, ok := blueprint["repo"].(map[string]any)
	if !ok {
		return fmt.Errorf("field %q must be an object", "repo")
	}
	if err := rejectGuardrailsAssetsUnknownFields(repo, "repo", "name", "layout", "summary"); err != nil {
		return err
	}
	for _, field := range []string{"name", "layout", "summary"} {
		if err := requireGuardrailsAssetsString(repo, field, "repo"); err != nil {
			return err
		}
	}
	allowedLayouts := map[string]bool{
		"single_component": true,
		"monorepo":         true,
		"mixed":            true,
		"unknown":          true,
	}
	if layout := repo["layout"].(string); !allowedLayouts[layout] {
		return fmt.Errorf("field %q has unsupported value %q", "repo.layout", layout)
	}

	metrics, ok := blueprint["metrics"].(map[string]any)
	if !ok {
		return fmt.Errorf("field %q must be an object", "metrics")
	}
	if err := rejectGuardrailsAssetsUnknownFields(metrics, "metrics", "source_files", "source_lines"); err != nil {
		return err
	}
	for _, field := range []string{"source_files", "source_lines"} {
		if !guardrailsAssetsNonNegativeInteger(metrics[field]) {
			return fmt.Errorf("field %q must be a non-negative integer", "metrics."+field)
		}
	}

	languages, err := guardrailsAssetsObjectRecords(blueprint, "languages")
	if err != nil {
		return err
	}
	for index, language := range languages {
		context := fmt.Sprintf("languages[%d]", index)
		if err := rejectGuardrailsAssetsUnknownFields(language, context, "name", "files", "lines"); err != nil {
			return err
		}
		if err := requireGuardrailsAssetsString(language, "name", context); err != nil {
			return err
		}
		for _, field := range []string{"files", "lines"} {
			if !guardrailsAssetsNonNegativeInteger(language[field]) {
				return fmt.Errorf("field %q must be a non-negative integer", context+"."+field)
			}
		}
	}

	components, err := guardrailsAssetsObjectRecords(blueprint, "components")
	if err != nil {
		return err
	}
	allowedComponentKinds := map[string]bool{
		"service": true, "frontend": true, "worker": true, "job": true,
		"cli": true, "library": true, "infrastructure": true, "unknown": true,
	}
	for index, component := range components {
		context := fmt.Sprintf("components[%d]", index)
		if err := rejectGuardrailsAssetsUnknownFields(
			component,
			context,
			"id", "name", "kind", "root", "entrypoints", "languages", "evidence",
		); err != nil {
			return err
		}
		for _, field := range []string{"id", "name", "kind", "root"} {
			if err := requireGuardrailsAssetsString(component, field, context); err != nil {
				return err
			}
		}
		if kind := component["kind"].(string); !allowedComponentKinds[kind] {
			return fmt.Errorf("field %q has unsupported value %q", context+".kind", kind)
		}
		for _, field := range []string{"entrypoints", "languages"} {
			if err := requireGuardrailsAssetsStringArray(component, field, context); err != nil {
				return err
			}
		}
		if err := validateGuardrailsAssetsEvidence(component, "evidence", context); err != nil {
			return err
		}
	}

	for _, field := range []string{"frameworks", "databases", "orms"} {
		records, recordsErr := guardrailsAssetsObjectRecords(blueprint, field)
		if recordsErr != nil {
			return recordsErr
		}
		for index, record := range records {
			context := fmt.Sprintf("%s[%d]", field, index)
			if err := rejectGuardrailsAssetsUnknownFields(
				record,
				context,
				"name", "component_ids", "evidence",
			); err != nil {
				return err
			}
			if err := requireGuardrailsAssetsString(record, "name", context); err != nil {
				return err
			}
			if err := requireGuardrailsAssetsStringArray(record, "component_ids", context); err != nil {
				return err
			}
			if err := validateGuardrailsAssetsEvidence(record, "evidence", context); err != nil {
				return err
			}
		}
	}
	return requireGuardrailsAssetsStringArray(blueprint, "unknowns", "blueprint")
}

func guardrailsAssetsObjectRecords(record map[string]any, field string) ([]map[string]any, error) {
	values, ok := record[field].([]any)
	if !ok {
		return nil, fmt.Errorf("field %q must be an array", field)
	}
	result := make([]map[string]any, 0, len(values))
	for index, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("field %q item %d must be an object", field, index)
		}
		result = append(result, item)
	}
	return result, nil
}

func requireGuardrailsAssetsString(record map[string]any, field, context string) error {
	if _, ok := record[field].(string); !ok {
		return fmt.Errorf("field %q must be a string", context+"."+field)
	}
	return nil
}

func requireGuardrailsAssetsStringArray(record map[string]any, field, context string) error {
	values, ok := record[field].([]any)
	if !ok {
		return fmt.Errorf("field %q must be an array", context+"."+field)
	}
	for index, value := range values {
		if _, ok := value.(string); !ok {
			return fmt.Errorf("field %q item %d must be a string", context+"."+field, index)
		}
	}
	return nil
}

func validateGuardrailsAssetsEvidence(record map[string]any, field, context string) error {
	values, ok := record[field].([]any)
	if !ok {
		return fmt.Errorf("field %q must be an array", context+"."+field)
	}
	for index, value := range values {
		evidence, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("field %q item %d must be an object", context+"."+field, index)
		}
		evidenceContext := fmt.Sprintf("%s.%s[%d]", context, field, index)
		if err := rejectGuardrailsAssetsUnknownFields(evidence, evidenceContext, "path", "quote"); err != nil {
			return err
		}
		for _, evidenceField := range []string{"path", "quote"} {
			if err := requireGuardrailsAssetsString(evidence, evidenceField, evidenceContext); err != nil {
				return err
			}
		}
	}
	return nil
}

func rejectGuardrailsAssetsUnknownFields(
	record map[string]any,
	context string,
	allowed ...string,
) error {
	allowedFields := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		allowedFields[field] = struct{}{}
	}
	for field := range record {
		if _, ok := allowedFields[field]; !ok {
			return fmt.Errorf("field %q is not supported", context+"."+field)
		}
	}
	return nil
}

func completedGuardrailsAssetsError(repositoryID, artifact string, cause error) *clierrors.CLIError {
	return guardrailsAssetsError(
		"GUARDRAILS_BASELINE_INCOMPLETE",
		fmt.Sprintf("stored baseline %q is not complete: could not load %s: %v", repositoryID, artifact, cause),
		clierrors.ExitGeneralError,
	)
}

func guardrailsAssetsAvailableIDs(entries []guardrailsAssetsEntry) []string {
	ids := make([]string, len(entries))
	for i := range entries {
		ids[i] = entries[i].id
	}
	sort.Strings(ids)
	return ids
}

func resolveGuardrailsAssetsEntry(
	entries []guardrailsAssetsEntry,
	requested string,
	load func(guardrailsAssetsEntry) (*guardrailsAssetsBaseline, error),
) (guardrailsAssetsEntry, *guardrailsAssetsBaseline, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		if len(entries) == 1 {
			baseline, err := load(entries[0])
			return entries[0], baseline, err
		}
		return guardrailsAssetsEntry{}, nil, guardrailsAssetsSelectionError(
			"more than one stored baseline is available; select one with --repo",
			entries,
		)
	}

	for i := range entries {
		if requested == entries[i].id {
			entry := entries[i]
			baseline, err := load(entry)
			return entry, baseline, err
		}
	}
	return guardrailsAssetsEntry{}, nil, guardrailsAssetsSelectionError(
		fmt.Sprintf("stored baseline repository %q was not found", requested),
		entries,
	)
}

func guardrailsAssetsSelectionError(message string, entries []guardrailsAssetsEntry) *clierrors.CLIError {
	ids := guardrailsAssetsAvailableIDs(entries)
	err := guardrailsAssetsError(
		"GUARDRAILS_BASELINE_REPOSITORY_REQUIRED",
		fmt.Sprintf("%s (available repository IDs: %s)", message, strings.Join(ids, ", ")),
		clierrors.ExitUsageError,
	)
	err.Context = map[string]any{"available_repositories": ids}
	return err
}

func runGuardrailsAssets(
	cmd *cobra.Command,
	storeRoot, requestedRepository, explicitFormat string,
	deps guardrailsAssetsDependencies,
) error {
	format, err := guardrailsAssetsOutputFormat(explicitFormat)
	if err != nil {
		return err
	}
	entries, err := loadGuardrailsAssetsRegistry(storeRoot)
	if err != nil {
		return err
	}

	if format == output.JSON {
		_, baseline, err := resolveGuardrailsAssetsEntry(entries, requestedRepository, loadGuardrailsAssetsBaseline)
		if err != nil {
			return err
		}
		err = output.WriteString(cmd.OutOrStdout(), output.FormatJSON(guardrailsAssetsJSONPayload(baseline))+"\n")
		return wrapGuardrailsAssetsError("GUARDRAILS_BASELINE_OUTPUT_FAILED", "could not write baseline output", err)
	}

	if !deps.interactive() {
		_, baseline, err := resolveGuardrailsAssetsEntry(entries, requestedRepository, loadGuardrailsAssetsBaseline)
		if err != nil {
			return err
		}
		rendered, err := deps.static(baseline)
		if err != nil {
			return wrapGuardrailsAssetsError(
				"GUARDRAILS_BASELINE_INVALID",
				"could not render the stored baseline",
				err,
			)
		}
		err = output.WriteString(cmd.OutOrStdout(), rendered)
		return wrapGuardrailsAssetsError("GUARDRAILS_BASELINE_OUTPUT_FAILED", "could not write baseline output", err)
	}

	if strings.TrimSpace(requestedRepository) != "" {
		_, baseline, err := resolveGuardrailsAssetsEntry(entries, requestedRepository, loadGuardrailsAssetsBaseline)
		if err != nil {
			return err
		}
		outcome, err := deps.browse(baseline)
		if err != nil {
			return wrapGuardrailsAssetsError(
				"GUARDRAILS_BASELINE_WORKSPACE_FAILED",
				"could not browse the stored baseline",
				err,
			)
		}
		if outcome == guardrailsAssetsBrowseCancelled {
			return writeGuardrailsAssetsCancelled(cmd)
		}
		return nil
	}

	err = browseGuardrailsAssets(entries, deps)
	if errors.Is(err, errGuardrailsAssetsCancelled) {
		return writeGuardrailsAssetsCancelled(cmd)
	}
	return err
}

func writeGuardrailsAssetsCancelled(cmd *cobra.Command) error {
	err := output.WriteString(cmd.OutOrStdout(), "Cancelled.\n")
	return wrapGuardrailsAssetsError(
		"GUARDRAILS_BASELINE_OUTPUT_FAILED",
		"could not write cancellation output",
		err,
	)
}

func guardrailsAssetsOutputFormat(explicit string) (output.OutputFormat, error) {
	explicit = strings.ToLower(strings.TrimSpace(explicit))
	if explicit != "" && explicit != "json" && explicit != "table" {
		return output.JSON, guardrailsAssetsError(
			"INVALID_ARGUMENTS",
			fmt.Sprintf("unsupported output format %q; use table or json", explicit),
			clierrors.ExitUsageError,
		)
	}
	return output.DetectOutputFormat(explicit), nil
}

func browseGuardrailsAssets(entries []guardrailsAssetsEntry, deps guardrailsAssetsDependencies) error {
	type repositoryChoice struct {
		entry  guardrailsAssetsEntry
		option guardrailsAssetsRepositoryOption
	}

	options, err := guardrailsAssetsRepositoryOptions(entries)
	if err != nil {
		return err
	}
	choices := make([]repositoryChoice, len(entries))
	for i := range entries {
		choices[i] = repositoryChoice{entry: entries[i], option: options[i]}
	}
	sort.SliceStable(choices, func(i, j int) bool {
		left := strings.ToLower(choices[i].option.DisplayName)
		right := strings.ToLower(choices[j].option.DisplayName)
		if left != right {
			return left < right
		}
		if choices[i].option.DisplayName != choices[j].option.DisplayName {
			return choices[i].option.DisplayName < choices[j].option.DisplayName
		}
		return choices[i].option.ID < choices[j].option.ID
	})
	entries = make([]guardrailsAssetsEntry, len(choices))
	for i := range choices {
		entries[i] = choices[i].entry
		options[i] = choices[i].option
	}
	selected := 0
	for {
		index, opened, err := deps.pick(options, selected)
		if err != nil {
			if errors.Is(err, output.ErrBaselineCancelled) {
				return errGuardrailsAssetsCancelled
			}
			return guardrailsAssetsError(
				"GUARDRAILS_BASELINE_SELECTION_FAILED",
				fmt.Sprintf("could not select a stored baseline: %v", err),
				clierrors.ExitGeneralError,
			)
		}
		if !opened {
			return nil
		}
		if index < 0 || index >= len(options) {
			return guardrailsAssetsError(
				"GUARDRAILS_BASELINE_SELECTION_FAILED",
				"the stored baseline picker returned an invalid selection",
				clierrors.ExitGeneralError,
			)
		}
		selected = index
		entry := entries[index]
		baseline, err := loadGuardrailsAssetsBaseline(entry)
		if err != nil {
			return err
		}
		outcome, err := deps.browse(baseline)
		if err != nil {
			return wrapGuardrailsAssetsError(
				"GUARDRAILS_BASELINE_WORKSPACE_FAILED",
				"could not browse the stored baseline",
				err,
			)
		}
		if outcome == guardrailsAssetsBrowseCancelled {
			return errGuardrailsAssetsCancelled
		}
		if outcome != guardrailsAssetsBrowseBack {
			return nil
		}
	}
}

func guardrailsAssetsRepositoryOptions(
	entries []guardrailsAssetsEntry,
) ([]guardrailsAssetsRepositoryOption, error) {
	options := make([]guardrailsAssetsRepositoryOption, len(entries))
	for i := range entries {
		baseline, err := loadGuardrailsAssetsBaseline(entries[i])
		if err != nil {
			return nil, err
		}
		options[i] = guardrailsAssetsRepositoryOption{
			ID:          entries[i].id,
			DisplayName: entries[i].id,
			Commit:      guardrailsAssetsCommit(baseline),
		}
	}
	return options, nil
}

func guardrailsAssetsCommit(baseline *guardrailsAssetsBaseline) string {
	for _, source := range []map[string]any{
		baseline.protections,
		getMap(baseline.protections, "source"),
		baseline.blueprint,
		getMap(baseline.blueprint, "source"),
		getMap(baseline.blueprint, "repo"),
	} {
		for _, key := range []string{"commit", "commit_sha", "git_commit_sha"} {
			if value, _ := source[key].(string); strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func guardrailsAssetsJSONPayload(baseline *guardrailsAssetsBaseline) map[string]any {
	repository := make(map[string]any, len(baseline.entry.raw)+1)
	for key, value := range baseline.entry.raw {
		repository[key] = value
	}
	repository["id"] = baseline.entry.id
	return map[string]any{
		"repository": repository,
		"run":        baseline.run,
		"blueprint":  baseline.blueprint,
		"baseline":   baseline.protections,
	}
}

func defaultGuardrailsAssetsDependencies() guardrailsAssetsDependencies {
	return guardrailsAssetsDependencies{
		interactive: output.BaselineTerminalInteractive,
		pick: func(options []guardrailsAssetsRepositoryOption, selected int) (int, bool, error) {
			pickerOptions := make([]output.BaselineRepositoryOption, len(options))
			for i := range options {
				pickerOptions[i] = output.BaselineRepositoryOption{
					ID:          options[i].ID,
					DisplayName: options[i].DisplayName,
					Commit:      options[i].Commit,
				}
			}
			return output.PickBaselineRepository(pickerOptions, selected)
		},
		browse: func(baseline *guardrailsAssetsBaseline) (guardrailsAssetsBrowseOutcome, error) {
			workspace, err := output.NewBaselineWorkspace(
				baseline.protections,
				baseline.blueprint,
				baseline.entry.id,
				guardrailsAssetsCommit(baseline),
			)
			if err != nil {
				return guardrailsAssetsBrowseQuit, err
			}
			outcome, err := workspace.Browse()
			switch outcome {
			case output.BaselineWorkspaceBack:
				return guardrailsAssetsBrowseBack, err
			case output.BaselineWorkspaceCancelled:
				return guardrailsAssetsBrowseCancelled, err
			default:
				return guardrailsAssetsBrowseQuit, err
			}
		},
		static: renderGuardrailsAssetsStatic,
	}
}

func renderGuardrailsAssetsStatic(baseline *guardrailsAssetsBaseline) (string, error) {
	workspace, err := output.NewBaselineWorkspace(
		baseline.protections,
		baseline.blueprint,
		baseline.entry.id,
		guardrailsAssetsCommit(baseline),
	)
	if err != nil {
		return "", err
	}
	return workspace.StaticSummary(), nil
}

func init() {
	guardrailsAssetsCmd.Flags().String("repo", "", "Stored repository ID to browse")
	guardrailsAssetsCmd.Flags().StringP("output", "o", "", "Output format: table, json")
	guardrailsCmd.AddCommand(guardrailsAssetsCmd)
}
