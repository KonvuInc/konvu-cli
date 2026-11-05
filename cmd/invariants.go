package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	"github.com/KonvuInc/konvu-cli/pkg/config"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const (
	invariantsPath      = "/services/guardrails/v1/prevent/invariants"
	invariantsGuidePath = invariantsPath + "/guide"

	// A push with an onboarding code goes here instead, with no login token.
	onboardingRulesPath  = "/onboarding/rules"
	onboardingCodeHeader = "X-Onboarding-Code"
)

// onboardingCodePattern is a code from Konvu's web onboarding once normalized: 8 capital letters
// and digits, without the look-alikes 0, O, 1, I and L. Anything else is refused before a
// request, so it never reaches a header.
var onboardingCodePattern = regexp.MustCompile(`^[ABCDEFGHJKMNPQRSTUVWXYZ23456789]{8}$`)

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

--onboarding <code> saves them without a login, with the 8-character onboarding code from
Konvu's web onboarding, like K7P2XQ9M (any case; spaces and dashes are ignored, so k7p2-xq9m
is the same code). No login token is read or sent. Each comes back saved (valid on a dry
run) or unchanged, and moves into your account when you create it in the browser. The code is
only ever taken from this flag: without it, push is the normal logged-in push. If that
onboarding already has an account, or the code expired, run 'konvu login', then push again
without --onboarding.

