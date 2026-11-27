package cmd

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

const testEditID = "0193a1b2-c3d4-e5f6-a7b8-c9d0e1f2a3b4"

func TestHistoryListsEdits(t *testing.T) {
	long := strings.Repeat("a", 60)
	_, calls := reviewServer(t, map[string][]invariantReply{
		"GET /data_edits": {{http.StatusOK, map[string]any{"total": 5, "items": []any{
			map[string]any{
				"id": testEditID, "object": "threat-profile", "object_key": testRepoID, "field": "internet_exposed",
				"before": true, "after": false, "actor": "jane@acme.com", "via": "cli", "created_at": "2026-11-24T09:30:12+01:00",
			},
			map[string]any{
				"id": "e2", "object": "company", "object_key": "company", "field": "customer_segments\u001b[2J",
				"before": nil, "after": long, "actor": "Agent\u009b of Jane [API]", "via": "api", "created_at": "2026-11-23T10:00:00Z",
			},
			map[string]any{
				"id": "e3", "object": "company", "object_key": "company", "field": "hosting_footprint",
				"before": []any{"AWS"}, "after": []any{"AWS", "GCP"}, "actor": "jane@acme.com", "via": "dashboard", "created_at": "2026-11-22T10:00:00Z",
			},
		}}}},
	})
	out, err := runInvariants(newHistoryCmd(), "--object", "threat-profile", "--key", " "+testRepoID+" ", "--limit", "2", "--offset", "1", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	query, _ := url.ParseQuery(calls()[0].query)
	if query.Get("object") != "threat-profile" || query.Get("object_key") != testRepoID || query.Get("limit") != "2" || query.Get("offset") != "1" {
		t.Errorf("query = %v", query)
	}
	for _, want := range []string{
		"WHEN", "WHO", "OBJECT", "FIELD", "BEFORE → AFTER", "ID",
		"2026-11-24 08:30", "jane@acme.com (cli)", "threat-profile " + testRepoID, "internet_exposed", "yes → no", testEditID[:8] + "\n",
		"customer_segments [2J", "(none) → " + strings.Repeat("a", historyValueRunes-1) + "…", "Agent of Jane [API] (api)",
		"AWS → AWS, GCP",
		"Showing edits 2 to 4 of 5, newest first. Pass --offset 4 to see the next ones.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	// An object the company has one of is not named twice.
	if strings.Contains(out, testEditID) || strings.Contains(out, "company company") || strings.Contains(out, "\x1b") || strings.ContainsRune(out, '\u009b') || strings.Contains(out, long) {
		t.Errorf("table:\n%q", out)
	}
}

func TestHistoryJSONIsTheServersAnswer(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /data_edits": {{http.StatusOK, map[string]any{"total": 0, "items": []any{}}}},
	})
	out, err := runInvariants(newHistoryCmd(), "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil || result["items"] == nil {
		t.Errorf("out = %s", out)
	}
}

func TestHistoryRefusesBadFlagsBeforeAnyRequest(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{})
	for _, args := range [][]string{
		{"--object", "findings"},
		{"--key", " "},
		{"--limit", "0"},
		{"--limit", "201"},
		{"--offset", "-1"},
		{"-o", "yaml"},
		{"revert", "../admin", "--revision", "r1"},
		{"revert", "a/b", "--revision", "r1"},
		{"revert", testEditID},
		{"revert", testEditID, "--revision", " "},
	} {
		_, err := runInvariants(newHistoryCmd(), args...)
		if cliErr := bookDataCLIError(t, err); cliErr.ExitCode != clierrors.ExitUsageError {
			t.Errorf("%v: error = %#v", args, cliErr)
		}
	}
	if len(calls()) != 0 {
		t.Errorf("calls %v", calls())
	}
}

func TestHistoryEmptyAndOlderServer(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /data_edits": {
			{http.StatusOK, map[string]any{"total": 0, "items": []any{}}},
			{http.StatusNotFound, map[string]any{"detail": "Not Found"}},
		},
	})
	out, err := runInvariants(newHistoryCmd(), "-o", "table")
	if err != nil || out != "No edits yet.\n" {
		t.Fatalf("got %q, %v", out, err)
	}
	_, err = runInvariants(newHistoryCmd(), "-o", "table")
	if cliErr := bookDataCLIError(t, err); cliErr.Code != "DATA_EDITS_UNAVAILABLE" || cliErr.ExitCode != clierrors.ExitNotFound {
		t.Errorf("error = %#v", cliErr)
	}
}

