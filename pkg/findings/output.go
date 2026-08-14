package findings

import (
	"fmt"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

// Row is the per-type row shape produced by cmd/finding_<type>.go's
// transform<Type> function. Keys differ by type; Row is a plain
// map[string]any so each subcommand emits its native schema without
// ceremony.
type Row = map[string]any

// Render writes rows in the format the caller requested via the -o flag.
// columns is used for table and csv; ignored for json.
// Output goes to cmd.OutOrStdout(), so tests can capture via SetOut.
func Render(cmd *cobra.Command, rows []Row, columns []string) error {
	return RenderColumns(cmd, rows, columns, columns)
}

// RenderColumns is like Render but uses distinct column sets for table
// (compact) and csv (report-oriented, may carry wide fields like URLs).
// Both are ignored for json, which dumps every row key.
func RenderColumns(cmd *cobra.Command, rows []Row, tableColumns, csvColumns []string) error {
	format, _ := cmd.Flags().GetString("output")
	w := cmd.OutOrStdout()

	switch format {
	case "json", "":
		_, err := fmt.Fprintln(w, output.FormatJSON(rows))
		return err
	case "table":
		return renderTable(cmd, rows, tableColumns)
	case "csv":
		return renderCSV(cmd, rows, csvColumns)
	default:
		return fmt.Errorf("unknown output format: %q (use json, table, or csv)", format)
	}
}

// RequireJSON returns nil when the current -o flag is empty or "json", and a
// user-facing CLIError otherwise. Use in `get` / `rate` handlers whose
// responses are nested enough that a flat table/csv would drop information.
func RequireJSON(cmd *cobra.Command, op string) error {
	format, _ := cmd.Flags().GetString("output")
	if format == "" || format == "json" {
		return nil
	}
	return &clierrors.CLIError{
		Message:    fmt.Sprintf("`%s` output is not supported for %s", format, op),
		Suggestion: "Use -o json (default). Detail responses are too nested for a flat table.",
	}
}

// RenderBareIDs writes one ID per line for -q. Empty ID strings are skipped
// so downstream `xargs`-style pipelines don't blow up on rows that expose no
// usable identifier (SAST detections without a Konvu investigation, for
// example, have an empty investigation id but are still surfaced in list
// output). idKey names the row key holding the identifier; today all finding
// types use "id".
func RenderBareIDs(cmd *cobra.Command, rows []Row, idKey string) error {
	w := cmd.OutOrStdout()
	for _, r := range rows {
		v, _ := r[idKey].(string)
		if v == "" {
			continue
		}
		if _, err := fmt.Fprintln(w, v); err != nil {
			return err
		}
	}
	return nil
}

func renderTable(cmd *cobra.Command, rows []Row, columns []string) error {
	_, err := fmt.Fprint(cmd.OutOrStdout(), output.FormatTable(wrapRows(rows), columns, "findings", output.DefaultStyleCell))
	return err
}

func renderCSV(cmd *cobra.Command, rows []Row, columns []string) error {
	_, err := fmt.Fprint(cmd.OutOrStdout(), output.FormatCSV(wrapRows(rows), columns, "findings"))
	return err
}

// wrapRows adapts our []Row shape to the map[string]any{"findings": [...]}
// shape pkg/output.FormatTable/FormatCSV expect.
func wrapRows(rows []Row) map[string]any {
	items := make([]any, len(rows))
	for i, r := range rows {
		items[i] = map[string]any(r)
	}
	return map[string]any{"findings": items}
}
