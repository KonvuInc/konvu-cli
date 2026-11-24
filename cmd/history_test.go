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
		"2026-11-24 08:30", "jane@acme.com (cli)", "threat-profile " + testRepoID, "internet_exposed", "yes → no", testEditID,
		"customer_segments [2J", "(none) → " + strings.Repeat("a", historyValueRunes-1) + "…", "Agent of Jane [API] (api)",
		"AWS → AWS, GCP",
		"Showing edits 2 to 4 of 5, newest first. Pass --offset 4 to see the next ones.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	// An object the company has one of is not named twice.
	if strings.Contains(out, "company company") || strings.Contains(out, "\x1b") || strings.ContainsRune(out, '\u009b') || strings.Contains(out, long) {
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
		// A server error can come after the commit: the revert may have landed.
		{http.StatusBadGateway, map[string]any{"detail": "upstream timed out"}, "OUTCOME_UNKNOWN", clierrors.ExitGeneralError},
		{dropConnection, nil, "OUTCOME_UNKNOWN", clierrors.ExitGeneralError},
	} {
		reviewServer(t, map[string][]invariantReply{
			"POST /data_edits/" + testEditID + "/revert": {{test.status, test.body}},
		})
		_, err := runInvariants(newHistoryCmd(), "revert", testEditID, "--revision", "r8")
		if cliErr := bookDataCLIError(t, err); cliErr.Code != test.code || cliErr.ExitCode != test.exit {
			t.Errorf("%d: error = %#v", test.status, cliErr)
		}
	}
}
