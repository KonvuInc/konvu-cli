package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const (
	dataEditsPath = "/data_edits"
	// How much of a value BEFORE → AFTER shows on each side.
	historyValueRunes = 40
	// The most edits the server lists in one answer.
	historyMaxLimit = 200
	// The object_key of an object the company has one of, such as its facts.
	historyCompanyKey = "company"
)

// The objects whose edits Konvu records, as --object takes them.
var historyObjects = []string{"company", "threat-model", "threat-profile", "catalog", "note"}

func newHistoryCmd() *cobra.Command {
	history := &cobra.Command{
		Use:   "history",
		Short: "List the edits made to the data behind your Security Book",
		Long: `List the edits made to the data behind your Security Book, newest first: who
changed which field of which object, when, and its value before and after.

Every edit is recorded, from the CLI ('konvu company apply',
'konvu threat-model apply', 'konvu threat-profile apply',
'konvu inventory catalog apply', 'konvu knowledge note add', 'edit' and 'rm')
or from Konvu's web app, and 'konvu history revert <id>' undoes one. A note's
edits are keyed by its id: --object note --key <note-id>. Values are shortened in the table; -o json
has them whole.

Exit codes: 0 success, 1 error, 2 invalid arguments, 3 a server that does not
keep this history yet, 4 not logged in or no access`,
		Example: `  konvu history
  konvu history --object threat-profile --key <repository-id>
  konvu history --limit 20 --offset 20 -o json`,
		Args: cobra.NoArgs,
		RunE: withJSONErrors(runHistory),
	}
	history.Flags().String("object", "", "Only edits of this object: "+strings.Join(historyObjects, ", "))
	history.Flags().String("key", "", "Only edits of this object id, such as a repository id for threat-profile or a note id for note")
	history.Flags().Int("limit", 0, "How many edits to list, from 1 to 200 (default: the server's, 50)")
	history.Flags().Int("offset", 0, "How many of the newest edits to skip")
	history.Flags().StringP("output", "o", "", "Output format: json, table")

	revert := &cobra.Command{
		Use:   "revert <id>",
		Short: "Undo one edit of the data behind your Security Book",
		Long: `Undo one edit, by its id from 'konvu history': its field goes back to its value
before the edit. The revert is itself recorded as an edit.

--revision is the object's current revision, from its 'get' ('konvu company get',
'konvu threat-model get', or 'konvu threat-profile get --repo <id>' for a
repository's), or for a note its updated_at from
'konvu knowledge note list -o json': if the data changed since, the revert is
refused. Reverting a note's removal brings the note back; reverting its addition
removes it. A catalog edit is undone by applying the catalog again, not here.

Exit codes: 0 reverted, 1 the data changed since or another error, 2 invalid
arguments or an edit that cannot be reverted, 3 no such edit or a server that
does not support this yet, 4 not logged in, a read-only key or the demo view`,
		Example: `  konvu history revert 0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b4 --revision <revision>`,
		Args:    cobra.ExactArgs(1),
		RunE:    withJSONErrors(runHistoryRevert),
	}
	revert.Flags().String("revision", "", "The object's current revision, from its 'get' (required); the revert is refused if it changed")
	revert.Flags().StringP("output", "o", "", "Output format: json, table")
	history.AddCommand(revert)
	return history
}

