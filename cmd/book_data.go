package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const (
	companyFactsPath     = "/company_profile/facts/company"
	threatModelFactsPath = "/company_profile/facts/threat-model"
	// A data document is a few hundred facts; 4 MiB is far above any real one.
	bookDataMaxBytes = 4 << 20
)

// bookDataObject is data behind Security Book pages that a person edits as one JSON document:
// get prints it, apply sends it back with the person's edits.
type bookDataObject struct {
	// name is the command, and history's --object.
	name string
	// what names the data in messages.
	what string
	// required are the document's objects besides "revision": a file without one is refused, as
	// not a document 'get' printed.
	required []string
	// readOnly are the keys 'get' prints that apply never sends: Konvu sets them.
	readOnly []string
	// byRepo is true for data kept per repository, addressed with --repo.
	byRepo bool
	path   string
	long   string
}

var (
	companyData = bookDataObject{
		name: "company", what: "company facts", required: []string{"company", "infrastructure"}, readOnly: []string{"provenance"}, path: companyFactsPath,
		long: `What Konvu knows about your company and its infrastructure, as one JSON document:

  revision        the version you read; apply is refused once it changed since
  company         each fact about the company, by key: a text, a list of texts,
                  or null for none
  infrastructure  each fact about where and how it runs, the same way
  provenance      who set each fact (Konvu or a person): read-only, never sent

Konvu writes company.md and infrastructure.md in your Security Book from these
facts, and rewrites them after a change. Every change is recorded: 'konvu history'
lists them and 'konvu history revert' undoes one.`,
	}
	threatModelData = bookDataObject{
		name: "threat-model", what: "threat model", required: []string{"threat_model"}, readOnly: []string{"provenance", "overrides"}, path: threatModelFactsPath,
		long: `Your company's threat model as one JSON document:

  revision      the version you read; apply is refused once it changed since
  threat_model  what attackers would go after, by key (data_classes,
                attackers_of_interest, assets, boundaries, exposures): a list
                of texts, or null for none
  provenance    who set each value (Konvu or a person): read-only, never sent
  overrides     the items people added to or hid from each section:
                read-only, never sent

The sections assets, boundaries and exposures show exactly the list you apply:
an item you add is kept, and one you leave out stays hidden, even after later
Konvu runs find it again.

Any other key 'get' prints is sent back as it is in your file. Konvu writes
threat-model.md in your Security Book from this data, and rewrites it after a
change. Every change is recorded: 'konvu history' lists them and
'konvu history revert' undoes one.`,
	}
	threatProfileData = bookDataObject{
		name: "threat-profile", what: "repository threat profile", required: []string{"attributes"}, readOnly: []string{"provenance"}, byRepo: true,
		long: `One repository's threat profile attributes as one JSON document, such as
whether it is internet exposed or holds customer data:

  revision    the version you read; apply is refused once it changed since
  attributes  each attribute, by key: its value, or null
  provenance  who set each attribute (Konvu or a person): read-only, never sent

--repo is the repository as owner/name, a repository URL, or its Konvu id.
Konvu recomputes the repository's score and rewrites the book's pages that show
it after a change. Every change is recorded: 'konvu history' lists them and
'konvu history revert' undoes one.`,
	}
)

