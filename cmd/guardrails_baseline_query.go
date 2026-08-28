package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	baseline "github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const guardrailsBaselineScanSuggestion = "Run 'konvu guardrails baseline scan <codebase>' to create a baseline."

var defaultGuardrailsBaselineStore = baseline.DefaultStore

func guardrailsBaselineOutputFormat(explicit string) (output.OutputFormat, error) {
	explicit = strings.ToLower(strings.TrimSpace(explicit))
	if explicit != "" && explicit != "json" && explicit != "table" {
		return output.JSON, guardrailsBaselineError(
			"INVALID_ARGUMENTS",
			fmt.Sprintf("unsupported output format %q; use table or json", explicit),
			clierrors.ExitUsageError,
		)
	}
	return output.DetectOutputFormat(explicit), nil
}

func guardrailsBaselineSelector(runID, repository string) (baseline.Selector, error) {
	runID = strings.TrimSpace(runID)
	repository = strings.TrimSpace(repository)
	if runID != "" && repository != "" {
		return baseline.Selector{}, guardrailsBaselineError(
			"INVALID_ARGUMENTS",
			"--run and --repo cannot be used together",
			clierrors.ExitUsageError,
		)
	}
	return baseline.Selector{RunID: runID, Repository: repository}, nil
}

func guardrailsBaselineValidateOptionalFlag(
	cmd *cobra.Command,
	name, value string,
) error {
	if !cmd.Flags().Changed(name) || strings.TrimSpace(value) != "" {
		return nil
	}
	return guardrailsBaselineError(
		"INVALID_ARGUMENTS",
		fmt.Sprintf("--%s requires a non-empty value", name),
		clierrors.ExitUsageError,
	)
}

func selectGuardrailsBaselineCatalog(
	store baseline.Store,
	selector baseline.Selector,
) (*baseline.RunEntry, *baseline.Catalog, error) {
	run, err := store.Select(selector)
	if err != nil {
		return nil, nil, wrapGuardrailsBaselineError(err)
	}
	catalog, err := baseline.NewCatalog(run.Document)
	if err != nil {
		return nil, nil, wrapGuardrailsBaselineError(err)
	}
	return run, catalog, nil
}

func guardrailsBaselineError(code, message string, exitCode int) *clierrors.CLIError {
	suggestion := guardrailsBaselineScanSuggestion
	switch code {
	case "INVALID_ARGUMENTS":
		suggestion = "Run 'konvu guardrails baseline --help' to see valid commands and selectors."
	case "GUARDRAILS_BASELINE_NOT_FOUND":
		suggestion = "Run 'konvu guardrails baseline list' to see stored runs."
	case "GUARDRAILS_BASELINE_RECORD_NOT_FOUND":
		suggestion = "Run 'konvu guardrails baseline records search <query> --run <run-id>' to find matching records."
	case "GUARDRAILS_BASELINE_AMBIGUOUS":
		suggestion = "Select an exact run with --run, or an unambiguous codebase with --repo."
	case "GUARDRAILS_BASELINE_INCOMPLETE":
		suggestion = "Get the run with '--include log', or select a completed run."
	case "GUARDRAILS_BASELINE_OUTPUT_FAILED":
		suggestion = "Check that the output destination is writable, then try again."
	case "GUARDRAILS_BASELINE_INVALID":
		suggestion = "Run 'konvu guardrails baseline get <run-id> --include log' for diagnostics."
	}
	return &clierrors.CLIError{
		Code:       code,
		Message:    sanitizeGuardrailsBaselineText(message),
		Suggestion: suggestion,
		ExitCode:   exitCode,
	}
}

func wrapGuardrailsBaselineError(err error) error {
	if err == nil {
		return nil
	}
	var cliErr *clierrors.CLIError
	if errors.As(err, &cliErr) {
		return cliErr
	}
	var baselineErr *baseline.Error
	if errors.As(err, &baselineErr) {
		switch baselineErr.Code {
		case baseline.ErrorRunNotFound:
			return guardrailsBaselineError(
				"GUARDRAILS_BASELINE_NOT_FOUND",
				baselineErr.Error(),
				clierrors.ExitNotFound,
			)
		case baseline.ErrorRunAmbiguous:
			return guardrailsBaselineError(
				"GUARDRAILS_BASELINE_AMBIGUOUS",
				baselineErr.Error(),
				clierrors.ExitUsageError,
			)
		case baseline.ErrorRunIncomplete:
			return guardrailsBaselineError(
				"GUARDRAILS_BASELINE_INCOMPLETE",
				baselineErr.Error(),
				clierrors.ExitGeneralError,
			)
		default:
			return guardrailsBaselineError(
				"GUARDRAILS_BASELINE_INVALID",
				baselineErr.Error(),
				clierrors.ExitGeneralError,
			)
		}
	}
	return guardrailsBaselineError(
		"GUARDRAILS_BASELINE_INVALID",
		fmt.Sprintf("could not read stored baselines: %v", err),
		clierrors.ExitGeneralError,
	)
}

