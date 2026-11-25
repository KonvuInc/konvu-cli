package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

// invariantEditable are the fields an edit sends; the server keeps any left out of the file.
var invariantEditable = []string{"statement", "why_it_matters", "property", "rank", "protected_assets"}

// invariantReadOnly are the other fields 'principles get -o json' prints. The computed ones are
// ignored; status and repository_id are refused when the file changes them, as no edit can.
var invariantReadOnly = []string{"id", "repository_id", "status", "revision", "threat_links", "hooks", "controls", "steering"}

// Swapped in tests: whether a person can answer a prompt, and the prompt itself.
var (
	deleteCanPrompt = output.IsInteractive
	deleteConfirm   = output.Confirm
)

func newInvariantsEditCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit an active principle from a file",
		Long: `Edit an active (validated) principle: get it, edit the file, and send it back.

--file (or stdin with '-') is the principle as 'konvu principles get <id> -o json' prints it,
or an object with only "revision" and the fields to change. These change:
  statement        at most 200 characters
  why_it_matters   at most 500 characters
  property         authentication, authorization, confidentiality, integrity,
                   availability or non_repudiation
  rank             1 to 32767, not held by another live principle
  protected_assets [{"kind": "...", "name": "..."}], at most 20 (other keys are dropped)
A field left out keeps its value. "revision" is required: it names the version the file
was read from, and the edit is refused, writing nothing, once the principle changed since
(someone else's edit). Get it again and redo the edit then. The other fields 'get' prints are
ignored, except that "id" must be this principle's, and "status" and "repository_id" cannot
change: delete a principle with 'konvu principles delete'.

The edit is attributed to you, and Konvu maps the principle to controls again; until then
coding agents keep the guidance they have. A file that changes nothing is safe to send again,
which also retries a mapping that failed to queue. --dry-run checks it on the server and
writes nothing.

Exit codes: 0 edited (or nothing to change), 1 general error (changed since the file was read,
a rank or wording another principle holds, not active, or the request failed), 2 invalid id,
flag or file, 3 principle not found, or this Konvu server cannot edit principles yet,
4 auth failed or refused`,
		Example: `  konvu principles get 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b -o json > principle.json
  konvu principles edit 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b --file principle.json --dry-run
  konvu principles edit 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b --file principle.json
  konvu principles get 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b -o json \
    | jq '.rank = 2' | konvu principles edit 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b --file -`,
		Args: cobra.ExactArgs(1),
		RunE: withJSONErrors(runInvariantsEdit),
	}
	c.Flags().String("file", "", "JSON principle file, or '-' for stdin")
	c.Flags().Bool("dry-run", false, "Check on the server without writing anything")
	c.Flags().StringP("output", "o", "", "Output format: json, table")
	return c
}

func newInvariantsDeleteCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an active principle",
		Long: `Delete an active (validated) principle: Konvu stops checking pull requests against it and
stops steering coding agents with it, from their next sync, and frees its rank. Pull request
checks that already cited it keep it, and Konvu does not suggest the same principle again.
There is no undo: push it again to restore it. A principle you already deleted comes back
unchanged (exit 0), so a delete is safe to repeat; a suggestion is declined instead, with
'konvu principles decline'.

delete names the principle and asks before deleting it. Without a terminal to ask on (a
script or a coding agent), --yes is required.

Exit codes: 0 deleted (or already deleted), 1 general error (not active, not confirmed, or the
request failed), 2 invalid id or flag, or --yes missing without a terminal, 3 principle not
found, or this Konvu server cannot delete principles yet, 4 auth failed or refused`,
		Example: `  konvu principles delete 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b
  konvu principles delete 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b --yes -o json`,
		Args: cobra.ExactArgs(1),
		RunE: withJSONErrors(runInvariantsDelete),
	}
	c.Flags().BoolP("yes", "y", false, "Delete without asking")
	c.Flags().StringP("output", "o", "", "Output format: json, table")
	return c
}

