package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const (
	invariantsPath      = "/services/guardrails/v1/prevent/invariants"
	invariantsGuidePath = invariantsPath + "/guide"
)

// invariantListStatuses are the values `list --status` takes; "all" keeps every live invariant.
var invariantListStatuses = []string{"all", "proposed", "validated"}

var invariantsCmd = &cobra.Command{
	Use:     "invariants",
	Aliases: []string{"invariant"},
	Short:   "Your company's security invariants (security rules)",
	Long: `Security invariants are the rules your code must never break, in your own words.
Konvu maps each one to the controls that keep it true and enforces it on every change.`,
}

func newInvariantsListCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "list",
		Short: "List your company's live invariants, by rank",
		Long: `List your company's live invariants, by rank: the ones Konvu suggested (proposed) and the
active ones (validated). --status keeps only one of the two.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed or refused`,
		Example: `  konvu invariants list
  konvu invariants list --status proposed
  konvu invariants list -o json`,
		RunE: runInvariantsList,
	}
	c.Flags().String("status", "all", "Which invariants: proposed (suggested by Konvu), validated (active) or all")
	c.Flags().StringP("output", "o", "", "Output format: json, table")
	return c
}

func newInvariantsGuideCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "guide",
		Short: "Print Konvu's guide to writing a good security rule",
		Long: `Print Konvu's guide to writing a good security rule: what one is, the right level of
detail, and how to rank candidates. Konvu's own suggestions follow the same text.

The guide prints as Markdown, even when piped. -o json prints {"guide", "version"}; the
version changes whenever the guide does.

Exit codes: 0 success, 1 general error (this Konvu server has no guide yet, or the request
failed), 2 invalid --output, 4 auth failed or refused`,
		Example: `  konvu invariants guide
  konvu invariants guide -o json`,
		Args: cobra.NoArgs,
		RunE: runInvariantsGuide,
	}
	c.Flags().StringP("output", "o", "", "Output format: markdown (the default) or json")
	return c
}

func newInvariantsPushCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "push",
		Short: "Add the invariants you wrote",
		Long: `Add invariants from --file (or stdin with '-'), as validated and attributed to you.

The file is a JSON array of invariants, or an object with an "invariants" array (1 to 10).
Each invariant accepts:
  statement        (required) the rule, at most 200 characters
  why_it_matters   (required) at most 500 characters
  property         (required) authentication, authorization, confidentiality,
                   integrity, availability or non_repudiation
  rank             1 or more; the first free rank when omitted
  repository_id    the repository it was written for
  protected_assets [{"kind": "...", "name": "..."}]

A statement already live comes back unchanged (or validated, if it was a proposal), so a
push is safe to repeat. --dry-run checks everything on the server and writes nothing.

Exit codes: 0 success, 1 general error, 2 invalid invariants, 3 repository not found, 4 auth failed or refused`,
		Example: `  konvu invariants push --file invariants.json --dry-run
  konvu invariants push --file invariants.json
  cat invariants.json | konvu invariants push --file -`,
		RunE: runInvariantsPush,
	}
	c.Flags().String("file", "", "JSON file of invariants, or '-' for stdin")
	c.Flags().Bool("dry-run", false, "Check on the server without writing anything")
	c.Flags().StringP("output", "o", "", "Output format: json, table")
	return c
}

func runInvariantsList(cmd *cobra.Command, args []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	status, _ := cmd.Flags().GetString("status")
	if err := validValues("status", []string{status}, invariantListStatuses); err != nil {
		return err
	}

	client := api.NewClient("", "")
	defer client.Close()
	items, err := client.GetList(invariantsPath, nil)
	if err != nil {
		return invariantsListError(err)
	}
	if status != "all" {
		items = invariantsWithStatus(items, status)
	}
	if format == output.JSON {
		fmt.Fprintln(cmd.OutOrStdout(), output.FormatJSON(items))
		return nil
	}
	rows := make([]any, 0, len(items))
	for _, item := range items {
		m, _ := item.(map[string]any)
		rows = append(rows, map[string]any{
			"rank":      intField(m, "rank"),
			"statement": getStr(m, "statement"),
			"property":  getStr(m, "property"),
			"status":    getStr(m, "status"),
			"controls":  len(getSlice(m, "controls")),
			"id":        getStr(m, "id"),
		})
	}
	// The id goes last: the last column is never truncated, and get, accept and decline take it.
	cols := []string{"rank", "statement", "property", "status", "controls", "id"}
	fmt.Fprint(cmd.OutOrStdout(), output.FormatTable(map[string]any{"invariants": rows}, cols, "invariants", nil))
	return nil
}