func runHistory(cmd *cobra.Command, _ []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	params := map[string]any{}
	if object, _ := cmd.Flags().GetString("object"); cmd.Flags().Changed("object") {
		object = strings.TrimSpace(object)
		if !containsString(historyObjects, object) {
			return bookDataUsage(fmt.Sprintf("unknown --object %q", clean(object)), "Use one of: "+strings.Join(historyObjects, ", ")+".")
		}
		params["object"] = object
	}
	if key, _ := cmd.Flags().GetString("key"); cmd.Flags().Changed("key") {
		if key = strings.TrimSpace(key); key == "" {
			return bookDataUsage("--key is empty", "Pass the object's id, or leave --key out.")
		}
		params["object_key"] = key
	}
	if cmd.Flags().Changed("limit") {
		limit, _ := cmd.Flags().GetInt("limit")
		if limit < 1 || limit > historyMaxLimit {
			return bookDataUsage("--limit must be from 1 to 200", "Pass how many edits to list, newest first; --offset skips the newest ones.")
		}
		params["limit"] = limit
	}
	offset, _ := cmd.Flags().GetInt("offset")
	if offset < 0 {
		return bookDataUsage("--offset cannot be negative", "Pass how many of the newest edits to skip.")
	}
	if offset > 0 {
		params["offset"] = offset
	}
	client := api.NewClient("", "")
	defer client.Close()
	result, err := client.Get(dataEditsPath, params)
	if err != nil {
		return historyError(err, "")
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), terminalJSON(result)+"\n")
	}
	items := getSlice(result, "items")
	total, hasTotal := getFloat(result, "total")
	if len(items) == 0 {
		if offset > 0 && hasTotal {
			return output.WriteString(cmd.OutOrStdout(), fmt.Sprintf("No edits past the newest %d; there are %.0f.\n", offset, total))
		}
		if params["object"] != nil || params["object_key"] != nil {
			return output.WriteString(cmd.OutOrStdout(), "No edits match --object or --key.\n")
		}
		return output.WriteString(cmd.OutOrStdout(), "No edits yet.\n")
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		edit, _ := item.(map[string]any)
		object := getStr(edit, "object")
		if key := getStr(edit, "object_key"); key != "" && key != historyCompanyKey {
			object += " " + key
		}
		who := getStr(edit, "actor")
		if via := getStr(edit, "via"); via != "" {
			who += " (" + via + ")"
		}
		rows = append(rows, []string{
			historyWhen(getStr(edit, "created_at")),
			clean(who),
			clean(object),
			clean(getStr(edit, "field")),
			historyValue(edit["before"]) + " → " + historyValue(edit["after"]),
			clean(getStr(edit, "id")),
		})
	}
	text := alignedTable([]string{"WHEN", "WHO", "OBJECT", "FIELD", "BEFORE → AFTER", "ID"}, rows)
	if hasTotal && int(total) > offset+len(items) {
		text += fmt.Sprintf("Showing edits %d to %d of %.0f, newest first. Pass --offset %d to see the next ones.\n", offset+1, offset+len(items), total, offset+len(items))
	}
	return output.WriteString(cmd.OutOrStdout(), text)
}

func runHistoryRevert(cmd *cobra.Command, args []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args[0])
	if !uuidRe.MatchString(id) {
		return bookDataUsage(fmt.Sprintf("invalid edit id %q", clean(args[0])), "Use an id from the ID column of 'konvu history'.")
	}
	revision, _ := cmd.Flags().GetString("revision")
	if revision = strings.TrimSpace(revision); revision == "" {
		return bookDataUsage("--revision is required", "Pass the object's current revision, as its 'get' prints it, such as 'konvu company get'.")
	}
	client := api.NewClient("", "")
	defer client.Close()
	result, err := client.Post(dataEditsPath+"/"+url.PathEscape(id)+"/revert", map[string]any{"revision": revision})
	if err != nil {
		if outcomeUnknown(err) {
			return writeOutcomeUnknown(err, "the revert", "konvu history")
		}
		return historyError(err, id)
	}
	// Only an answer that says so is a revert: an empty one, such as a proxy's, is not.
	if applied, _ := getBool(result, "applied"); !applied {
		return &clierrors.CLIError{
			Code:       "NOT_APPLIED",
			Message:    fmt.Sprintf("the server's answer does not show edit %s reverted", id),
			Suggestion: "Check KONVU_API_URL, then run 'konvu history' to see what was recorded.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), terminalJSON(result)+"\n")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Reverted edit %s.\n", id)
	for _, change := range stringsOf(getSlice(result, "changes")) {
		fmt.Fprintf(&b, "  %s\n", clean(change))
	}
	if revision := revisionText(result["revision"]); revision != "" {
		fmt.Fprintf(&b, "Now at revision %s. Konvu rewrites the Security Book's pages that show this data.\n", revision)
	}
	return output.WriteString(cmd.OutOrStdout(), b.String())
}