func TestHistoryRevert(t *testing.T) {
	route := "POST /data_edits/" + testEditID + "/revert"
	_, calls := reviewServer(t, map[string][]invariantReply{
		route: {
			{http.StatusOK, map[string]any{"applied": true, "changes": []any{"Set internet_exposed: no → yes"}, "revision": "r9", "edit": map[string]any{"id": "e9"}}},
		},
	})
	out, err := runInvariants(newHistoryCmd(), "revert", testEditID, "--revision", " r8 ", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if body := calls()[0].body; body["revision"] != "r8" || len(body) != 1 {
		t.Errorf("body = %v", body)
	}
	for _, want := range []string{"Reverted edit " + testEditID + ".", "Set internet_exposed: no → yes", "Now at revision r9."} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
}

func TestHistoryRevertRefusals(t *testing.T) {
	for _, test := range []struct {
		status int
		body   any
		code   string
		exit   int
	}{
		{http.StatusConflict, map[string]any{"detail": "The facts changed since revision r8"}, "DATA_CHANGED", clierrors.ExitGeneralError},
		{http.StatusNotFound, map[string]any{"detail": "Edit not found"}, "NOT_FOUND", clierrors.ExitNotFound},
		{http.StatusNotFound, map[string]any{"detail": "Not Found"}, "DATA_EDITS_UNAVAILABLE", clierrors.ExitNotFound},
		{http.StatusForbidden, map[string]any{"detail": "Only a user of the company can revert its edits"}, "FORBIDDEN", clierrors.ExitAuthFailed},
		{http.StatusUnprocessableEntity, map[string]any{"detail": "A catalog change is undone by applying the catalog again"}, "INVALID_ARGUMENTS", clierrors.ExitUsageError},
		{http.StatusOK, map[string]any{"applied": false, "changes": []any{}}, "NOT_APPLIED", clierrors.ExitGeneralError},
		// An answer that does not say it reverted, such as a proxy's, is not a revert.
		{http.StatusOK, map[string]any{}, "NOT_APPLIED", clierrors.ExitGeneralError},
		// A server error can come after the commit: twice, with no revert in the history, the
		// outcome is unknown.
		{http.StatusBadGateway, map[string]any{"detail": "upstream timed out"}, "OUTCOME_UNKNOWN", clierrors.ExitGeneralError},
		{dropConnection, nil, "OUTCOME_UNKNOWN", clierrors.ExitGeneralError},
	} {
		posts := []invariantReply{{test.status, test.body}}
		var reads []invariantReply
		switch {
		case test.code == "OUTCOME_UNKNOWN":
			posts = append(posts, invariantReply{test.status, test.body})
			reads = []invariantReply{historyWithout(), historyWithout()}
		case test.status == http.StatusConflict:
			reads = []invariantReply{historyWithout()}
		}
		reviewServer(t, map[string][]invariantReply{
			"POST /data_edits/" + testEditID + "/revert": posts,
			"GET /data_edits": reads,
		})
		_, err := runInvariants(newHistoryCmd(), "revert", testEditID, "--revision", "r8")
		if cliErr := bookDataCLIError(t, err); cliErr.Code != test.code || cliErr.ExitCode != test.exit {
			t.Errorf("%d: error = %#v", test.status, cliErr)
		}
	}
}

// historyWithout is a history with no revert of testEditID: another edit's revert, and the edit.
func historyWithout() invariantReply {
	return invariantReply{http.StatusOK, map[string]any{"total": 2, "items": []any{
		map[string]any{"id": "0193a1b2-ffff-7000-8000-000000000001", "reverted_edit_id": "0193a1b2-eeee-7000-8000-000000000001"},
		map[string]any{"id": testEditID},
	}}}
}

func historyWithRevert() invariantReply {
	return invariantReply{http.StatusOK, map[string]any{"total": 2, "items": []any{
		map[string]any{
			"id": "0193a1b2-ffff-7000-8000-000000000002", "reverted_edit_id": strings.ToUpper(testEditID),
			"actor": "jane@acme.com", "created_at": "2026-11-27T10:00:00Z",
		},
		map[string]any{"id": testEditID},
	}}}
}

func TestHistoryShortIDs(t *testing.T) {
	ids := []string{
		"0193a1b2-c3d4-7e6f-a7b8-c9d0e1f2a3b4",
		"0193a1b2-c3d4-7e6f-a7b8-000000000000",
		"0193a1b2-c3ff-7e6f-a7b8-c9d0e1f2a3b4",
		"0193a1b3-0000-7000-8000-000000000000",
		"e2",
	}
	want := []string{"0193a1b2-c3d4-7e6f-a7b8-c", "0193a1b2-c3d4-7e6f-a7b8-0", "0193a1b2-c3f", "0193a1b3", "e2"}
	if got := historyShortIDs(ids); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestHistoryRevertTakesAnIDStart(t *testing.T) {
	other := "0193a1b2-c3d4-7000-8000-000000000000"
	_, calls := reviewServer(t, map[string][]invariantReply{
		"GET /data_edits": {
			{http.StatusOK, map[string]any{"total": 2, "items": []any{map[string]any{"id": testEditID}, map[string]any{"id": other}}}},
			{http.StatusOK, map[string]any{"total": 2, "items": []any{map[string]any{"id": testEditID}, map[string]any{"id": other}}}},
			{http.StatusOK, map[string]any{"total": 2, "items": []any{map[string]any{"id": testEditID}, map[string]any{"id": other}}}},
		},
		"POST /data_edits/" + testEditID + "/revert": {{http.StatusOK, map[string]any{"applied": true, "changes": []any{}, "revision": "r9"}}},
	})
	out, err := runInvariants(newHistoryCmd(), "revert", "0193A1B2-C3D4-E5", "--revision", "r8", "-o", "table")
	if err != nil || !strings.Contains(out, "Reverted edit "+testEditID+".") {
		t.Fatalf("got %q, %v", out, err)
	}
	if query, _ := url.ParseQuery(calls()[0].query); query.Get("limit") != "200" {
		t.Errorf("query = %v", query)
	}
	_, err = runInvariants(newHistoryCmd(), "revert", "0193a1b2-c3d4", "--revision", "r8")
	if cliErr := bookDataCLIError(t, err); cliErr.ExitCode != clierrors.ExitUsageError || !strings.Contains(cliErr.Message, other) {
		t.Errorf("shared start: error = %#v", cliErr)
	}
	_, err = runInvariants(newHistoryCmd(), "revert", "ffff", "--revision", "r8")
	if cliErr := bookDataCLIError(t, err); cliErr.Code != "NOT_FOUND" || cliErr.ExitCode != clierrors.ExitNotFound {
		t.Errorf("no match: error = %#v", cliErr)
	}
}

func TestHistoryRevertIsSafeToRetry(t *testing.T) {
	route := "POST /data_edits/" + testEditID + "/revert"
	applied := invariantReply{http.StatusOK, map[string]any{"applied": true, "changes": []any{"Set internet_exposed: no → yes"}, "revision": "r9"}}
	badGateway := invariantReply{http.StatusBadGateway, map[string]any{"detail": "upstream timed out"}}
	for _, test := range []struct {
		name  string
		posts []invariantReply
		reads []invariantReply
		want  string
	}{
		// The 502 came after the commit: the history shows the revert, which is not sent again.
		{"landed", []invariantReply{badGateway}, []invariantReply{historyWithRevert()}, "Edit " + testEditID + " is reverted: Konvu answered 502, but the history shows its revert, edit 0193a1b2-ffff-7000-8000-000000000002 by jane@acme.com at 2026-11-27 10:00."},
		// It did not land: sent once more.
		{"retried", []invariantReply{badGateway, applied}, []invariantReply{historyWithout()}, "Reverted edit " + testEditID + "."},
		{"dropped", []invariantReply{{dropConnection, nil}, applied}, []invariantReply{historyWithout()}, "Reverted edit " + testEditID + "."},
		// A repeat of a revert that landed is refused as stale; the history says it landed.
		{"repeated", []invariantReply{{http.StatusConflict, map[string]any{"detail": "The facts changed since revision r8"}}}, []invariantReply{historyWithRevert()}, "Edit " + testEditID + " is reverted: Konvu answered 409"},
	} {
		_, calls := reviewServer(t, map[string][]invariantReply{route: test.posts, "GET /data_edits": test.reads})
		out, err := runInvariants(newHistoryCmd(), "revert", testEditID, "--revision", "r8", "-o", "table")
		if err != nil || !strings.Contains(out, test.want) {
			t.Errorf("%s: got %q, %v", test.name, out, err)
		}
		if got := len(calls()); got != len(test.posts)+len(test.reads) {
			t.Errorf("%s: calls %v", test.name, calls())
		}
	}
	reviewServer(t, map[string][]invariantReply{route: {badGateway}, "GET /data_edits": {historyWithRevert()}})
	out, err := runInvariants(newHistoryCmd(), "revert", testEditID, "--revision", "r8", "-o", "json")
	var result map[string]any
	if err != nil || json.Unmarshal([]byte(out), &result) != nil || result["applied"] != true || result["confirmed_from_history"] != true {
		t.Errorf("json: got %s, %v", out, err)
	}
}
