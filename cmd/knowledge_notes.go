package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const (
	knowledgeNotesPath = "/knowledge/notes"
	// The server's limit on a note's text, in characters.
	noteMaxChars = 20000
	// How much of a note's text the list's TEXT column shows.
	noteTextRunes = 60
)

// The labels a note takes: Konvu writes one page per label, at context/<label>.md.
var noteLabels = []string{"deployment", "ownership", "compliance", "architecture", "other"}

func newKnowledgeNoteCmd() *cobra.Command {
	note := &cobra.Command{
		Use:   "note",
		Short: "Add, list and remove notes: what your team knows that no other data of the book holds",
		Long: `A note is something your team knows that none of the data behind the Security
Book holds (company facts, threat model, threat profiles, endpoints, data
objects, catalog, principles), such as how a service is deployed or who owns it.
Each note has a label (deployment, ownership, compliance, architecture or other)
and names the repositories it concerns.

Konvu writes one page per label, context/<label>.md, from the notes with that
label, and rewrites it after a note is added or removed. The page is read-only:
'konvu book read context/deployment.md' prints it.`,
		Example: `  konvu knowledge note add --label deployment --repo acme/api --text "Deployed to EKS by Argo CD from the deploy repo"
  konvu knowledge note list --label deployment
  konvu knowledge note rm 0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b4 --yes`,
	}
	add := &cobra.Command{
		Use:   "add --label <label> (--text <text> | --file <path>|-) [--repo <repo> ...]",
		Short: "Add a note",
		Long: `Add a note with --label and its text, from --text or from --file (or stdin
with --file -): UTF-8, at most 20 000 characters. --repo names a repository the
note concerns, as owner/name, a repository URL or its Konvu id; repeat it for
each one. Konvu then rewrites context/<label>.md from the notes with that label.

Exit codes: 0 saved, 1 error, or a save that failed once sent and may have landed
(list the notes before adding it again), 2 invalid arguments or a note the server
refused (every problem listed), 3 an unknown repository or a server that does not
support notes yet, 4 not logged in, a read-only key or the demo view`,
		Example: `  konvu knowledge note add --label ownership --repo acme/api --repo acme/web --text "Team Payments owns both"
  konvu knowledge note add --label compliance --file soc2-scope.md
  cat runbook.md | konvu knowledge note add --label deployment --repo acme/api --file -`,
		Args: cobra.NoArgs,
		RunE: withJSONErrors(runNoteAdd),
	}
	add.Flags().String("label", "", "The note's label: "+strings.Join(noteLabels, ", ")+" (required)")
	add.Flags().StringArray("repo", nil, "A repository the note concerns: owner/name, a repository URL or its Konvu id; repeat for each")
	add.Flags().String("text", "", "The note's text")
	add.Flags().String("file", "", "A file with the note's text, or '-' for stdin")
	add.Flags().StringP("output", "o", "", "Output format: json, table")

	list := &cobra.Command{
		Use:   "list",
		Short: "List your team's notes, newest first",
		Long: `List your team's notes, newest first: id, label, the repositories each one
concerns, its author, when it was added, and its text on one line, shortened.
-o json prints the server's answer, with the texts whole.

Exit codes: 0 success, 1 error, 2 invalid arguments, 3 an unknown repository or
a server that does not support notes yet, 4 not logged in or no access`,
		Example: `  konvu knowledge note list
  konvu knowledge note list --label deployment --repo acme/api
  konvu knowledge note list --limit 20 --offset 20 -o json`,
		Args: cobra.NoArgs,
		RunE: withJSONErrors(runNoteList),
	}
	list.Flags().String("label", "", "Only notes with this label: "+strings.Join(noteLabels, ", "))
	list.Flags().String("repo", "", "Only notes about this repository: owner/name, a repository URL or its Konvu id")
	list.Flags().Int("limit", 0, "How many notes to list (default: the server's)")
	list.Flags().Int("offset", 0, "How many of the newest notes to skip")
	list.Flags().StringP("output", "o", "", "Output format: json, table")

	rm := &cobra.Command{
		Use:   "rm <id>",
		Short: "Remove a note",
		Long: `Remove a note, by its id from 'konvu knowledge note list'. Konvu then rewrites
the page of its label without it. There is no undo: add the note again to
restore it.

rm asks before removing the note. Without a terminal to ask on (a script or a
coding agent), --yes is required.

Exit codes: 0 removed, 1 error, or a removal that failed once sent (list the notes
to check), 2 invalid id, or --yes missing without a terminal, 3 no such note or
a server that does not support notes yet, 4 not logged in, a read-only key or
the demo view`,
		Example: `  konvu knowledge note rm 0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b4
  konvu knowledge note rm 0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b4 --yes -o json`,
		Args: cobra.ExactArgs(1),
		RunE: withJSONErrors(runNoteRm),
	}
	rm.Flags().BoolP("yes", "y", false, "Remove without asking")
	rm.Flags().StringP("output", "o", "", "Output format: json, table")
	note.AddCommand(add, list, rm)
	return note
}

