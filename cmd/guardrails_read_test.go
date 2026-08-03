package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
	for _, flag := range []string{"branch", "rules-only", "output"} {
		if guardrailsShowCmd.Flags().Lookup(flag) == nil {
			t.Errorf("guardrails show missing flag: --%s", flag)
		}
	}
}

// baseline takes --policy as a file; show must not reuse that name for a boolean, or the
// same flag means two things in sibling commands.
func TestShowDoesNotReuseBaselinesPolicyFlag(t *testing.T) {
	if guardrailsShowCmd.Flags().Lookup("policy") != nil {
		t.Error("show should use --rules-only; --policy is scan's file flag")
	}
	if f := guardrailsScanCmd.Flags().Lookup("policy"); f == nil || f.Value.Type() != "string" {
		t.Error("scan --policy should still be the policy file")
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
			"repo": "acme/web", "branch": "release-2.3", "ratified": true,
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
	_ = cmd.Flags().Set("branch", "release-2.3")
	defer func() { _ = cmd.Flags().Set("branch", "main") }()

	if err := runGuardrailsShow(cmd, []string{"acme/web"}); err != nil {
		t.Fatalf("show: %v", err)
	}
	want := guardrailsAPI + "/dashboard/repos/acme/web/baseline"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotBranch != "release-2.3" {
		t.Errorf("branch = %q, want release-2.3", gotBranch)
	}
}

func TestListReadsBaselinesAndSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != guardrailsAPI+"/dashboard/baselines" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baselines": []any{map[string]any{
				"repo": "acme/web", "branch": "main", "fingerprint": "abc",
				"ratified": true, "created_at": "2026-07-14T10:00:00Z", "n_paths": 213.0,
			}},
			"skipped": []any{"acme/docs"},
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

// A repository whose baseline was attempted and not recorded holds no row in the table, so
// dropping these left it mentioned nowhere: indistinguishable from a repository nobody asked
// about, on the one command whose job is saying what is covered.
func TestListNamesRepositoriesWithNoBaseline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baselines": []any{map[string]any{"repo": "acme/web", "branch": "main"}},
			"skipped":   []any{},
			"onboarding": []any{
				// Already in the table above, so it must not be listed again.
				map[string]any{"repo": "acme/web", "status": "done", "outcome": "ok"},
				map[string]any{
					"repo": "acme/api", "status": "error", "outcome": "action_required",
					"action_required": true, "error": "could not read the routes",
				},
				map[string]any{"repo": "acme/jobs", "status": "running", "outcome": "pending"},
			},
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	// Asked for explicitly: capturing stdout hands the command a pipe, and the format auto-detects
	// to JSON off a TTY, so the table is not what an uninstructed run would print here.
	_ = guardrailsListCmd.Flags().Set("output", "table")
	defer func() { _ = guardrailsListCmd.Flags().Set("output", "") }()

	out := captureStdout(t, func() {
		if err := runGuardrailsList(guardrailsListCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"acme/api", "needs attention", "could not read the routes", "acme/jobs",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q\n%s", want, out)
		}
	}
	if got := strings.Count(out, "acme/web"); got != 1 {
		t.Errorf("acme/web named %d times, want 1 — it has a baseline\n%s", got, out)
	}
}

// The JSON output is what a program reads, so it carries the rows unfiltered. It used to be
// rebuilt from two of the server's three lists, which dropped these there as well.
func TestListJSONKeepsTheOnboardingRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baselines":  []any{},
			"skipped":    []any{},
			"onboarding": []any{map[string]any{"repo": "acme/api", "status": "running"}},
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	_ = guardrailsListCmd.Flags().Set("output", "json")
	defer func() { _ = guardrailsListCmd.Flags().Set("output", "") }()

	out := captureStdout(t, func() {
		if err := runGuardrailsList(guardrailsListCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	rows, _ := got["onboarding"].([]any)
	if len(rows) != 1 {
		t.Errorf("onboarding = %v, want the server's row\n%s", got["onboarding"], out)
	}
}

func TestOnboardingStateUsesTheServersWords(t *testing.T) {
	// Falls back to `status` when there is no outcome yet, and never renders an empty state.
	for _, c := range []struct {
		row  map[string]any
		want string
	}{
		{map[string]any{"outcome": "pending"}, "pending"},
		{map[string]any{"status": "running"}, "running"},
		{map[string]any{}, "unknown"},
		{map[string]any{"outcome": "error", "action_required": true}, "error (needs attention)"},
	} {
		if got := onboardingState(c.row); got != c.want {
			t.Errorf("onboardingState(%v) = %q, want %q", c.row, got, c.want)
		}
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

// One repo can hold a baseline per branch; --quiet exists for piping, so a repeated name
// would make the piped list wrong.
func TestListQuietPrintsEachRepoOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"baselines": []any{
				map[string]any{"repo": "a/one", "branch": "main"},
				map[string]any{"repo": "a/one", "branch": "release-2.3"},
				map[string]any{"repo": "a/two", "branch": "main"},
			},
			"skipped": []any{},
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	_ = guardrailsListCmd.Flags().Set("quiet", "true")
	defer func() { _ = guardrailsListCmd.Flags().Set("quiet", "false") }()

	out := captureStdout(t, func() {
		if err := runGuardrailsList(guardrailsListCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if got := strings.Count(out, "a/one"); got != 1 {
		t.Errorf("a/one printed %d times, want 1\n%s", got, out)
	}
	if !strings.Contains(out, "a/two") {
		t.Errorf("a/two missing\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b)
}