// historyError maps a refused history read (id "") or revert of edit id.
func historyError(err error, id string) error {
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return clierrors.NewAuthError(clean(authErr.Message))
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return clierrors.NewAPIError(clean(err.Error()))
	}
	detail := invariantDetail(apiErr)
	reverting := id != ""
	switch {
	case bookDataUnsupported(apiErr) || (apiErr.StatusCode == 404 && !reverting):
		return &clierrors.CLIError{
			Code:       "DATA_EDITS_UNAVAILABLE",
			Message:    "this Konvu server doesn't keep a history of data edits yet",
			Suggestion: "Check KONVU_API_URL, or try again once your Konvu server is up to date.",
			ExitCode:   clierrors.ExitNotFound,
		}
	case apiErr.StatusCode == 404:
		return &clierrors.CLIError{
			Code:       "NOT_FOUND",
			Message:    withDetail(fmt.Sprintf("no edit %s", id), detail),
			Suggestion: "Run 'konvu history' for the edits and their ids.",
			ExitCode:   clierrors.ExitNotFound,
		}
	case apiErr.StatusCode == 403 && reverting:
		return &clierrors.CLIError{
			Code:       "FORBIDDEN",
			Message:    withDetail("Konvu refused the revert: your key is read-only, or this is a demo view", detail),
			Suggestion: "Use a read-write API key, or log in as a person of the company with 'konvu login'.",
			ExitCode:   clierrors.ExitAuthFailed,
		}
	case apiErr.StatusCode == 403:
		return &clierrors.CLIError{
			Code: "FORBIDDEN", Message: orDefault(detail, "Konvu refused the request"),
			Suggestion: "Run 'konvu whoami' to check who you are signed in as, and for which company.",
			ExitCode:   clierrors.ExitAuthFailed,
		}
	case apiErr.StatusCode == 409:
		return &clierrors.CLIError{
			Code:       "DATA_CHANGED",
			Message:    withDetail(fmt.Sprintf("the data changed since, so edit %s was not reverted", id), detail),
			Suggestion: "Nothing was written. Run 'konvu history' to see the edits made since, and the object's 'get' for its current revision, then decide again.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	case apiErr.StatusCode == 422 && reverting:
		return &clierrors.CLIError{
			Code:       "INVALID_ARGUMENTS",
			Message:    fmt.Sprintf("edit %s cannot be reverted:\n  %s", id, strings.Join(validationProblems(apiErr), "\n  ")),
			Suggestion: "Run 'konvu history' to check the edit, and pass --revision as the object's 'get' prints it.",
			ExitCode:   clierrors.ExitUsageError,
		}
	case apiErr.StatusCode == 422:
		return bookDataUsage(orDefault(detail, "Konvu refused the arguments"), "Check --object, --key, --limit and --offset against 'konvu history --help'.")
	}
	return clierrors.NewAPIError(orDefault(detail, fmt.Sprintf("Konvu answered %d", apiErr.StatusCode)))
}

// historyWhen is an edit's time as "2006-01-02 15:04" UTC, or the server's text when it is not
// RFC 3339.
func historyWhen(value string) string {
	when, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return clean(value)
	}
	return when.UTC().Format("2006-01-02 15:04")
}

// historyValue is one side of BEFORE → AFTER as Konvu's change lines write it: a text, a list
// joined by commas, yes or no, "(none)" for null or empty; on one line, cut to historyValueRunes.
func historyValue(value any) string {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case bool:
		text = map[bool]string{true: "yes", false: "no"}[v]
	case []any:
		text = strings.Join(stringsOf(v), ", ")
	case nil:
	default:
		encoded, _ := json.Marshal(v)
		text = string(encoded)
	}
	if text = clean(text); text == "" {
		return "(none)"
	}
	if utf8.RuneCountInString(text) > historyValueRunes {
		text = string([]rune(text)[:historyValueRunes-1]) + "…"
	}
	return text
}

func init() {
	rootCmd.AddCommand(newHistoryCmd())
}