func runNoteAdd(cmd *cobra.Command, _ []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("label") {
		return knowledgeUsage("--label is required", "Pass one of: "+strings.Join(noteLabels, ", ")+".")
	}
	value, _ := cmd.Flags().GetString("label")
	label, err := noteLabel(value)
	if err != nil {
		return err
	}
	text, err := noteText(cmd)
	if err != nil {
		return err
	}
	refs, _ := cmd.Flags().GetStringArray("repo")
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			return noteEmptyRepo()
		}
	}
	client := api.NewClient("", "")
	defer client.Close()
	ids, err := noteRepositories(client, refs)
	if err != nil {
		return err
	}
	listLabel := "konvu knowledge note list --label " + label
	saved, err := client.Post(knowledgeNotesPath, map[string]any{"label": label, "text": text, "repository_ids": ids})
	if err != nil {
		if outcomeUnknown(err) {
			return noteOutcomeUnknown(err, "the save", fmt.Sprintf("Run '%s' to see whether it was saved before adding it again: a second add saves a second note.", listLabel))
		}
		return noteError(err, "add", "")
	}
	// Only an answer naming the new note shows it saved: an empty one, such as a proxy's, does not.
	if id, _ := saved["id"].(string); strings.TrimSpace(id) == "" {
		return &clierrors.CLIError{
			Code:       "UNEXPECTED_ANSWER",
			Message:    "the server's answer does not show the note saved",
			Suggestion: fmt.Sprintf("Check KONVU_API_URL, then run '%s' to see whether it was.", listLabel),
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	page := "context/" + label + ".md"
	rewrite := fmt.Sprintf("Konvu will rewrite %s from your notes labeled %s: 'konvu book read %s' prints it.\n", page, label, page)
	if format == output.JSON {
		if err := output.WriteString(cmd.ErrOrStderr(), rewrite); err != nil {
			return err
		}
		return output.WriteString(cmd.OutOrStdout(), terminalJSON(saved)+"\n")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Saved note %s:\n", clean(getStr(saved, "id")))
	fmt.Fprintf(&b, "  label    %s\n", clean(getStr(saved, "label")))
	fmt.Fprintf(&b, "  repos    %s\n", noteRepos(saved))
	fmt.Fprintf(&b, "  author   %s\n", noteAuthor(saved["author"]))
	fmt.Fprintf(&b, "  created  %s\n\n", historyWhen(getStr(saved, "created_at")))
	b.WriteString(terminalText(getStr(saved, "text")) + "\n" + rewrite)
	return output.WriteString(cmd.OutOrStdout(), b.String())
}

func runNoteList(cmd *cobra.Command, _ []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	params := map[string]any{}
	if value, _ := cmd.Flags().GetString("label"); cmd.Flags().Changed("label") {
		label, err := noteLabel(value)
		if err != nil {
			return err
		}
		params["label"] = label
	}
	if cmd.Flags().Changed("limit") {
		limit, _ := cmd.Flags().GetInt("limit")
		if limit < 1 {
			return knowledgeUsage("--limit must be at least 1", "Pass how many notes to list, newest first; --offset skips the newest ones.")
		}
		params["limit"] = limit
	}
	offset, _ := cmd.Flags().GetInt("offset")
	if offset < 0 {
		return knowledgeUsage("--offset cannot be negative", "Pass how many of the newest notes to skip.")
	}
	if offset > 0 {
		params["offset"] = offset
	}
	ref, _ := cmd.Flags().GetString("repo")
	if cmd.Flags().Changed("repo") && strings.TrimSpace(ref) == "" {
		return noteEmptyRepo()
	}
	client := api.NewClient("", "")
	defer client.Close()
	if cmd.Flags().Changed("repo") {
		id, err := bookDataRepository(client, ref)
		if err != nil {
			return err
		}
		params["repository_id"] = id
	}
	result, err := client.Get(knowledgeNotesPath, params)
	if err != nil {
		return noteError(err, "list", "")
	}
	items, ok := result["items"].([]any)
	if !ok {
		return clierrors.NewAPIError("the server's answer is not a list of notes")
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), terminalJSON(result)+"\n")
	}
	total, hasTotal := getFloat(result, "total")
	if len(items) == 0 {
		switch {
		case offset > 0 && hasTotal:
			return output.WriteString(cmd.OutOrStdout(), fmt.Sprintf("No notes past the newest %d; there are %.0f.\n", offset, total))
		case params["label"] != nil || params["repository_id"] != nil:
			return output.WriteString(cmd.OutOrStdout(), "No notes match --label or --repo.\n")
		}
		return output.WriteString(cmd.OutOrStdout(), "No notes yet. Add one with 'konvu knowledge note add'.\n")
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		note, _ := item.(map[string]any)
		rows = append(rows, []string{
			clean(getStr(note, "id")),
			clean(getStr(note, "label")),
			noteRepos(note),
			noteAuthor(note["author"]),
			historyWhen(getStr(note, "created_at")),
			noteOneLine(getStr(note, "text")),
		})
	}
	text := alignedTable([]string{"ID", "LABEL", "REPOS", "AUTHOR", "WHEN", "TEXT"}, rows)
	if hasTotal && int(total) > offset+len(items) {
		text += fmt.Sprintf("Showing notes %d to %d of %.0f, newest first. Pass --offset %d to see the next ones.\n", offset+1, offset+len(items), total, offset+len(items))
	}
	return output.WriteString(cmd.OutOrStdout(), text)
}

