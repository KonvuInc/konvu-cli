package cmd

import (
	"fmt"
	"io"
	"strings"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	baselinemodel "github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var guardrailsBaselineExplainCmd = newGuardrailsBaselineExplainCmd()

func newGuardrailsBaselineExplainCmd() *cobra.Command {
	var runID string
	var repository string
	var collectionName string
	var explicitFormat string
	command := &cobra.Command{
		Use:    "explain <record-id>",
		Short:  "Explain one baseline record and its relationships",
		Hidden: true,
		Long: `Explain one baseline record and its direct relationships.
Use --collection when an ID is represented in more than one baseline section.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runGuardrailsBaselineCommand(cmd, func() error {
				for _, flag := range []struct{ name, value string }{
					{name: "run", value: runID},
					{name: "repo", value: repository},
					{name: "collection", value: collectionName},
				} {
					if err := guardrailsBaselineValidateOptionalFlag(cmd, flag.name, flag.value); err != nil {
						return err
					}
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
				return writeGuardrailsBaselineExplainCollection(
					cmd.OutOrStdout(),
					store,
					args[0],
					selector,
					collectionName,
					format,
				)
			})
		},
	}
	command.Flags().StringVar(&runID, "run", "", "select an exact stored run ID")
	command.Flags().StringVar(&repository, "repo", "", "select the latest completed run for a codebase name or absolute path")
	command.Flags().StringVar(&collectionName, "collection", "", "resolve the record inside one exact baseline collection")
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	return command
}

type guardrailsBaselineRelatedRecord struct {
	Depth  int
	Entity baselinemodel.Entity
}

func writeGuardrailsBaselineExplainDepth(
	writer io.Writer,
	store baselinemodel.Store,
	target string,
	selector baselinemodel.Selector,
	collectionName string,
	depth int,
	format output.OutputFormat,
) error {
	if depth < 1 || depth > 5 {
		return guardrailsBaselineError(
			"INVALID_ARGUMENTS",
			"--depth must be between 1 and 5",
			clierrors.ExitUsageError,
		)
	}
	run, catalog, err := selectGuardrailsBaselineCatalog(store, selector)
	if err != nil {
		return err
	}
	collectionName = strings.ToLower(strings.TrimSpace(collectionName))
	entity, ok, err := lookupGuardrailsBaselineEntity(catalog, target, collectionName)
	if err != nil {
		return err
	}
	if !ok {
		return guardrailsBaselineError(
			"GUARDRAILS_BASELINE_RECORD_NOT_FOUND",
			fmt.Sprintf("baseline record %q was not found", target),
			clierrors.ExitNotFound,
		)
	}
	related := guardrailsBaselineTraverseRelationships(catalog, entity, depth)
	if format == output.JSON {
		values := make([]any, 0, len(related))
		for _, item := range related {
			values = append(values, map[string]any{
				"depth":      item.Depth,
				"collection": string(item.Entity.Collection),
				"record":     item.Entity.Value,
			})
		}
		return writeGuardrailsBaselineOutput(writer, output.FormatJSON(map[string]any{
			"run": guardrailsBaselineRunValue(*run),
			"record": map[string]any{
				"collection": string(entity.Collection),
				"data":       entity.Value,
			},
			"related": values,
		})+"\n")
	}
	if err := writeGuardrailsBaselineOutput(
		writer,
		fmt.Sprintf("%s %s\n\n", guardrailsBaselineCollectionTitle(entity.Collection), sanitizeGuardrailsBaselineText(entity.ID)),
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
	for _, item := range related {
		rows = append(rows, map[string]any{
			"depth":      item.Depth,
			"collection": string(item.Entity.Collection),
			"id":         sanitizeGuardrailsBaselineText(item.Entity.ID),
			"name":       guardrailsBaselineRelatedName(item.Entity.Value),
			"location":   guardrailsBaselineLocation(item.Entity.Value),
		})
	}
	return writeGuardrailsBaselineOutput(writer, output.FormatTable(
		map[string]any{"related": rows},
		[]string{"depth", "collection", "name", "location", "id"},
		"related",
		nil,
	))
}

func guardrailsBaselineTraverseRelationships(
	catalog *baselinemodel.Catalog,
	root baselinemodel.Entity,
	maxDepth int,
) []guardrailsBaselineRelatedRecord {
	key := func(entity baselinemodel.Entity) string {
		return string(entity.Collection) + "\x00" + entity.ID
	}
	seen := map[string]bool{key(root): true}
	frontier := []baselinemodel.Entity{root}
	result := make([]guardrailsBaselineRelatedRecord, 0)
	for currentDepth := 1; currentDepth <= maxDepth && len(frontier) > 0; currentDepth++ {
		next := make([]baselinemodel.Entity, 0)
		for _, current := range frontier {
			for _, related := range catalog.RelatedIn(current.Collection, current.ID) {
				relatedKey := key(related)
				if seen[relatedKey] {
					continue
				}
				seen[relatedKey] = true
				result = append(result, guardrailsBaselineRelatedRecord{Depth: currentDepth, Entity: related})
				next = append(next, related)
			}
		}
		frontier = next
	}
	return result
}

func writeGuardrailsBaselineExplain(
	writer io.Writer,
	store baselinemodel.Store,
	target string,
	selector baselinemodel.Selector,
	format output.OutputFormat,
) error {
	return writeGuardrailsBaselineExplainCollection(
		writer,
		store,
		target,
		selector,
		"",
		format,
	)
}

func writeGuardrailsBaselineExplainCollection(
	writer io.Writer,
	store baselinemodel.Store,
	target string,
	selector baselinemodel.Selector,
	collectionName string,
	format output.OutputFormat,
) error {
	run, catalog, err := selectGuardrailsBaselineCatalog(store, selector)
	if err != nil {
		return err
	}
	collectionName = strings.ToLower(strings.TrimSpace(collectionName))
	entity, ok, err := lookupGuardrailsBaselineEntity(catalog, target, collectionName)
	if err != nil {
		return err
	}
	if !ok {
		return guardrailsBaselineError(
			"GUARDRAILS_BASELINE_RECORD_NOT_FOUND",
			fmt.Sprintf("baseline record %q was not found", target),
			clierrors.ExitNotFound,
		)
	}
	related := catalog.Related(target)
	if collectionName != "" {
		related = catalog.RelatedIn(entity.Collection, target)
	}
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
		fmt.Sprintf(
			"%s %s\n\n",
			guardrailsBaselineCollectionTitle(entity.Collection),
			sanitizeGuardrailsBaselineText(entity.ID),
		),
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
			"id":         sanitizeGuardrailsBaselineText(relatedEntity.ID),
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
	case baselinemodel.CollectionAssetObservations:
		return "Asset observation"
	case baselinemodel.CollectionUnresolved:
		return "Unresolved observation"
	default:
		return "Record"
	}
}

func init() {
	guardrailsBaselineCmd.AddCommand(guardrailsBaselineExplainCmd)
}
