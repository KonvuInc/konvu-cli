package cmd

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

const editedStatement = "Session tokens are redacted before any log line."

func activeInvariant(revision int) map[string]any {
	inv := oneInvariant(invariantValidated)
	inv["revision"] = revision
	inv["protected_assets"] = []any{map[string]any{"kind": "data", "name": "session token", "graph_asset_id": "a1", "route": nil}}
	return inv
}

// editFile is the principle as 'get -o json' prints it, with edits applied.
func editFile(t *testing.T, revision int, edits map[string]any) string {
	t.Helper()
	doc := activeInvariant(revision)
	for k, v := range edits {
		if v == nil {
			delete(doc, k)
			continue
		}
		doc[k] = v
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return writeInvariants(t, string(data))
}

func edited(revision int) map[string]any {
	inv := activeInvariant(revision)
	inv["statement"] = editedStatement
	return inv
}

func setDeletePrompt(t *testing.T, canPrompt, answer bool) *[]string {
	t.Helper()
	asked := &[]string{}
	oldCan, oldConfirm := deleteCanPrompt, deleteConfirm
	deleteCanPrompt = func() bool { return canPrompt }
	deleteConfirm = func(prompt string, _ bool) bool {
		*asked = append(*asked, prompt)
		return answer
	}
	t.Cleanup(func() { deleteCanPrompt, deleteConfirm = oldCan, oldConfirm })
	return asked
}

// --- edit ---

func TestInvariantsEditSendsWhatTheFileChangesWithItsRevision(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):   {{200, activeInvariant(3)}},
		invariantRoute("PATCH"): {{200, edited(4)}},
	})
	file := editFile(t, 3, map[string]any{"statement": "  " + editedStatement + " "})

	out, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", file, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["statement"] != editedStatement || got["revision"] != float64(4) {
		t.Fatalf("out = %q", out)
	}
	sent := calls()[1]
	want := map[string]any{
		"revision": float64(3), "dry_run": false,
		"statement": "  " + editedStatement + " ", "why_it_matters": "A leaked token is a stolen session.",
		"property": "confidentiality", "rank": float64(4),
		"protected_assets": []any{map[string]any{"kind": "data", "name": "session token"}},
	}
	if sent.method != "PATCH" || !reflect.DeepEqual(sent.body, want) {
		t.Fatalf("sent %s %#v, want %#v", sent.method, sent.body, want)
	}
}

func TestInvariantsEditTakesOnlyTheFieldsToChangeFromStdin(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):   {{200, activeInvariant(3)}},
		invariantRoute("PATCH"): {{200, activeInvariant(4)}},
	})
	stdin := writeInvariants(t, `{"revision": 3, "rank": 2}`)
	f, err := os.Open(stdin)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old }()

	if _, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", "-", "-o", "table"); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"revision": float64(3), "rank": float64(2), "dry_run": false}
	if body := calls()[1].body; !reflect.DeepEqual(body, want) {
		t.Fatalf("sent %#v, want %#v", body, want)
	}
}

func TestInvariantsEditDryRunWritesNothing(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):   {{200, activeInvariant(3)}},
		invariantRoute("PATCH"): {{200, edited(4)}},
	})
	file := editFile(t, 3, map[string]any{"statement": editedStatement})

	if _, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", file, "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if body := calls()[1].body; body["dry_run"] != true {
		t.Fatalf("sent %#v", body)
	}
}

func TestInvariantsEditRefusesAFileReadBeforeAnotherEdit(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, activeInvariant(4)}}})
	file := editFile(t, 3, map[string]any{"statement": editedStatement})

	_, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", file)

	e := cliErrorOf(t, err)
	if e.Code != "STALE_REVISION" || e.ExitCode != clierrors.ExitGeneralError ||
		!strings.Contains(e.Message, "revision 3") || !strings.Contains(e.Message, "revision 4") ||
		!strings.Contains(e.Suggestion, "konvu principles get "+testInvariantID+" -o json") {
		t.Fatalf("err = %#v", e)
	}
	if len(calls()) != 1 {
		t.Fatalf("calls = %#v, want only the read", calls())
	}
}

func TestInvariantsEditOfAnOlderFileThatChangesNothingIsSent(t *testing.T) {
	// A repeat of an edit that landed: the server answers it unchanged and queues its mapping.
	reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):   {{200, edited(4)}},
		invariantRoute("PATCH"): {{200, edited(4)}},
	})
	file := editFile(t, 3, map[string]any{"statement": editedStatement})

	if _, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", file); err != nil {
		t.Fatal(err)
	}
}

