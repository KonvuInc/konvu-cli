package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

var prCheckRules = []any{map[string]any{
	"id": "inv-1", "revision": 2.0, "statement": "Only owners read their records",
	"why_it_matters": "Records hold customer data",
}}

// prCheckServer fakes the PR check reads; decide, when set, answers verdict POSTs.
func prCheckServer(t *testing.T, decide http.HandlerFunc) *httptest.Server {
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
		if r.Method == http.MethodPost && decide != nil {
			decide(w, r)
			return
		}

		// Every PR check read asks for security findings only.
		if r.URL.Query().Get("findings") != prCheckFindingScope {
			t.Fatalf("findings scope = %q on %s", r.URL.Query().Get("findings"), r.URL.Path)
		}
		switch r.URL.Path {
		case prCheckReviewedPath:
			q := r.URL.Query()
			if q.Get("limit") == "30" && (strings.Join(q["outcome"], ",") != "pass" || q.Get("state") != "merged" || q.Get("offset") != "60" || q.Get("sort") != "score" || q.Get("rules") != "broken") {
				t.Fatalf("list query = %v", q)
			}
			if q.Has("number") && (q.Get("repository") != "acme/app" || q.Get("number") != "42") {
				t.Fatalf("get query = %v", q)
			}
			if q.Get("limit") == "1" && !q.Has("number") && (q.Get("repository") != "acme/app" || !strings.HasPrefix(q.Get("since"), "20")) {
				t.Fatalf("counts query = %v", q)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []any{reviewed},
				"counts": map[string]any{
					"pull_requests": 7, "findings": 3,
					"outcomes": map[string]any{"pass": 5, "violation": 2},
					"modes":    map[string]any{"monitor": 6, "warn": 1, "unknown": 0},
				},
			})
		case "/services/guardrails/v1/ci/pull-requests/repo-1/42/runs":
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "run-2", "outcome": "error", "findings": []any{}},
				map[string]any{
					"id": "run-1", "outcome": "violation", "target_base_sha": "base",
					// The first finding is unscored, as from a server without scores.
					"findings": []any{
						map[string]any{
							"finding_id": "run-1:1", "title": "Member reads another workspace",
							"attack_path": "member → GET /w/other", "rule_id": "RULE-1",
							"security_rules": prCheckRules,
						},
						map[string]any{"finding_id": "run-1:2", "severity": "high", "score": 0.72},
					},
					"decisions": []any{map[string]any{
						"finding_id": "run-1:1", "action": "dismissed", "reason": "Admin only",
						"actor": "Ada", "created_at": "2026-11-16T10:00:00Z",
					}},
				},
			})
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
}

func TestPRCheckListAndGet(t *testing.T) {
	server := prCheckServer(t, nil)
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
	list := run(listCmd, "--outcome", "pass", "--state", "merged", "--limit", "30", "--offset", "60", "--sort", "score", "--broke-principle", "-q")
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
	// Scored first; the unscored finding still comes through, last.
	flaws := review["findings"].([]any)
	top, flaw := flaws[0].(map[string]any), flaws[1].(map[string]any)
	if top["score"] != 0.72 || top["severity"] != "high" || flaw["score"] != nil {
		t.Fatalf("findings = %#v", flaws)
	}
	if flaw["attack_path"] != "member → GET /w/other" || flaw["rule_id"] != nil ||
		!reflect.DeepEqual(flaw["security_rules"], prCheckRules) {
		t.Fatalf("flaw = %#v", flaw)
	}
	// Each flaw carries its verdict, if any.
	if verdict, _ := flaw["decision"].(map[string]any); verdict["action"] != "dismissed" || verdict["actor"] != "Ada" || top["decision"] != nil {
		t.Fatalf("decisions = %#v / %#v", flaw["decision"], top["decision"])
	}
	if runs := review["runs"].([]any); len(runs) != 1 {
		t.Fatalf("failed runs must be skipped: %#v", runs)
	}
	if review["pr"].(map[string]any)["base_sha"] != "base" || review["mode"] != "monitor" {
		t.Fatalf("review = %#v", review)
	}

	countsCmd := &cobra.Command{Use: "counts", RunE: runPRCheckCounts}
	addPRCheckFilterFlags(countsCmd)
	var counts []map[string]any
	if err := json.Unmarshal([]byte(run(countsCmd, "--repo", "acme/app", "--since", "30d")), &counts); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"pull_requests": 7, "pass": 5, "violation": 2, "findings": 3, "monitor": 6, "warn": 1}
	for key, value := range want {
		if counts[0][key] != value {
			t.Fatalf("counts[%s] = %v, want %v", key, counts[0][key], value)
		}
	}
}

func TestPRCheckDecision(t *testing.T) {
	// Decisions are recorded here; reads come from the shared fake.
	var posted []map[string]any
	status := http.StatusCreated
	server := prCheckServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/services/guardrails/v1/ci/pull-requests/repo-1/42/findings/run-1:2/decisions" {
			t.Fatalf("decision path = %q", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		posted = append(posted, body)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"actor": "Ada", "created_at": "2026-11-16T11:00:00Z"})
	})
	defer server.Close()
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "test-token")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	decide := func(verb prCheckVerb, args ...string) (map[string]any, error) {
		c := newPRCheckDecisionCommand(verb)
		out := &bytes.Buffer{}
		c.SetOut(out)
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(args)
		if err := c.Execute(); err != nil {
			return nil, err
		}
		var rows []map[string]any
		if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
			t.Fatalf("output %q: %v", out.String(), err)
		}
		return rows[0], nil
	}
	url := "https://github.com/acme/app/pull/42"

	// Dry run checks the finding but sends nothing.
	row, err := decide(prCheckVerbDismiss, url, "run-1:2", "--reason", " Admin only ", "--dry-run")
	if err != nil || row["status"] != "preview" || row["reason"] != "Admin only" || len(posted) != 0 {
		t.Fatalf("dry run = %#v, %v, posted %d", row, err, len(posted))
	}

	// A real verdict is pinned to the judged run.
	row, err = decide(prCheckVerbAcceptRisk, url, "run-1:2", "--reason", "Tracked in SEC-1")
	want := map[string]any{"action": "risk_accepted", "reason": "Tracked in SEC-1", "run_id": "run-1"}
	if err != nil || !reflect.DeepEqual(posted[0], want) || row["status"] != "recorded" || row["actor"] != "Ada" {
		t.Fatalf("decide = %#v, %v, posted %#v", row, err, posted)
	}

	// Unknown finding and blank reason fail before any request.
	if _, err := decide(prCheckVerbDismiss, url, "run-9:1", "--reason", "x"); err == nil || !strings.Contains(err.Error(), "not in the latest review") {
		t.Fatalf("unknown finding err = %v", err)
	}
	if _, err := decide(prCheckVerbDismiss, url, "run-1:2", "--reason", "  "); err == nil {
		t.Fatal("blank reason must fail")
	}
	if len(posted) != 1 {
		t.Fatalf("posted %d decisions, want 1", len(posted))
	}

	// A new push since the read: the server refuses, the CLI says to re-read.
	status = http.StatusConflict
	if _, err := decide(prCheckVerbDismiss, url, "run-1:2", "--reason", "x"); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("conflict err = %v", err)
	}
}
