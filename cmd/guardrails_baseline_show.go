package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	baselinemodel "github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var guardrailsBaselineShowCmd = newGuardrailsBaselineShowCmd()

func newGuardrailsBaselineShowCmd() *cobra.Command {
	var runID string
	var repository string
	var showLog bool
	var explicitFormat string
	command := &cobra.Command{
		Use:   "show <run-or-record-id>",
		Short: "Show one baseline run or record",
		Long: `Show a stored run summary or an exact record from a completed baseline.
JSON output for a run is the complete baseline.json. Use --log with an exact
run ID to read execution details for completed, failed, or cancelled runs.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runGuardrailsBaselineCommand(cmd, func() error {
				format, err := guardrailsBaselineOutputFormat(explicitFormat)
				if err != nil {
					return err
				}
				selector, err := guardrailsBaselineSelector(runID, repository)
				if err != nil {
					return err
				}
				store, err := defaultGuardrailsBaselineStore()
				if err != nil {
					return wrapGuardrailsBaselineError(err)
				}
				return writeGuardrailsBaselineShow(
					cmd.OutOrStdout(),
					store,
					args[0],
					selector,
					showLog,
					format,
				)
			})
		},
	}
	command.Flags().StringVar(&runID, "run", "", "select an exact stored run ID for record lookup")
	command.Flags().StringVar(&repository, "repo", "", "select the latest completed run for a codebase name or absolute path")
	command.Flags().BoolVar(&showLog, "log", false, "show run.log for an exact run ID")
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	return command
}

func writeGuardrailsBaselineShow(
	writer io.Writer,
	store baselinemodel.Store,
	target string,
	selector baselinemodel.Selector,
	showLog bool,
	format output.OutputFormat,
) error {
	target = strings.TrimSpace(target)
	if showLog {
		if selector.RunID != "" || selector.Repository != "" {
			return guardrailsBaselineError(
				"INVALID_ARGUMENTS",
				"--log accepts an exact run ID and cannot be combined with --run or --repo",
				clierrors.ExitUsageError,
			)
		}
		run, found, err := findGuardrailsBaselineRun(store, target)
		if err != nil {
			return err
		}
		if !found {
			return guardrailsBaselineError(
				"GUARDRAILS_BASELINE_NOT_FOUND",
				fmt.Sprintf("run %q was not found", target),
				clierrors.ExitNotFound,
			)
		}
		log, err := readGuardrailsBaselineLog(*run)
		if err != nil {
			return guardrailsBaselineError(
				"GUARDRAILS_BASELINE_INVALID",
				fmt.Sprintf("could not read run log for %q: %v", run.ID, err),
				clierrors.ExitGeneralError,
			)
		}
		if format == output.JSON {
			return writeGuardrailsBaselineOutput(
				writer,
				output.FormatJSON(map[string]any{
					"run": guardrailsBaselineRunValue(*run),
					"log": log,
				})+"\n",
			)
		}
		return writeGuardrailsBaselineOutput(writer, log)
	}

	exactRun, found, err := findGuardrailsBaselineRun(store, target)
	if err != nil {
		return err
	}
	if found {
		if !exactRun.Valid {
			if format == output.Table {
				return writeGuardrailsBaselineRunSummary(writer, *exactRun)
			}
			_, selectErr := store.Select(baselinemodel.Selector{RunID: target})
			return wrapGuardrailsBaselineError(selectErr)
		}
		if format == output.JSON {
			return writeGuardrailsBaselineOutput(
				writer,
				output.FormatJSON(exactRun.Document.Raw())+"\n",
			)
		}
		return writeGuardrailsBaselineRunSummary(writer, *exactRun)
	}

	_, catalog, err := selectGuardrailsBaselineCatalog(store, selector)
	if err != nil {
		return err
	}
	entity, ok := catalog.Lookup(target)
	if !ok {
		return guardrailsBaselineError(
			"GUARDRAILS_BASELINE_RECORD_NOT_FOUND",
			fmt.Sprintf("baseline record %q was not found", target),
			clierrors.ExitNotFound,
		)
	}
	if format == output.JSON {
		return writeGuardrailsBaselineOutput(writer, output.FormatJSON(entity.Value)+"\n")
	}
	return writeGuardrailsBaselineEntity(writer, entity)
}

func findGuardrailsBaselineRun(
	store baselinemodel.Store,
	target string,
) (*baselinemodel.RunEntry, bool, error) {
	runs, err := store.List()
	if err != nil {
		return nil, false, wrapGuardrailsBaselineError(err)
	}
	for index := range runs {
		if runs[index].ID == target {
			return &runs[index], true, nil
		}
	}
	return nil, false, nil
}

func readGuardrailsBaselineLog(run baselinemodel.RunEntry) (string, error) {
	directoryInfo, err := os.Lstat(run.Dir)
	if err != nil {
		return "", err
	}
	if directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() {
		return "", fmt.Errorf("run directory must be a non-symlinked directory")
	}
	info, err := os.Lstat(run.LogPath)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("run.log must be a regular, non-symlinked file")
	}
	value, err := os.ReadFile(run.LogPath)
	if err != nil {
		return "", err
	}
	return string(value), nil
}

func writeGuardrailsBaselineRunSummary(writer io.Writer, run baselinemodel.RunEntry) error {
	value := guardrailsBaselineRunTableValue(run)
	fields := []string{
		"run", "status", "repository", "codebase_path", "commit", "branch", "scanned",
		"duration", "assets", "controls", "implementations", "resources", "routes", "classes",
		"roles", "control_observations", "unresolved",
	}
	if run.Problem != "" {
		fields = append(fields, "problem")
	}
	return writeGuardrailsBaselineFields(writer, value, fields)
}

func writeGuardrailsBaselineEntity(writer io.Writer, entity baselinemodel.Entity) error {
	keys := make([]string, 0, len(entity.Value)+1)
	for key := range entity.Value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return writeGuardrailsBaselineFields(writer, entity.Value, keys)
}

func writeGuardrailsBaselineFields(
	writer io.Writer,
	value map[string]any,
	fields []string,
) error {
	rows := make([]any, 0, len(fields))
	for _, field := range fields {
		rows = append(rows, map[string]any{
			"field": field,
			"value": guardrailsBaselineDisplayValue(value[field]),
		})
	}
	return writeGuardrailsBaselineOutput(
		writer,
		output.FormatTable(
			map[string]any{"fields": rows},
			[]string{"field", "value"},
			"fields",
			nil,
		),
	)
}

func guardrailsBaselineString(value map[string]any, key string) string {
	text, _ := value[key].(string)
	if strings.TrimSpace(text) == "" {
		return "—"
	}
	return sanitizeGuardrailsBaselineText(text)
}

func guardrailsBaselineLocation(value map[string]any) string {
	for _, key := range []string{"location", "decl"} {
		if location := guardrailsBaselineLocationValue(value[key]); location != "" {
			return location
		}
	}
	locations, ok := value["locations"].([]any)
	if ok {
		values := make([]string, 0, len(locations))
		for _, location := range locations {
			if rendered := guardrailsBaselineLocationValue(location); rendered != "" {
				values = append(values, rendered)
			}
		}
		if len(values) > 0 {
			return strings.Join(values, ", ")
		}
	}
	if module := guardrailsBaselineString(value, "module"); module != "—" {
		if line := guardrailsBaselineLineValue(value["line"]); line != "" {
			return module + ":" + line
		}
		return module
	}
	return "—"
}

func guardrailsBaselineLocationValue(value any) string {
	switch location := value.(type) {
	case string:
		return sanitizeGuardrailsBaselineText(location)
	case map[string]any:
		path := ""
		for _, key := range []string{"path", "file", "module"} {
			if candidate, ok := location[key].(string); ok && strings.TrimSpace(candidate) != "" {
				path = sanitizeGuardrailsBaselineText(candidate)
				break
			}
		}
		line := guardrailsBaselineLineValue(location["line"])
		if path != "" && line != "" {
			return path + ":" + line
		}
		if path != "" {
			return path
		}
	}
	return ""
}

func guardrailsBaselineLineValue(value any) string {
	if value == nil {
		return ""
	}
	line := guardrailsBaselineDisplayValue(value)
	if line == "—" || line == "null" {
		return ""
	}
	return line
}

func guardrailsBaselineDisplayValue(value any) string {
	if value == nil {
		return "—"
	}
	if text, ok := value.(string); ok {
		if text == "" {
			return "—"
		}
		return sanitizeGuardrailsBaselineText(text)
	}
	if encoded, err := json.Marshal(value); err == nil {
		return string(encoded)
	}
	return sanitizeGuardrailsBaselineText(fmt.Sprintf("%v", value))
}

func writeGuardrailsBaselineOutput(writer io.Writer, rendered string) error {
	if err := output.WriteString(writer, rendered); err != nil {
		return guardrailsBaselineError(
			"GUARDRAILS_BASELINE_OUTPUT_FAILED",
			fmt.Sprintf("could not write baseline output: %v", err),
			clierrors.ExitGeneralError,
		)
	}
	return nil
}

func init() {
	guardrailsBaselineCmd.AddCommand(guardrailsBaselineShowCmd)
}
