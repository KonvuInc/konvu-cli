package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuardrailsReadCommandsRegistered(t *testing.T) {
	want := map[string]bool{"list": false, "show": false}
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

func TestGuardrailsListFlags(t *testing.T) {
	for _, flag := range []string{"output", "quiet"} {
		if guardrailsListCmd.Flags().Lookup(flag) == nil {
			t.Errorf("guardrails list missing flag: --%s", flag)
		}
	}
}

func TestGuardrailsShowFlags(t *testing.T) {
	for _, flag := range []string{"branch", "policy-only", "output"} {
		if guardrailsShowCmd.Flags().Lookup(flag) == nil {
			t.Errorf("guardrails show missing flag: --%s", flag)
		}
	}
}

// baseline takes --policy as a file; show must not reuse that name for a boolean, or the
// same flag means two things in sibling commands.
func TestShowDoesNotReuseBaselinesPolicyFlag(t *testing.T) {
	if guardrailsShowCmd.Flags().Lookup("policy") != nil {
		t.Error("show should use --policy-only; --policy is baseline's file flag")
	}
	if f := guardrailsBaselineCmd.Flags().Lookup("policy"); f == nil || f.Value.Type() != "string" {
		t.Error("baseline --policy should still be the policy file")
	}
}

// The route matches on a path, so the repo's slash is part of the URL. Escaping it sends
// the request somewhere that does not exist.
func TestShowSendsTheRepoSlashUnescaped(t *testing.T) {
	var gotPath, gotBranch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBranch = r.URL.Query().Get("branch")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"repo": "AcmeKonvu/litellm", "branch": "demo/x", "ratified": true,
			"fingerprint": "abc123", "n_paths": 213.0, "n_guarded": 189.0, "n_unguarded": 24.0,
			"policy": []any{map[string]any{
				"role": "USER", "action": "read", "resource": "Document",
				"condition": "owns(USER, Document)",
			}},
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	cmd := guardrailsShowCmd
	_ = cmd.Flags().Set("branch", "demo/x")
	defer func() { _ = cmd.Flags().Set("branch", "main") }()

	if err := runGuardrailsShow(cmd, []string{"AcmeKonvu/litellm"}); err != nil {
		t.Fatalf("show: %v", err)
	}
	want := guardrailsAPI + "/dashboard/repos/AcmeKonvu/litellm/baseline"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotBranch != "demo/x" {
		t.Errorf("branch = %q, want demo/x", gotBranch)
	}
}

func TestListReadsBaselinesAndSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != guardrailsAPI+"/dashboard/baselines" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baselines": []any{map[string]any{
				"repo": "AcmeKonvu/litellm", "branch": "main", "fingerprint": "abc",
				"ratified": true, "created_at": "2026-07-14T10:00:00Z", "n_paths": 213.0,
			}},
			"skipped": []any{"AcmeKonvu/docs"},
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	if err := runGuardrailsList(guardrailsListCmd, nil); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestCountDisplayMarksAnAbsentCountUnknown(t *testing.T) {
	// An omitted count must not read as zero routes.
	if got := countDisplay(nil); got != "N/A" {
		t.Errorf("countDisplay(nil) = %q, want N/A", got)
	}
	if got := countDisplay(213.0); got != "213" {
		t.Errorf("countDisplay(213.0) = %q", got)
	}
}

func TestDateDisplayKeepsOnlyTheDate(t *testing.T) {
	for in, want := range map[string]string{
		"2026-07-14T10:00:00Z": "2026-07-14",
		"2026-07-14 10:00:00":  "2026-07-14",
		"2026-07-14":           "2026-07-14",
		"":                     "N/A",
	} {
		if got := dateDisplay(in); got != want {
			t.Errorf("dateDisplay(%q) = %q, want %q", in, got, want)
		}
	}
}

// FormatTable measures byte length, so a multi-byte placeholder pads short and skews the
// column. Keep these ASCII.
func TestPlaceholdersAreSingleByte(t *testing.T) {
	for _, s := range []string{countDisplay(nil), dateDisplay("")} {
		if len(s) != len([]rune(s)) {
			t.Errorf("placeholder %q is multi-byte; it will misalign the table", s)
		}
	}
}
