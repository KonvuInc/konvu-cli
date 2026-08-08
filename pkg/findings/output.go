package findings

import (
	"encoding/json"
	"fmt"

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
	format, _ := cmd.Flags().GetString("output")
	w := cmd.OutOrStdout()

	switch format {
	case "json", "":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	case "table":
		return renderTable(cmd, rows, columns)
	case "csv":
		return renderCSV(cmd, rows, columns)
	default:
		return fmt.Errorf("unknown output format: %q (use json, table, or csv)", format)
	}
}

// RenderBareIDs writes one ID per line for -q. idKey names the row key
// holding the identifier; today all finding types use "id" (SAST's "id"
// is the investigation ID after transformDetection promotes it).
func RenderBareIDs(cmd *cobra.Command, rows []Row, idKey string) error {
	w := cmd.OutOrStdout()
	for _, r := range rows {
		if v, ok := r[idKey].(string); ok {
			if _, err := fmt.Fprintln(w, v); err != nil {
				return err
			}
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
