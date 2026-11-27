package cmd

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

const (
	testNoteID   = "0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b4"
	testNoteHex  = "0193a1b2c3d4e5f6a7b8c9d0e1f2a3b4"
	notesRoute   = "/knowledge/notes"
	noteDelete   = "DELETE " + notesRoute + "/" + testNoteID
	notePost     = "POST " + notesRoute
	notesGet     = "GET " + notesRoute
	coverageRead = "GET /repositories/assessment_config"
)

func savedNote() map[string]any {
	return map[string]any{
		"id": testNoteHex, "label": "deployment", "revision": 3, "text": "Deployed by Argo CD\u001b[2J\nfrom the deploy repo",
		"repositories": []any{
			map[string]any{"id": testRepoID, "name": "acme/api"},
			map[string]any{"id": otherTestRepoID, "name": "acme/web\u009b2J"},
		},
		"author": "alice@acme.com", "created_at": "2026-11-25T10:00:00Z",
	}
}

func noteFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNoteAddSendsTheNoteAndLooksRepositoriesUpOnce(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		coverageRead: {threatProfileCoverage()},
		notePost:     {{http.StatusCreated, savedNote()}},
	})
	out, err := runInvariants(newKnowledgeNoteCmd(), "add", "--label", " Deployment ",
		"--repo", "acme/api", "--repo", "https://gitlab.com/acme/web", "--repo", strings.ToUpper(testRepoID),
		"--text", "Deployed by Argo CD\r\nfrom the deploy repo", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(calls()); got != 2 {
		t.Fatalf("%d calls, want one lookup and one POST: %v", got, calls())
	}
	body := calls()[1].body
	ids, _ := body["repository_ids"].([]any)
	// The repository named twice, once by name and once by id, is sent once.
	if body["label"] != "deployment" || body["text"] != "Deployed by Argo CD\nfrom the deploy repo" || len(ids) != 2 || ids[0] != testRepoID || ids[1] != otherTestRepoID {
		t.Errorf("body = %v", body)
	}
	for _, want := range []string{"Saved note " + testNoteHex, "acme/api, acme/web 2J", "alice@acme.com", "2026-11-25 10:00", "Konvu places it in the Security Book"} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	if strings.ContainsAny(out, "\x1b\u009b") {
		t.Errorf("a control character reached the terminal: %q", out)
	}
}

func TestNoteAddReadsAFileOrStdin(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{notePost: {{http.StatusCreated, savedNote()}, {http.StatusCreated, savedNote()}}})
	// Exactly the server's limit in characters, at twice as many bytes, is sent whole.
	longest := strings.Repeat("é", noteMaxChars)
	if _, err := runInvariants(newKnowledgeNoteCmd(), "add", "--label", "other", "--file", noteFile(t, longest), "-o", "table"); err != nil {
		t.Fatal(err)
	}
	note := newKnowledgeNoteCmd()
	note.SetIn(strings.NewReader("Owned by Payments\r\n"))
	out, err := runInvariants(note, "add", "--label", "ownership", "--file", "-", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var printed map[string]any
	if err := json.Unmarshal([]byte(out), &printed); err != nil || printed["id"] != testNoteHex {
		t.Errorf("out is not the saved note: %v\n%s", err, out)
	}
	first, second := calls()[0].body, calls()[1].body
	// No --repo is an empty list, never null.
	if ids, ok := first["repository_ids"].([]any); !ok || len(ids) != 0 || first["text"] != longest {
		t.Errorf("first body: repository_ids = %#v, %d characters", first["repository_ids"], len([]rune(first["text"].(string))))
	}
	if second["label"] != "ownership" || second["text"] != "Owned by Payments\n" {
		t.Errorf("second body = %v", second)
	}
}