func runNoteRm(cmd *cobra.Command, args []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args[0])
	if !uuidRe.MatchString(id) {
		return knowledgeUsage(fmt.Sprintf("invalid note id %q", clean(args[0])), "Use an id from the ID column of 'konvu knowledge note list'.")
	}
	if yes, _ := cmd.Flags().GetBool("yes"); !yes {
		if !deleteCanPrompt() {
			return &clierrors.CLIError{
				Code:       "CONFIRMATION_REQUIRED",
				Message:    fmt.Sprintf("refusing to remove note %s without --yes: there is no terminal to ask on", id),
				Suggestion: fmt.Sprintf("Check it is the one to remove with 'konvu knowledge note list', then run 'konvu knowledge note rm %s --yes'.", id),
				ExitCode:   clierrors.ExitUsageError,
			}
		}
		if !deleteConfirm(fmt.Sprintf("Remove note %s? Konvu rewrites the page of its label without it.", id), false) {
			return &clierrors.CLIError{Code: "NOT_CONFIRMED", Message: "not removed: note " + id, Suggestion: "Nothing was written.", ExitCode: clierrors.ExitGeneralError}
		}
	}
	client := api.NewClient("", "")
	defer client.Close()
	if _, err := client.Delete(knowledgeNotesPath+"/"+url.PathEscape(id), nil); err != nil {
		if outcomeUnknown(err) {
			return noteOutcomeUnknown(err, "the removal", fmt.Sprintf("Run 'konvu knowledge note list' to see whether note %s is still there before removing it again.", id))
		}
		return noteError(err, "rm", id)
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), terminalJSON(map[string]any{"id": id, "deleted": true})+"\n")
	}
	return output.WriteString(cmd.OutOrStdout(), fmt.Sprintf("Removed note %s. Konvu rewrites the page of its label without it.\n", id))
}

