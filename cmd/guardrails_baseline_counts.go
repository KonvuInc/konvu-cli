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

var guardrailsBaselineCountsCmd = newGuardrailsBaselineCountsCmd()

func newGuardrailsBaselineCountsCmd() *cobra.Command {
	var runID, repository, groupBy, explicitFormat string
	command := &cobra.Command{
		Use:   "counts [run-id]",
		Short: "Count records in a baseline",
		Example: `  konvu guardrails baseline counts <run-id>
  konvu guardrails baseline counts --repo <repository> --group-by collection
  konvu guardrails baseline counts <run-id> --group-by property -o json`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runGuardrailsBaselineCommand(cmd, func() error {
				if len(args) == 1 {
					if strings.TrimSpace(runID) != "" || strings.TrimSpace(repository) != "" {
						return guardrailsBaselineError(
							"INVALID_ARGUMENTS",
							"a run ID argument cannot be combined with --run or --repo",
							clierrors.ExitUsageError,
						)
					}
					runID = args[0]
				}
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
				return writeGuardrailsBaselineCounts(cmd.OutOrStdout(), store, selector, groupBy, format)
			})
		},
	}
	command.Flags().StringVar(&runID, "run", "", "select an exact stored run ID")
	command.Flags().StringVar(&repository, "repo", "", "select the latest completed run for a repository name or absolute path")
	command.Flags().StringVar(&groupBy, "group-by", "collection", "Group by: collection,kind,property,status")
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	return command
}

func writeGuardrailsBaselineCounts(
	writer io.Writer,
	store baselinemodel.Store,
	selector baselinemodel.Selector,
	groupBy string,
	format output.OutputFormat,
) error {
	groupBy = strings.ToLower(strings.TrimSpace(groupBy))
	switch groupBy {
	case "collection", "kind", "property", "status":
	default:
		return guardrailsBaselineError(
			"INVALID_ARGUMENTS",
			fmt.Sprintf("unsupported group %q; use collection, kind, property, or status", groupBy),
			clierrors.ExitUsageError,
		)
	}
	run, catalog, err := selectGuardrailsBaselineCatalog(store, selector)
	if err != nil {
		return err
	}
	counts := make(map[string]int)
	for _, collection := range catalog.Collections() {
		entities, entityErr := catalog.Entities(collection)
		if entityErr != nil {
			return wrapGuardrailsBaselineError(entityErr)
		}
		if groupBy == "collection" {
			counts[string(collection)] = len(entities)
			continue
		}
		for _, entity := range entities {
			value, _ := entity.Value[groupBy].(string)
			value = strings.ToLower(strings.TrimSpace(value))
			if value != "" {
				counts[value]++
			}
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]any, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, map[string]any{"group": key, "count": counts[key]})
	}
	payload := map[string]any{
		"run":      guardrailsBaselineRunValue(*run),
		"group_by": groupBy,
		"counts":   rows,
	}
	if format == output.JSON {
		return writeGuardrailsBaselineOutput(writer, output.FormatJSON(payload)+"\n")
	}
	return writeGuardrailsBaselineOutput(writer, output.FormatTable(
		payload, []string{"group", "count"}, "counts", nil,
	))
}

func init() {
	guardrailsBaselineCmd.AddCommand(guardrailsBaselineCountsCmd)
}
