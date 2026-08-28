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

var guardrailsBaselineRecordsCmd = &cobra.Command{
	Use:   "records",
	Short: "Explore records within a baseline",
	Long: `Explore records within one completed baseline.

Record collections are assets, asset-observations, controls, implementations,
resources, routes, classes, roles, control-observations, and unresolved.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

type guardrailsBaselineRecordListOptions struct {
	Collection  string
	Kind        string
	Property    string
	Status      string
	HasControls bool
	Limit       int
	Offset      int
	Sort        string
	Order       string
	Quiet       bool
}

func newGuardrailsBaselineRecordsListCmd() *cobra.Command {
	var runID, repository, explicitFormat string
	var options guardrailsBaselineRecordListOptions
	command := &cobra.Command{
		Use:   "list",
		Short: "List records in one collection",
		Example: `  konvu guardrails baseline records list --run <run-id> --collection assets
  konvu guardrails baseline records list --repo <repository> --collection controls -q
  konvu guardrails baseline records list --run <run-id> --collection assets --kind object --has-controls --limit 25`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			runGuardrailsBaselineCommand(cmd, func() error {
				for _, flag := range []struct{ name, value string }{
					{name: "run", value: runID},
					{name: "repo", value: repository},
					{name: "collection", value: options.Collection},
				} {
					if err := guardrailsBaselineValidateOptionalFlag(cmd, flag.name, flag.value); err != nil {
						return err
					}
				}
				if strings.TrimSpace(options.Collection) == "" {
					return guardrailsBaselineError(
						"INVALID_ARGUMENTS",
						"--collection is required",
						clierrors.ExitUsageError,
					)
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
				return writeGuardrailsBaselineRecordsList(cmd.OutOrStdout(), store, selector, options, format)
			})
		},
	}
	command.Flags().StringVar(&runID, "run", "", "select an exact stored run ID")
	command.Flags().StringVar(&repository, "repo", "", "select the latest completed run for a repository name or absolute path")
	command.Flags().StringVar(&options.Collection, "collection", "", "Record collection to list (required)")
	command.Flags().StringVar(&options.Kind, "kind", "", "Filter by record kind")
	command.Flags().StringVar(&options.Property, "property", "", "Filter by security property")
	command.Flags().StringVar(&options.Status, "status", "", "Filter by record status")
	command.Flags().BoolVar(&options.HasControls, "has-controls", false, "Only include records linked to controls")
	command.Flags().IntVarP(&options.Limit, "limit", "n", 50, "Maximum records to return")
	command.Flags().IntVar(&options.Offset, "offset", 0, "Skip N records")
	command.Flags().StringVar(&options.Sort, "sort", "id", "Sort by: id,name,kind,property,status,location")
	command.Flags().StringVar(&options.Order, "order", "asc", "Order: asc,desc")
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	command.Flags().BoolVarP(&options.Quiet, "quiet", "q", false, "Print only record IDs")
	return command
}

func writeGuardrailsBaselineRecordsList(
	writer io.Writer,
	store baselinemodel.Store,
	selector baselinemodel.Selector,
	options guardrailsBaselineRecordListOptions,
	format output.OutputFormat,
) error {
	collection, ok := guardrailsBaselineCollection(options.Collection)
	if !ok {
		return guardrailsBaselineUnknownCollection(options.Collection)
	}
	run, catalog, err := selectGuardrailsBaselineCatalog(store, selector)
	if err != nil {
		return err
	}
	entities, err := catalog.Entities(collection)
	if err != nil {
		return wrapGuardrailsBaselineError(err)
	}
	entities, err = filterAndPageGuardrailsBaselineEntities(catalog, entities, options)
	if err != nil {
		return err
	}
	if options.Quiet {
		return writeGuardrailsBaselineEntityIDs(writer, entities)
	}
	if format == output.JSON {
		values := make([]any, 0, len(entities))
		for _, entity := range entities {
			values = append(values, entity.Value)
		}
		return writeGuardrailsBaselineOutput(writer, output.FormatJSON(map[string]any{
			"run":                                 guardrailsBaselineRunValue(*run),
			guardrailsBaselineJSONKey(collection): values,
		})+"\n")
	}
	rows, columns := guardrailsBaselineCollectionRows(catalog, collection, entities)
	return writeGuardrailsBaselineOutput(writer, output.FormatTable(
		map[string]any{"records": rows}, columns, "records", nil,
	))
}

func filterAndPageGuardrailsBaselineEntities(
	catalog *baselinemodel.Catalog,
	entities []baselinemodel.Entity,
	options guardrailsBaselineRecordListOptions,
) ([]baselinemodel.Entity, error) {
	if err := guardrailsBaselineValidatePage(options.Limit, options.Offset); err != nil {
		return nil, err
	}
	filtered := make([]baselinemodel.Entity, 0, len(entities))
	for _, entity := range entities {
		if !guardrailsBaselineEqualOptional(entity.Value, "kind", options.Kind) ||
			!guardrailsBaselineEqualOptional(entity.Value, "property", options.Property) ||
			!guardrailsBaselineEqualOptional(entity.Value, "status", options.Status) {
			continue
		}
		if options.HasControls && len(guardrailsBaselineEntityControlLinks(catalog, entity)) == 0 {
			continue
		}
		filtered = append(filtered, entity)
	}
	sortBy := strings.ToLower(strings.TrimSpace(options.Sort))
	switch sortBy {
	case "id", "name", "kind", "property", "status", "location":
	default:
		return nil, guardrailsBaselineError(
			"INVALID_ARGUMENTS",
			fmt.Sprintf("unsupported sort %q; use id, name, kind, property, status, or location", options.Sort),
			clierrors.ExitUsageError,
		)
	}
	order := strings.ToLower(strings.TrimSpace(options.Order))
	if order != "asc" && order != "desc" {
		return nil, guardrailsBaselineError(
			"INVALID_ARGUMENTS",
			fmt.Sprintf("unsupported order %q; use asc or desc", options.Order),
			clierrors.ExitUsageError,
		)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left := guardrailsBaselineEntitySortValue(filtered[i], sortBy)
		right := guardrailsBaselineEntitySortValue(filtered[j], sortBy)
		if left == right {
			left, right = filtered[i].ID, filtered[j].ID
		}
		if order == "desc" {
			return left > right
		}
		return left < right
	})
	return guardrailsBaselinePageEntities(filtered, options.Offset, options.Limit), nil
}

func guardrailsBaselineEqualOptional(value map[string]any, key, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	actual, _ := value[key].(string)
	return strings.EqualFold(strings.TrimSpace(actual), expected)
}

func guardrailsBaselineEntityControlLinks(
	catalog *baselinemodel.Catalog,
	entity baselinemodel.Entity,
) []baselinemodel.ControlLink {
	switch entity.Collection {
	case baselinemodel.CollectionAssets:
		return catalog.LinksForAsset(entity.ID)
	case baselinemodel.CollectionControls:
		return catalog.LinksForControl(entity.ID)
	case baselinemodel.CollectionImplementations:
		return catalog.LinksForImplementation(entity.ID)
	case baselinemodel.CollectionControlObservations:
		return catalog.LinksForObservation(entity.ID)
	default:
		return nil
	}
}

func guardrailsBaselineEntitySortValue(entity baselinemodel.Entity, sortBy string) string {
	switch sortBy {
	case "name":
		return strings.ToLower(guardrailsBaselineRelatedName(entity.Value))
	case "kind", "property", "status":
		value, _ := entity.Value[sortBy].(string)
		return strings.ToLower(value)
	case "location":
		return strings.ToLower(guardrailsBaselineLocation(entity.Value))
	default:
		return strings.ToLower(entity.ID)
	}
}

type guardrailsBaselineSearchOptions struct {
	Collections []string
	Limit       int
	Offset      int
	Quiet       bool
}

func newGuardrailsBaselineRecordsSearchCmd() *cobra.Command {
	var runID, repository, explicitFormat string
	var options guardrailsBaselineSearchOptions
	command := &cobra.Command{
		Use:   "search <query>",
		Short: "Search records across baseline collections",
		Long:  "Search record IDs, names, descriptions, source locations, evidence, and other string fields.",
		Example: `  konvu guardrails baseline records search "manual override" --run <run-id>
  konvu guardrails baseline records search auth --repo <repository> --collection assets,controls
  konvu guardrails baseline records search shared/config.py --run <run-id> -q`,
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
				return writeGuardrailsBaselineRecordsSearch(
					cmd.OutOrStdout(), store, selector, args[0], options, format,
				)
			})
		},
	}
	command.Flags().StringVar(&runID, "run", "", "select an exact stored run ID")
	command.Flags().StringVar(&repository, "repo", "", "select the latest completed run for a repository name or absolute path")
	command.Flags().StringSliceVar(&options.Collections, "collection", nil, "Search only these collections (repeatable or comma-separated)")
	command.Flags().IntVarP(&options.Limit, "limit", "n", 50, "Maximum matches to return")
	command.Flags().IntVar(&options.Offset, "offset", 0, "Skip N matches")
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	command.Flags().BoolVarP(&options.Quiet, "quiet", "q", false, "Print only record IDs")
	return command
}

type guardrailsBaselineSearchMatch struct {
	Entity baselinemodel.Entity
}

func writeGuardrailsBaselineRecordsSearch(
	writer io.Writer,
	store baselinemodel.Store,
	selector baselinemodel.Selector,
	query string,
	options guardrailsBaselineSearchOptions,
	format output.OutputFormat,
) error {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return guardrailsBaselineError("INVALID_ARGUMENTS", "search query cannot be empty", clierrors.ExitUsageError)
	}
	if err := guardrailsBaselineValidatePage(options.Limit, options.Offset); err != nil {
		return err
	}
	run, catalog, err := selectGuardrailsBaselineCatalog(store, selector)
	if err != nil {
		return err
	}
	collections, err := guardrailsBaselineSearchCollections(catalog, options.Collections)
	if err != nil {
		return err
	}
	matches := make([]guardrailsBaselineSearchMatch, 0)
	for _, collection := range collections {
		entities, entityErr := catalog.Entities(collection)
		if entityErr != nil {
			return wrapGuardrailsBaselineError(entityErr)
		}
		for _, entity := range entities {
			if strings.Contains(strings.ToLower(entity.ID), query) || guardrailsBaselineValueContains(entity.Value, query) {
				matches = append(matches, guardrailsBaselineSearchMatch{Entity: entity})
			}
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Entity.Collection != matches[j].Entity.Collection {
			return matches[i].Entity.Collection < matches[j].Entity.Collection
		}
		return matches[i].Entity.ID < matches[j].Entity.ID
	})
	if options.Offset >= len(matches) {
		matches = nil
	} else {
		matches = matches[options.Offset:]
	}
	if len(matches) > options.Limit {
		matches = matches[:options.Limit]
	}
	if options.Quiet {
		entities := make([]baselinemodel.Entity, 0, len(matches))
		for _, match := range matches {
			entities = append(entities, match.Entity)
		}
		return writeGuardrailsBaselineEntityIDs(writer, entities)
	}
	if format == output.JSON {
		values := make([]any, 0, len(matches))
		for _, match := range matches {
			values = append(values, map[string]any{
				"collection": string(match.Entity.Collection),
				"record":     match.Entity.Value,
			})
		}
		return writeGuardrailsBaselineOutput(writer, output.FormatJSON(map[string]any{
			"run":     guardrailsBaselineRunValue(*run),
			"query":   query,
			"matches": values,
		})+"\n")
	}
	rows := make([]any, 0, len(matches))
	for _, match := range matches {
		entity := match.Entity
		rows = append(rows, map[string]any{
			"collection": string(entity.Collection),
			"name":       guardrailsBaselineRelatedName(entity.Value),
			"kind":       guardrailsBaselineString(entity.Value, "kind"),
			"property":   guardrailsBaselineString(entity.Value, "property"),
			"status":     guardrailsBaselineString(entity.Value, "status"),
			"location":   guardrailsBaselineLocation(entity.Value),
			"id":         sanitizeGuardrailsBaselineText(entity.ID),
		})
	}
	return writeGuardrailsBaselineOutput(writer, output.FormatTable(
		map[string]any{"matches": rows},
		[]string{"collection", "id", "name"},
		"matches",
		nil,
	))
}

func guardrailsBaselineSearchCollections(
	catalog *baselinemodel.Catalog,
	requested []string,
) ([]baselinemodel.Collection, error) {
	if len(requested) == 0 {
		return catalog.Collections(), nil
	}
	seen := make(map[baselinemodel.Collection]bool)
	collections := make([]baselinemodel.Collection, 0, len(requested))
	for _, values := range requested {
		for _, value := range strings.Split(values, ",") {
			collection, ok := guardrailsBaselineCollection(value)
			if !ok {
				return nil, guardrailsBaselineUnknownCollection(value)
			}
			if !seen[collection] {
				seen[collection] = true
				collections = append(collections, collection)
			}
		}
	}
	sort.Slice(collections, func(i, j int) bool { return collections[i] < collections[j] })
	return collections, nil
}

func guardrailsBaselineValueContains(value any, query string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(strings.ToLower(typed), query)
	case []any:
		for _, item := range typed {
			if guardrailsBaselineValueContains(item, query) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if guardrailsBaselineValueContains(item, query) {
				return true
			}
		}
	}
	return false
}

func newGuardrailsBaselineRecordsGetCmd() *cobra.Command {
	var runID, repository, collectionName, explicitFormat string
	command := &cobra.Command{
		Use:   "get <record-id>",
		Short: "Get a baseline record by ID",
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
				return writeGuardrailsBaselineRecordGet(
					cmd.OutOrStdout(), store, args[0], selector, collectionName, format,
				)
			})
		},
	}
	command.Flags().StringVar(&runID, "run", "", "select an exact stored run ID")
	command.Flags().StringVar(&repository, "repo", "", "select the latest completed run for a repository name or absolute path")
	command.Flags().StringVar(&collectionName, "collection", "", "Resolve the record inside one exact collection")
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	return command
}

func writeGuardrailsBaselineRecordGet(
	writer io.Writer,
	store baselinemodel.Store,
	target string,
	selector baselinemodel.Selector,
	collectionName string,
	format output.OutputFormat,
) error {
	_, catalog, err := selectGuardrailsBaselineCatalog(store, selector)
	if err != nil {
		return err
	}
	collectionName = strings.ToLower(strings.TrimSpace(collectionName))
	entity, found, err := lookupGuardrailsBaselineEntity(catalog, strings.TrimSpace(target), collectionName)
	if err != nil {
		return err
	}
	if !found {
		return guardrailsBaselineError(
			"GUARDRAILS_BASELINE_RECORD_NOT_FOUND",
			fmt.Sprintf("baseline record %q was not found", strings.TrimSpace(target)),
			clierrors.ExitNotFound,
		)
	}
	if format == output.JSON {
		return writeGuardrailsBaselineOutput(writer, output.FormatJSON(entity.Value)+"\n")
	}
	value, fields := guardrailsBaselineRecordSummary(catalog, entity)
	return writeGuardrailsBaselineFields(writer, value, fields)
}

func guardrailsBaselineRecordSummary(
	catalog *baselinemodel.Catalog,
	entity baselinemodel.Entity,
) (map[string]any, []string) {
	value := map[string]any{
		"collection":  string(entity.Collection),
		"id":          entity.ID,
		"name":        guardrailsBaselineRelatedName(entity.Value),
		"location":    guardrailsBaselineLocation(entity.Value),
		"description": entity.Value["description"],
	}
	fields := []string{"collection", "id", "name"}
	for _, field := range []string{"kind", "property", "status", "origin", "parent", "method", "path", "handler"} {
		if text, ok := entity.Value[field].(string); ok && strings.TrimSpace(text) != "" {
			value[field] = text
			fields = append(fields, field)
		}
	}
	fields = append(fields, "location")
	if description, _ := entity.Value["description"].(string); strings.TrimSpace(description) != "" {
		fields = append(fields, "description")
	}
	if quote, _ := entity.Value["quote"].(string); strings.TrimSpace(quote) != "" {
		value["quote"] = quote
		fields = append(fields, "quote")
	}
	counts := make(map[baselinemodel.Collection]int)
	related := catalog.RelatedIn(entity.Collection, entity.ID)
	for _, candidate := range related {
		counts[candidate.Collection]++
	}
	for _, relationship := range []struct {
		collection baselinemodel.Collection
		field      string
	}{
		{baselinemodel.CollectionAssets, "related_assets"},
		{baselinemodel.CollectionControls, "related_controls"},
		{baselinemodel.CollectionImplementations, "related_implementations"},
		{baselinemodel.CollectionControlObservations, "related_observations"},
		{baselinemodel.CollectionResources, "related_resources"},
		{baselinemodel.CollectionRoutes, "related_routes"},
		{baselinemodel.CollectionClasses, "related_classes"},
		{baselinemodel.CollectionRoles, "related_roles"},
	} {
		if count := counts[relationship.collection]; count > 0 {
			value[relationship.field] = count
			fields = append(fields, relationship.field)
		}
	}
	return value, fields
}

func newGuardrailsBaselineRecordsExplainCmd() *cobra.Command {
	var runID, repository, collectionName, explicitFormat string
	var depth int
	command := &cobra.Command{
		Use:   "explain <record-id>",
		Short: "Explain a record and its related records",
		Example: `  konvu guardrails baseline records explain <record-id> --run <run-id>
  konvu guardrails baseline records explain <record-id> --repo <repository> --depth 2`,
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
				return writeGuardrailsBaselineExplainDepth(
					cmd.OutOrStdout(), store, args[0], selector, collectionName, depth, format,
				)
			})
		},
	}
	command.Flags().StringVar(&runID, "run", "", "select an exact stored run ID")
	command.Flags().StringVar(&repository, "repo", "", "select the latest completed run for a repository name or absolute path")
	command.Flags().StringVar(&collectionName, "collection", "", "Resolve the record inside one exact collection")
	command.Flags().IntVar(&depth, "depth", 1, "Relationship depth to traverse (1-5)")
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	return command
}

func guardrailsBaselineUnknownCollection(name string) error {
	return guardrailsBaselineError(
		"INVALID_ARGUMENTS",
		fmt.Sprintf(
			"unknown baseline collection %q; use assets, asset-observations, controls, implementations, resources, routes, classes, roles, control-observations, or unresolved",
			strings.TrimSpace(name),
		),
		clierrors.ExitUsageError,
	)
}

func guardrailsBaselineValidatePage(limit, offset int) error {
	if limit < 1 || limit > 1000 {
		return guardrailsBaselineError("INVALID_ARGUMENTS", "--limit must be between 1 and 1000", clierrors.ExitUsageError)
	}
	if offset < 0 {
		return guardrailsBaselineError("INVALID_ARGUMENTS", "--offset must be zero or greater", clierrors.ExitUsageError)
	}
	return nil
}

func guardrailsBaselinePageEntities(
	entities []baselinemodel.Entity,
	offset, limit int,
) []baselinemodel.Entity {
	if offset >= len(entities) {
		return []baselinemodel.Entity{}
	}
	entities = entities[offset:]
	if len(entities) > limit {
		entities = entities[:limit]
	}
	return entities
}

func writeGuardrailsBaselineEntityIDs(writer io.Writer, entities []baselinemodel.Entity) error {
	ids := make([]string, 0, len(entities))
	for _, entity := range entities {
		ids = append(ids, sanitizeGuardrailsBaselineText(entity.ID))
	}
	value := strings.Join(ids, "\n")
	if value != "" {
		value += "\n"
	}
	return writeGuardrailsBaselineOutput(writer, value)
}

func init() {
	guardrailsBaselineRecordsCmd.AddCommand(
		newGuardrailsBaselineRecordsListCmd(),
		newGuardrailsBaselineRecordsSearchCmd(),
		newGuardrailsBaselineRecordsGetCmd(),
		newGuardrailsBaselineRecordsExplainCmd(),
	)
	guardrailsBaselineCmd.AddCommand(guardrailsBaselineRecordsCmd)
}
