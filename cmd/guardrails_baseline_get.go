package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	baselinemodel "github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var guardrailsBaselineGetCmd = newGuardrailsBaselineGetCmd()

func newGuardrailsBaselineGetCmd() *cobra.Command {
	var includes []string
	var explicitFormat string
	command := &cobra.Command{
		Use:   "get <run-id>",
		Short: "Get a baseline run by ID",
		Long: `Get one locally stored baseline run by its exact ID.

Table output defaults to a run summary. JSON output defaults to the complete
baseline document. Use --include to select architecture, counts, stages, cost,
usage, unknowns, or log.`,
		Example: `  konvu guardrails baseline get <run-id>
  konvu guardrails baseline get <run-id> --include architecture,counts,stages
  konvu guardrails baseline get <run-id> --include log -o json`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runGuardrailsBaselineCommand(cmd, func() error {
				format, err := guardrailsBaselineOutputFormat(explicitFormat)
				if err != nil {
					return err
				}
				store, err := defaultGuardrailsBaselineStore()
				if err != nil {
					return wrapGuardrailsBaselineError(err)
				}
				return writeGuardrailsBaselineGet(cmd.OutOrStdout(), store, args[0], includes, format)
			})
		},
	}
	command.Flags().StringSliceVarP(
		&includes,
		"include",
		"i",
		nil,
		"Data to include: architecture,counts,stages,cost,usage,unknowns,log",
	)
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	return command
}

func writeGuardrailsBaselineGet(
	writer io.Writer,
	store baselinemodel.Store,
	runID string,
	includes []string,
	format output.OutputFormat,
) error {
	runID = strings.TrimSpace(runID)
	run, found, err := findGuardrailsBaselineRun(store, runID)
	if err != nil {
		return err
	}
	if !found {
		return guardrailsBaselineError(
			"GUARDRAILS_BASELINE_NOT_FOUND",
			fmt.Sprintf("run %q was not found", runID),
			clierrors.ExitNotFound,
		)
	}
	includeSet, err := guardrailsBaselineIncludeSet(includes)
	if err != nil {
		return err
	}
	if len(includeSet) == 0 {
		if format == output.JSON && run.Valid && run.Document != nil {
			if run.Run.Status == baselinemodel.StatusCompleted {
				catalog, catalogErr := baselinemodel.NewCatalog(run.Document)
				if catalogErr != nil {
					return wrapGuardrailsBaselineError(catalogErr)
				}
				return writeGuardrailsBaselineOutput(writer, output.FormatJSON(catalog.Raw())+"\n")
			}
			return writeGuardrailsBaselineOutput(writer, output.FormatJSON(run.Document.Raw())+"\n")
		}
		return writeGuardrailsBaselineRunSummary(writer, *run)
	}
	if !run.Valid || run.Document == nil {
		return guardrailsBaselineError(
			"GUARDRAILS_BASELINE_INVALID",
			fmt.Sprintf("run %q cannot provide included data: %s", run.ID, run.Problem),
			clierrors.ExitGeneralError,
		)
	}
	payload, err := guardrailsBaselineGetPayload(*run, includeSet)
	if err != nil {
		return err
	}
	if format == output.JSON {
		return writeGuardrailsBaselineOutput(writer, output.FormatJSON(payload)+"\n")
	}
	return writeGuardrailsBaselineGetTable(writer, *run, payload, includeSet)
}

func guardrailsBaselineIncludeSet(values []string) (map[string]bool, error) {
	result := make(map[string]bool)
	for _, value := range values {
		for _, include := range strings.Split(value, ",") {
			include = strings.ToLower(strings.TrimSpace(include))
			if include == "" {
				continue
			}
			switch include {
			case "architecture", "counts", "stages", "cost", "usage", "unknowns", "log":
				result[include] = true
			default:
				return nil, guardrailsBaselineError(
					"INVALID_ARGUMENTS",
					fmt.Sprintf("unsupported include %q; use architecture, counts, stages, cost, usage, unknowns, or log", include),
					clierrors.ExitUsageError,
				)
			}
		}
	}
	return result, nil
}

func guardrailsBaselineGetPayload(
	run baselinemodel.RunEntry,
	includeSet map[string]bool,
) (map[string]any, error) {
	raw := run.Document.Raw()
	counts := run.Counts
	if run.Run.Status == baselinemodel.StatusCompleted {
		catalog, err := baselinemodel.NewCatalog(run.Document)
		if err != nil {
			return nil, wrapGuardrailsBaselineError(err)
		}
		raw = catalog.Raw()
		counts = catalog.Counts()
	}
	rawRun, _ := raw["run"].(map[string]any)
	codebase, _ := raw["codebase"].(map[string]any)
	payload := map[string]any{"run": guardrailsBaselineRunValue(run)}
	if includeSet["architecture"] {
		payload["architecture"] = codebase
	}
	if includeSet["counts"] {
		payload["counts"] = guardrailsBaselineCountsValue(counts)
	}
	for _, include := range []string{"stages", "cost", "usage"} {
		if includeSet[include] {
			payload[include] = rawRun[include]
		}
	}
	if includeSet["unknowns"] {
		payload["unknowns"] = codebase["unknowns"]
	}
	if includeSet["log"] {
		log, err := readGuardrailsBaselineLog(run)
		if err != nil {
			return nil, guardrailsBaselineError(
				"GUARDRAILS_BASELINE_INVALID",
				fmt.Sprintf("could not read run log for %q: %v", run.ID, err),
				clierrors.ExitGeneralError,
			)
		}
		payload["log"] = log
	}
	return payload, nil
}