func TestNoteAddRefusesBeforeAnyRequest(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{"no label", []string{"--text", "x"}},
		{"invalid label", []string{"--label", "on call", "--text", "x"}},
		{"text and file", []string{"--label", "other", "--text", "x", "--file", noteFile(t, "y")}},
		{"no text", []string{"--label", "other"}},
		{"blank text", []string{"--label", "other", "--text", " \n"}},
		{"too long", []string{"--label", "other", "--text", strings.Repeat("é", noteMaxChars+1)}},
		{"NUL", []string{"--label", "other", "--text", "a\x00b"}},
		{"not UTF-8", []string{"--label", "other", "--file", noteFile(t, "a\xffb")}},
		{"unreadable file", []string{"--label", "other", "--file", filepath.Join(t.TempDir(), "missing.md")}},
		{"blank repo", []string{"--label", "other", "--text", "x", "--repo", " "}},
		{"not a repo", []string{"--label", "other", "--text", "x", "--repo", "../../admin"}},
	} {
		_, calls := reviewServer(t, map[string][]invariantReply{})
		_, err := runInvariants(newKnowledgeNoteCmd(), append([]string{"add", "-o", "table"}, test.args...)...)
		if cliErr := bookDataCLIError(t, err); cliErr.ExitCode != clierrors.ExitUsageError {
			t.Errorf("%s: error = %#v", test.name, cliErr)
		}
		if len(calls()) != 0 {
			t.Errorf("%s: calls %v", test.name, calls())
		}
	}
}

func TestNoteAddServerAnswers(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply invariantReply
		code  string
		exit  int
		want  string
	}{
		{"every problem", invariantReply{http.StatusUnprocessableEntity, map[string]any{"detail": []any{
			map[string]any{"loc": []any{"body", "text"}, "msg": "too long"},
			map[string]any{"loc": []any{"body", "repository_ids", 0}, "msg": "unknown repository"},
		}}}, "INVALID_DATA", clierrors.ExitUsageError, "text: too long\n  repository_ids.0: unknown repository"},
		{"older server", invariantReply{http.StatusNotFound, map[string]any{"detail": "Not Found"}}, "NOTES_UNAVAILABLE", clierrors.ExitNotFound, "doesn't support knowledge notes"},
		{"no POST", invariantReply{http.StatusMethodNotAllowed, map[string]any{"detail": "Method Not Allowed"}}, "NOTES_UNAVAILABLE", clierrors.ExitNotFound, "doesn't support knowledge notes"},
		{"read-only key", invariantReply{http.StatusForbidden, map[string]any{"detail": "Read-only key"}}, "FORBIDDEN", clierrors.ExitAuthFailed, "Read-only key"},
		{"server error", invariantReply{http.StatusBadGateway, map[string]any{"detail": "upstream"}}, "OUTCOME_UNKNOWN", clierrors.ExitGeneralError, "may have been applied"},
		{"lost connection", invariantReply{dropConnection, nil}, "OUTCOME_UNKNOWN", clierrors.ExitGeneralError, "may have been applied"},
		{"no id", invariantReply{http.StatusOK, map[string]any{}}, "UNEXPECTED_ANSWER", clierrors.ExitGeneralError, "does not show the note saved"},
	} {
		reviewServer(t, map[string][]invariantReply{notePost: {test.reply}})
		_, err := runInvariants(newKnowledgeNoteCmd(), "add", "--label", "other", "--text", "x", "-o", "table")
		cliErr := bookDataCLIError(t, err)
		if cliErr.Code != test.code || cliErr.ExitCode != test.exit || !strings.Contains(cliErr.Message, test.want) {
			t.Errorf("%s: error = %#v", test.name, cliErr)
		}
		if test.code == "OUTCOME_UNKNOWN" && !strings.Contains(cliErr.Suggestion, "konvu knowledge note list --label other") {
			t.Errorf("%s: suggestion = %q", test.name, cliErr.Suggestion)
		}
	}
}

