package cmd

import (
	"encoding/json"
	"io"
	"strings"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	baselinemodel "github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var guardrailsBaselineDiffCmd = newGuardrailsBaselineDiffCmd()

func newGuardrailsBaselineDiffCmd() *cobra.Command {
	var collectionName, explicitFormat string
	command := &cobra.Command{
		Use:   "diff <base-run> <head-run>",
		Short: "Compare two completed baseline runs",
		Long:  "Compare collection counts and added, removed, or changed record IDs between two completed runs.",
		Example: `  konvu guardrails baseline diff <base-run> <head-run>
  konvu guardrails baseline diff <base-run> <head-run> --collection controls -o json`,
		Args: cobra.ExactArgs(2),
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
				return writeGuardrailsBaselineDiff(
					cmd.OutOrStdout(), store, args[0], args[1], collectionName, format,
				)
			})
		},
	}
	command.Flags().StringVar(&collectionName, "collection", "", "Compare only one record collection")
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	return command
}

func writeGuardrailsBaselineDiff(
	writer io.Writer,
	store baselinemodel.Store,
	baseRunID, headRunID, collectionName string,
	format output.OutputFormat,
) error {
	baseRunID, headRunID = strings.TrimSpace(baseRunID), strings.TrimSpace(headRunID)
	if baseRunID == headRunID {
		return guardrailsBaselineError(
			"INVALID_ARGUMENTS",
			"base and head run IDs must be different",
			clierrors.ExitUsageError,
		)
	}
	baseRun, baseCatalog, err := selectGuardrailsBaselineCatalog(store, baselinemodel.Selector{RunID: baseRunID})
	if err != nil {
		return err
	}
	headRun, headCatalog, err := selectGuardrailsBaselineCatalog(store, baselinemodel.Selector{RunID: headRunID})
	if err != nil {
		return err
	}
	collections := baseCatalog.Collections()
	if strings.TrimSpace(collectionName) != "" {
		collection, ok := guardrailsBaselineCollection(collectionName)
		if !ok {
			return guardrailsBaselineUnknownCollection(collectionName)
		}
		collections = []baselinemodel.Collection{collection}
	}
	rows := make([]any, 0, len(collections))
	for _, collection := range collections {
		baseEntities, entityErr := baseCatalog.Entities(collection)
		if entityErr != nil {
			return wrapGuardrailsBaselineError(entityErr)
		}
		headEntities, entityErr := headCatalog.Entities(collection)
		if entityErr != nil {
			return wrapGuardrailsBaselineError(entityErr)
		}
		added, removed, changed := guardrailsBaselineEntityDiff(baseEntities, headEntities)
		rows = append(rows, map[string]any{
			"collection":  string(collection),
			"base":        len(baseEntities),
			"head":        len(headEntities),
			"delta":       len(headEntities) - len(baseEntities),
			"added":       len(added),
			"removed":     len(removed),
			"changed":     len(changed),
			"added_ids":   added,
			"removed_ids": removed,
			"changed_ids": changed,
		})
	}
	payload := map[string]any{
		"base_run":    guardrailsBaselineRunValue(*baseRun),
		"head_run":    guardrailsBaselineRunValue(*headRun),
		"collections": rows,
	}
	if format == output.JSON {
		return writeGuardrailsBaselineOutput(writer, output.FormatJSON(payload)+"\n")
	}
	return writeGuardrailsBaselineOutput(writer, output.FormatTable(
		payload,
		[]string{"collection", "base", "head", "delta", "added", "removed", "changed"},
		"collections",
		nil,
	))
}

func guardrailsBaselineEntityDiff(
	baseEntities, headEntities []baselinemodel.Entity,
) (added, removed, changed []string) {
	base := make(map[string]map[string]any, len(baseEntities))
	head := make(map[string]map[string]any, len(headEntities))
	for _, entity := range baseEntities {
		base[entity.ID] = entity.Value
	}
	for _, entity := range headEntities {
		head[entity.ID] = entity.Value
	}
	for id, value := range head {
		baseValue, found := base[id]
		if !found {
			added = append(added, id)
			continue
		}
		left, _ := json.Marshal(baseValue)
		right, _ := json.Marshal(value)
		if string(left) != string(right) {
			changed = append(changed, id)
		}
	}
	for id := range base {
		if _, found := head[id]; !found {
			removed = append(removed, id)
		}
	}
	guardrailsBaselineSortStrings(added)
	guardrailsBaselineSortStrings(removed)
	guardrailsBaselineSortStrings(changed)
	return added, removed, changed
}

func guardrailsBaselineSortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func init() {
	guardrailsBaselineCmd.AddCommand(guardrailsBaselineDiffCmd)
}
