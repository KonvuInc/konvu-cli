package cmd

import (
	"fmt"
	"io"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	baselinemodel "github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var guardrailsBaselineExplainCmd = newGuardrailsBaselineExplainCmd()

func newGuardrailsBaselineExplainCmd() *cobra.Command {
	var runID string
	var repository string
	var explicitFormat string
	command := &cobra.Command{
		Use:   "explain <record-id>",
		Short: "Explain one baseline record and its relationships",
		Args:  cobra.ExactArgs(1),
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
				return writeGuardrailsBaselineExplain(
					cmd.OutOrStdout(),
					store,
					args[0],
					selector,
					format,
				)
			})
		},
	}
	command.Flags().StringVar(&runID, "run", "", "select an exact stored run ID")
	command.Flags().StringVar(&repository, "repo", "", "select the latest completed run for a codebase name or absolute path")
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	return command
}

func writeGuardrailsBaselineExplain(
	writer io.Writer,
	store baselinemodel.Store,
	target string,
	selector baselinemodel.Selector,
	format output.OutputFormat,
) error {
	run, catalog, err := selectGuardrailsBaselineCatalog(store, selector)
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
	related := catalog.Related(target)
	if format == output.JSON {
		relatedValues := make([]any, 0, len(related))
		for _, relatedEntity := range related {
			relatedValues = append(relatedValues, map[string]any{
				"collection": string(relatedEntity.Collection),
				"record":     relatedEntity.Value,
			})
		}
		return writeGuardrailsBaselineOutput(
			writer,
			output.FormatJSON(map[string]any{
				"run": guardrailsBaselineRunValue(*run),
				"record": map[string]any{
					"collection": string(entity.Collection),
					"data":       entity.Value,
				},
				"related": relatedValues,
			})+"\n",
		)
	}

	if err := writeGuardrailsBaselineOutput(
		writer,
		fmt.Sprintf("%s %s\n\n", guardrailsBaselineCollectionTitle(entity.Collection), entity.ID),
	); err != nil {
		return err
	}
	if err := writeGuardrailsBaselineEntity(writer, entity); err != nil {
		return err
	}
	if err := writeGuardrailsBaselineOutput(writer, "\nRelated\n\n"); err != nil {
		return err
	}
	rows := make([]any, 0, len(related))
	for _, relatedEntity := range related {
		rows = append(rows, map[string]any{
			"collection": string(relatedEntity.Collection),
			"id":         relatedEntity.ID,
			"name":       guardrailsBaselineRelatedName(relatedEntity.Value),
			"location":   guardrailsBaselineLocation(relatedEntity.Value),
		})
	}
	return writeGuardrailsBaselineOutput(
		writer,
		output.FormatTable(
			map[string]any{"related": rows},
			[]string{"collection", "name", "location", "id"},
			"related",
			nil,
		),
	)
}

func guardrailsBaselineRelatedName(value map[string]any) string {
	for _, key := range []string{"name", "description", "status"} {
		if text := guardrailsBaselineString(value, key); text != "—" {
			return text
		}
	}
	return "—"
}

func guardrailsBaselineCollectionTitle(collection baselinemodel.Collection) string {
	switch collection {
	case baselinemodel.CollectionAssets:
		return "Asset"
	case baselinemodel.CollectionControls:
		return "Control"
	case baselinemodel.CollectionImplementations:
		return "Implementation"
	case baselinemodel.CollectionResources:
		return "Resource"
	case baselinemodel.CollectionRoutes:
		return "Route"
	case baselinemodel.CollectionClasses:
		return "Class"
	case baselinemodel.CollectionRoles:
		return "Role"
	case baselinemodel.CollectionControlObservations:
		return "Control observation"
	default:
		return "Record"
	}
}

func init() {
	guardrailsBaselineCmd.AddCommand(guardrailsBaselineExplainCmd)
}