func TestNoteListPrintsOneLinePerNote(t *testing.T) {
	long := savedNote()
	long["id"], long["label"], long["repositories"] = "0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b5", "ownership", []any{}
	long["text"] = "Payments owns it.\n" + strings.Repeat("a", 100)
	answer := map[string]any{"items": []any{savedNote(), long}, "total": 5}
	_, calls := reviewServer(t, map[string][]invariantReply{
		coverageRead: {threatProfileCoverage()},
		notesGet:     {{http.StatusOK, answer}, {http.StatusOK, answer}},
	})
	out, err := runInvariants(newKnowledgeNoteCmd(), "list", "--label", "Ownership", "--repo", "acme/api", "--limit", "2", "--offset", "1", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	query, _ := url.ParseQuery(calls()[1].query)
	if query.Get("label") != "ownership" || query.Get("repository_id") != testRepoID || query.Get("limit") != "2" || query.Get("offset") != "1" {
		t.Errorf("query = %q", calls()[1].query)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "ID") || !strings.HasSuffix(strings.TrimSpace(lines[0]), "TEXT") {
		t.Fatalf("want a header, two rows and the next page's hint:\n%s", out)
	}
	for _, want := range []string{"Deployed by Argo CD [2J from the deploy repo", "acme/api, acme/web 2J", "Payments owns it. aaaa", "…", " - ", "Showing notes 2 to 3 of 5, newest first. Pass --offset 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	if strings.ContainsAny(out, "\x1b\u009b") || strings.Contains(out, strings.Repeat("a", 60)) {
		t.Errorf("a control character or the whole text reached the table:\n%q", out)
	}
	out, err = runInvariants(newKnowledgeNoteCmd(), "list", "-o", "json")
	if err != nil || !strings.Contains(out, `"total": 5`) || !strings.Contains(out, strings.Repeat("a", 100)) || strings.ContainsRune(out, '\u009b') {
		t.Errorf("json: %v\n%s", err, out)
	}
}

func TestNoteListRefusalsAndAnswers(t *testing.T) {
	for _, args := range [][]string{{"--limit", "0"}, {"--offset", "-1"}, {"--label", "-secrets"}, {"--repo", ""}} {
		_, calls := reviewServer(t, map[string][]invariantReply{})
		_, err := runInvariants(newKnowledgeNoteCmd(), append([]string{"list", "-o", "table"}, args...)...)
		if cliErr := bookDataCLIError(t, err); cliErr.ExitCode != clierrors.ExitUsageError || len(calls()) != 0 {
			t.Errorf("%v: error = %#v, calls %v", args, cliErr, calls())
		}
	}
	reviewServer(t, map[string][]invariantReply{notesGet: {{http.StatusOK, map[string]any{"items": []any{}, "total": 0}}}})
	if out, err := runInvariants(newKnowledgeNoteCmd(), "list", "-o", "table"); err != nil || out != "No notes yet. Add one with 'konvu knowledge note add'.\n" {
		t.Errorf("empty: %v, %q", err, out)
	}
	for _, test := range []struct {
		reply invariantReply
		code  string
		exit  int
	}{
		{invariantReply{http.StatusNotFound, map[string]any{"detail": "Not Found"}}, "NOTES_UNAVAILABLE", clierrors.ExitNotFound},
		{invariantReply{http.StatusMethodNotAllowed, map[string]any{"detail": "Method Not Allowed"}}, "NOTES_UNAVAILABLE", clierrors.ExitNotFound},
		// An answer that is not a list of notes is never printed as no notes.
		{invariantReply{http.StatusOK, map[string]any{"detail": "maintenance"}}, "API_ERROR", clierrors.ExitGeneralError},
	} {
		for _, format := range []string{"table", "json"} {
			reviewServer(t, map[string][]invariantReply{notesGet: {test.reply}})
			_, err := runInvariants(newKnowledgeNoteCmd(), "list", "-o", format)
			if cliErr := bookDataCLIError(t, err); cliErr.Code != test.code || cliErr.ExitCode != test.exit {
				t.Errorf("%d %s: error = %#v", test.reply.status, format, cliErr)
			}
		}
	}
}

func TestNoteRmAsksOrNeedsYes(t *testing.T) {
	// No terminal and no --yes: refused before any request.
	setDeletePrompt(t, false, false)
	_, calls := reviewServer(t, map[string][]invariantReply{})
	_, err := runInvariants(newKnowledgeNoteCmd(), "rm", testNoteID, "-o", "table")
	if cliErr := bookDataCLIError(t, err); cliErr.Code != "CONFIRMATION_REQUIRED" || cliErr.ExitCode != clierrors.ExitUsageError || len(calls()) != 0 {
		t.Errorf("no terminal: error = %#v, calls %v", cliErr, calls())
	}
	// A terminal and a no: nothing sent.
	asked := setDeletePrompt(t, true, false)
	_, err = runInvariants(newKnowledgeNoteCmd(), "rm", testNoteID, "-o", "table")
	if cliErr := bookDataCLIError(t, err); cliErr.Code != "NOT_CONFIRMED" || len(*asked) != 1 || len(calls()) != 0 {
		t.Errorf("declined: error = %#v, asked %v, calls %v", cliErr, *asked, calls())
	}
	// A terminal and a yes, then --yes without asking.
	asked = setDeletePrompt(t, true, true)
	_, calls = reviewServer(t, map[string][]invariantReply{noteDelete: {{http.StatusNoContent, nil}, {http.StatusNoContent, nil}}})
	out, err := runInvariants(newKnowledgeNoteCmd(), "rm", testNoteID, "-o", "table")
	if err != nil || !strings.HasPrefix(out, "Removed note "+testNoteID+". Konvu rewrites the page it was placed on without it.\n") || !strings.Contains(out, "konvu history --object note --key "+testNoteID) || len(*asked) != 1 {
		t.Errorf("confirmed: %v, %q, asked %v", err, out, *asked)
	}
	out, err = runInvariants(newKnowledgeNoteCmd(), "rm", testNoteID, "--yes", "-o", "json")
	var printed map[string]any
	if err != nil || json.Unmarshal([]byte(out), &printed) != nil || printed["deleted"] != true || len(*asked) != 1 || len(calls()) != 2 {
		t.Errorf("--yes: %v, %q, asked %v, calls %v", err, out, *asked, calls())
	}
}

func TestNoteRmRefusals(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{})
	if _, err := runInvariants(newKnowledgeNoteCmd(), "rm", "../admin", "--yes"); bookDataCLIError(t, err).ExitCode != clierrors.ExitUsageError || len(calls()) != 0 {
		t.Errorf("invalid id: %v, calls %v", err, calls())
	}
	for _, test := range []struct {
		reply invariantReply
		code  string
		exit  int
	}{
		{invariantReply{http.StatusNotFound, map[string]any{"detail": "Context note not found"}}, "NOT_FOUND", clierrors.ExitNotFound},
		{invariantReply{http.StatusNotFound, map[string]any{"detail": "Not Found"}}, "NOTES_UNAVAILABLE", clierrors.ExitNotFound},
		{invariantReply{http.StatusMethodNotAllowed, map[string]any{"detail": "Method Not Allowed"}}, "NOTES_UNAVAILABLE", clierrors.ExitNotFound},
		{invariantReply{http.StatusForbidden, map[string]any{"detail": "Demo view"}}, "FORBIDDEN", clierrors.ExitAuthFailed},
		{invariantReply{http.StatusInternalServerError, map[string]any{"detail": "boom"}}, "OUTCOME_UNKNOWN", clierrors.ExitGeneralError},
	} {
		reviewServer(t, map[string][]invariantReply{noteDelete: {test.reply}})
		_, err := runInvariants(newKnowledgeNoteCmd(), "rm", testNoteID, "--yes", "-o", "table")
		if cliErr := bookDataCLIError(t, err); cliErr.Code != test.code || cliErr.ExitCode != test.exit {
			t.Errorf("%d %v: error = %#v", test.reply.status, test.reply.body, cliErr)
		}
	}
}