// noteLabel is a label as the server takes it, in lowercase.
func noteLabel(value string) (string, error) {
	label := strings.ToLower(strings.TrimSpace(value))
	if !slices.Contains(noteLabels, label) {
		return "", knowledgeUsage(fmt.Sprintf("unknown --label %q", clean(value)), "Use one of: "+strings.Join(noteLabels, ", ")+".")
	}
	return label, nil
}

// noteText reads the note's text from --text or --file like a page's text: UTF-8 with no NUL, not
// empty, CRLF made LF, and refused rather than cut over the server's limit.
func noteText(cmd *cobra.Command) (string, error) {
	hasText, hasFile := cmd.Flags().Changed("text"), cmd.Flags().Changed("file")
	if hasText == hasFile {
		return "", knowledgeUsage("pass the note's text with exactly one of --text and --file", "Use --text \"...\", or --file <path> (--file - reads stdin).")
	}
	text, _ := cmd.Flags().GetString("text")
	if hasFile {
		file, _ := cmd.Flags().GetString("file")
		var reader io.Reader = cmd.InOrStdin()
		if file != "-" {
			opened, err := os.Open(file)
			if err != nil {
				return "", knowledgeUsage(fmt.Sprintf("cannot read %s", clean(file)), "Pass a readable text file, or --file - for stdin.")
			}
			defer opened.Close()
			reader = opened
		}
		// A character is at most 4 bytes, so more bytes than this is over the limit for sure.
		data, err := io.ReadAll(io.LimitReader(reader, 4*noteMaxChars+1))
		if err != nil {
			return "", knowledgeUsage(fmt.Sprintf("cannot read %s", clean(file)), "Pass a readable text file, or --file - for stdin.")
		}
		text = string(data)
	}
	if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return "", knowledgeUsage("the note's text must be UTF-8 with no NUL character", "Save the text as UTF-8.")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return "", knowledgeUsage("the note's text is empty", "Write what the note should say.")
	}
	if utf8.RuneCountInString(text) > noteMaxChars {
		return "", knowledgeUsage("the note's text is over 20 000 characters", "Shorten it, or split it into several notes: the server refuses a longer one.")
	}
	return text, nil
}

