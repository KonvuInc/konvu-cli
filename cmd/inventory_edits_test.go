package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

const inventoryDocument = `{"revision": "sha256:r1",
	"endpoints": [{"name": "admin routes", "description": "", "location": "app.py#admin", "routes": [{"method": "GET", "path": "/admin"}]}],
	"data_objects": [{"name": "User", "description": "", "location": "models.py#User", "fields": ["email"]}],
	"provenance": {"endpoints": {"admin routes": {"asserted_by": "a@acme.com", "asserted_at": "2026-11-25T10:00:00Z"}}, "data_objects": {}},
	"hidden": {"endpoints": [], "data_objects": [{"name": "Scratch\u009b2J", "asserted_by": "a@acme.com", "asserted_at": "2026-11-25T10:00:00Z"}]}}`

var inventoryRoute = graphInventoryPath + testRepoID + "/inventory"

func TestInventoryGetPrintsOnlyItsList(t *testing.T) {
	for _, test := range []struct {
		object  bookDataObject
		list    string
		other   string
		hidden  int
		authors int
	}{
		{endpointsData, "endpoints", "data_objects", 0, 1},
		{dataObjectsData, "data_objects", "endpoints", 1, 0},
	} {
		_, calls := reviewServer(t, map[string][]invariantReply{
			"GET /repositories/assessment_config": {threatProfileCoverage()},
			"GET " + inventoryRoute:               {{http.StatusOK, json.RawMessage(inventoryDocument)}},
		})
		out, err := runInvariants(newBookDataCmd(test.object), "get", "--repo", "acme/api")
		if err != nil {
			t.Fatalf("%s: %v", test.list, err)
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("%s: not JSON: %v\n%s", test.list, err, out)
		}
		provenance, _ := doc["provenance"].(map[string]any)
		hidden, _ := doc["hidden"].([]any)
		if doc["revision"] != "sha256:r1" || doc[test.list] == nil || doc[test.other] != nil || len(provenance) != test.authors || len(hidden) != test.hidden {
			t.Errorf("%s: printed %v", test.list, doc)
		}
		if strings.ContainsRune(out, '\u009b') {
			t.Errorf("a C1 control character reached the terminal: %q", out)
		}
		if got := calls()[1].path; got != inventoryRoute {
			t.Errorf("route = %s", got)
		}
	}
}

func TestInventoryApplySendsOnlyItsList(t *testing.T) {
	for _, test := range []struct {
		object bookDataObject
		list   string
		file   string
	}{
		{endpointsData, "endpoints", `{"revision": "sha256:r1", "endpoints": [{"name": "Billing webhook", "routes": [{"method": "POST", "path": "/hook"}]}], "provenance": {}, "hidden": []}`},
		{dataObjectsData, "data_objects", `{"revision": "sha256:r1", "data_objects": [], "provenance": {}, "hidden": []}`},
	} {
		_, calls := reviewServer(t, map[string][]invariantReply{
			"PUT " + inventoryRoute: {{http.StatusOK, map[string]any{"applied": true, "changes": []any{"Added endpoint Billing webhook\u001b[2J"}, "revision": "sha256:r2"}}},
		})
		out, err := runInvariants(newBookDataCmd(test.object), "apply", "--repo", testRepoID, "--file", writeInvariants(t, test.file), "-o", "table")
		if err != nil {
			t.Fatalf("%s: %v", test.list, err)
		}
		call := calls()[0]
		if call.query != "dry_run=false" || call.body["revision"] != "sha256:r1" || call.body[test.list] == nil || len(call.body) != 2 {
			t.Errorf("%s: sent %s %v", test.list, call.query, call.body)
		}
		for _, want := range []string{"Applied 1 change:\n", "Added endpoint Billing webhook [2J", "Now at revision sha256:r2.", "on their next read"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: out lacks %q:\n%s", test.list, want, out)
			}
		}
		if strings.Contains(out, "\x1b") {
			t.Errorf("a control character reached the terminal: %q", out)
		}
	}
}

func TestInventoryApplyDryRun(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		"PUT " + inventoryRoute: {{http.StatusOK, map[string]any{"applied": false, "changes": []any{"Hid data object User"}, "revision": "sha256:r1"}}},
	})
	file := writeInvariants(t, `{"revision": "sha256:r1", "data_objects": []}`)
	out, err := runInvariants(newBookDataCmd(dataObjectsData), "apply", "--repo", testRepoID, "--file", file, "--dry-run", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if calls()[0].query != "dry_run=true" || !strings.Contains(out, "Would apply 1 change (dry run, nothing written):\n  Hid data object User\n") {
		t.Errorf("query %q, out %q", calls()[0].query, out)
	}
}