const notePut = "PUT " + notesRoute + "/" + testNoteHex

func TestNoteLabelIsAnySlug(t *testing.T) {
	for value, want := range map[string]string{
		"on-call": "on-call", " Runbooks ": "runbooks", "a": "a", "9-to-5": "9-to-5", strings.Repeat("a", 40): strings.Repeat("a", 40),
	} {
		if got, err := noteLabel(value); err != nil || got != want {
			t.Errorf("noteLabel(%q) = %q, %v; want %q", value, got, err, want)
		}
	}
	for _, value := range []string{"", " ", "-deploy", "on call", "on_call", "ops/deploy", "café", strings.Repeat("a", 41)} {
		if _, err := noteLabel(value); bookDataCLIError(t, err).ExitCode != clierrors.ExitUsageError {
			t.Errorf("noteLabel(%q) accepted", value)
		}
	}
	_, calls := reviewServer(t, map[string][]invariantReply{notePost: {{http.StatusCreated, savedNote()}}})
	if _, err := runInvariants(newKnowledgeNoteCmd(), "add", "--label", "On-Call", "--text", "x", "-o", "table"); err != nil {
		t.Fatal(err)
	}
	if label := calls()[0].body["label"]; label != "on-call" {
		t.Errorf("label sent = %v", label)
	}
}