func newBookDataCmd(object bookDataObject) *cobra.Command {
	repo := ""
	if object.byRepo {
		repo = " --repo acme/api"
	}
	parent := &cobra.Command{
		Use:   object.name,
		Short: fmt.Sprintf("Read and edit your %s, the data behind your Security Book", object.what),
		Long:  object.long,
		Example: fmt.Sprintf(`  konvu %[1]s get%[2]s > %[1]s.json
  konvu %[1]s apply%[2]s --file %[1]s.json --dry-run
  konvu %[1]s apply%[2]s --file %[1]s.json`, object.name, repo),
	}
	get := &cobra.Command{
		Use:   "get",
		Short: fmt.Sprintf("Print your %s as a JSON document, ready to edit", object.what),
		Args:  cobra.NoArgs,
		RunE:  withJSONErrors(func(cmd *cobra.Command, _ []string) error { return runBookDataGet(cmd, object) }),
	}
	get.Flags().StringP("output", "o", "", "Output format: json (the only one: get prints the document)")
	apply := &cobra.Command{
		Use:   "apply",
		Short: fmt.Sprintf("Make your %s match a file", object.what),
		Long: fmt.Sprintf(`Make your %[1]s match --file (or stdin with '-'), a document as
'konvu %[2]s get' prints it, with your edits: %[3]s
are required. A value left out of the file keeps its value; null clears it.

"revision" names the version the file was read from: once the data changed
since (someone else's edit, or Konvu's), apply is refused and writes nothing.
Get it again and redo the edit then. %[4]s read-only and never sent.

--dry-run lists the changes and writes nothing. Nothing is written either when
the file has a problem: every problem is listed. After a change, Konvu rewrites
the Security Book's pages that show this data.

Exit codes: 0 success, 1 the data changed since you read it or another error,
2 invalid file, 3 not found or a server that does not support this yet, 4 not
logged in, no access, a read-only key or the demo view`, object.what, object.name, requiredKeys(object), readOnlyKeys(object)),
		Example: fmt.Sprintf(`  konvu %[1]s apply%[2]s --file %[1]s.json --dry-run
  konvu %[1]s apply%[2]s --file %[1]s.json
  konvu %[1]s get%[2]s | jq '...' | konvu %[1]s apply%[2]s --file - --dry-run`, object.name, repo),
		Args: cobra.NoArgs,
		RunE: withJSONErrors(func(cmd *cobra.Command, _ []string) error { return runBookDataApply(cmd, object) }),
	}
	apply.Flags().String("file", "", "JSON document to apply, or '-' for stdin")
	apply.Flags().Bool("dry-run", false, "List the changes without writing anything")
	apply.Flags().StringP("output", "o", "", "Output format: json, table")
	for _, command := range []*cobra.Command{get, apply} {
		if object.byRepo {
			command.Flags().String("repo", "", "Repository: owner/name, a repository URL, or its Konvu id (required)")
		}
	}
	parent.AddCommand(get, apply)
	return parent
}

// requiredKeys lists the keys an apply needs, quoted: "revision", "company" and "infrastructure".
func requiredKeys(object bookDataObject) string {
	return quotedList(append([]string{"revision"}, object.required...))
}

// readOnlyKeys names the keys apply never sends, as the subject of "is" or "are".
func readOnlyKeys(object bookDataObject) string {
	if len(object.readOnly) == 1 {
		return strconv.Quote(object.readOnly[0]) + " is"
	}
	return quotedList(object.readOnly) + " are"
}

func quotedList(keys []string) string {
	quoted := make([]string, len(keys))
	for i, key := range keys {
		quoted[i] = strconv.Quote(key)
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
}

func runBookDataGet(cmd *cobra.Command, object bookDataObject) error {
	if explicit, _ := cmd.Flags().GetString("output"); explicit != "" && !strings.EqualFold(strings.TrimSpace(explicit), "json") {
		return bookDataUsage(fmt.Sprintf("unsupported output format %q", explicit), "get prints the JSON document: pass -o json or leave -o out.")
	}
	client := api.NewClient("", "")
	defer client.Close()
	route, err := bookDataRoute(cmd, client, object)
	if err != nil {
		return err
	}
	// Read as text and only indented, so every value prints exactly as Konvu holds it: apply then
	// sends back unchanged what the person did not edit.
	raw, err := client.GetText(route, nil, bookDataMaxBytes)
	if err != nil {
		return bookDataError(err, object, false)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, []byte(raw), "", "  "); err != nil || !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return clierrors.NewAPIError(fmt.Sprintf("the server's answer is not your %s as a JSON object", object.what))
	}
	return output.WriteString(cmd.OutOrStdout(), terminalSafeJSON(indented.String())+"\n")
}

