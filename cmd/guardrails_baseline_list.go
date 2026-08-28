package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	baselinemodel "github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const guardrailsBaselineRunsCollection = "runs"

var guardrailsBaselineListCmd = newGuardrailsBaselineListCmd()

func newGuardrailsBaselineListCmd() *cobra.Command {
	var runID string
	var repository string
	var explicitFormat string
	var statuses []string
	var limit int
	var offset int
	var sortBy string
	var order string
	var quiet bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List baseline runs",
		Long: `List locally stored baseline runs from any working directory.

Use --repo to filter by repository name or absolute path. The legacy
'list <collection>' form remains available; use 'baseline records list' for
new scripts and interactive navigation.`,
		Example: `  konvu guardrails baseline list
  konvu guardrails baseline list --repo <repository>
  konvu guardrails baseline list --status completed --limit 20
  konvu guardrails baseline list --repo <repository> -q`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runGuardrailsBaselineCommand(cmd, func() error {
				for _, flag := range []struct{ name, value string }{
					{name: "run", value: runID},
					{name: "repo", value: repository},
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
				if len(args) == 1 {
					return writeGuardrailsBaselineList(
						cmd.OutOrStdout(),
						store,
						strings.ToLower(strings.TrimSpace(args[0])),
						selector,
						format,
					)
				}
				return writeGuardrailsBaselineRunList(
					cmd.OutOrStdout(),
					store,
					selector,
					guardrailsBaselineRunListOptions{
						Statuses: statuses,
						Limit:    limit,
						Offset:   offset,
						Sort:     sortBy,
						Order:    order,
						Quiet:    quiet,
					},
					format,
				)
			})
		},
	}
	command.Flags().StringVar(&runID, "run", "", "filter by exact run ID")
	command.Flags().StringVar(&repository, "repo", "", "filter runs or select a codebase by name or absolute path")
	command.Flags().StringSliceVar(&statuses, "status", nil, "Filter by status: running,completed,failed,cancelled,invalid")
	command.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum runs to return")
	command.Flags().IntVar(&offset, "offset", 0, "Skip N runs")
	command.Flags().StringVar(&sortBy, "sort", "scanned", "Sort by: scanned,repository,status,duration")
	command.Flags().StringVar(&order, "order", "desc", "Order: asc,desc")
	command.Flags().StringVarP(&explicitFormat, "output", "o", "", "Output format: table, json")
	command.Flags().BoolVarP(&quiet, "quiet", "q", false, "Print only run IDs")
	return command
}

type guardrailsBaselineRunListOptions struct {
	Statuses []string
	Limit    int
	Offset   int
	Sort     string
	Order    string
	Quiet    bool
}

func writeGuardrailsBaselineRunList(
	writer io.Writer,
	store baselinemodel.Store,
	selector baselinemodel.Selector,
	options guardrailsBaselineRunListOptions,
	format output.OutputFormat,
) error {
	runs, err := store.List()
	if err != nil {
		return wrapGuardrailsBaselineError(err)
	}
	runs, err = filterGuardrailsBaselineRuns(runs, selector)
	if err != nil {
		return err
	}
	runs, err = filterAndPageGuardrailsBaselineRuns(runs, options)
	if err != nil {
		return err
	}
	if options.Quiet {
		ids := make([]string, 0, len(runs))
		for _, run := range runs {
			ids = append(ids, sanitizeGuardrailsBaselineText(run.ID))
		}
		value := strings.Join(ids, "\n")
		if value != "" {
			value += "\n"
		}
		return writeGuardrailsBaselineOutput(writer, value)
	}
	return writeGuardrailsBaselineRunsValue(writer, runs, format)
}