// invariantsWithStatus keeps the listed invariants whose status is status. The list is never
// paginated, so filtering it here sees every invariant.
func invariantsWithStatus(items []any, status string) []any {
	kept := make([]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok && getStr(m, "status") == status {
			kept = append(kept, item)
		}
	}
	return kept
}

func runInvariantsGuide(cmd *cobra.Command, args []string) error {
	format, _ := cmd.Flags().GetString("output")
	format = strings.ToLower(strings.TrimSpace(format))
	if format != "" && format != "markdown" && format != "json" {
		return &clierrors.CLIError{
			Code:       "INVALID_ARGUMENTS",
			Message:    fmt.Sprintf("unsupported output format %q", format),
			Suggestion: "Use --output markdown or --output json.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}

	client := api.NewClient("", "")
	defer client.Close()
	resp, err := client.Get(invariantsGuidePath, nil)
	if err != nil {
		return invariantsGuideError(err)
	}
	guide := getStr(resp, "guide")
	if strings.TrimSpace(guide) == "" {
		return &clierrors.CLIError{
			Code:       "GUIDE_EMPTY",
			Message:    "Konvu sent no security rule guide",
			Suggestion: "Run the same command again; if it still fails, contact Konvu support.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	if format == "json" {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(resp)+"\n")
	}
	return output.WriteString(cmd.OutOrStdout(), guideText(guide))
}

// guideText is the guide as the terminal gets it: control characters other than newlines and
// tabs are dropped so the server's text cannot drive the terminal, and it ends with a newline.
func guideText(guide string) string {
	var text strings.Builder
	text.Grow(len(guide) + 1)
	for _, character := range strings.ReplaceAll(guide, "\r\n", "\n") {
		if character == '\n' || character == '\t' || !unicode.IsControl(character) {
			text.WriteRune(character)
		}
	}
	if !strings.HasSuffix(text.String(), "\n") {
		text.WriteByte('\n')
	}
	return text.String()
}

// invariantsGuideError maps a failed guide read. An older server has no guide route: the path
// falls through to the one-invariant route, which answers 404 or 422 (guide is not an id).
func invariantsGuideError(err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == 404 || apiErr.StatusCode == 422) {
		return &clierrors.CLIError{
			Code:       "GUIDE_NOT_AVAILABLE",
			Message:    "this Konvu server does not have the security rule guide yet",
			Suggestion: "The guide comes with a newer Konvu server: try again once yours is updated, or contact Konvu support.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return invariantsListError(err)
}

func runInvariantsPush(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}

	if file == "" {
		return &clierrors.CLIError{
			Code:       "MISSING_FILE",
			Message:    "--file is required",
			Suggestion: "Pass a JSON file of invariants, or '-' to read stdin.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	invariants, err := readInvariantsFile(file)
	if err != nil {
		return &clierrors.CLIError{
			Code:       "INVALID_FILE",
			Message:    err.Error(),
			Suggestion: "Run 'konvu invariants push --help' for the expected format.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}

	client := api.NewClient("", "")
	defer client.Close()
	resp, err := client.Post(invariantsPath, map[string]any{
		"dry_run":    dryRun,
		"invariants": invariants,
	})
	if err != nil {
		return invariantsPushError(err, invariants)
	}
	if format == output.JSON {
		fmt.Fprintln(cmd.OutOrStdout(), output.FormatJSON(resp))
		return nil
	}
	if dryRun {
		output.PrintStderr("Dry run: nothing was written. Run without --dry-run to add them.")
	}
	rows := make([]any, 0)
	for _, r := range getSlice(resp, "invariants") {
		m, _ := r.(map[string]any)
		rows = append(rows, map[string]any{
			"rank":      intField(m, "rank"),
			"result":    getStr(m, "result"),
			"statement": getStr(m, "statement"),
		})
	}
	cols := []string{"rank", "result", "statement"}
	fmt.Fprint(cmd.OutOrStdout(), output.FormatTable(map[string]any{"invariants": rows}, cols, "invariants", nil))
	return nil
}

func readInvariantsFile(path string) ([]any, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", path, err)
	}
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	switch v := parsed.(type) {
	case []any:
		if len(v) > 0 {
			return v, nil
		}
	case map[string]any:
		if items, ok := v["invariants"].([]any); ok && len(items) > 0 {
			return items, nil
		}
	}
	return nil, errors.New(`expected a non-empty JSON array of invariants, or {"invariants": [...]}`)
}

// invariantsListError maps a failed read: no session or a refused caller (exit 4), anything
// else a general error.
func invariantsListError(err error) error {
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return clierrors.NewAuthError(err.Error())
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == 403 {
			return &clierrors.CLIError{
				Code:       "FORBIDDEN",
				Message:    invariantDetail(apiErr),
				Suggestion: "Run 'konvu whoami' to check who you are signed in as, and for which company.",
				ExitCode:   clierrors.ExitAuthFailed,
			}
		}
		return clierrors.NewAPIError(invariantDetail(apiErr))
	}
	return clierrors.NewAPIError(err.Error())
}

// invariantDetail is the server's own reason for refusing a call, fit for a terminal.
func invariantDetail(apiErr *api.APIError) string {
	return sanitizeGuardrailsBaselineText(api.ServerDetail([]byte(strings.TrimPrefix(apiErr.Message, "API error: "))))
}

// invariantsPushError maps a refused push to its next step. It never claims the push was
// saved: the server's own detail says so when it is, and a push is safe to repeat either way.
func invariantsPushError(err error, invariants []any) error {
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return clierrors.NewAuthError(err.Error())
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    err.Error(),
			Suggestion: "A push is safe to repeat: run the same command again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	detail := invariantDetail(apiErr)
	switch {
	case apiErr.StatusCode == 403:
		return &clierrors.CLIError{
			Code:       "FORBIDDEN",
			Message:    detail,
			Suggestion: "Run 'konvu whoami' to check who you are signed in as: pushed invariants are attributed to a person.",
			ExitCode:   clierrors.ExitAuthFailed,
		}
	case apiErr.StatusCode == 409:
		return &clierrors.CLIError{
			Code:       "INVARIANT_RANK_UNAVAILABLE",
			Message:    detail,
			Suggestion: "Run 'konvu invariants list' to see the ranks in use, then push again with a free rank or none.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	case apiErr.StatusCode == 404 && anyRepositoryID(invariants):
		return &clierrors.CLIError{
			Code:       "REPOSITORY_NOT_FOUND",
			Message:    detail,
			Suggestion: "Check repository_id: it must be one of your company's repositories.",
			ExitCode:   clierrors.ExitNotFound,
		}
	case apiErr.StatusCode == 422:
		return &clierrors.CLIError{
			Code:       "INVALID_INVARIANTS",
			Message:    detail,
			Suggestion: "Run 'konvu invariants push --help' for the expected format.",
			ExitCode:   clierrors.ExitUsageError,
		}
	case apiErr.StatusCode >= 500:
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    detail,
			Suggestion: "A push is safe to repeat: run the same command again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return clierrors.NewAPIError(detail)
}

// anyRepositoryID reports whether a pushed invariant names a repository, the only way a
// push can be refused for an unknown one.
func anyRepositoryID(invariants []any) bool {
	for _, item := range invariants {
		if m, ok := item.(map[string]any); ok && m["repository_id"] != nil {
			return true
		}
	}
	return false
}

func init() {
	invariantsCmd.AddCommand(
		newInvariantsListCmd(),
		newInvariantsGuideCmd(),
		newInvariantsGetCmd(),
		newInvariantsPushCmd(),
		newInvariantsAcceptCmd(),
		newInvariantsAmendCmd(),
		newInvariantsDeclineCmd(),
	)
	rootCmd.AddCommand(invariantsCmd)
}