func guardrailsBaselineCountsValue(counts baselinemodel.Counts) map[string]any {
	return map[string]any{
		"assets":               counts.Assets,
		"asset_observations":   counts.AssetObservations,
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

func writeGuardrailsBaselineGetTable(
	writer io.Writer,
	run baselinemodel.RunEntry,
	payload map[string]any,
	includeSet map[string]bool,
) error {
	if err := writeGuardrailsBaselineRunSummary(writer, run); err != nil {
		return err
	}
	order := []string{"architecture", "counts", "stages", "cost", "usage", "unknowns", "log"}
	for _, include := range order {
		if !includeSet[include] {
			continue
		}
		if err := writeGuardrailsBaselineOutput(writer, "\n"+strings.ToUpper(include[:1])+include[1:]+"\n\n"); err != nil {
			return err
		}
		if include == "log" {
			if err := writeGuardrailsBaselineOutput(writer, fmt.Sprint(payload[include])); err != nil {
				return err
			}
			continue
		}
		if include == "stages" {
			if err := writeGuardrailsBaselineStagesTable(writer, payload[include]); err != nil {
				return err
			}
			continue
		}
		if include == "architecture" {
			value, _ := payload[include].(map[string]any)
			summary, fields := guardrailsBaselineArchitectureSummary(value)
			if err := writeGuardrailsBaselineFields(writer, summary, fields); err != nil {
				return err
			}
			continue
		}
		if include == "unknowns" {
			if err := writeGuardrailsBaselineValuesTable(writer, payload[include]); err != nil {
				return err
			}
			continue
		}
		value, _ := payload[include].(map[string]any)
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if err := writeGuardrailsBaselineFields(writer, value, keys); err != nil {
			return err
		}
	}
	return nil
}

func guardrailsBaselineArchitectureSummary(value map[string]any) (map[string]any, []string) {
	summary := map[string]any{
		"name":       value["name"],
		"path":       value["path"],
		"layout":     value["layout"],
		"summary":    value["summary"],
		"languages":  guardrailsBaselineNamedValues(value["languages"], false),
		"components": guardrailsBaselineNamedValues(value["components"], true),
		"frameworks": guardrailsBaselineNamedValues(value["frameworks"], false),
		"databases":  guardrailsBaselineNamedValues(value["databases"], false),
		"orms":       guardrailsBaselineNamedValues(value["orms"], false),
	}
	if metrics, ok := value["metrics"].(map[string]any); ok {
		summary["source_files"] = metrics["source_files"]
		summary["source_lines"] = metrics["source_lines"]
	}
	return summary, []string{
		"name", "path", "layout", "summary", "source_files", "source_lines",
		"languages", "components", "frameworks", "databases", "orms",
	}
}

func guardrailsBaselineNamedValues(value any, includeKind bool) string {
	items, _ := value.([]any)
	names := make([]string, 0, len(items))
	for _, item := range items {
		record, _ := item.(map[string]any)
		name, _ := record["name"].(string)
		if name == "" {
			continue
		}
		if includeKind {
			if kind, _ := record["kind"].(string); kind != "" {
				name += " (" + kind + ")"
			}
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func writeGuardrailsBaselineStagesTable(writer io.Writer, value any) error {
	items, _ := value.([]any)
	rows := make([]any, 0, len(items))
	for _, item := range items {
		stage, _ := item.(map[string]any)
		rows = append(rows, map[string]any{
			"name":             stage["name"],
			"status":           stage["status"],
			"duration_seconds": stage["duration_seconds"],
			"summary":          stage["summary"],
		})
	}
	return writeGuardrailsBaselineOutput(writer, output.FormatTable(
		map[string]any{"stages": rows},
		[]string{"name", "status", "duration_seconds", "summary"},
		"stages",
		nil,
	))
}

func writeGuardrailsBaselineValuesTable(writer io.Writer, value any) error {
	items, _ := value.([]any)
	rows := make([]any, 0, len(items))
	for index, item := range items {
		rows = append(rows, map[string]any{"number": index + 1, "value": guardrailsBaselineDisplayValue(item)})
	}
	return writeGuardrailsBaselineOutput(writer, output.FormatTable(
		map[string]any{"values": rows}, []string{"number", "value"}, "values", nil,
	))
}

func init() {
	guardrailsBaselineCmd.AddCommand(guardrailsBaselineGetCmd)
}