func runBookDataApply(cmd *cobra.Command, object bookDataObject) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	file, _ := cmd.Flags().GetString("file")
	if strings.TrimSpace(file) == "" {
		return bookDataUsage("--file is required", fmt.Sprintf("Pass the edited output of '%s', or '-' to read stdin.", bookDataGetCommand(object)))
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	doc, err := readBookDataFile(cmd, file, object)
	if err != nil {
		return err
	}
	client := api.NewClient("", "")
	defer client.Close()
	route, err := bookDataRoute(cmd, client, object)
	if err != nil {
		return err
	}
	result, err := client.Put(route+"?dry_run="+strconv.FormatBool(dryRun), doc)
	if err != nil {
		if !dryRun && outcomeUnknown(err) {
			return writeOutcomeUnknown(err, "the apply", "konvu history --object "+object.name)
		}
		return bookDataError(err, object, true)
	}
	applied, known := getBool(result, "applied")
	changes := stringsOf(getSlice(result, "changes"))
	answerErr := bookDataAnswerError(object, known, applied, dryRun, changes)
	if format == output.JSON {
		// One JSON document: the answer, or the error carrying what it said, fit for a terminal.
		if answerErr != nil {
			cleaned := make([]string, len(changes))
			for i, change := range changes {
				cleaned[i] = clean(change)
			}
			answerErr.Context = map[string]any{"applied": applied, "changes": cleaned, "revision": revisionText(result["revision"])}
			return answerErr
		}
		return output.WriteString(cmd.OutOrStdout(), terminalJSON(result)+"\n")
	}
	// An answer that fails its checks prints nothing as if it had worked: the error says what it did.
	if answerErr != nil {
		return answerErr
	}
	return output.WriteString(cmd.OutOrStdout(), bookDataChanges(object, result, applied, changes))
}