func TestNoteListShowsWhereEachNoteWasPlaced(t *testing.T) {
	placed, unplaced, older := savedNote(), savedNote(), savedNote()
	placed["placement"], placed["updated_at"] = "infrastructure.md", "2026-11-26T08:30:00Z"
	unplaced["placement"] = nil
	answer := map[string]any{"items": []any{placed, unplaced, older}, "total": 3}
	reviewServer(t, map[string][]invariantReply{notesGet: {{http.StatusOK, answer}}})
	out, err := runInvariants(newKnowledgeNoteCmd(), "list", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[0], "REVISION") || !strings.Contains(lines[1], " 3 ") ||
		!strings.Contains(lines[0], "PLACEMENT") || !strings.Contains(lines[1], "infrastructure.md") || !strings.Contains(lines[1], "2026-11-26 08:30") ||
		!strings.Contains(lines[2], "not placed yet") || strings.Contains(lines[3], "not placed yet") {
		t.Errorf("table:\n%s", out)
	}
}

func TestNoteEditKeepsWhatItDoesNotChange(t *testing.T) {
	other := savedNote()
	other["id"] = "0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b5"
	edited := savedNote()
	edited["text"], edited["updated_at"] = "Deployed by Flux", "2026-11-26T08:30:00Z"
	_, calls := reviewServer(t, map[string][]invariantReply{
		// The note is on the list's second page.
		notesGet: {
			{http.StatusOK, map[string]any{"items": []any{other}, "total": 2}},
			{http.StatusOK, map[string]any{"items": []any{savedNote()}, "total": 2}},
		},
		notePut: {{http.StatusOK, edited}},
	})
	out, err := runInvariants(newKnowledgeNoteCmd(), "edit", strings.ToUpper(testNoteID), "--text", "Deployed by Flux\r\n", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(calls()); got != 3 {
		t.Fatalf("calls = %v", calls())
	}
	second, _ := url.ParseQuery(calls()[1].query)
	if second.Get("offset") != "1" || second.Get("limit") != "200" {
		t.Errorf("second page query = %q", calls()[1].query)
	}
	put := calls()[2]
	ids, _ := put.body["repository_ids"].([]any)
	if put.path != notesRoute+"/"+testNoteHex || put.body["label"] != "deployment" || put.body["text"] != "Deployed by Flux\n" ||
		len(ids) != 2 || ids[0] != testRepoID || ids[1] != otherTestRepoID {
		t.Errorf("PUT %s %v", put.path, put.body)
	}
	for _, want := range []string{"Saved note " + testNoteHex, "changed  2026-11-26 08:30", "Deployed by Flux", "konvu knowledge note list"} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}

	// A new label and repositories, the text kept; --repo replaces the whole list.
	_, calls = reviewServer(t, map[string][]invariantReply{
		coverageRead: {threatProfileCoverage()},
		notesGet:     {{http.StatusOK, map[string]any{"items": []any{savedNote()}, "total": 1}}},
		notePut:      {{http.StatusOK, savedNote()}},
	})
	out, err = runInvariants(newKnowledgeNoteCmd(), "edit", testNoteID, "--label", "On-Call", "--repo", "acme/api", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var printed map[string]any
	if json.Unmarshal([]byte(out), &printed) != nil || printed["id"] != testNoteHex {
		t.Errorf("out is not the saved note:\n%s", out)
	}
	body := calls()[2].body
	ids, _ = body["repository_ids"].([]any)
	if body["label"] != "on-call" || body["text"] != savedNote()["text"] || len(ids) != 1 || ids[0] != testRepoID {
		t.Errorf("body = %v", body)
	}
}

func TestNoteEditRefusesBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{
		{testNoteID},
		{"../admin", "--text", "x"},
		{testNoteID, "--label", "on call"},
		{testNoteID, "--text", "x", "--file", noteFile(t, "y")},
		{testNoteID, "--text", " "},
		{testNoteID, "--repo", " "},
	} {
		_, calls := reviewServer(t, map[string][]invariantReply{})
		_, err := runInvariants(newKnowledgeNoteCmd(), append([]string{"edit", "-o", "table"}, args...)...)
		if cliErr := bookDataCLIError(t, err); cliErr.ExitCode != clierrors.ExitUsageError || len(calls()) != 0 {
			t.Errorf("%v: error = %#v, calls %v", args, cliErr, calls())
		}
	}
}