func runInvariantsEdit(cmd *cobra.Command, args []string) error {
	id, err := invariantID(args[0])
	if err != nil {
		return err
	}
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
			Suggestion: fmt.Sprintf("Pass the edited output of 'konvu principles get %s -o json', or '-' to read stdin.", id),
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	doc, revision, err := readInvariantEditFile(file, id)
	if err != nil {
		return invalidEditFile(err.Error(), id)
	}

	client := api.NewClient("", "")
	defer client.Close()
	current, err := client.Get(invariantPath(id), nil)
	if err != nil {
		return invariantReadError(err, id)
	}
	if _, ok := getFloat(current, "revision"); !ok {
		return principlesNotSupported("edit")
	}
	if status := getStr(current, "status"); status != invariantValidated {
		return invariantNotActive("edited", id, status)
	}
	for _, field := range []string{"status", "repository_id"} {
		if value, ok := doc[field]; ok && !reflect.DeepEqual(value, current[field]) {
			return invalidEditFile(fmt.Sprintf("the file changes %q, which an edit cannot change", field), id)
		}
	}
	body := map[string]any{"revision": revision, "dry_run": dryRun}
	for _, field := range invariantEditable {
		if value, ok := doc[field]; ok {
			body[field] = value
		}
	}
	if serverRevision := intField(current, "revision"); serverRevision != revision && editChanges(body, current) {
		return staleEdit(id, revision, serverRevision)
	}

	resp, err := client.Patch(invariantPath(id), body)
	if err != nil {
		if dryRun || !outcomeUnknown(err) {
			return invariantEditError(client, err, id, revision, dryRun)
		}
		return confirmEdit(cmd, client, err, id, format, body, revision)
	}
	edited, _ := resp.(map[string]any)
	switch {
	case dryRun:
		output.PrintStderr("Dry run: nothing was written. Run without --dry-run to edit it.")
	case intField(edited, "revision") == intField(current, "revision"):
		output.PrintStderr("No changes: the principle already matches the file.")
	default:
		output.PrintStderr("Edited. Konvu maps it to controls again; coding agents keep the guidance they have until then.")
	}
	return printManagedInvariant(cmd, format, edited)
}