func TestInvariantsEditTellsAStaleRevisionFromAConflict(t *testing.T) {
	file := editFile(t, 3, map[string]any{"rank": 1})
	cases := []struct {
		name     string
		readBack map[string]any
		code     string
	}{
		{"edited meanwhile", activeInvariant(4), "STALE_REVISION"},
		{"rank held", activeInvariant(3), "INVARIANT_CONFLICT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reviewServer(t, map[string][]invariantReply{
				invariantRoute("GET"):   {{200, activeInvariant(3)}, {200, tc.readBack}},
				invariantRoute("PATCH"): {{409, detail("rank 1 is held by another live principle")}},
			})
			_, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", file)
			if e := cliErrorOf(t, err); e.Code != tc.code || e.ExitCode != clierrors.ExitGeneralError {
				t.Fatalf("err = %#v", e)
			}
		})
	}
}

func TestInvariantsEditRefusesABadFileBeforeWriting(t *testing.T) {
	cases := map[string]string{
		"not an object":    `[1]`,
		"no revision":      `{"statement": "x"}`,
		"zero revision":    `{"revision": 0, "statement": "x"}`,
		"string revision":  `{"revision": "3", "statement": "x"}`,
		"nothing to edit":  `{"revision": 3}`,
		"unknown key":      `{"revision": 3, "statment": "x"}`,
		"other id":         `{"revision": 3, "id": "` + otherInvariantID + `", "statement": "x"}`,
		"unknown property": `{"revision": 3, "property": "secrecy"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, calls := reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, activeInvariant(3)}}})
			_, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", writeInvariants(t, body))
			if e := cliErrorOf(t, err); e.Code != "INVALID_EDIT" || e.ExitCode != clierrors.ExitUsageError {
				t.Fatalf("err = %#v", e)
			}
			if len(calls()) != 1 {
				t.Fatalf("calls = %#v, want only the read", calls())
			}
		})
	}
	for _, field := range []string{"status", "repository_id"} {
		t.Run("changes "+field, func(t *testing.T) {
			_, calls := reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, activeInvariant(3)}}})
			file := editFile(t, 3, map[string]any{field: "retired"})
			_, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", file)
			if e := cliErrorOf(t, err); e.Code != "INVALID_EDIT" || !strings.Contains(e.Message, field) {
				t.Fatalf("err = %#v", e)
			}
			if len(calls()) != 1 {
				t.Fatalf("calls = %#v", calls())
			}
		})
	}
	t.Run("no file", func(t *testing.T) {
		reviewServer(t, nil)
		_, err := runInvariants(newInvariantsEditCmd(), testInvariantID)
		if e := cliErrorOf(t, err); e.ExitCode != clierrors.ExitUsageError {
			t.Fatalf("err = %#v", e)
		}
	})
}

func TestInvariantsEditRefusesWhatIsNotActive(t *testing.T) {
	for _, status := range []string{invariantProposed, "declined", "retired"} {
		t.Run(status, func(t *testing.T) {
			inv := activeInvariant(1)
			inv["status"] = status
			reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, inv}}})
			_, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", editFile(t, 1, map[string]any{"rank": 2}))
			if e := cliErrorOf(t, err); e.Code != "INVARIANT_NOT_ACTIVE" || !strings.Contains(e.Message, status) {
				t.Fatalf("err = %#v", e)
			}
		})
	}
}

func TestInvariantsEditOnAnOlderServerSaysSo(t *testing.T) {
	// The second file is what an older server's 'get -o json' prints: no revision.
	for _, file := range []string{editFile(t, 1, map[string]any{"rank": 2}), editFile(t, 1, map[string]any{"rank": 2, "revision": nil})} {
		reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, oneInvariant(invariantValidated)}}})
		_, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", file)
		if e := cliErrorOf(t, err); e.Code != "NOT_SUPPORTED" || e.ExitCode != clierrors.ExitNotFound {
			t.Fatalf("err = %#v", e)
		}
	}
}

func TestInvariantsEditMapsRefusals(t *testing.T) {
	cases := []struct {
		status int
		code   string
		exit   int
	}{
		{403, "FORBIDDEN", clierrors.ExitAuthFailed},
		{404, "INVARIANT_NOT_FOUND", clierrors.ExitNotFound},
		{405, "NOT_SUPPORTED", clierrors.ExitNotFound},
		{422, "INVALID_EDIT", clierrors.ExitUsageError},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			reviewServer(t, map[string][]invariantReply{
				invariantRoute("GET"):   {{200, activeInvariant(1)}},
				invariantRoute("PATCH"): {{tc.status, map[string]any{"detail": []any{map[string]any{"loc": []any{"body", "statement"}, "msg": "too long"}}}}},
			})
			_, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", editFile(t, 1, map[string]any{"rank": 2}))
			e := cliErrorOf(t, err)
			if e.Code != tc.code || e.ExitCode != tc.exit {
				t.Fatalf("err = %#v", e)
			}
			if tc.status == 422 && !strings.Contains(e.Message, "statement") {
				t.Fatalf("422 should list the field: %q", e.Message)
			}
		})
	}
}

func TestInvariantsEditAfterAServerErrorReadsTheOutcomeBack(t *testing.T) {
	file := editFile(t, 3, map[string]any{"statement": editedStatement})
	t.Run("landed", func(t *testing.T) {
		reviewServer(t, map[string][]invariantReply{
			invariantRoute("GET"):   {{200, activeInvariant(3)}, {200, edited(4)}},
			invariantRoute("PATCH"): {{502, detail("principle edited but not yet mapped to controls; retry the same request")}},
		})
		out, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", file, "-o", "json")
		if err != nil || !strings.Contains(out, editedStatement) {
			t.Fatalf("out = %q, err = %v", out, err)
		}
	})
	t.Run("not yet", func(t *testing.T) {
		reviewServer(t, map[string][]invariantReply{
			invariantRoute("GET"):   {{200, activeInvariant(3)}, {200, activeInvariant(3)}},
			invariantRoute("PATCH"): {{dropConnection, nil}},
		})
		_, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", file)
		if e := cliErrorOf(t, err); e.Code != "EDIT_UNCONFIRMED" || !e.Retryable {
			t.Fatalf("err = %#v", e)
		}
	})
	t.Run("changed by someone else", func(t *testing.T) {
		other := activeInvariant(5)
		other["statement"] = "Someone else's wording."
		reviewServer(t, map[string][]invariantReply{
			invariantRoute("GET"):   {{200, activeInvariant(3)}, {200, other}},
			invariantRoute("PATCH"): {{500, detail("boom")}},
		})
		_, err := runInvariants(newInvariantsEditCmd(), testInvariantID, "--file", file)
		if e := cliErrorOf(t, err); e.Code != "INVARIANT_CHANGED" {
			t.Fatalf("err = %#v", e)
		}
	})
}

// --- delete ---

func retiredInvariant() map[string]any {
	inv := activeInvariant(1)
	inv["status"] = "retired"
	return inv
}

func TestInvariantsDeleteWithYesDeletesIt(t *testing.T) {
	setDeletePrompt(t, false, false)
	_, calls := reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):    {{200, activeInvariant(1)}},
		invariantRoute("DELETE"): {{200, retiredInvariant()}},
	})

	out, err := runInvariants(newInvariantsDeleteCmd(), testInvariantID, "--yes", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"retired"`) || calls()[1].method != "DELETE" {
		t.Fatalf("out = %q, calls = %#v", out, calls())
	}
}