// bookDataAnswerError is the error for an apply's answer that does not show the write as asked, or
// nil: an answer is never reported as success unless it says the changes were applied, or that a
// dry run wrote nothing.
func bookDataAnswerError(object bookDataObject, known, applied, dryRun bool, changes []string) *clierrors.CLIError {
	switch {
	case !known:
		return &clierrors.CLIError{
			Code:       "UNEXPECTED_ANSWER",
			Message:    fmt.Sprintf("the server's answer does not say whether the changes to your %s were applied", object.what),
			Suggestion: fmt.Sprintf("Check KONVU_API_URL, then run '%s' and 'konvu history' to see what it holds now.", bookDataGetCommand(object)),
			ExitCode:   clierrors.ExitGeneralError,
		}
	case dryRun && applied:
		lines := ""
		for _, change := range changes {
			lines += "\n  " + clean(change)
		}
		return &clierrors.CLIError{
			Code:       "DRY_RUN_APPLIED",
			Message:    fmt.Sprintf("the server applied %s to your %s though --dry-run was passed%s", plural(len(changes), "change"), object.what, lines),
			Suggestion: "Run 'konvu history' to see the edit, and 'konvu history revert <id> --revision <revision>' to undo it.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	case !dryRun && !applied && len(changes) > 0:
		return &clierrors.CLIError{
			Code:       "NOT_APPLIED",
			Message:    fmt.Sprintf("the server's answer lists %s to your %s but does not show them applied", plural(len(changes), "change"), object.what),
			Suggestion: fmt.Sprintf("Check KONVU_API_URL, then run '%s' to see what it holds now.", bookDataGetCommand(object)),
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return nil
}

// bookDataChanges prints an apply's answer that passed bookDataAnswerError: what changed or, on a dry
// run, would change, then the new revision.
func bookDataChanges(object bookDataObject, result map[string]any, applied bool, changes []string) string {
	var b strings.Builder
	switch {
	case applied:
		fmt.Fprintf(&b, "Applied %s:\n", plural(len(changes), "change"))
	case len(changes) == 0:
		fmt.Fprintf(&b, "No changes: the file matches your %s. Nothing was written.\n", object.what)
	default:
		fmt.Fprintf(&b, "Would apply %s (dry run, nothing written):\n", plural(len(changes), "change"))
	}
	for _, change := range changes {
		fmt.Fprintf(&b, "  %s\n", clean(change))
	}
	if applied {
		if revision := revisionText(result["revision"]); revision != "" {
			fmt.Fprintf(&b, "Now at revision %s. Konvu rewrites the Security Book's pages that show this data.\n", revision)
		}
	}
	return b.String()
}

// readBookDataFile reads a document as 'get' prints it, keeping every value as it is (numbers
// included), and refuses one that is not: no revision, as the text 'get' printed, or a required
// object missing. Its read-only keys (provenance, overrides) are never sent.
func readBookDataFile(cmd *cobra.Command, file string, object bookDataObject) (map[string]any, error) {
	start := fmt.Sprintf("Start from '%s' and edit that file.", bookDataGetCommand(object))
	var reader io.Reader = cmd.InOrStdin()
	if file != "-" {
		opened, err := os.Open(file)
		if err != nil {
			return nil, bookDataInvalid(fmt.Sprintf("cannot read %s", clean(file)), "Pass a readable JSON file, or '-' for stdin.")
		}
		defer opened.Close()
		reader = opened
	}
	data, err := io.ReadAll(io.LimitReader(reader, bookDataMaxBytes+1))
	if err != nil {
		return nil, bookDataInvalid(fmt.Sprintf("cannot read %s", clean(file)), "Pass a readable JSON file, or '-' for stdin.")
	}
	if len(data) > bookDataMaxBytes {
		return nil, bookDataInvalid("the file is over 4 MiB", start)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil || doc == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, bookDataInvalid(fmt.Sprintf("expected one JSON object, as '%s' prints it", bookDataGetCommand(object)), start)
	}
	var problems []string
	if revision, ok := doc["revision"].(string); !ok || strings.TrimSpace(revision) == "" {
		problems = append(problems, `no "revision" as 'get' printed it`)
	}
	for _, key := range object.required {
		if _, ok := doc[key].(map[string]any); !ok {
			problems = append(problems, fmt.Sprintf("no %q object", key))
		}
	}
	if len(problems) > 0 {
		return nil, bookDataInvalid(fmt.Sprintf("the file has %s", strings.Join(problems, "; ")), start)
	}
	for _, key := range object.readOnly {
		delete(doc, key)
	}
	return doc, nil
}

// bookDataRoute is the object's route; for a repository's data it resolves --repo first.
func bookDataRoute(cmd *cobra.Command, client *api.Client, object bookDataObject) (string, error) {
	if !object.byRepo {
		return object.path, nil
	}
	ref, _ := cmd.Flags().GetString("repo")
	id, err := bookDataRepository(client, ref)
	if err != nil {
		return "", err
	}
	return threatProfileRepoPath + url.PathEscape(id) + "/attributes", nil
}

// bookDataRepository resolves --repo to a Konvu repository id: an id as given, else the one
// repository whose owner/name it is, exactly (any case). A name of several repositories, such as
// a GitHub and a GitLab one, is refused unless --repo names where it lives.
func bookDataRepository(client *api.Client, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", bookDataUsage("--repo is required", "Pass the repository as owner/name, such as acme/api, or its Konvu id.")
	}
	if uuidRe.MatchString(ref) {
		return strings.ToLower(ref), nil
	}
	name, _ := konvuRepositoryName(ref)
	if name == "" {
		name = strings.Trim(ref, "/")
	}
	if len(name) > maxRepositoryChars || !repositoryNamePattern.MatchString(name) || slices.ContainsFunc(strings.Split(name, "/"), isDotSegment) {
		return "", bookDataUsage(fmt.Sprintf("--repo %q is not a repository", clean(ref)), "Pass the repository as owner/name, such as acme/api, a repository URL, or its Konvu id.")
	}
	byName, err := konvuRepositoriesByName(client, repositoryLookupTimeout)
	if err != nil {
		var authErr *api.AuthenticationError
		if errors.As(err, &authErr) {
			return "", clierrors.NewAuthError(clean(authErr.Message))
		}
		return "", clierrors.NewAPIError(fmt.Sprintf("could not look up repository %s (%s)", clean(name), lookupReason(err)))
	}
	matches := byName[strings.ToLower(name)]
	// A --repo that names where the repository lives matches only there.
	where := name
	if host := repositoryHost(ref); host != "" {
		matches = slices.DeleteFunc(slices.Clone(matches), func(match konvuRepository) bool { return match.host != host })
		where = host + "/" + name
	}
	switch len(matches) {
	case 1:
		return matches[0].id, nil
	case 0:
		return "", &clierrors.CLIError{
			Code:       "REPOSITORY_NOT_FOUND",
			Message:    fmt.Sprintf("none of your Konvu repositories is %s", clean(where)),
			Suggestion: "Run 'konvu coverage list' to see your repositories and their ids.",
			ExitCode:   clierrors.ExitNotFound,
		}
	}
	found := make([]string, len(matches))
	for i, match := range matches {
		found[i] = match.label + " (id " + match.id + ")"
	}
	return "", bookDataUsage(fmt.Sprintf("%d Konvu repositories are %s: %s", len(matches), clean(name), strings.Join(found, ", ")), "Pass the id of the one you mean with --repo.")
}

func isDotSegment(segment string) bool { return segment == "." || segment == ".." }

// The hosts of the short repository URLs Konvu uses, like github:acme/api.
var repositorySchemeHosts = map[string]string{"github": "github.com", "gitlab": "gitlab.com", "bitbucket": "bitbucket.org"}

// repositoryHost is the host a repository URL names, lowercased and without its port, so
// github:acme/api and https://github.com/acme/api are both github.com; "" when it names none.
func repositoryHost(repoURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(repoURL))
	if err != nil || parsed.Scheme == "" {
		return ""
	}
	if parsed.Opaque != "" {
		scheme := strings.ToLower(parsed.Scheme)
		return orDefault(repositorySchemeHosts[scheme], scheme)
	}
	return strings.ToLower(parsed.Hostname())
}

// bookDataGetCommand is the command that prints the object's document.
func bookDataGetCommand(object bookDataObject) string {
	if object.byRepo {
		return "konvu " + object.name + " get --repo <repo>"
	}
	return "konvu " + object.name + " get"
}

func bookDataUsage(message, suggestion string) error {
	return &clierrors.CLIError{Code: "INVALID_ARGUMENTS", Message: message, Suggestion: suggestion, ExitCode: clierrors.ExitUsageError}
}

func bookDataInvalid(message, suggestion string) error {
	return &clierrors.CLIError{Code: "INVALID_DATA", Message: message, Suggestion: suggestion, ExitCode: clierrors.ExitUsageError}
}

// bookDataUnsupported reports whether a 404 or 405 is a server without the route itself rather
// than a route that found nothing: FastAPI answers an unknown route with the bare "Not Found".
func bookDataUnsupported(apiErr *api.APIError) bool {
	return apiErr.StatusCode == 405 || (apiErr.StatusCode == 404 && invariantDetail(apiErr) == "Not Found")
}

// bookDataError maps a refused read (writing false) or apply of an object's data.
func bookDataError(err error, object bookDataObject, writing bool) error {
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return clierrors.NewAuthError(clean(authErr.Message))
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return clierrors.NewAPIError(clean(err.Error()))
	}
	detail := invariantDetail(apiErr)
	get := bookDataGetCommand(object)
	switch {
	case bookDataUnsupported(apiErr):
		return &clierrors.CLIError{
			Code:       "DATA_EDITS_UNAVAILABLE",
			Message:    fmt.Sprintf("this Konvu server doesn't support editing your %s yet", object.what),
			Suggestion: "Check KONVU_API_URL, or try again once your Konvu server is up to date.",
			ExitCode:   clierrors.ExitNotFound,
		}
	case apiErr.StatusCode == 404:
		suggestion := "Run 'konvu whoami' to check your company."
		if object.byRepo {
			suggestion = "Konvu keeps a threat profile once it has analyzed the repository: check it with 'konvu inventory show <repo>'."
		}
		return &clierrors.CLIError{Code: "NOT_FOUND", Message: orDefault(detail, "not found"), Suggestion: suggestion, ExitCode: clierrors.ExitNotFound}
	case apiErr.StatusCode == 403 && writing:
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
	case apiErr.StatusCode == 409 && writing:
		return &clierrors.CLIError{
			Code:       "DATA_CHANGED",
			Message:    withDetail(fmt.Sprintf("your %s changed since the file was read", object.what), detail),
			Suggestion: fmt.Sprintf("Nothing was written. Run '%s' again, redo the edit on the new document, then apply it.", get),
			ExitCode:   clierrors.ExitGeneralError,
		}
	case apiErr.StatusCode == 422 && writing:
		return &clierrors.CLIError{
			Code:       "INVALID_DATA",
			Message:    fmt.Sprintf("the file has problems, nothing was written:\n  %s", strings.Join(validationProblems(apiErr), "\n  ")),
			Suggestion: fmt.Sprintf("Fix them in the file and apply again; '%s' shows the current document.", get),
			ExitCode:   clierrors.ExitUsageError,
		}
	case apiErr.StatusCode == 422:
		return bookDataUsage(orDefault(detail, "Konvu refused the arguments"), "Check the arguments against the command's --help.")
	}
	return clierrors.NewAPIError(orDefault(detail, fmt.Sprintf("Konvu answered %d", apiErr.StatusCode)))
}