// readInvariantEditFile reads an edit: a JSON object with a positive integer "revision", only
// the fields 'principles get' prints, and, when it names one, this principle's id.
func readInvariantEditFile(path, id string) (map[string]any, int, error) {
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
		return nil, 0, fmt.Errorf("could not read %s: %w", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil || doc == nil {
		return nil, 0, errors.New("expected a JSON object like the output of 'konvu principles get <id> -o json'")
	}
	var problems []string
	revision, ok := doc["revision"].(float64)
	if !ok || revision < 1 || revision != math.Trunc(revision) || revision > math.MaxInt32 {
		problems = append(problems, `no "revision" (a positive integer, as 'get -o json' prints it)`)
	}
	if fileID, ok := doc["id"]; ok {
		if s, _ := fileID.(string); !strings.EqualFold(strings.TrimSpace(s), id) {
			problems = append(problems, fmt.Sprintf("an \"id\" that is not %s", id))
		}
	}
	if property, ok := doc["property"]; ok {
		if s, _ := property.(string); !contains(invariantSecurityProperties, s) {
			problems = append(problems, fmt.Sprintf("an unknown \"property\" (use %s)", strings.Join(invariantSecurityProperties, ", ")))
		}
	}
	if assets, ok := doc["protected_assets"].([]any); ok {
		doc["protected_assets"] = assetsKindAndName(assets)
	}
	var unknown []string
	for key := range doc {
		if !contains(invariantEditable, key) && !contains(invariantReadOnly, key) {
			unknown = append(unknown, fmt.Sprintf("%q", key))
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		problems = append(problems, "unknown keys "+strings.Join(unknown, ", "))
	}
	edits := 0
	for _, field := range invariantEditable {
		if _, ok := doc[field]; ok {
			edits++
		}
	}
	if edits == 0 {
		problems = append(problems, "no field to change (one of "+strings.Join(invariantEditable, ", ")+")")
	}
	if len(problems) > 0 {
		return nil, 0, fmt.Errorf("the file has %s", strings.Join(problems, "; "))
	}
	return doc, int(revision), nil
}

// assetsKindAndName keeps what an edit accepts of each protected asset; 'get' also prints the
// graph links an asset was matched to, which the server computes.
func assetsKindAndName(assets []any) []any {
	kept := make([]any, 0, len(assets))
	for _, a := range assets {
		m, ok := a.(map[string]any)
		if !ok {
			kept = append(kept, a)
			continue
		}
		item := map[string]any{}
		for _, key := range []string{"kind", "name"} {
			if v, ok := m[key]; ok {
				item[key] = v
			}
		}
		kept = append(kept, item)
	}
	return kept
}

// editChanges reports whether an edit body differs from the principle as Konvu holds it.
func editChanges(body, current map[string]any) bool {
	for _, field := range invariantEditable {
		value, ok := body[field]
		if !ok {
			continue
		}
		held := current[field]
		switch field {
		case "protected_assets":
			assets, _ := held.([]any)
			held = assetsKindAndName(assets)
		case "statement", "why_it_matters":
			if s, ok := value.(string); ok {
				value = strings.TrimSpace(s)
			}
		}
		if !reflect.DeepEqual(value, held) {
			return true
		}
	}
	return false
}

func staleEdit(id string, fileRevision, serverRevision int) error {
	return &clierrors.CLIError{
		Code:       "STALE_REVISION",
		Message:    fmt.Sprintf("principle %s changed after your file was read: the file is at revision %d, Konvu at revision %d; nothing was written", id, fileRevision, serverRevision),
		Suggestion: fmt.Sprintf("Run 'konvu principles get %s -o json' again and redo the edit on the new file.", id),
		ExitCode:   clierrors.ExitGeneralError,
	}
}

func invalidEditFile(message, id string) error {
	return &clierrors.CLIError{
		Code:       "INVALID_EDIT",
		Message:    message,
		Suggestion: fmt.Sprintf("Start from 'konvu principles get %s -o json'; 'konvu principles edit --help' lists what can change.", id),
		ExitCode:   clierrors.ExitUsageError,
	}
}

// principlesNotSupported explains a server that predates editing and deleting principles: its
// reads carry no revision, and it answers both methods 405.
func principlesNotSupported(verb string) error {
	return &clierrors.CLIError{
		Code:       "NOT_SUPPORTED",
		Message:    fmt.Sprintf("this Konvu server cannot %s principles yet", verb),
		Suggestion: "It comes with a newer Konvu server: try again once yours is updated, or contact Konvu support.",
		ExitCode:   clierrors.ExitNotFound,
	}
}

// invariantNotActive refuses an edit or a delete of a principle that is not active.
func invariantNotActive(verb, id, status string) error {
	suggestion := "Run 'konvu principles list --status validated' to see the active ones."
	if status == invariantProposed {
		suggestion = fmt.Sprintf("It is a suggestion: accept, amend or decline it ('konvu principles accept|amend|decline %s').", id)
	}
	return &clierrors.CLIError{
		Code:       "INVARIANT_NOT_ACTIVE",
		Message:    fmt.Sprintf("principle %s is %s; only an active (validated) one can be %s", id, statusOrUnknown(status), verb),
		Suggestion: suggestion,
		ExitCode:   clierrors.ExitGeneralError,
	}
}

// invariantEditError maps a refused edit whose outcome is known: nothing was written. A 409 is
// read back, so a stale revision is told apart from a rank or wording another principle holds.
func invariantEditError(client *api.Client, err error, id string, revision int, dryRun bool) error {
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return clierrors.NewAuthError(err.Error())
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    err.Error(),
			Suggestion: "A dry run writes nothing: run it again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	detail := invariantDetail(apiErr)
	switch {
	case apiErr.StatusCode == 403:
		return invariantDecisionForbidden(apiErr)
	case apiErr.StatusCode == 404:
		return invariantNotFound(apiErr, id)
	case apiErr.StatusCode == 405:
		return principlesNotSupported("edit")
	case apiErr.StatusCode == 409:
		if now, readErr := client.Get(invariantPath(id), nil); readErr == nil && intField(now, "revision") != revision {
			return staleEdit(id, revision, intField(now, "revision"))
		}
		return &clierrors.CLIError{
			Code:       "INVARIANT_CONFLICT",
			Message:    detail,
			Suggestion: "Nothing was written. Change the file and edit again; 'konvu principles list' shows the ranks and wordings in use.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	case apiErr.StatusCode == 422:
		return &clierrors.CLIError{
			Code:       "INVALID_EDIT",
			Message:    "the file has problems, nothing was written:\n  " + strings.Join(validationProblems(apiErr), "\n  "),
			Suggestion: "Fix them in the file and edit again; 'konvu principles edit --help' lists the limits.",
			ExitCode:   clierrors.ExitUsageError,
		}
	case apiErr.StatusCode >= 500 && dryRun:
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    detail,
			Suggestion: "A dry run writes nothing: run it again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return clierrors.NewAPIError(detail)
}

// confirmEdit reads the principle back after an edit whose outcome is unknown: a server error
// or a lost connection can come after the commit (Guardrails could not queue the mapping).
func confirmEdit(cmd *cobra.Command, client *api.Client, patchErr error, id string, format output.OutputFormat, body map[string]any, revision int) error {
	reason := patchErr.Error()
	var apiErr *api.APIError
	if errors.As(patchErr, &apiErr) {
		reason = invariantDetail(apiErr)
	}
	reason = sanitizeGuardrailsBaselineText(reason)
	again := "Run the same command again: an edit that landed comes back unchanged, and repeating it queues its mapping."
	now, err := client.Get(invariantPath(id), nil)
	if err != nil {
		return &clierrors.CLIError{
			Code:       "EDIT_UNCONFIRMED",
			Message:    fmt.Sprintf("the edit failed (%s) and the principle could not be read back", reason),
			Suggestion: again,
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	if getStr(now, "status") == invariantValidated && !editChanges(body, now) {
		output.PrintStderr("The edit is saved, but the server failed after it, so its mapping to controls may wait. " + again)
		return printManagedInvariant(cmd, format, now)
	}
	if intField(now, "revision") == revision {
		return &clierrors.CLIError{
			Code:       "EDIT_UNCONFIRMED",
			Message:    fmt.Sprintf("the edit failed (%s) and the principle is unchanged, though the request may still be running on the server", reason),
			Suggestion: "Wait a minute, then " + strings.ToLower(again[:1]) + again[1:],
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return &clierrors.CLIError{
		Code:       "INVARIANT_CHANGED",
		Message:    fmt.Sprintf("the edit failed (%s) and someone else changed the principle meanwhile", reason),
		Suggestion: fmt.Sprintf("Run 'konvu principles get %s -o json' to see what was saved, and redo the edit on the new file.", id),
		ExitCode:   clierrors.ExitGeneralError,
	}
}

func runInvariantsDelete(cmd *cobra.Command, args []string) error {
	id, err := invariantID(args[0])
	if err != nil {
		return err
	}
	yes, _ := cmd.Flags().GetBool("yes")
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}

	client := api.NewClient("", "")
	defer client.Close()
	current, err := client.Get(invariantPath(id), nil)
	if err != nil {
		return invariantReadError(err, id)
	}
	status := getStr(current, "status")
	if status != invariantValidated && status != "retired" {
		return invariantNotActive("deleted", id, status)
	}
	named := fmt.Sprintf("principle %s (rank %d): %s", id, intField(current, "rank"), displayText(current, "statement"))
	// Deleting again has nothing left to lose: the server answers it with the first delete.
	if !yes && status == invariantValidated {
		if !deleteCanPrompt() {
			return &clierrors.CLIError{
				Code:       "CONFIRMATION_REQUIRED",
				Message:    "refusing to delete " + named + " without --yes: there is no terminal to ask on",
				Suggestion: fmt.Sprintf("Check it is the one to delete, then run 'konvu principles delete %s --yes'.", id),
				ExitCode:   clierrors.ExitUsageError,
			}
		}
		if !deleteConfirm("Delete "+named+"? Konvu stops enforcing it.", false) {
			return &clierrors.CLIError{
				Code:       "NOT_CONFIRMED",
				Message:    "not deleted: " + named,
				Suggestion: "Nothing was written.",
				ExitCode:   clierrors.ExitGeneralError,
			}
		}
	}

	deleted, err := client.Delete(invariantPath(id), nil)
	if err != nil {
		if !outcomeUnknown(err) {
			return invariantDeleteError(err, id)
		}
		deleted, err = confirmDelete(client, err, id)
		if err != nil {
			return err
		}
	}
	if status == invariantValidated {
		output.PrintStderr("Deleted " + named + ". Konvu no longer checks pull requests against it or steers coding agents with it.")
	} else {
		output.PrintStderr("Already deleted: " + named + ".")
	}
	return printManagedInvariant(cmd, format, deleted)
}

// confirmDelete reads the principle back after a delete whose outcome is unknown.
func confirmDelete(client *api.Client, deleteErr error, id string) (map[string]any, error) {
	reason := deleteErr.Error()
	var apiErr *api.APIError
	if errors.As(deleteErr, &apiErr) {
		reason = invariantDetail(apiErr)
	}
	reason = sanitizeGuardrailsBaselineText(reason)
	now, err := client.Get(invariantPath(id), nil)
	if err == nil && getStr(now, "status") == "retired" {
		return now, nil
	}
	return nil, &clierrors.CLIError{
		Code:       "DELETE_UNCONFIRMED",
		Message:    fmt.Sprintf("the delete failed (%s) and the principle is not deleted yet, though the request may still be running on the server", reason),
		Suggestion: fmt.Sprintf("A delete is safe to repeat: run 'konvu principles delete %s --yes' again.", id),
		Retryable:  true,
		ExitCode:   clierrors.ExitGeneralError,
	}
}

// invariantDeleteError maps a refused delete whose outcome is known: nothing was written.
func invariantDeleteError(err error, id string) error {
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return clierrors.NewAuthError(err.Error())
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return clierrors.NewAPIError(err.Error())
	}
	detail := invariantDetail(apiErr)
	switch apiErr.StatusCode {
	case 403:
		return invariantDecisionForbidden(apiErr)
	case 404:
		return invariantNotFound(apiErr, id)
	case 405:
		return principlesNotSupported("delete")
	case 409:
		return &clierrors.CLIError{
			Code:       "INVARIANT_NOT_ACTIVE",
			Message:    detail,
			Suggestion: fmt.Sprintf("Only an active principle can be deleted. Run 'konvu principles get %s' to see where it stands.", id),
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return clierrors.NewAPIError(detail)
}

func printManagedInvariant(cmd *cobra.Command, format output.OutputFormat, inv map[string]any) error {
	if format == output.JSON {
		fmt.Fprintln(cmd.OutOrStdout(), output.FormatJSON(inv))
		return nil
	}
	row := map[string]any{
		"rank":      intField(inv, "rank"),
		"status":    displayText(inv, "status"),
		"revision":  intField(inv, "revision"),
		"statement": displayText(inv, "statement"),
	}
	cols := []string{"rank", "status", "revision", "statement"}
	fmt.Fprint(cmd.OutOrStdout(), output.FormatTable(map[string]any{"invariants": []any{row}}, cols, "invariants", nil))
	return nil
}
