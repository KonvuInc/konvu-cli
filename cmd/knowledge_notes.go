package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
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
	// The most notes the server lists in one answer.
	noteListMaxLimit = 200
	noteLabelRule    = "a label is 1 to 40 lowercase letters, digits and dashes, starting with a letter or digit, such as deployment or on-call"
	// What Konvu does with a note after any change to it.
	notePlacedHint = "Konvu places it in the Security Book and rewrites that page: 'konvu knowledge note list' shows where.\n"
)

// The same rule the server checks for a note's label.
var noteLabelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

func newKnowledgeNoteCmd() *cobra.Command {
	note := &cobra.Command{
		Use:   "note",
		Short: "Add, edit, list and remove notes: what your team knows that no other data of the book holds",
		Long: `A note is something your team knows that none of the data behind the Security
Book holds (company facts, threat model, threat profiles, endpoints, data
objects, catalog, principles), such as how a service is deployed or who owns it.
Each note has a label you choose, such as deployment or ownership, and names the
repositories it concerns.

After a note is added, edited or removed, Konvu places it in the book: it blends
it into one of its pages, such as infrastructure.md, or writes a new page for it
under context/. 'konvu knowledge note list' shows where each note went. People
never write the pages: to change what a page says, edit the notes behind it.

Every change to a note is in 'konvu history --object note', and
'konvu history revert <id>' undoes it.`,
		Example: `  konvu knowledge note add --label deployment --repo acme/api --text "Deployed to EKS by Argo CD from the deploy repo"
  konvu knowledge note list --label deployment
  konvu knowledge note edit 0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b4 --text "Deployed to EKS by Flux"
  konvu knowledge note rm 0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b4 --yes`,
	}
	add := &cobra.Command{
		Use:   "add --label <label> (--text <text> | --file <path>|-) [--repo <repo> ...]",
		Short: "Add a note",
		Long: `Add a note with --label and its text, from --text or from --file (or stdin
with --file -): UTF-8, at most 20 000 characters. The label is yours to choose:
1 to 40 lowercase letters, digits and dashes. --repo names a repository the note
concerns, as owner/name, a repository URL or its Konvu id; repeat it for each
one. Konvu then places the note in the Security Book and rewrites that page.

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
	add.Flags().String("label", "", "The note's label, such as deployment or ownership (required)")
	add.Flags().StringArray("repo", nil, "A repository the note concerns: owner/name, a repository URL or its Konvu id; repeat for each")
	add.Flags().String("text", "", "The note's text")
	add.Flags().String("file", "", "A file with the note's text, or '-' for stdin")
	add.Flags().StringP("output", "o", "", "Output format: json, table")

	edit := &cobra.Command{
		Use:   "edit <id> [--label <label>] [--text <text> | --file <path>|-] [--repo <repo> ...]",
		Short: "Edit a note",
		Long: `Edit a note, by its id from 'konvu knowledge note list'. Pass what changes:
--label, the text from --text or --file (or stdin with --file -), or --repo for
each repository the note concerns, which replaces its whole list. What you leave
out keeps its value. Konvu then places the note in the Security Book again and
rewrites that page. Each edit moves the note's revision up by one, and
'konvu history revert <id> --revision <revision>' undoes it.

Exit codes: 0 saved, 1 error, or a save that failed once sent and may have landed
(list the notes to check), 2 invalid arguments or a note the server refused
(every problem listed), 3 no such note, an unknown repository or a server that
does not support editing notes yet, 4 not logged in, a read-only key or the demo
view`,
		Example: `  konvu knowledge note edit 0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b4 --text "Deployed to EKS by Flux"
  konvu knowledge note edit 0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b4 --label on-call --repo acme/api --repo acme/web
  konvu knowledge note edit 0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b4 --file runbook.md -o json`,
		Args: cobra.ExactArgs(1),
		RunE: withJSONErrors(runNoteEdit),
	}
	edit.Flags().String("label", "", "The note's new label")
	edit.Flags().StringArray("repo", nil, "A repository the note concerns, replacing the list: owner/name, a repository URL or its Konvu id; repeat for each")
	edit.Flags().String("text", "", "The note's new text")
	edit.Flags().String("file", "", "A file with the note's new text, or '-' for stdin")
	edit.Flags().StringP("output", "o", "", "Output format: json, table")

	list := &cobra.Command{
		Use:   "list",
		Short: "List your team's notes, newest first",
		Long: `List your team's notes, newest first: id, label, the repositories each one
concerns, its author, when it was last changed, its revision (what
'konvu history revert' takes as --revision), the page of the Security Book Konvu
placed it on ("not placed yet" until Konvu has), and its text on one line,
shortened. -o json prints the server's answer, with the texts whole.

Exit codes: 0 success, 1 error, 2 invalid arguments, 3 an unknown repository or
a server that does not support notes yet, 4 not logged in or no access`,
		Example: `  konvu knowledge note list
  konvu knowledge note list --label deployment --repo acme/api
  konvu knowledge note list --limit 20 --offset 20 -o json`,
		Args: cobra.NoArgs,
		RunE: withJSONErrors(runNoteList),
	}
	list.Flags().String("label", "", "Only notes with this label")
	list.Flags().String("repo", "", "Only notes about this repository: owner/name, a repository URL or its Konvu id")
	list.Flags().Int("limit", 0, "How many notes to list (default: the server's)")
	list.Flags().Int("offset", 0, "How many of the newest notes to skip")
	list.Flags().StringP("output", "o", "", "Output format: json, table")

	rm := &cobra.Command{
		Use:   "rm <id>",
		Short: "Remove a note",
		Long: `Remove a note, by its id from 'konvu knowledge note list'. Konvu then rewrites
the page it was placed on without it. 'konvu history --object note --key <id>'
lists the removal, and 'konvu history revert <edit-id> --revision <revision>'
brings the note back: the removal moved the note one revision past the one
'note list' showed.

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
	note.AddCommand(add, edit, list, rm)
	return note
}

func runNoteAdd(cmd *cobra.Command, _ []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("label") {
		return knowledgeUsage("--label is required", "Pass a label for the note, such as deployment or ownership.")
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
	refs, err := noteRepoFlags(cmd)
	if err != nil {
		return err
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
	return printSavedNote(cmd, format, saved, "", listLabel)
}

func runNoteEdit(cmd *cobra.Command, args []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	id, err := noteID(args[0])
	if err != nil {
		return err
	}
	changes := map[string]any{}
	if value, _ := cmd.Flags().GetString("label"); cmd.Flags().Changed("label") {
		if changes["label"], err = noteLabel(value); err != nil {
			return err
		}
	}
	if cmd.Flags().Changed("text") || cmd.Flags().Changed("file") {
		if changes["text"], err = noteText(cmd); err != nil {
			return err
		}
	}
	refs, err := noteRepoFlags(cmd)
	if err != nil {
		return err
	}
	if len(changes) == 0 && len(refs) == 0 {
		return knowledgeUsage("nothing to change", "Pass --label, --text or --file, or --repo for each repository the note concerns.")
	}
	client := api.NewClient("", "")
	defer client.Close()
	if len(refs) > 0 {
		if changes["repository_ids"], err = noteRepositories(client, refs); err != nil {
			return err
		}
	}
	// The PUT replaces the whole note, so what is not changed is sent as it is now.
	current, err := findNote(client, id)
	if err != nil {
		return err
	}
	id = getStr(current, "id")
	body := map[string]any{"label": getStr(current, "label"), "text": getStr(current, "text"), "repository_ids": noteRepositoryIDs(current)}
	for key, value := range changes {
		body[key] = value
	}
	list := "konvu knowledge note list"
	saved, err := client.Put(knowledgeNotesPath+"/"+url.PathEscape(id), body)
	if err != nil {
		if outcomeUnknown(err) {
			return noteOutcomeUnknown(err, "the edit", fmt.Sprintf("Run '%s' to see whether note %s changed before editing it again.", list, id))
		}
		return noteError(err, "edit", id)
	}
	return printSavedNote(cmd, format, saved, id, list)
}

// printSavedNote prints the note an add or edit (of note id) saved, once its answer shows it saved.
func printSavedNote(cmd *cobra.Command, format output.OutputFormat, saved map[string]any, id, list string) error {
	// Only an answer naming the note shows it saved: an empty one, such as a proxy's, does not.
	if got := strings.TrimSpace(getStr(saved, "id")); got == "" || id != "" && hexID(got) != hexID(id) {
		return &clierrors.CLIError{
			Code:       "UNEXPECTED_ANSWER",
			Message:    "the server's answer does not show the note saved",
			Suggestion: fmt.Sprintf("Check KONVU_API_URL, then run '%s' to see whether it was.", list),
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	if format == output.JSON {
		if err := output.WriteString(cmd.ErrOrStderr(), notePlacedHint); err != nil {
			return err
		}
		return output.WriteString(cmd.OutOrStdout(), terminalJSON(saved)+"\n")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Saved note %s:\n", clean(getStr(saved, "id")))
	fmt.Fprintf(&b, "  label    %s\n", clean(getStr(saved, "label")))
	fmt.Fprintf(&b, "  repos    %s\n", noteRepos(saved))
	fmt.Fprintf(&b, "  author   %s\n", noteAuthor(saved["author"]))
	fmt.Fprintf(&b, "  changed  %s\n\n", historyWhen(noteWhen(saved)))
	b.WriteString(terminalText(getStr(saved, "text")) + "\n" + notePlacedHint)
	return output.WriteString(cmd.OutOrStdout(), b.String())
}

// findNote is note id as the list has it, read page by page: the server has no read of one note.
func findNote(client *api.Client, id string) (map[string]any, error) {
	for offset := 0; ; {
		result, err := client.Get(knowledgeNotesPath, map[string]any{"limit": noteListMaxLimit, "offset": offset})
		if err != nil {
			return nil, noteError(err, "list", "")
		}
		items, ok := result["items"].([]any)
		if !ok {
			return nil, clierrors.NewAPIError("the server's answer is not a list of notes")
		}
		for _, item := range items {
			// The server sends ids as hex without dashes; a UUID with them names the same note.
			if note, _ := item.(map[string]any); hexID(getStr(note, "id")) == hexID(id) {
				return note, nil
			}
		}
		offset += len(items)
		total, hasTotal := getFloat(result, "total")
		if len(items) == 0 || hasTotal && offset >= int(total) || !hasTotal && len(items) < noteListMaxLimit {
			return nil, noteNotFound(id, "")
		}
	}
}

func noteNotFound(id, detail string) error {
	return &clierrors.CLIError{
		Code: "NOT_FOUND", Message: withDetail("no note "+id, detail),
		Suggestion: "Run 'konvu knowledge note list' for the notes and their ids.", ExitCode: clierrors.ExitNotFound,
	}
}

// noteRepositoryIDs is the ids of the repositories a note concerns, as a PUT takes them.
func noteRepositoryIDs(note map[string]any) []string {
	ids := []string{}
	for _, item := range getSlice(note, "repositories") {
		if repo, _ := item.(map[string]any); getStr(repo, "id") != "" {
			ids = append(ids, getStr(repo, "id"))
		}
	}
	return ids
}

// noteRepoFlags is the --repo values, refused when one is blank.
func noteRepoFlags(cmd *cobra.Command) ([]string, error) {
	refs, _ := cmd.Flags().GetStringArray("repo")
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			return nil, noteEmptyRepo()
		}
	}
	return refs, nil
}

// noteWhen is when a note last changed: older servers send only when it was added.
func noteWhen(note map[string]any) string {
	return orDefault(getStr(note, "updated_at"), getStr(note, "created_at"))
}

// noteRevision is a note's revision, which 'konvu history revert' takes; "-" from an older server.
func noteRevision(note map[string]any) string {
	if revision, ok := getFloat(note, "revision"); ok {
		return fmt.Sprintf("%.0f", revision)
	}
	return "-"
}

// notePlacement is the page Konvu placed a note on; "-" from a server that does not place notes.
func notePlacement(note map[string]any) string {
	placement, ok := note["placement"]
	switch {
	case !ok:
		return "-"
	case placement == nil:
		return "not placed yet"
	}
	text, _ := placement.(string)
	return orDefault(clean(text), "not placed yet")
}

func noteID(value string) (string, error) {
	id := strings.TrimSpace(value)
	if !uuidRe.MatchString(id) {
		return "", knowledgeUsage(fmt.Sprintf("invalid note id %q", clean(value)), "Use an id from the ID column of 'konvu knowledge note list'.")
	}
	return id, nil
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
			historyWhen(noteWhen(note)),
			noteRevision(note),
			notePlacement(note),
			noteOneLine(getStr(note, "text")),
		})
	}
	text := alignedTable([]string{"ID", "LABEL", "REPOS", "AUTHOR", "WHEN", "REVISION", "PLACEMENT", "TEXT"}, rows)
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
	id, err := noteID(args[0])
	if err != nil {
		return err
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
		if !deleteConfirm(fmt.Sprintf("Remove note %s? Konvu rewrites the page it was placed on without it.", id), false) {
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
	return output.WriteString(cmd.OutOrStdout(), fmt.Sprintf("Removed note %s. Konvu rewrites the page it was placed on without it.\n'konvu history --object note --key %s' lists the removal; 'konvu history revert <edit-id> --revision <revision>' undoes it.\n", id, id))
}

// noteLabel is a label as the server takes it, in lowercase.
func noteLabel(value string) (string, error) {
	label := strings.ToLower(strings.TrimSpace(value))
	if !noteLabelPattern.MatchString(label) {
		return "", knowledgeUsage(fmt.Sprintf("invalid --label %q: %s", clean(value), noteLabelRule), "Pass a label such as deployment, ownership or on-call.")
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

// noteError maps a refused add, list, edit or rm (of note id) whose outcome is known: nothing was written.
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
		message := "this Konvu server doesn't support knowledge notes yet"
		if action == "edit" {
			message = "this Konvu server doesn't support editing notes yet"
		}
		return &clierrors.CLIError{
			Code:       "NOTES_UNAVAILABLE",
			Message:    message,
			Suggestion: "Check KONVU_API_URL, or try again once your Konvu server is up to date.",
			ExitCode:   clierrors.ExitNotFound,
		}
	case apiErr.StatusCode == 404 && (action == "rm" || action == "edit"):
		return noteNotFound(id, detail)
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
	case apiErr.StatusCode == 422 && (action == "add" || action == "edit"):
		return &clierrors.CLIError{
			Code:       "INVALID_DATA",
			Message:    fmt.Sprintf("Konvu refused the note, nothing was saved:\n  %s", strings.Join(validationProblems(apiErr), "\n  ")),
			Suggestion: "Fix them and run the same command again.",
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
