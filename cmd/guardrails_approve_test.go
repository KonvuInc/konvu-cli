package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApproveExplainReviewRegistered(t *testing.T) {
	want := map[string]bool{"approve": false, "review": false, "explain": false}
	for _, c := range guardrailsCmd.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("guardrails missing subcommand: %s", name)
		}
	}
}

func TestApproveSendsTheBranchAndKeepsTheSlash(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"repo": "acme/web", "branch": "release-2.3", "ratified": true,
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	_ = guardrailsApproveCmd.Flags().Set("branch", "release-2.3")
	defer func() { _ = guardrailsApproveCmd.Flags().Set("branch", "main") }()

	if err := approveFlow(guardrailsApproveCmd, []string{"acme/web"}); err != nil {
		t.Fatalf("approveFlow: %v", err)
	}
	want := guardrailsAPI + "/dashboard/repos/acme/web/ratify"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if !strings.Contains(gotBody, `"branch":"release-2.3"`) {
		t.Errorf("body = %q, want the branch", gotBody)
	}
}

func TestExplainSendsTheTokenAndOmitsAnEmptyIntent(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != guardrailsAPI+"/ratify" {
			t.Errorf("path = %q", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"repo": "a/b", "branch": "main", "pr_number": 412.0, "pr_title": "t",
			"head_sha": "abcdef1234", "flagged": []any{}, "instruction": "push a guard",
			"intent_status": "none",
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	if err := explainFlow(guardrailsExplainCmd, []string{"kb-36-9f3a1c"}); err != nil {
		t.Fatalf("explainFlow: %v", err)
	}
	if !strings.Contains(body, `"ratify_token":"kb-36-9f3a1c"`) {
		t.Errorf("token missing: %q", body)
	}
	// An empty --intent must not be sent: the server treats a present intent as one to record.
	if strings.Contains(body, "intent") && !strings.Contains(body, "ratify_token") {
		t.Errorf("unexpected body: %q", body)
	}
	if strings.Contains(body, `"intent"`) {
		t.Errorf("empty intent was sent: %q", body)
	}
}

func TestExplainSendsAnIntentWhenGiven(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"repo": "a/b", "flagged": []any{}, "intent_status": "recorded",
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	_ = guardrailsExplainCmd.Flags().Set("intent", "only the owner may read it")
	defer func() { _ = guardrailsExplainCmd.Flags().Set("intent", "") }()

	if err := explainFlow(guardrailsExplainCmd, []string{"tok-1"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "only the owner may read it") {
		t.Errorf("intent missing: %q", body)
	}
}

func TestReviewReadsWithoutDecisions(t *testing.T) {
	var method, gotPR string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		gotPR = r.URL.Query().Get("pr")
		_ = json.NewEncoder(w).Encode(_reviewPayload())
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	_ = guardrailsReviewCmd.Flags().Set("pr", "412")
	defer func() { _ = guardrailsReviewCmd.Flags().Set("pr", "0") }()

	if err := reviewFlow(guardrailsReviewCmd, []string{"acme/web"}); err != nil {
		t.Fatalf("reviewFlow: %v", err)
	}
	if method != http.MethodGet {
		t.Errorf("method = %q, want GET when no decision is passed", method)
	}
	if gotPR != "412" {
		t.Errorf("pr = %q", gotPR)
	}
}

func TestReviewPostsDecisionsWithPrInTheQuery(t *testing.T) {
	var method, gotPR, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		gotPR = r.URL.Query().Get("pr")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_ = json.NewEncoder(w).Encode(_reviewPayload())
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	_ = guardrailsReviewCmd.Flags().Set("pr", "412")
	_ = guardrailsReviewCmd.Flags().Set("allow", "USER|read|Document|GET /d/{id}")
	_ = guardrailsReviewCmd.Flags().Set("deny", "ANON|read|Document|GET /d/{id}")
	defer func() {
		_ = guardrailsReviewCmd.Flags().Set("pr", "0")
		guardrailsReviewCmd.Flags().Lookup("allow").Value.Set("")
		guardrailsReviewCmd.Flags().Lookup("deny").Value.Set("")
	}()

	if err := reviewFlow(guardrailsReviewCmd, []string{"acme/web"}); err != nil {
		t.Fatalf("reviewFlow: %v", err)
	}
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST when a decision is passed", method)
	}
	// pr is a query parameter on the write too, not a body field.
	if gotPR != "412" {
		t.Errorf("pr = %q, want it in the query string", gotPR)
	}
	if !strings.Contains(body, `"decision":"allow"`) || !strings.Contains(body, `"decision":"deny"`) {
		t.Errorf("both decisions should be sent: %q", body)
	}
}

func TestBuildDecisionsCoversAllThreeVerbs(t *testing.T) {
	got, err := buildDecisions([]string{"a"}, []string{"b"}, []string{"c", "  "})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (blank entries dropped): %v", len(got), got)
	}
	seen := map[string]string{}
	for _, d := range got {
		seen[d["capability_key"].(string)] = d["decision"].(string)
	}
	if seen["a"] != "allow" || seen["b"] != "deny" || seen["c"] != "clear" {
		t.Errorf("wrong mapping: %v", seen)
	}
}

func _reviewPayload() map[string]any {
	return map[string]any{
		"repo": "acme/web", "pr": 412.0, "pr_title": "add export",
		"pr_author": "someone", "branch": "main", "gh_url": "https://github.com/x/y/pull/412",
		"pending": true, "n_total": 1, "can_decide": true,
		"open_rows": []any{map[string]any{
			"key": "USER|read|Document|GET /d/{id}", "method": "GET", "path": "/d/{id}",
			"role": "USER", "action": "read", "resource": "Document", "guard": "NONE",
		}},
		"scoped_rows": []any{},
	}
}

// FlaggedCapability.route already carries the method, unlike the review rows which split
// method and path, so printing both doubled it.
func TestExplainDoesNotDoubleTheMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"repo": "a/b", "branch": "main", "pr_number": 1.0, "flagged": []any{
				map[string]any{
					"route": "DELETE /v1/models/{id}/purge", "method": "DELETE",
					"role": "USER", "action": "delete", "resource": "Model",
					"current_guard": "NONE", "reason": "coverage gap",
				},
			},
			"intent_status": "none",
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	out := captureStdout(t, func() {
		if err := explainFlow(guardrailsExplainCmd, []string{"tok-1"}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "DELETE DELETE") {
		t.Errorf("method doubled:\n%s", out)
	}
	if !strings.Contains(out, "DELETE /v1/models/{id}/purge") {
		t.Errorf("route missing:\n%s", out)
	}
}

// The server applies decisions in order, so allowing and denying the same capability in one
// command would let argument order settle an authorization question silently.
func TestBuildDecisionsRefusesAConflict(t *testing.T) {
	_, err := buildDecisions([]string{"k"}, []string{"k"}, nil)
	if err == nil {
		t.Fatal("want an error when one capability is both allowed and denied")
	}
	if !strings.Contains(err.Error(), "--allow") || !strings.Contains(err.Error(), "--deny") {
		t.Errorf("the error should name both flags: %v", err)
	}
}

func TestBuildDecisionsToleratesTheSameVerbTwice(t *testing.T) {
	got, err := buildDecisions([]string{"k", "k"}, nil, nil)
	if err != nil {
		t.Fatalf("repeating one verb is harmless: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want the duplicate collapsed", len(got))
	}
}
