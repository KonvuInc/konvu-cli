package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
	Use:     "principles",
	Aliases: []string{"principle", "invariants", "invariant"},
	Short:   "Your company's security principles",
	Long: `Security principles describe what your code must never break, in your own words.
Konvu maps each one to the controls that keep it true and enforces it on every change.`,
}

func newInvariantsListCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "list",
		Short: "List your company's live principles, by rank",
		Long: `List your company's live principles, by rank: the ones Konvu suggested (proposed) and the
active ones (validated). --status keeps only one of the two.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed or refused`,
		Example: `  konvu principles list
  konvu principles list --status proposed
  konvu principles list -o json`,
		RunE: runInvariantsList,
	}
	c.Flags().String("status", "all", "Which principles: proposed (suggested by Konvu), validated (active) or all")
	c.Flags().StringP("output", "o", "", "Output format: json, table")
	return c
}

func newInvariantsGuideCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "guide",
		Short: "Print Konvu's guide to writing a good principle",
		Long: `Print Konvu's guide to writing a good principle: what one is, the right level of
detail, and how to rank candidates. Konvu's own suggestions follow the same text.

The guide prints as Markdown, even when piped. -o json prints {"guide", "version"}; the
version changes whenever the guide does.

Exit codes: 0 success, 1 general error (this Konvu server has no guide yet, or the request
failed), 2 invalid --output, 4 auth failed or refused`,
		Example: `  konvu principles guide
  konvu principles guide -o json`,
		Args: cobra.NoArgs,
		RunE: runInvariantsGuide,
	}
	c.Flags().StringP("output", "o", "", "Output format: markdown (the default) or json")
	return c
}

func newInvariantsPushCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "push",
		Short: "Add the principles you wrote",
		Long: `Add principles from --file (or stdin with '-'), as validated and attributed to you.

The file is a JSON array of principles, or an object with an "invariants" array (1 to 10).
Each principle accepts:
  statement        (required) the rule, at most 200 characters
  why_it_matters   (required) at most 500 characters
  property         (required) authentication, authorization, confidentiality,
                   integrity, availability or non_repudiation
  rank             1 or more; the first free rank when omitted
  repository       the repository it was written for, as owner/name like acme/api (its git
                   remote's owner and name). Logged in, the CLI sends the id of your Konvu
                   repository of that name instead; if none or more than one matches, the
                   lookup fails, or Konvu has not mapped it yet, the principle goes without
                   a repository, with a warning on stderr
  repository_id    the Konvu repository's id; it wins over repository (not with --onboarding)
  protected_assets [{"kind": "...", "name": "..."}], at most 20

A statement already live comes back unchanged (or validated, if it was a proposal), keeping
its repository, so a push is safe to repeat. --dry-run checks everything on the server and
writes nothing.

--onboarding <code> saves them without a login, with the 8-character onboarding code from
Konvu's web onboarding, like K7P2XQ9M (any case; spaces and dashes are ignored, so k7p2-xq9m
is the same code). No login token is read or sent. Konvu keeps them unread until you create
your account in the browser, so the CLI checks every field above first (no unknown fields, no
duplicate statements or ranks) and sends nothing if one is off; repository_id is refused, as
there is no account yet: use repository. A dry run names the company the onboarding belongs to
("Saves to the Konvu onboarding for acme.com"; "domain" in JSON): check it is yours before
saving, since the code decides who gets the rules. Redirects are never followed, so the code
and the rules reach no other server. Each comes back saved (valid on a dry run) or unchanged,
and moves into your account when you create it. The code is only ever taken from
this flag: without it, push is the normal logged-in push. If that onboarding already has an
account, or the code expired, run 'konvu login', then push again without --onboarding.

Exit codes: 0 success, 1 general error, 2 invalid principles or onboarding code, 3 repository_id
not found (with --onboarding: the code is unknown or expired), 4 auth failed or refused (with
--onboarding: the onboarding already has an account, or Konvu refused the code)`,
		Example: `  konvu principles push --file invariants.json --dry-run
  konvu principles push --file invariants.json
  cat invariants.json | konvu principles push --file -
  konvu principles push --file invariants.json --onboarding K7P2XQ9M`,
		RunE: runInvariantsPush,
	}
	c.Flags().String("file", "", "JSON file of principles, or '-' for stdin")
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
			Message:    "Konvu sent no principle guide",
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
			Message:    "this Konvu server does not have the principle guide yet",
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
			Suggestion: "Pass a JSON file of principles, or '-' to read stdin.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	invariants, err := readInvariantsFile(file)
	if err != nil {
		return &clierrors.CLIError{
			Code:       "INVALID_FILE",
			Message:    err.Error(),
			Suggestion: "Run 'konvu principles push --help' for the expected format.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	if code != nil {
		if err := checkOnboardingInvariants(invariants); err != nil {
			return err
		}
	}

	client := api.NewClient("", "")
	defer client.Close()
	var lookedUp []string
	if code == nil {
		invariants, lookedUp = withRepositoryIDs(cmd.ErrOrStderr(), client, invariants)
	}
	body := map[string]any{"dry_run": dryRun, "invariants": invariants}
	var resp map[string]any
	if code != nil {
		resp, err = client.PostWithoutLogin(onboardingRulesPath, body, map[string]string{onboardingCodeHeader: code.value})
		if err != nil {
			return onboardingPushError(err, code)
		}
		if resp, err = onboardingSummary(invariants, resp, dryRun); err != nil {
			return err
		}
	} else {
		resp, err = client.Post(invariantsPath, body)
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 && slices.ContainsFunc(lookedUp, isNamed) {
			// Guardrails refuses the whole push, before writing anything, for a repository it has
			// not mapped yet: send it once more without the ids looked up here.
			invariants = withoutLookedUpIDs(invariants, lookedUp)
			body["invariants"] = invariants
			if resp, err = client.Post(invariantsPath, body); err == nil {
				for _, name := range distinctNames(lookedUp) {
					warnf(cmd.ErrOrStderr(), "Konvu has not mapped repository %s yet, so its principles go without a repository.", name)
				}
			}
		}
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
	cols := []string{"rank", "result", "statement"}
	if code != nil {
		// Where the rules go, before anything else: the code may not be the user's own.
		if domain := getStr(resp, "domain"); domain != "" {
			verb := "Saved"
			if dryRun {
				verb = "Saves"
			}
			if err := output.WriteString(cmd.OutOrStdout(), verb+" to the Konvu onboarding for "+domain+"\n\n"); err != nil {
				return err
			}
		}
		cols = []string{"rank", "result", "repository", "statement"}
	}
	rows := make([]any, 0)
	for _, r := range getSlice(resp, "invariants") {
		m, _ := r.(map[string]any)
		// An onboarding rule pushed without a rank has none yet: a blank, not rank 0.
		var rank any = ""
		if f, ok := getFloat(m, "rank"); ok {
			rank = int(f)
		}
		rows = append(rows, map[string]any{
			"rank":       rank,
			"result":     getStr(m, "result"),
			"repository": getStr(m, "repository"),
			"statement":  getStr(m, "statement"),
		})
	}
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
	return nil, errors.New(`expected a non-empty JSON array of principles, or {"invariants": [...]}`)
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
			Suggestion: "Run 'konvu whoami' to check who you are signed in as: pushed principles are attributed to a person.",
			ExitCode:   clierrors.ExitAuthFailed,
		}
	case apiErr.StatusCode == 409:
		return &clierrors.CLIError{
			Code:       "INVARIANT_RANK_UNAVAILABLE",
			Message:    detail,
			Suggestion: "Run 'konvu principles list' to see the ranks in use, then push again with a free rank or none.",
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
			Suggestion: "Run 'konvu principles push --help' for the expected format.",
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

// Guardrails' limits on an invariant (konvu-guardrails backend/services/user_invariants/schema.py).
const (
	maxInvariantsPerPush = 10
	maxStatementChars    = 200
	maxWhyChars          = 500
	maxProtectedAssets   = 20
	maxAssetKindChars    = 64
	maxAssetNameChars    = 200
	maxInvariantRank     = 32767
	maxRepositoryChars   = 200
)

// invariantProperties are the security properties Guardrails takes.
var invariantProperties = []string{"authentication", "authorization", "confidentiality", "integrity", "availability", "non_repudiation"}

// onboardingFields are what an onboarding push may send: Guardrails' InvariantIn, with repository
// (owner/name) in place of repository_id, which cannot exist before the account does.
var onboardingFields = map[string]bool{
	"statement": true, "why_it_matters": true, "property": true, "rank": true, "protected_assets": true, "repository": true,
}

// repositoryNamePattern is a repository's name as owner/name; GitLab subgroups add segments.
var repositoryNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$`)

// checkOnboardingInvariants checks the invariants as Guardrails will once the user signs up
// (InvariantIn and UploadRequest): Konvu stores an onboarding push unread, so a rule this lets
// through would only fail after sign-up. Every problem is reported at once.
func checkOnboardingInvariants(invariants []any) error {
	var problems []string
	if len(invariants) > maxInvariantsPerPush {
		problems = append(problems, fmt.Sprintf("%d principles, at most %d per push", len(invariants), maxInvariantsPerPush))
	}
	statements, ranks := map[string]int{}, map[int]int{}
	for i, item := range invariants {
		n := i + 1
		m, ok := item.(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("principle %d is not an object", n))
			continue
		}
		problem := func(format string, args ...any) {
			problems = append(problems, fmt.Sprintf("principle %d: ", n)+fmt.Sprintf(format, args...))
		}
		keys := make([]string, 0, len(m))
		for key := range m {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			switch {
			case key == "repository_id":
				problem("repository_id cannot be used with --onboarding, as there is no account yet: name the repository in repository instead, as owner/name")
			case !onboardingFields[key]:
				problem("unknown field %q", key)
			}
		}
		if statement, err := invariantText(m["statement"], "statement", maxStatementChars); err != "" {
			problem("%s", err)
		} else if first, dup := statements[statementKey(statement)]; dup {
			problem("same statement as principle %d", first)
		} else {
			statements[statementKey(statement)] = n
		}
		if _, err := invariantText(m["why_it_matters"], "why_it_matters", maxWhyChars); err != "" {
			problem("%s", err)
		}
		if property, _ := m["property"].(string); !slices.Contains(invariantProperties, property) {
			problem("property must be one of %s", strings.Join(invariantProperties, ", "))
		}
		if value := m["rank"]; value != nil {
			rank, ok := value.(float64)
			if !ok || rank != math.Trunc(rank) || rank < 1 || rank > maxInvariantRank {
				problem("rank must be a whole number from 1 to %d", maxInvariantRank)
			} else if first, dup := ranks[int(rank)]; dup {
				problem("same rank as principle %d", first)
			} else {
				ranks[int(rank)] = n
			}
		}
		if value, present := m["protected_assets"]; present {
			problems = append(problems, protectedAssetProblems(value, n)...)
		}
		if value, present := m["repository"]; present {
			name, _ := value.(string)
			if len(name) > maxRepositoryChars || !repositoryNamePattern.MatchString(name) {
				problem("repository must be the repository's name as owner/name, like acme/api")
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return &clierrors.CLIError{
		Code:       "INVALID_INVARIANTS",
		Message:    strings.Join(problems, "; "),
		Suggestion: "Nothing was sent. Fix the file and push again; 'konvu principles push --help' lists the fields.",
		ExitCode:   clierrors.ExitUsageError,
	}
}

// protectedAssetProblems checks protected_assets: a list of at most 20 {kind, name}.
func protectedAssetProblems(value any, n int) []string {
	prefix := fmt.Sprintf("principle %d: protected_assets", n)
	assets, ok := value.([]any)
	if !ok {
		return []string{prefix + ` must be a list of {"kind", "name"}`}
	}
	var problems []string
	if len(assets) > maxProtectedAssets {
		problems = append(problems, fmt.Sprintf("%s has %d items, at most %d", prefix, len(assets), maxProtectedAssets))
	}
	for j, item := range assets {
		label := fmt.Sprintf("%s[%d]", prefix, j)
		asset, ok := item.(map[string]any)
		if !ok {
			problems = append(problems, label+` must be {"kind", "name"}`)
			continue
		}
		for key := range asset {
			if key != "kind" && key != "name" {
				problems = append(problems, fmt.Sprintf("%s: unknown field %q", label, key))
			}
		}
		if _, err := invariantText(asset["kind"], "kind", maxAssetKindChars); err != "" {
			problems = append(problems, label+": "+err)
		}
		if _, err := invariantText(asset["name"], "name", maxAssetNameChars); err != "" {
			problems = append(problems, label+": "+err)
		}
	}
	sort.Strings(problems)
	return problems
}

// invariantText reads a required text field as Guardrails does: surrounding spaces dropped, then
// 1 to max characters, with no NUL. It returns the text, or what is wrong with it.
func invariantText(value any, field string, max int) (string, string) {
	if value == nil {
		return "", field + " is required"
	}
	text, ok := value.(string)
	if !ok {
		return "", field + " must be text"
	}
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return "", field + " is empty"
	case strings.ContainsRune(text, 0):
		return "", field + " must not contain NUL"
	case utf8.RuneCountInString(text) > max:
		return "", fmt.Sprintf("%s is longer than %d characters", field, max)
	}
	return text, ""
}

// statementKey is how Guardrails tells statements apart: case and spacing do not count.
func statementKey(statement string) string {
	return strings.ToLower(strings.Join(strings.Fields(statement), " "))
}

// withRepositoryIDs readies a logged-in push for Guardrails, which refuses repository: an
// invariant naming its repository as owner/name, and no repository_id, gets the id of the
// company's Konvu repository of that name instead. A name that is malformed, matches none or more
// than one, or a failed lookup leaves the invariant without a repository, with a warning on
// stderr, and the push goes on. It returns the invariants to send (the file's own objects are
// left untouched) and, for each, the name whose id it looked up, or "".
func withRepositoryIDs(stderr io.Writer, client *api.Client, invariants []any) ([]any, []string) {
	sent := make([]any, len(invariants))
	wanted := make([]string, len(invariants))
	for i, item := range invariants {
		sent[i] = item
		m, ok := item.(map[string]any)
		value, has := m["repository"]
		if !ok || !has {
			continue
		}
		sent[i] = withoutField(m, "repository")
		// An explicit repository_id wins, and a null repository names none.
		if m["repository_id"] != nil || value == nil {
			continue
		}
		name, _ := value.(string)
		name = strings.TrimSpace(name)
		if len(name) > maxRepositoryChars || !repositoryNamePattern.MatchString(name) {
			warnf(stderr, "principle %d's repository is not owner/name (like acme/api), so it goes without a repository.", i+1)
			continue
		}
		wanted[i] = name
	}
	if !slices.ContainsFunc(wanted, isNamed) {
		return sent, nil
	}

	byName, lookupErr := konvuRepositoriesByName(client, repositoryLookupTimeout)
	lookedUp := make([]string, len(invariants))
	warned := map[string]bool{}
	for i, name := range wanted {
		key := strings.ToLower(name)
		matches := byName[key]
		switch {
		case name == "":
			continue
		case lookupErr == nil && len(matches) == 1:
			sent[i].(map[string]any)["repository_id"] = matches[0].id
			lookedUp[i] = name
			continue
		case warned[key]:
			continue
		}
		warned[key] = true
		switch {
		case lookupErr != nil:
			warnf(stderr, "could not look up Konvu repository %s (%s), so its principles go without a repository.", name, lookupReason(lookupErr))
		case len(matches) == 0:
			warnf(stderr, "none of your Konvu repositories is %s, so its principles go without a repository.", name)
		default:
			found := make([]string, len(matches))
			for j, match := range matches {
				found[j] = match.label + " (id " + match.id + ")"
			}
			warnf(stderr, "%d Konvu repositories are %s (%s), so its principles go without a repository: set repository_id to the one you mean.", len(matches), name, strings.Join(found, ", "))
		}
	}
	return sent, lookedUp
}

// konvuRepository is one of the company's Konvu repositories: its id, and where it lives, like
// github:acme/api.
type konvuRepository struct{ id, label string }

// konvuRepositoriesByName lists the company's Konvu repositories (the ones coverage lists) by
// lowercased owner/name: a GitHub and a GitLab repository can share one. A lookup that takes
// longer than timeout fails, so a stalled one never holds the push.
func konvuRepositoriesByName(client *api.Client, timeout time.Duration) (map[string][]konvuRepository, error) {
	type answer struct {
		data map[string]any
		err  error
	}
	answers := make(chan answer, 1)
	go func() {
		data, err := client.Get(coverageConfigPath, nil)
		answers <- answer{data, err}
	}()
	var got answer
	select {
	case got = <-answers:
	case <-time.After(timeout):
		return nil, fmt.Errorf("no answer within %s", timeout)
	}
	if got.err != nil {
		return nil, got.err
	}
	repos, ok := got.data["repositories"].([]any)
	if !ok {
		return nil, errors.New("Konvu sent no repository list")
	}
	byName := map[string][]konvuRepository{}
	seen := map[string]bool{}
	for _, item := range repos {
		repo, _ := item.(map[string]any)
		id := strings.ToLower(getStr(repo, "id"))
		name, label := konvuRepositoryName(getStr(repo, "url"))
		if name == "" || !uuidRe.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		key := strings.ToLower(name)
		byName[key] = append(byName[key], konvuRepository{id: id, label: label})
	}
	for _, matches := range byName {
		sort.Slice(matches, func(a, b int) bool {
			if matches[a].label != matches[b].label {
				return matches[a].label < matches[b].label
			}
			return matches[a].id < matches[b].id
		})
	}
	return byName, nil
}

// konvuRepositoryName reads a Konvu repository URL, like github:acme/api or
// https://gitlab.example.com/acme/team/api.git: its owner/name, GitLab subgroups included, and
// where it lives, like github:acme/api or gitlab.example.com/acme/team/api. Both are "" when the
// URL names no repository.
func konvuRepositoryName(repoURL string) (name, label string) {
	parsed, err := url.Parse(strings.TrimSpace(repoURL))
	if err != nil || parsed.Scheme == "" {
		return "", ""
	}
	path, where := parsed.Opaque, parsed.Scheme+":"
	if path == "" {
		if parsed.Host == "" {
			return "", ""
		}
		// Escaped, so an encoded slash never turns into a name.
		path, where = parsed.EscapedPath(), parsed.Host+"/"
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if len(path) > maxRepositoryChars || !repositoryNamePattern.MatchString(path) {
		return "", ""
	}
	return path, sanitizeGuardrailsBaselineText(where + path)
}

// lookupReason is why the repository lookup failed, short enough for a one-line warning.
func lookupReason(err error) string {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return fmt.Sprintf("Konvu answered %d", apiErr.StatusCode)
	}
	return sanitizeGuardrailsBaselineText(err.Error())
}

// withoutLookedUpIDs drops repository_id from each invariant whose id this push looked up.
func withoutLookedUpIDs(invariants []any, lookedUp []string) []any {
	kept := make([]any, len(invariants))
	for i, item := range invariants {
		kept[i] = item
		if m, ok := item.(map[string]any); ok && i < len(lookedUp) && lookedUp[i] != "" {
			kept[i] = withoutField(m, "repository_id")
		}
	}
	return kept
}

// distinctNames is the repository names given, each once whatever its case, in order.
func distinctNames(names []string) []string {
	var distinct []string
	seen := map[string]bool{}
	for _, name := range names {
		if key := strings.ToLower(name); name != "" && !seen[key] {
			seen[key] = true
			distinct = append(distinct, name)
		}
	}
	return distinct
}

func isNamed(name string) bool { return name != "" }

// withoutField is a copy of m without key.
func withoutField(m map[string]any, key string) map[string]any {
	copied := make(map[string]any, len(m))
	for k, v := range m {
		if k != key {
			copied[k] = v
		}
	}
	return copied
}

// warnf prints a warning on stderr, never stdout, so -o json output stays valid.
func warnf(stderr io.Writer, format string, args ...any) {
	_ = output.WriteString(stderr, "Warning: "+fmt.Sprintf(format, args...)+"\n")
}

// domainPattern is a domain name, like acme.com: the only text Konvu's answer may name the
// onboarding's company with, so it reads plainly when the user is asked to confirm it.
var domainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// onboardingDomain is the company domain of the onboarding the code belongs to, lowercased, or
// "" when Konvu sent none or something that is not a domain name.
func onboardingDomain(resp map[string]any) string {
	domain := strings.ToLower(strings.TrimSpace(getStr(resp, "domain")))
	if len(domain) > 253 || !domainPattern.MatchString(domain) {
		return ""
	}
	return domain
}

// onboardingSummary pairs each invariant pushed with Konvu's result for it: an onboarding push
// answers one result per invariant, in the order sent, and no statement or rank. It names the
// onboarding's company domain, which a dry run must have: it is how the user checks the code is
// theirs before anything is saved. A reply that does not line up is an error, never a guess.
func onboardingSummary(invariants []any, resp map[string]any, dryRun bool) (map[string]any, error) {
	results := getSlice(resp, "invariants")
	domain := onboardingDomain(resp)
	if dryRun && domain == "" {
		return nil, &clierrors.CLIError{
			Code:       "ONBOARDING_DOMAIN_MISSING",
			Message:    "Konvu did not say which company's onboarding this code belongs to",
			Suggestion: "Nothing was written. Check the code is the one your Konvu tab shows and run the dry run again, or run " + onboardingLoginStep + ".",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	mismatch := func(message string) error {
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    message,
			Suggestion: "A push is safe to repeat: run the same command again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	if len(results) != len(invariants) {
		return nil, mismatch(fmt.Sprintf("Konvu answered %d results for %d principles", len(results), len(invariants)))
	}
	rows := make([]any, 0, len(invariants))
	for i, item := range invariants {
		sent, _ := item.(map[string]any)
		answer, _ := results[i].(map[string]any)
		result := getStr(answer, "result")
		if result == "" {
			return nil, mismatch(fmt.Sprintf("Konvu sent no result for principle %d", i+1))
		}
		statement, _ := sent["statement"].(string)
		row := map[string]any{"rank": sent["rank"], "statement": strings.TrimSpace(statement), "result": result}
		if repository, ok := sent["repository"].(string); ok {
			row["repository"] = repository
		}
		rows = append(rows, row)
	}
	summary := map[string]any{"dry_run": dryRun, "invariants": rows}
	if domain != "" {
		summary["domain"] = domain
	}
	return summary, nil
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
	var redirect *api.RedirectError
	if errors.As(err, &redirect) {
		to := ""
		if where, parseErr := url.Parse(redirect.Location); parseErr == nil && where.Host != "" {
			to = " to " + sanitizeGuardrailsBaselineText(where.Host)
		}
		return &clierrors.CLIError{
			Code:       "REDIRECT_REFUSED",
			Message:    fmt.Sprintf("Konvu's API answered %d, a redirect%s; push --onboarding never follows one, so the code and the rules went nowhere else", redirect.StatusCode, to),
			Suggestion: "Check that KONVU_API_URL, if set, is Konvu's API itself (the default is " + config.DefaultAPIBaseURL + "), then run the same command again.",
			ExitCode:   clierrors.ExitGeneralError,
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
			Suggestion: "Run 'konvu principles push --help' for the expected format.",
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
