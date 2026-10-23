package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func prCheckServer(t *testing.T) *httptest.Server {
	reviewed := map[string]any{
		"repository_id": "repo-1",
		"repository":    "acme/app",
		"number":        42,
		"url":           "https://github.com/acme/app/pull/42",
		"title":         "Add export",
		"state":         "merged",
		"ci_mode":       "monitor",
		"run_id":        "run-1",
		"head_sha":      "abc",
		"outcome":       "violation",
		"finding_count": 1,
		"findings":      []any{"Member reads another workspace"},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case prCheckReviewedPath:
			q := r.URL.Query()
			if q.Get("per_page") == "30" && (strings.Join(q["outcome"], ",") != "pass" || q.Get("state") != "merged") {
				t.Fatalf("list query = %v", q)
			}
			if q.Has("number") && (q.Get("repository") != "acme/app" || q.Get("number") != "42") {
				t.Fatalf("get query = %v", q)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{reviewed}})
		case "/services/guardrails/v1/ci/pull-requests/repo-1/42/runs":
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "run-2", "outcome": "error", "findings": []any{}},
				map[string]any{
					"id": "run-1", "outcome": "violation", "target_base_sha": "base",
					"findings": []any{map[string]any{
						"finding_id": "run-1:1", "title": "Member reads another workspace",
						"attack_path": "member → GET /w/other", "rule_id": "RULE-1",
					}},
				},
			})
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
}

func TestPRCheckListAndGet(t *testing.T) {
	server := prCheckServer(t)
	defer server.Close()
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "test-token")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	run := func(c *cobra.Command, args ...string) string {
		out := &bytes.Buffer{}
		c.SetOut(out)
		c.SetArgs(args)
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	listCmd := &cobra.Command{Use: "list", RunE: runPRCheckList}
	addPRCheckListFlags(listCmd)
	list := run(listCmd, "--outcome", "pass", "--state", "merged", "--limit", "30", "-q")
	if strings.TrimSpace(list) != "https://github.com/acme/app/pull/42" {
		t.Fatalf("list = %q", list)
	}

	getCmd := &cobra.Command{Use: "get", Args: cobra.ExactArgs(1), RunE: runPRCheckGet}
	addPRCheckGetFlags(getCmd)
	var got []map[string]any
	if err := json.Unmarshal([]byte(run(getCmd, "https://github.com/acme/app/pull/42")), &got); err != nil {
		t.Fatal(err)
	}
	review := got[0]
	flaw := review["findings"].([]any)[0].(map[string]any)
	if flaw["attack_path"] != "member → GET /w/other" || flaw["rule_id"] != nil {
		t.Fatalf("flaw = %#v", flaw)
	}
	if runs := review["runs"].([]any); len(runs) != 1 {
		t.Fatalf("failed runs must be skipped: %#v", runs)
	}
	if review["pr"].(map[string]any)["base_sha"] != "base" || review["mode"] != "monitor" {
		t.Fatalf("review = %#v", review)
	}
}
