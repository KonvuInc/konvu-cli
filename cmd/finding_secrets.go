package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/findings"
	"github.com/spf13/cobra"
)

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Leaked-credential findings from repository secret scanning",
	Long: `Search and rate secret findings.

Findings are grouped by (provider, secret_hash) — the same secret detected
in multiple locations reads as one row. 'get' on any group-member ID returns
the group.

Ratings use a bulk-only endpoint; a single-ID 'rate' internally batches with
one element. Pipe IDs into --stdin to rate many at once (chunked at 500).`,
}

var secretsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List secret findings",
	RunE:  runSecretsList,
}

var secretsGetCmd = &cobra.Command{
	Use:   "get [finding-id]",
	Short: "Get a secret finding (returns the whole group)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSecretsGet,
}

var secretsCountsCmd = &cobra.Command{
	Use:   "counts",
	Short: "Count secret findings (with breakdown by assessment)",
	RunE:  runSecretsCounts,
}

var secretsRateCmd = &cobra.Command{
	Use:   "rate [finding-id] [assessment]",
	Short: "Rate one or many secret findings",
	Long: `Rate a secret finding (single or batch).

Single:   konvu finding secrets rate <id> applicable
Batch:    konvu finding secrets list --assessment unknown -q | konvu finding secrets rate --stdin applicable

Valid assessments: applicable, unknown, not_applicable.
The bulk endpoint applies to whole (provider, secret_hash) groups; a rating
on one location propagates to every finding of the same secret.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSecretsRate,
}

var validSecretsAssessments = map[string]bool{
	"applicable":     true,
	"unknown":        true,
	"not_applicable": true,
}

const secretsRateBatch = 500

func transformSecretGroup(raw map[string]any) findings.Row {
	return findings.Row{
		"id":                  getStr(raw, "id"),
		"provider":            getStr(raw, "provider"),
		"verification_status": getStr(raw, "verification_status"),
		"assessment":          getStr(raw, "assessment"),
		"first_seen":          getStr(raw, "first_seen"),
		"last_seen":           getStr(raw, "last_seen"),
	}
}

var secretsDefaultColumns = []string{"id", "provider", "verification_status", "assessment", "first_seen", "last_seen"}

// mapSecretsError converts a 403 (entitlement off) into a friendly CLIError.
// Other errors are wrapped with a generic suggestion.
func mapSecretsError(err error) error {
	if err == nil {
		return nil
	}
	if ae, ok := err.(*api.APIError); ok && ae.StatusCode == 403 {
		return &clierrors.CLIError{
			Message:    "Secret scanning is not enabled for this company.",
			Suggestion: "Enable secret scanning in the Konvu console (Integrations → per-repo settings), then retry.",
		}
	}
	return &clierrors.CLIError{
		Message:    fmt.Sprintf("secret findings: %v", err),
		Suggestion: "Check auth and permissions.",
	}
}

func runSecretsList(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	f := findings.ReadCommonFilters(cmd)
	params := map[string]any{"per_page": f.LimitOr(30), "page": 1}
	if len(f.Assessment) > 1 {
		return &clierrors.CLIError{
			Message:    "secrets --assessment accepts only one value",
			Suggestion: "The /secret_findings endpoint filter is a single value: applicable, unknown, or not_applicable.",
		}
	}
	if len(f.Assessment) == 1 {
		a := f.Assessment[0]
		if !validSecretsAssessments[a] {
			return &clierrors.CLIError{
				Message:    fmt.Sprintf("invalid --assessment value %q for secrets", a),
				Suggestion: "Valid: applicable, unknown, not_applicable.",
			}
		}
		params["assessment"] = a
	}

	resp, err := client.Get("/secret_findings", params)
	if err != nil {
		return mapSecretsError(err)
	}

	items := getSlice(resp, "items")
	rows := make([]findings.Row, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, transformSecretGroup(m))
	}
	if f.QuietIDs {
		return findings.RenderBareIDs(cmd, rows, "id")
	}
	return findings.Render(cmd, rows, secretsDefaultColumns)
}

func runSecretsGet(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	if err := findings.RequireJSON(cmd, "secrets get"); err != nil {
		return err
	}
	resp, err := client.Get(fmt.Sprintf("/secret_findings/%s", args[0]), nil)
	if err != nil {
		return mapSecretsError(err)
	}
	return findings.Render(cmd, []findings.Row{resp}, nil)
}

func runSecretsCounts(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	resp, err := client.Get("/secret_findings", map[string]any{"per_page": 1})
	if err != nil {
		return mapSecretsError(err)
	}
	row := findings.Row{}
	if t, ok := resp["total"].(float64); ok {
		row["total"] = int(t)
	}
	if breakdown, ok := resp["assessment_counts"].(map[string]any); ok {
		for k, v := range breakdown {
			row[k] = v
		}
	}
	return findings.Render(cmd, []findings.Row{row}, []string{"total", "applicable", "unknown", "not_applicable"})
}

func runSecretsRate(cmd *cobra.Command, args []string) error {
	client := api.NewClient("", "")
	defer client.Close()

	fromStdin, _ := cmd.Flags().GetBool("stdin")
	var ids []string
	var assessment string

	if fromStdin {
		if len(args) != 1 {
			return &clierrors.CLIError{
				Message:    "with --stdin, pass exactly one argument: the assessment",
				Suggestion: "Example: <finding IDs from stdin> | konvu finding secrets rate --stdin applicable",
			}
		}
		assessment = args[0]
		s := bufio.NewScanner(os.Stdin)
		for s.Scan() {
			id := strings.TrimSpace(s.Text())
			if id != "" {
				ids = append(ids, id)
			}
		}
		if err := s.Err(); err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
	} else {
		if len(args) != 2 {
			return &clierrors.CLIError{
				Message:    "rate requires: <finding-id> <assessment>",
				Suggestion: "Or pass --stdin to batch IDs from stdin.",
			}
		}
		ids = []string{args[0]}
		assessment = args[1]
	}

	if !validSecretsAssessments[assessment] {
		return &clierrors.CLIError{
			Message:    fmt.Sprintf("invalid assessment %q", assessment),
			Suggestion: "Valid: applicable, unknown, not_applicable.",
		}
	}
	if len(ids) == 0 {
		return &clierrors.CLIError{Message: "no finding IDs provided"}
	}

	totalUpdated := 0
	for start := 0; start < len(ids); start += secretsRateBatch {
		end := start + secretsRateBatch
		if end > len(ids) {
			end = len(ids)
		}
		resp, err := client.Post("/secret_findings/bulk_assessment", map[string]any{
			"finding_ids": ids[start:end],
			"assessment":  assessment,
		})
		if err != nil {
			return mapSecretsError(err)
		}
		if u, ok := resp["updated"].(float64); ok {
			totalUpdated += int(u)
		}
	}
	return findings.Render(cmd, []findings.Row{{"submitted": len(ids), "updated": totalUpdated}}, []string{"submitted", "updated"})
}

func init() {
	// list — assessment + limit + -o + -q (severity/repo/since not supported by the endpoint)
	secretsListCmd.Flags().StringSlice("assessment", nil, "Filter by assessment (repeatable): applicable, unknown, not_applicable")
	secretsListCmd.Flags().Int("limit", 30, "Maximum rows to return (per_page)")
	secretsListCmd.Flags().StringP("output", "o", "", "Output format: json, table, csv")
	secretsListCmd.Flags().BoolP("quiet", "q", false, "Print bare IDs, one per line")

	secretsGetCmd.Flags().StringP("output", "o", "", "Output format: json (default)")

	secretsCountsCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	secretsRateCmd.Flags().Bool("stdin", false, "Read finding IDs from stdin (one per line); batches into 500-max requests")
	secretsRateCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	secretsCmd.AddCommand(secretsListCmd, secretsGetCmd, secretsRateCmd, secretsCountsCmd)
	findingCmd.AddCommand(secretsCmd)
}