Exit codes: 0 success, 1 general error, 2 invalid invariants or onboarding code, 3 repository
not found (with --onboarding: the code is unknown or expired), 4 auth failed or refused (with
--onboarding: the onboarding already has an account, or Konvu refused the code)`,
		Example: `  konvu invariants push --file invariants.json --dry-run
  konvu invariants push --file invariants.json
  cat invariants.json | konvu invariants push --file -
  konvu invariants push --file invariants.json --onboarding K7P2XQ9M`,
		RunE: runInvariantsPush,
	}
	c.Flags().String("file", "", "JSON file of invariants, or '-' for stdin")
	c.Flags().Bool("dry-run", false, "Check on the server without writing anything")
	c.Flags().String("onboarding", "", "8-character onboarding code from Konvu's web onboarding (like K7P2XQ9M): save without a login")
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
	code, err := pushOnboardingCode(cmd)
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
	body := map[string]any{"dry_run": dryRun, "invariants": invariants}
	var resp map[string]any
	if code != nil {
		resp, err = client.PostWithoutLogin(onboardingRulesPath, body, map[string]string{onboardingCodeHeader: code.value})
		if err != nil {
			return onboardingPushError(err, code)
		}
	} else {
		resp, err = client.Post(invariantsPath, body)
		if err != nil {
			return invariantsPushError(err, invariants)
		}
	}
	if format == output.JSON {
		fmt.Fprintln(cmd.OutOrStdout(), output.FormatJSON(resp))
		return nil
	}
	if dryRun {
		output.PrintStderr("Dry run: nothing was written. Run without --dry-run to add them.")
	} else if code != nil {
		output.PrintStderr("Saved to your Konvu onboarding: go back to your Konvu tab and create your account to keep it.")
	}
	rows := make([]any, 0)
	for _, r := range getSlice(resp, "invariants") {
		m, _ := r.(map[string]any)
		// An onboarding push has no rank yet: a blank, not rank 0.
		var rank any = ""
		if f, ok := getFloat(m, "rank"); ok {
			rank = int(f)
		}
		rows = append(rows, map[string]any{
			"rank":      rank,
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

// onboardingCode is the code a push saves with instead of a login, from --onboarding: value is
// normalized (what the header carries), given is what the user typed.
type onboardingCode struct {
	value string
	given string
}

// hint is the code as errors may show it: its first 2 characters only.
func (c *onboardingCode) hint() string {
	return c.value[:2] + "…"
}

// redact replaces the whole code in text (a server's detail can echo a header), as sent or as
// typed, with its hint.
func (c *onboardingCode) redact(text string) string {
	text = strings.ReplaceAll(text, c.value, c.hint())
	if c.given != "" {
		text = strings.ReplaceAll(text, c.given, c.hint())
	}
	return text
}

// normalizeOnboardingCode is the code as Konvu reads it: spaces and dashes dropped, ASCII
// letters uppercased. Any other character is kept, so the pattern refuses it; uppercasing only
// ASCII stops a letter like ſ from turning into a valid S.
func normalizeOnboardingCode(given string) string {
	var code strings.Builder
	for _, r := range given {
		switch {
		case r == '-' || unicode.IsSpace(r):
		case r >= 'a' && r <= 'z':
			code.WriteRune(r - 'a' + 'A')
		default:
			code.WriteRune(r)
		}
	}
	return code.String()
}

// onboardingLoginStep is the way out once the code cannot save: log in, then push without it.
// The code only ever comes from the flag, so dropping the flag is all it takes.
const onboardingLoginStep = "'konvu login', then push again without --onboarding"

// pushOnboardingCode reads the push's onboarding code from --onboarding, the only place it comes
// from: nil means a normal, logged-in push. A code that is given but empty or malformed is
// refused, never treated as absent, so the push cannot fall back to the login unnoticed.
func pushOnboardingCode(cmd *cobra.Command) (*onboardingCode, error) {
	if !cmd.Flags().Changed("onboarding") {
		return nil, nil
	}
	given, _ := cmd.Flags().GetString("onboarding")
	code := &onboardingCode{value: normalizeOnboardingCode(given), given: strings.TrimSpace(given)}
	if !onboardingCodePattern.MatchString(code.value) {
		return nil, &clierrors.CLIError{
			Code:       "INVALID_ONBOARDING_CODE",
			Message:    "--onboarding is not a Konvu onboarding code",
			Suggestion: "Copy the 8-character code from your Konvu tab (like K7P2XQ9M), or run " + onboardingLoginStep + ".",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	return code, nil
}

// onboardingPushError maps a refused onboarding push to its next step. Only a refused or
// already-claimed code sends the user to 'konvu login', and no message carries the whole code.
func onboardingPushError(err error, code *onboardingCode) error {
	loginFirst := "Run " + onboardingLoginStep + "."
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return &clierrors.CLIError{
			Code:       "ONBOARDING_CODE_REFUSED",
			Message:    "Konvu refused onboarding code " + code.hint(),
			Suggestion: loginFirst,
			ExitCode:   clierrors.ExitAuthFailed,
		}
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    code.redact(err.Error()),
			Suggestion: "A push is safe to repeat: run the same command again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	detail := sanitizeGuardrailsBaselineText(api.ServerDetail([]byte(code.redact(strings.TrimPrefix(apiErr.Message, "API error: ")))))
	switch {
	case apiErr.StatusCode == 404:
		return &clierrors.CLIError{
			Code:       "ONBOARDING_CODE_NOT_FOUND",
			Message:    "onboarding code " + code.hint() + " is unknown or has expired",
			Suggestion: "Get a new code from the Konvu onboarding at " + config.GetDashboardURL() + "/start, or run " + onboardingLoginStep + ".",
			ExitCode:   clierrors.ExitNotFound,
		}
	case apiErr.StatusCode == 409:
		return &clierrors.CLIError{
			Code:       "ONBOARDING_HAS_ACCOUNT",
			Message:    "this onboarding already has a Konvu account",
			Suggestion: loginFirst,
			ExitCode:   clierrors.ExitAuthFailed,
		}
	case apiErr.StatusCode == 422:
		return &clierrors.CLIError{
			Code:       "INVALID_INVARIANTS",
			Message:    detail,
			Suggestion: "Run 'konvu invariants push --help' for the expected format.",
			ExitCode:   clierrors.ExitUsageError,
		}
	case apiErr.StatusCode == 429:
		return &clierrors.CLIError{
			Code:       "RATE_LIMITED",
			Message:    "Konvu got too many onboarding saves: it is rate limiting them",
			Suggestion: "Wait a minute, then run the same command again: a push is safe to repeat.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
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
	return &clierrors.CLIError{
		Code:       "API_ERROR",
		Message:    detail,
		Suggestion: loginFirst,
		ExitCode:   clierrors.ExitGeneralError,
	}
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