func filterAndPageGuardrailsBaselineRuns(
	runs []baselinemodel.RunEntry,
	options guardrailsBaselineRunListOptions,
) ([]baselinemodel.RunEntry, error) {
	if options.Limit < 1 || options.Limit > 1000 {
		return nil, guardrailsBaselineError(
			"INVALID_ARGUMENTS",
			"--limit must be between 1 and 1000",
			clierrors.ExitUsageError,
		)
	}
	if options.Offset < 0 {
		return nil, guardrailsBaselineError(
			"INVALID_ARGUMENTS",
			"--offset must be zero or greater",
			clierrors.ExitUsageError,
		)
	}
	statusSet := make(map[string]bool)
	for _, value := range options.Statuses {
		for _, status := range strings.Split(value, ",") {
			status = strings.ToLower(strings.TrimSpace(status))
			switch status {
			case "running", "completed", "failed", "cancelled", "invalid":
				statusSet[status] = true
			default:
				return nil, guardrailsBaselineError(
					"INVALID_ARGUMENTS",
					fmt.Sprintf("unsupported status %q; use running, completed, failed, cancelled, or invalid", status),
					clierrors.ExitUsageError,
				)
			}
		}
	}
	if len(statusSet) > 0 {
		filtered := make([]baselinemodel.RunEntry, 0, len(runs))
		for _, run := range runs {
			status := string(run.Run.Status)
			if !run.Valid {
				status = "invalid"
			}
			if statusSet[status] {
				filtered = append(filtered, run)
			}
		}
		runs = filtered
	}
	sortBy := strings.ToLower(strings.TrimSpace(options.Sort))
	if sortBy == "" {
		sortBy = "scanned"
	}
	switch sortBy {
	case "scanned", "repository", "status", "duration":
	default:
		return nil, guardrailsBaselineError(
			"INVALID_ARGUMENTS",
			fmt.Sprintf("unsupported sort %q; use scanned, repository, status, or duration", options.Sort),
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
	sort.SliceStable(runs, func(i, j int) bool {
		if sortBy == "duration" && runs[i].Run.DurationSeconds != runs[j].Run.DurationSeconds {
			if order == "asc" {
				return runs[i].Run.DurationSeconds < runs[j].Run.DurationSeconds
			}
			return runs[i].Run.DurationSeconds > runs[j].Run.DurationSeconds
		}
		left, right := guardrailsBaselineRunSortValue(runs[i], sortBy), guardrailsBaselineRunSortValue(runs[j], sortBy)
		if left == right {
			left, right = runs[i].ID, runs[j].ID
		}
		if order == "asc" {
			return left < right
		}
		return left > right
	})
	if options.Offset >= len(runs) {
		return []baselinemodel.RunEntry{}, nil
	}
	runs = runs[options.Offset:]
	if len(runs) > options.Limit {
		runs = runs[:options.Limit]
	}
	return runs, nil
}

func guardrailsBaselineRunSortValue(run baselinemodel.RunEntry, sortBy string) string {
	switch sortBy {
	case "repository":
		return strings.ToLower(run.Codebase.Name)
	case "status":
		if !run.Valid {
			return "invalid"
		}
		return string(run.Run.Status)
	default:
		if run.Run.CompletedAt != "" {
			return run.Run.CompletedAt
		}
		return run.Run.StartedAt
	}
}

func writeGuardrailsBaselineList(
	writer io.Writer,
	store baselinemodel.Store,
	collectionName string,
	selector baselinemodel.Selector,
	format output.OutputFormat,
) error {
	if collectionName == guardrailsBaselineRunsCollection || collectionName == "" {
		return writeGuardrailsBaselineRuns(writer, store, selector, format)
	}

	collection, ok := guardrailsBaselineCollection(collectionName)
	if !ok {
		return guardrailsBaselineError(
			"INVALID_ARGUMENTS",
			fmt.Sprintf(
				"unknown baseline collection %q; use assets, asset-observations, controls, implementations, resources, routes, classes, roles, control-observations, or unresolved",
				collectionName,
			),
			clierrors.ExitUsageError,
		)
	}
	run, catalog, err := selectGuardrailsBaselineCatalog(store, selector)
	if err != nil {
		return err
	}
	entities, err := catalog.Entities(collection)
	if err != nil {
		return wrapGuardrailsBaselineError(err)
	}

	if format == output.JSON {
		values := make([]any, 0, len(entities))
		for _, entity := range entities {
			values = append(values, entity.Value)
		}
		payload := map[string]any{
			"run":                                 guardrailsBaselineRunValue(*run),
			guardrailsBaselineJSONKey(collection): values,
		}
		return writeGuardrailsBaselineOutput(writer, output.FormatJSON(payload)+"\n")
	}

	rows, columns := guardrailsBaselineCollectionRows(catalog, collection, entities)
	data := map[string]any{"records": rows}
	return writeGuardrailsBaselineOutput(
		writer,
		output.FormatTable(data, columns, "records", nil),
	)
}

func writeGuardrailsBaselineRuns(
	writer io.Writer,
	store baselinemodel.Store,
	selector baselinemodel.Selector,
	format output.OutputFormat,
) error {
	runs, err := store.List()
	if err != nil {
		return wrapGuardrailsBaselineError(err)
	}
	runs, err = filterGuardrailsBaselineRuns(runs, selector)
	if err != nil {
		return err
	}
	return writeGuardrailsBaselineRunsValue(writer, runs, format)
}

func writeGuardrailsBaselineRunsValue(
	writer io.Writer,
	runs []baselinemodel.RunEntry,
	format output.OutputFormat,
) error {
	values := make([]any, 0, len(runs))
	for _, run := range runs {
		if format == output.JSON {
			values = append(values, guardrailsBaselineRunValue(run))
		} else {
			values = append(values, guardrailsBaselineRunTableValue(run))
		}
	}
	if format == output.JSON {
		return writeGuardrailsBaselineOutput(
			writer,
			output.FormatJSON(map[string]any{"runs": values})+"\n",
		)
	}
	return writeGuardrailsBaselineOutput(
		writer,
		output.FormatTable(
			map[string]any{"runs": values},
			[]string{"repository", "commit", "scanned", "duration", "assets", "controls", "implementations", "status", "run"},
			"runs",
			nil,
		),
	)
}

func filterGuardrailsBaselineRuns(
	runs []baselinemodel.RunEntry,
	selector baselinemodel.Selector,
) ([]baselinemodel.RunEntry, error) {
	if selector.RunID == "" && selector.Repository == "" {
		return runs, nil
	}
	filtered := make([]baselinemodel.RunEntry, 0, len(runs))
	if selector.RunID != "" {
		for _, run := range runs {
			if run.ID == selector.RunID {
				filtered = append(filtered, run)
			}
		}
	} else {
		repository := strings.TrimSpace(selector.Repository)
		absolute := filepath.IsAbs(repository)
		if absolute {
			repository = filepath.Clean(repository)
		}
		for _, run := range runs {
			if !run.Valid {
				continue
			}
			if (absolute && run.Codebase.Path == repository) ||
				(!absolute && run.Codebase.Name == repository) {
				filtered = append(filtered, run)
			}
		}
		if !absolute {
			paths := make(map[string]bool)
			for _, run := range filtered {
				paths[run.Codebase.Path] = true
			}
			if len(paths) > 1 {
				return nil, guardrailsBaselineError(
					"GUARDRAILS_BASELINE_AMBIGUOUS",
					fmt.Sprintf("repository name %q matches more than one stored path", repository),
					clierrors.ExitUsageError,
				)
			}
		}
	}
	if len(filtered) == 0 {
		target := selector.RunID
		if target == "" {
			target = selector.Repository
		}
		return nil, guardrailsBaselineError(
			"GUARDRAILS_BASELINE_NOT_FOUND",
			fmt.Sprintf("no stored baseline runs matched %q", target),
			clierrors.ExitNotFound,
		)
	}
	return filtered, nil
}

func guardrailsBaselineCollection(name string) (baselinemodel.Collection, bool) {
	collection := baselinemodel.Collection(strings.ToLower(strings.TrimSpace(name)))
	switch collection {
	case baselinemodel.CollectionAssets,
		baselinemodel.CollectionAssetObservations,
		baselinemodel.CollectionControls,
		baselinemodel.CollectionImplementations,
		baselinemodel.CollectionResources,
		baselinemodel.CollectionRoutes,
		baselinemodel.CollectionClasses,
		baselinemodel.CollectionRoles,
		baselinemodel.CollectionControlObservations,
		baselinemodel.CollectionUnresolved:
		return collection, true
	default:
		return "", false
	}
}

func guardrailsBaselineJSONKey(collection baselinemodel.Collection) string {
	switch collection {
	case baselinemodel.CollectionAssetObservations:
		return "asset_observations"
	case baselinemodel.CollectionControlObservations:
		return "control_observations"
	}
	return string(collection)
}

func guardrailsBaselineCollectionRows(
	catalog *baselinemodel.Catalog,
	collection baselinemodel.Collection,
	entities []baselinemodel.Entity,
) ([]any, []string) {
	rows := make([]any, 0, len(entities))
	columns := []string{"id", "name", "kind", "location"}
	for _, entity := range entities {
		value := entity.Value
		row := map[string]any{
			"id":       sanitizeGuardrailsBaselineText(entity.ID),
			"name":     guardrailsBaselineString(value, "name"),
			"kind":     guardrailsBaselineString(value, "kind"),
			"location": guardrailsBaselineLocation(value),
		}
		switch collection {
		case baselinemodel.CollectionAssets:
			row["controls"] = len(catalog.LinksForAsset(entity.ID))
			columns = []string{"kind", "name", "controls", "location", "id"}
		case baselinemodel.CollectionAssetObservations:
			row["description"] = guardrailsBaselineString(value, "description")
			columns = []string{"name", "description", "location", "id"}
		case baselinemodel.CollectionControls:
			links := catalog.LinksForControl(entity.ID)
			row["property"] = guardrailsBaselineString(value, "property")
			row["assets"] = len(guardrailsBaselineLinkAssetIDs(links))
			row["implementations"] = len(guardrailsBaselineLinkImplementationIDs(links))
			columns = []string{"property", "name", "assets", "implementations", "id"}
		case baselinemodel.CollectionImplementations:
			row["assets"] = len(guardrailsBaselineLinkAssetIDs(catalog.LinksForImplementation(entity.ID)))
			columns = []string{"kind", "name", "assets", "location", "id"}
		case baselinemodel.CollectionResources:
			columns = []string{"kind", "name", "location", "id"}
		case baselinemodel.CollectionRoutes:
			row["method"] = guardrailsBaselineString(value, "method")
			row["path"] = guardrailsBaselineString(value, "path")
			row["handler"] = guardrailsBaselineString(value, "handler")
			columns = []string{"method", "path", "handler", "location", "id"}
		case baselinemodel.CollectionClasses, baselinemodel.CollectionRoles:
			columns = []string{"name", "location", "id"}
		case baselinemodel.CollectionControlObservations:
			row["asset"] = guardrailsBaselineString(value, "asset_id")
			row["property"] = guardrailsBaselineString(value, "property")
			row["status"] = guardrailsBaselineString(value, "status")
			row["description"] = guardrailsBaselineString(value, "description")
			columns = []string{"asset", "property", "status", "description", "id"}
		case baselinemodel.CollectionUnresolved:
			row["observation"] = guardrailsBaselineString(value, "control_observation_id")
			row["reason"] = guardrailsBaselineString(value, "reason")
			row["checked"] = guardrailsBaselineDisplayValue(value["checked"])
			columns = []string{"reason", "checked", "observation"}
		}
		rows = append(rows, row)
	}
	return rows, columns
}

func guardrailsBaselineLinkAssetIDs(links []baselinemodel.ControlLink) []string {
	values := make(map[string]bool)
	for _, link := range links {
		values[link.AssetID] = true
	}
	return guardrailsBaselineSortedKeys(values)
}

func guardrailsBaselineLinkImplementationIDs(links []baselinemodel.ControlLink) []string {
	values := make(map[string]bool)
	for _, link := range links {
		for _, id := range link.ImplementationIDs {
			values[id] = true
		}
	}
	return guardrailsBaselineSortedKeys(values)
}

func guardrailsBaselineSortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func init() {
	guardrailsBaselineCmd.AddCommand(guardrailsBaselineListCmd)
}