func TestInvariantsDeleteRefusesWithoutYesOffATerminal(t *testing.T) {
	asked := setDeletePrompt(t, false, true)
	_, calls := reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, activeInvariant(1)}}})

	_, err := runInvariants(newInvariantsDeleteCmd(), testInvariantID)

	e := cliErrorOf(t, err)
	if e.Code != "CONFIRMATION_REQUIRED" || e.ExitCode != clierrors.ExitUsageError ||
		!strings.Contains(e.Message, testInvariantID) || !strings.Contains(e.Message, "Session tokens never reach logs.") ||
		!strings.Contains(e.Suggestion, "--yes") {
		t.Fatalf("err = %#v", e)
	}
	if len(calls()) != 1 || len(*asked) != 0 {
		t.Fatalf("calls = %#v, asked = %v", calls(), *asked)
	}
}

func TestInvariantsDeleteAsksOnATerminal(t *testing.T) {
	t.Run("no", func(t *testing.T) {
		asked := setDeletePrompt(t, true, false)
		_, calls := reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, activeInvariant(1)}}})
		_, err := runInvariants(newInvariantsDeleteCmd(), testInvariantID)
		if e := cliErrorOf(t, err); e.Code != "NOT_CONFIRMED" || len(calls()) != 1 {
			t.Fatalf("err = %#v, calls = %#v", e, calls())
		}
		if len(*asked) != 1 || !strings.Contains((*asked)[0], "Session tokens never reach logs.") {
			t.Fatalf("asked = %v", *asked)
		}
	})
	t.Run("yes", func(t *testing.T) {
		setDeletePrompt(t, true, true)
		reviewServer(t, map[string][]invariantReply{
			invariantRoute("GET"):    {{200, activeInvariant(1)}},
			invariantRoute("DELETE"): {{200, retiredInvariant()}},
		})
		if _, err := runInvariants(newInvariantsDeleteCmd(), testInvariantID); err != nil {
			t.Fatal(err)
		}
	})
}