func runGuardrailsBaselineCommand(cmd *cobra.Command, operation func() error) {
	if err := operation(); err != nil {
		explicit, _ := cmd.Flags().GetString("output")
		os.Exit(reportGuardrailsBaselineCommandError(cmd, err, explicit))
	}
}

func reportGuardrailsBaselineCommandError(
	cmd *cobra.Command,
	err error,
	explicitFormat string,
) int {
	wrapped := wrapGuardrailsBaselineError(err)
	cliErr, ok := wrapped.(*clierrors.CLIError)
	if !ok {
		cliErr = guardrailsBaselineError(
			"GUARDRAILS_BASELINE_INVALID",
			wrapped.Error(),
			clierrors.ExitGeneralError,
		)
	}
	destination := cmd.ErrOrStderr()
	rendered := "Error: " + cliErr.Message + "\n"
	if cliErr.Suggestion != "" {
		rendered += "  " + cliErr.Suggestion + "\n"
	}
	if strings.EqualFold(strings.TrimSpace(explicitFormat), "json") {
		destination = cmd.OutOrStdout()
		rendered = clierrors.FormatErrorJSON(cliErr) + "\n"
	}
	if writeErr := output.WriteString(destination, rendered); writeErr != nil {
		return clierrors.ExitGeneralError
	}
	if cliErr.ExitCode == 0 {
		return clierrors.ExitGeneralError
	}
	return cliErr.ExitCode
}

func sanitizeGuardrailsBaselineText(value string) string {
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

func guardrailsBaselineRunValue(run baseline.RunEntry) map[string]any {
	status := string(run.Run.Status)
	if !run.Valid {
		status = "invalid"
	}
	counts := run.Counts
	if run.Valid && run.Run.Status == baseline.StatusCompleted && run.Document != nil {
		if catalog, err := baseline.NewCatalog(run.Document); err == nil {
			counts = catalog.Counts()
		}
	}
	return map[string]any{
		"id":                   run.ID,
		"status":               status,
		"valid":                run.Valid,
		"problem":              run.Problem,
		"repository":           run.Codebase.Name,
		"codebase_path":        run.Codebase.Path,
		"commit":               run.Codebase.Git.Commit,
		"branch":               run.Codebase.Git.Branch,
		"dirty":                run.Codebase.Git.Dirty,
		"started_at":           run.Run.StartedAt,
		"completed_at":         run.Run.CompletedAt,
		"duration_seconds":     run.Run.DurationSeconds,
		"assets":               counts.Assets,
		"controls":             counts.Controls,
		"implementations":      counts.Implementations,
		"resources":            counts.Resources,
		"routes":               counts.Routes,
		"classes":              counts.Classes,
		"roles":                counts.Roles,
		"control_observations": counts.ControlObservations,
		"unresolved":           counts.Unresolved,
	}
}

func guardrailsBaselineRunTableValue(run baseline.RunEntry) map[string]any {
	value := guardrailsBaselineRunValue(run)
	for _, key := range []string{
		"id", "status", "problem", "repository", "codebase_path", "commit", "branch",
		"started_at", "completed_at",
	} {
		if text, ok := value[key].(string); ok {
			value[key] = sanitizeGuardrailsBaselineText(text)
		}
	}
	value["run"] = value["id"]
	value["scanned"] = value["completed_at"]
	if value["scanned"] == "" {
		value["scanned"] = value["started_at"]
	}
	if !run.Valid {
		value["repository"] = "—"
		value["commit"] = "—"
		value["scanned"] = "—"
		value["duration"] = "—"
		return value
	}
	value["duration"] = formatGuardrailsBaselineDuration(run.Run.DurationSeconds)
	if value["commit"] == "" {
		value["commit"] = "no-commit"
	} else if run.Codebase.Git.Dirty {
		value["commit"] = fmt.Sprintf("%v*", value["commit"])
	}
	return value
}

func formatGuardrailsBaselineDuration(seconds float64) string {
	if seconds == 0 {
		return "0s"
	}
	if seconds < 60 {
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", seconds), "0"), ".") + "s"
	}
	minutes := int(seconds) / 60
	remaining := int(seconds) % 60
	if remaining == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dm%ds", minutes, remaining)
}