// writeOutcomeUnknown is the error for a write whose request failed once sent: a server error or a
// lost connection can come after the server committed it. history is the command that shows it.
func writeOutcomeUnknown(err error, write, history string) error {
	reason := clean(err.Error())
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		reason = orDefault(invariantDetail(apiErr), fmt.Sprintf("Konvu answered %d", apiErr.StatusCode))
	}
	return &clierrors.CLIError{
		Code:       "OUTCOME_UNKNOWN",
		Message:    fmt.Sprintf("%s failed once sent (%s), so it may have been applied", write, reason),
		Suggestion: fmt.Sprintf("Run '%s' to see whether it was before trying again: a repeat of an applied change is refused as DATA_CHANGED.", history),
		ExitCode:   clierrors.ExitGeneralError,
	}
}

func withDetail(message, detail string) string {
	if detail == "" {
		return message
	}
	return message + " (" + detail + ")"
}

// validationProblems lists every problem a 422 names, fit for a terminal: FastAPI's
// [{"loc", "msg"}] objects, plain strings, or the bare detail.
func validationProblems(apiErr *api.APIError) []string {
	var payload struct {
		Detail []any `json:"detail"`
	}
	problems := []string{}
	if json.Unmarshal([]byte(strings.TrimPrefix(apiErr.Message, "API error: ")), &payload) == nil {
		for _, item := range payload.Detail {
			switch value := item.(type) {
			case string:
				problems = append(problems, clean(value))
			case map[string]any:
				problems = append(problems, clean(validationLocation(value["loc"])+fmt.Sprint(value["msg"])))
			}
		}
	}
	if len(problems) == 0 {
		problems = append(problems, invariantDetail(apiErr))
	}
	return problems
}