func TestInventoryApplyRefusesAWrongFileBeforeAnyRequest(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{})
	for name, body := range map[string]string{
		"no list":          `{"revision": "sha256:r1"}`,
		"an object":        `{"revision": "sha256:r1", "endpoints": {}}`,
		"null list":        `{"revision": "sha256:r1", "endpoints": null}`,
		"the other list":   `{"revision": "sha256:r1", "endpoints": [], "data_objects": []}`,
		"no revision":      `{"endpoints": []}`,
		"number revision":  `{"revision": 1, "endpoints": []}`,
		"the whole answer": inventoryDocument,
	} {
		_, err := runInvariants(newBookDataCmd(endpointsData), "apply", "--repo", testRepoID, "--file", writeInvariants(t, body))
		if cliErr := bookDataCLIError(t, err); cliErr.Code != "INVALID_DATA" || cliErr.ExitCode != clierrors.ExitUsageError {
			t.Errorf("%s: error = %#v", name, cliErr)
		}
	}
	if len(calls()) != 0 {
		t.Errorf("calls %v", calls())
	}
}

func TestInventoryRefusals(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   any
		code   string
		exit   int
		want   string
	}{
		{"stale revision", http.StatusConflict, map[string]any{"detail": "the endpoints and data objects changed since this document was read"}, "DATA_CHANGED", clierrors.ExitGeneralError, "your repository's endpoints changed since the file was read"},
		{"validation", http.StatusUnprocessableEntity, map[string]any{"detail": []any{"endpoints[0]: the name has no letters or digits", "endpoints[1].routes[0]: needs both a method and a path"}}, "INVALID_DATA", clierrors.ExitUsageError, "endpoints[0]: the name has no letters or digits\n  endpoints[1].routes[0]: needs both a method and a path"},
		{"demo view", http.StatusForbidden, map[string]any{"detail": "konvu principal type not permitted"}, "FORBIDDEN", clierrors.ExitAuthFailed, "read-only"},
		{"older server", http.StatusNotFound, map[string]any{"detail": "Not Found"}, "DATA_EDITS_UNAVAILABLE", clierrors.ExitNotFound, "doesn't support editing your repository's endpoints yet"},
		{"no mapping", http.StatusNotFound, map[string]any{"detail": "no mapping for this repository"}, "NOT_FOUND", clierrors.ExitNotFound, "no mapping for this repository"},
		{"server error", http.StatusBadGateway, map[string]any{"detail": "upstream"}, "OUTCOME_UNKNOWN", clierrors.ExitGeneralError, "may have been applied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewServer(t, map[string][]invariantReply{
				"PUT " + inventoryRoute: {{test.status, test.body}},
			})
			file := writeInvariants(t, `{"revision": "sha256:r1", "endpoints": []}`)
			_, err := runInvariants(newBookDataCmd(endpointsData), "apply", "--repo", testRepoID, "--file", file, "-o", "table")
			cliErr := bookDataCLIError(t, err)
			if cliErr.Code != test.code || cliErr.ExitCode != test.exit || !strings.Contains(cliErr.Message, test.want) {
				t.Errorf("error = %#v", cliErr)
			}
			// There is no 'konvu history' for these lists: get shows what landed.
			if test.code == "OUTCOME_UNKNOWN" && !strings.Contains(cliErr.Suggestion, "konvu inventory endpoints get --repo <repo>") {
				t.Errorf("suggestion = %q", cliErr.Suggestion)
			}
			if test.code == "NOT_FOUND" && !strings.Contains(cliErr.Suggestion, "once it has mapped it") {
				t.Errorf("suggestion = %q", cliErr.Suggestion)
			}
		})
	}
}

func TestInventoryOnAnOlderServer(t *testing.T) {
	for _, object := range []bookDataObject{endpointsData, dataObjectsData} {
		for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
			reviewServer(t, map[string][]invariantReply{
				"GET " + inventoryRoute: {{status, map[string]any{"detail": http.StatusText(status)}}},
			})
			_, err := runInvariants(newBookDataCmd(object), "get", "--repo", testRepoID)
			cliErr := bookDataCLIError(t, err)
			if cliErr.Code != "DATA_EDITS_UNAVAILABLE" || cliErr.ExitCode != clierrors.ExitNotFound || !strings.Contains(cliErr.Message, "doesn't support editing your "+object.what+" yet") {
				t.Errorf("%s %d: error = %#v", object.name, status, cliErr)
			}
		}
	}
}

// An answer without the list a command edits is not printed as if it were the document.
func TestInventoryGetRefusesAnAnswerWithoutItsList(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET " + inventoryRoute: {{http.StatusOK, json.RawMessage(`{"revision": "sha256:r1", "endpoints": []}`)}},
	})
	out, err := runInvariants(newBookDataCmd(dataObjectsData), "get", "--repo", testRepoID)
	if cliErr := bookDataCLIError(t, err); cliErr.ExitCode != clierrors.ExitGeneralError || strings.Contains(out, "revision") {
		t.Errorf("error = %#v, out %q", cliErr, out)
	}
}

func TestInventoryCommandsAreUnderInventory(t *testing.T) {
	for _, name := range []string{"endpoints", "data-objects"} {
		found, _, err := inventoryCmd.Find([]string{name, "apply"})
		if err != nil || found.Name() != "apply" || found.Flags().Lookup("repo") == nil || found.Flags().Lookup("dry-run") == nil {
			t.Errorf("konvu inventory %s apply: %v", name, err)
		}
	}
}