// noteRepositories resolves each --repo like threat-profile's, looking the repositories up once,
// and drops a repository named twice.
func noteRepositories(client *api.Client, refs []string) ([]string, error) {
	lookup := sync.OnceValues(func() (map[string][]konvuRepository, error) {
		return konvuRepositoriesByName(client, repositoryLookupTimeout)
	})
	ids := []string{}
	seen := map[string]bool{}
	for _, ref := range refs {
		id, err := repositoryByRef(ref, lookup)
		if err != nil {
			return nil, err
		}
		if !seen[hexID(id)] {
			seen[hexID(id)] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func noteEmptyRepo() error {
	return knowledgeUsage("--repo is empty", "Pass the repository as owner/name, such as acme/api, a repository URL, or its Konvu id.")
}

// noteRepos names a note's repositories on one line, or "-" for none.
func noteRepos(note map[string]any) string {
	var names []string
	for _, item := range getSlice(note, "repositories") {
		repo, _ := item.(map[string]any)
		names = append(names, clean(orDefault(getStr(repo, "name"), getStr(repo, "id"))))
	}
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ", ")
}

// noteAuthor is who wrote a note: the server's text, or the name or email of the person it sends.
func noteAuthor(value any) string {
	switch author := value.(type) {
	case string:
		return clean(author)
	case map[string]any:
		return clean(orDefault(getStr(author, "name"), getStr(author, "email")))
	}
	return ""
}

// noteOneLine is a note's text on one line, cut to noteTextRunes.
func noteOneLine(text string) string {
	text = clean(text)
	if utf8.RuneCountInString(text) > noteTextRunes {
		text = string([]rune(text)[:noteTextRunes-1]) + "…"
	}
	return text
}

// noteOutcomeUnknown is the error for a write whose request failed once sent: a server error or a
// lost connection can come after the server committed it.
func noteOutcomeUnknown(err error, write, suggestion string) error {
	reason := clean(err.Error())
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		reason = orDefault(invariantDetail(apiErr), fmt.Sprintf("Konvu answered %d", apiErr.StatusCode))
	}
	return &clierrors.CLIError{
		Code:       "OUTCOME_UNKNOWN",
		Message:    fmt.Sprintf("%s failed once sent (%s), so it may have been applied", write, reason),
		Suggestion: suggestion,
		ExitCode:   clierrors.ExitGeneralError,
	}
}

// noteError maps a refused add, list or rm (of note id) whose outcome is known: nothing was written.
func noteError(err error, action, id string) error {
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return clierrors.NewAuthError(clean(authErr.Message))
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return clierrors.NewAPIError(clean(err.Error()))
	}
	detail := invariantDetail(apiErr)
	switch {
	case bookDataUnsupported(apiErr):
		return &clierrors.CLIError{
			Code:       "NOTES_UNAVAILABLE",
			Message:    "this Konvu server doesn't support knowledge notes yet",
			Suggestion: "Check KONVU_API_URL, or try again once your Konvu server is up to date.",
			ExitCode:   clierrors.ExitNotFound,
		}
	case apiErr.StatusCode == 404 && action == "rm":
		return &clierrors.CLIError{
			Code: "NOT_FOUND", Message: withDetail("no note "+id, detail),
			Suggestion: "Run 'konvu knowledge note list' for the notes and their ids.", ExitCode: clierrors.ExitNotFound,
		}
	case apiErr.StatusCode == 404:
		return &clierrors.CLIError{
			Code: "NOT_FOUND", Message: orDefault(detail, "not found"),
			Suggestion: "Run 'konvu coverage list' to see your repositories and their ids.", ExitCode: clierrors.ExitNotFound,
		}
	case apiErr.StatusCode == 403 && action != "list":
		return &clierrors.CLIError{
			Code:       "FORBIDDEN",
			Message:    withDetail("Konvu refused the change: your key is read-only, or this is a demo view", detail),
			Suggestion: "Use a read-write API key, or log in as a person of the company with 'konvu login'.",
			ExitCode:   clierrors.ExitAuthFailed,
		}
	case apiErr.StatusCode == 403:
		return &clierrors.CLIError{
			Code: "FORBIDDEN", Message: orDefault(detail, "Konvu refused the request"),
			Suggestion: "Run 'konvu whoami' to check who you are signed in as, and for which company.",
			ExitCode:   clierrors.ExitAuthFailed,
		}
	case apiErr.StatusCode == 422 && action == "add":
		return &clierrors.CLIError{
			Code:       "INVALID_DATA",
			Message:    fmt.Sprintf("Konvu refused the note, nothing was saved:\n  %s", strings.Join(validationProblems(apiErr), "\n  ")),
			Suggestion: "Fix them and add the note again.",
			ExitCode:   clierrors.ExitUsageError,
		}
	case apiErr.StatusCode == 422:
		return knowledgeUsage(orDefault(detail, "Konvu refused the arguments"), fmt.Sprintf("Check the arguments against 'konvu knowledge note %s --help'.", action))
	}
	return clierrors.NewAPIError(orDefault(detail, fmt.Sprintf("Konvu answered %d", apiErr.StatusCode)))
}

func init() {
	knowledgeCmd.AddCommand(newKnowledgeNoteCmd())
}