func TestInvariantsDeleteOfADeletedPrincipleNeedsNoConfirmation(t *testing.T) {
	asked := setDeletePrompt(t, false, false)
	reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):    {{200, retiredInvariant()}},
		invariantRoute("DELETE"): {{200, retiredInvariant()}},
	})
	if _, err := runInvariants(newInvariantsDeleteCmd(), testInvariantID); err != nil || len(*asked) != 0 {
		t.Fatalf("err = %v, asked = %v", err, *asked)
	}
}

func TestInvariantsDeleteRefusesASuggestion(t *testing.T) {
	setDeletePrompt(t, false, false)
	for _, status := range []string{invariantProposed, "declined"} {
		t.Run(status, func(t *testing.T) {
			inv := activeInvariant(1)
			inv["status"] = status
			_, calls := reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, inv}}})
			_, err := runInvariants(newInvariantsDeleteCmd(), testInvariantID, "--yes")
			if e := cliErrorOf(t, err); e.Code != "INVARIANT_NOT_ACTIVE" || len(calls()) != 1 {
				t.Fatalf("err = %#v, calls = %#v", e, calls())
			}
		})
	}
}

func TestInvariantsDeleteOnAnOlderServerSaysSoBeforeAsking(t *testing.T) {
	for _, canPrompt := range []bool{false, true} {
		asked := setDeletePrompt(t, canPrompt, true)
		_, calls := reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, oneInvariant(invariantValidated)}}})
		_, err := runInvariants(newInvariantsDeleteCmd(), testInvariantID)
		if e := cliErrorOf(t, err); e.Code != "NOT_SUPPORTED" || e.ExitCode != clierrors.ExitNotFound {
			t.Fatalf("terminal=%v: err = %#v", canPrompt, e)
		}
		if len(*asked) != 0 || len(calls()) != 1 {
			t.Fatalf("terminal=%v: asked = %v, calls = %#v", canPrompt, *asked, calls())
		}
	}
}

func TestInvariantsDeleteMapsRefusals(t *testing.T) {
	setDeletePrompt(t, false, false)
	cases := []struct {
		status int
		code   string
		exit   int
	}{
		{403, "FORBIDDEN", clierrors.ExitAuthFailed},
		{404, "INVARIANT_NOT_FOUND", clierrors.ExitNotFound},
		{405, "NOT_SUPPORTED", clierrors.ExitNotFound},
		{409, "INVARIANT_NOT_ACTIVE", clierrors.ExitGeneralError},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			reviewServer(t, map[string][]invariantReply{
				invariantRoute("GET"):    {{200, activeInvariant(1)}},
				invariantRoute("DELETE"): {{tc.status, detail("refused")}},
			})
			_, err := runInvariants(newInvariantsDeleteCmd(), testInvariantID, "--yes")
			if e := cliErrorOf(t, err); e.Code != tc.code || e.ExitCode != tc.exit {
				t.Fatalf("err = %#v", e)
			}
		})
	}
	t.Run("not found on read", func(t *testing.T) {
		reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{404, detail("no such principle for this company")}}})
		_, err := runInvariants(newInvariantsDeleteCmd(), testInvariantID, "--yes")
		if e := cliErrorOf(t, err); e.ExitCode != clierrors.ExitNotFound {
			t.Fatalf("err = %#v", e)
		}
	})
}

func TestInvariantsDeleteAfterAServerErrorReadsTheOutcomeBack(t *testing.T) {
	setDeletePrompt(t, false, false)
	t.Run("landed", func(t *testing.T) {
		reviewServer(t, map[string][]invariantReply{
			invariantRoute("GET"):    {{200, activeInvariant(1)}, {200, retiredInvariant()}},
			invariantRoute("DELETE"): {{dropConnection, nil}},
		})
		if _, err := runInvariants(newInvariantsDeleteCmd(), testInvariantID, "--yes"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("not yet", func(t *testing.T) {
		reviewServer(t, map[string][]invariantReply{
			invariantRoute("GET"):    {{200, activeInvariant(1)}, {200, activeInvariant(1)}},
			invariantRoute("DELETE"): {{503, detail("unavailable")}},
		})
		_, err := runInvariants(newInvariantsDeleteCmd(), testInvariantID, "--yes")
		if e := cliErrorOf(t, err); e.Code != "DELETE_UNCONFIRMED" || !e.Retryable {
			t.Fatalf("err = %#v", e)
		}
	})
}