func TestNoteEditServerAnswers(t *testing.T) {
	found := invariantReply{http.StatusOK, map[string]any{"items": []any{savedNote()}, "total": 1}}
	// No such note on the list: nothing is sent.
	_, calls := reviewServer(t, map[string][]invariantReply{notesGet: {{http.StatusOK, map[string]any{"items": []any{}, "total": 0}}}})
	_, err := runInvariants(newKnowledgeNoteCmd(), "edit", testNoteID, "--text", "x", "-o", "table")
	if cliErr := bookDataCLIError(t, err); cliErr.Code != "NOT_FOUND" || cliErr.ExitCode != clierrors.ExitNotFound || len(calls()) != 1 {
		t.Errorf("not listed: error = %#v, calls %v", cliErr, calls())
	}
	for _, test := range []struct {
		name  string
		reply invariantReply
		code  string
		exit  int
		want  string
	}{
		{"removed meanwhile", invariantReply{http.StatusNotFound, map[string]any{"detail": "Context note not found"}}, "NOT_FOUND", clierrors.ExitNotFound, "no note " + testNoteHex},
		{"older server", invariantReply{http.StatusNotFound, map[string]any{"detail": "Not Found"}}, "NOTES_UNAVAILABLE", clierrors.ExitNotFound, "editing notes"},
		{"no PUT", invariantReply{http.StatusMethodNotAllowed, map[string]any{"detail": "Method Not Allowed"}}, "NOTES_UNAVAILABLE", clierrors.ExitNotFound, "editing notes"},
		{"refused", invariantReply{http.StatusUnprocessableEntity, map[string]any{"detail": []any{
			map[string]any{"loc": []any{"body", "label"}, "msg": "bad label"},
		}}}, "INVALID_DATA", clierrors.ExitUsageError, "label: bad label"},
		{"read-only key", invariantReply{http.StatusForbidden, map[string]any{"detail": "Read-only key"}}, "FORBIDDEN", clierrors.ExitAuthFailed, "Read-only key"},
		{"server error", invariantReply{http.StatusBadGateway, map[string]any{"detail": "upstream"}}, "OUTCOME_UNKNOWN", clierrors.ExitGeneralError, "may have been applied"},
		{"no id", invariantReply{http.StatusOK, map[string]any{}}, "UNEXPECTED_ANSWER", clierrors.ExitGeneralError, "does not show the note saved"},
	} {
		reviewServer(t, map[string][]invariantReply{notesGet: {found}, notePut: {test.reply}})
		_, err := runInvariants(newKnowledgeNoteCmd(), "edit", testNoteID, "--text", "x", "-o", "table")
		cliErr := bookDataCLIError(t, err)
		if cliErr.Code != test.code || cliErr.ExitCode != test.exit || !strings.Contains(cliErr.Message, test.want) {
			t.Errorf("%s: error = %#v", test.name, cliErr)
		}
	}
}