// validationLocation is a 422 item's place in the request, such as "company.employees: ", without
// FastAPI's leading "body".
func validationLocation(loc any) string {
	parts, _ := loc.([]any)
	var names []string
	for i, part := range parts {
		if i == 0 && part == "body" {
			continue
		}
		names = append(names, fmt.Sprint(part))
	}
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, ".") + ": "
}

// revisionText prints a revision the server sent, or "" for none.
func revisionText(value any) string {
	revision, _ := value.(string)
	return clean(revision)
}

// terminalJSON is FormatJSON made safe for a terminal, like terminalSafeJSON.
func terminalJSON(value any) string {
	return terminalSafeJSON(output.FormatJSON(value))
}

// terminalSafeJSON escapes DEL and the C1 control characters in JSON text: encoding/json escapes
// only those under U+0020, and a terminal can act on the others. They can only sit in strings, so
// the JSON reads back the same.
func terminalSafeJSON(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, character := range text {
		if character >= 0x7f && character <= 0x9f {
			fmt.Fprintf(&b, `\u%04x`, character)
			continue
		}
		b.WriteRune(character)
	}
	return b.String()
}

func init() {
	for _, object := range []bookDataObject{companyData, threatModelData, threatProfileData} {
		rootCmd.AddCommand(newBookDataCmd(object))
	}
}
