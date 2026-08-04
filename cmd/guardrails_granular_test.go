package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/spf13/pflag"
)

// stubServer answers every call with one reply and records the last request it saw.
type stubServer struct {
	method string
	path   string
	query  string
	body   map[string]any
}

func newStub(t *testing.T, reply map[string]any) (*httptest.Server, *stubServer) {
	t.Helper()
	seen := &stubServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.method, seen.path, seen.query = r.Method, r.URL.Path, r.URL.RawQuery
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &seen.body)
		}
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	return srv, seen
}

// clearRepeatable empties a repeatable flag. These commands are package-level singletons, so a
// value one test sets leaks into every test after it.
// Undoes a repeatable flag entirely: the collected values AND `Changed`. Both halves matter --
// `rulesBody` keys on Changed, so leaving it set makes the next test look like it passed --rule.
func clearRepeatable(f *pflag.Flag) {
	if sv, ok := f.Value.(interface{ Replace([]string) error }); ok {
		_ = sv.Replace(nil)
	}
	f.Changed = false
}

// Tests asserting on human output set --output table: stdout is a pipe under `go test`, and
// DetectOutputFormat answers JSON for a pipe.

// --- approve --rule ----------------------------------------------------------------------------

func TestApproveSendsOnlyTheRulesNamed(t *testing.T) {
	srv, seen := newStub(t, map[string]any{
		"repo": "acme/web", "branch": "main", "ratified": true,
		"n_ratified": 1.0, "n_proposals": 2.0,
	})
	defer srv.Close()

	f := guardrailsApproveCmd.Flags()
	_ = f.Set("rule", "USER|read|Document||02d8a853")
	_ = f.Set("rule", "USER|write|Document||a6466bf8")
	t.Cleanup(func() { clearRepeatable(f.Lookup("rule")) })

	if err := approveFlow(guardrailsApproveCmd, []string{"acme/web"}); err != nil {
		t.Fatalf("approveFlow: %v", err)
	}
	got, ok := seen.body["clauses"].([]any)
	if !ok {
		t.Fatalf("no selection on the wire: %v", seen.body)
	}
	if len(got) != 2 || got[0] != "USER|read|Document||02d8a853" {
		t.Errorf("selection = %v, want both rules in order", got)
	}
}

// A key is copied out of `show`, so it must travel intact: StringSlice would split a route
// containing a comma into two keys that match nothing.
func TestRuleFlagDoesNotSplitOnCommas(t *testing.T) {
	srv, seen := newStub(t, map[string]any{"repo": "a/b", "branch": "main", "ratified": true})
	defer srv.Close()

	key := "USER|read|Document|GET /docs/{a,b}|5b3ad0e1"
	f := guardrailsApproveCmd.Flags()
	_ = f.Set("rule", key)
	t.Cleanup(func() { clearRepeatable(f.Lookup("rule")) })

	if err := approveFlow(guardrailsApproveCmd, []string{"a/b"}); err != nil {
		t.Fatal(err)
	}
	if got := seen.body["clauses"].([]any); len(got) != 1 || got[0] != key {
		t.Errorf("selection = %v, want the one key intact", got)
	}
}

func TestApproveWithoutRulesSendsNoSelection(t *testing.T) {
	srv, seen := newStub(t, map[string]any{"repo": "a/b", "branch": "main", "ratified": true})
	defer srv.Close()

	if err := approveFlow(guardrailsApproveCmd, []string{"a/b"}); err != nil {
		t.Fatal(err)
	}
	if _, present := seen.body["clauses"]; present {
		t.Errorf("a selection was sent when every rule was meant: %v", seen.body)
	}
}

// --rule was passed but held nothing usable. Falling through to an omitted field would approve
// every rule.
func TestARuleFlagThatNamesNothingRefusesRatherThanApprovingEverything(t *testing.T) {
	srv, seen := newStub(t, map[string]any{"repo": "a/b", "branch": "main", "ratified": true})
	defer srv.Close()

	f := guardrailsApproveCmd.Flags()
	_ = f.Set("rule", "   ")
	t.Cleanup(func() { clearRepeatable(f.Lookup("rule")) })

	err := approveFlow(guardrailsApproveCmd, []string{"a/b"})
	if err == nil {
		t.Fatal("want a usage error, got nil")
	}
	if seen.method != "" {
		t.Errorf("a request was sent anyway: %s %s", seen.method, seen.path)
	}
	if !strings.Contains(err.Error(), "named no rule") {
		t.Errorf("error = %q, want it to name the problem", err)
	}
}

// --- unapprove ---------------------------------------------------------------------------------

func TestUnapprovePostsToTheUnapproveRoute(t *testing.T) {
	srv, seen := newStub(t, map[string]any{
		"repo": "acme/web", "branch": "main", "ratified": true,
		"withdrawn": 1.0, "n_ratified": 2.0, "n_proposals": 1.0,
	})
	defer srv.Close()

	f := guardrailsUnapproveCmd.Flags()
	_ = f.Set("rule", "USER|write|Document||a6466bf8")
	t.Cleanup(func() { clearRepeatable(f.Lookup("rule")) })

	if err := unapproveFlow(guardrailsUnapproveCmd, []string{"acme/web"}); err != nil {
		t.Fatalf("unapproveFlow: %v", err)
	}
	if want := guardrailsAPI + "/dashboard/repos/acme/web/unratify"; seen.path != want {
		t.Errorf("path = %q, want %q", seen.path, want)
	}
	if got := seen.body["clauses"].([]any); len(got) != 1 {
		t.Errorf("selection = %v, want one", got)
	}
}

// Losing the last approval stops pull requests being judged. The bare field alone leaves the
// reader to work out what that costs them.
func TestUnapprovingEverythingSaysChecksCannotJudge(t *testing.T) {
	srv, _ := newStub(t, map[string]any{
		"repo": "acme/web", "branch": "main", "ratified": false,
		"withdrawn": 3.0, "n_ratified": 0.0, "n_proposals": 3.0,
	})
	defer srv.Close()
	_ = guardrailsUnapproveCmd.Flags().Set("output", "table")
	t.Cleanup(func() { _ = guardrailsUnapproveCmd.Flags().Set("output", "") })

	out := captureStdout(t, func() {
		if err := unapproveFlow(guardrailsUnapproveCmd, []string{"acme/web"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "paused") {
		t.Errorf("consequence not stated:\n%s", out)
	}
	if !strings.Contains(out, "konvu guardrails approve acme/web") {
		t.Errorf("no way back offered:\n%s", out)
	}
}

// Two real states after a partial approval, and the message has to tell them apart: a repository
// Konvu drafted for you is checked with rules outstanding, one you are approving from scratch is not.
func TestAPartialApproveSaysWhetherChecksAreRunning(t *testing.T) {
	for _, tc := range []struct {
		name     string
		checking bool
		want     string
	}{
		{"from scratch", false, "stay paused"},
		{"already checked", true, "counts as"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newStub(t, map[string]any{
				"repo": "acme/web", "branch": "main", "ratified": tc.checking,
				"n_ratified": 1.0, "n_proposals": 4.0,
			})
			defer srv.Close()
			_ = guardrailsApproveCmd.Flags().Set("output", "table")
			t.Cleanup(func() { _ = guardrailsApproveCmd.Flags().Set("output", "") })

			out := captureStdout(t, func() {
				if err := approveFlow(guardrailsApproveCmd, []string{"acme/web"}); err != nil {
					t.Fatal(err)
				}
			})
			if !strings.Contains(out, "4 still drafted") {
				t.Errorf("counts missing:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("wrong standing for checking=%v, want %q:\n%s", tc.checking, tc.want, out)
			}
		})
	}
}

// --- delete ------------------------------------------------------------------------------------

func TestDeleteUsesDeleteAndSendsTheBranch(t *testing.T) {
	srv, seen := newStub(t, map[string]any{
		"repo": "acme/web", "deleted": []any{"main"}, "still_enabled": false,
	})
	defer srv.Close()

	f := guardrailsDeleteCmd.Flags()
	_ = f.Set("branch", "release-2.3")
	_ = f.Set("yes", "true")
	t.Cleanup(func() { _ = f.Set("branch", ""); _ = f.Set("yes", "false") })

	if err := deleteFlow(guardrailsDeleteCmd, []string{"acme/web"}); err != nil {
		t.Fatalf("deleteFlow: %v", err)
	}
	if seen.method != "DELETE" {
		t.Errorf("method = %q, want DELETE", seen.method)
	}
	if want := guardrailsAPI + "/dashboard/repos/acme/web/baseline"; seen.path != want {
		t.Errorf("path = %q, want %q", seen.path, want)
	}
	if !strings.Contains(seen.query, "branch=release-2.3") {
		t.Errorf("query = %q, want the branch", seen.query)
	}
}

func TestDeleteAllBranchesSendsTheFlagAndNoBranch(t *testing.T) {
	srv, seen := newStub(t, map[string]any{
		"repo": "acme/web", "deleted": []any{"main", "release-2.3"},
	})
	defer srv.Close()

	f := guardrailsDeleteCmd.Flags()
	_ = f.Set("all-branches", "true")
	_ = f.Set("yes", "true")
	_ = f.Set("output", "table")
	t.Cleanup(func() {
		_ = f.Set("all-branches", "false")
		_ = f.Set("yes", "false")
		_ = f.Set("output", "")
	})

	_ = captureStdout(t, func() {
		if err := deleteFlow(guardrailsDeleteCmd, []string{"acme/web"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(seen.query, "all_branches=true") {
		t.Errorf("query = %q, want all_branches", seen.query)
	}
	if strings.Contains(seen.query, "branch=") {
		t.Errorf("query = %q, must not also pin a branch", seen.query)
	}
}

func TestDeleteRefusesABranchAndAllBranchesTogether(t *testing.T) {
	srv, seen := newStub(t, map[string]any{"repo": "a/b", "deleted": []any{"main"}})
	defer srv.Close()

	f := guardrailsDeleteCmd.Flags()
	_ = f.Set("branch", "main")
	_ = f.Set("all-branches", "true")
	_ = f.Set("yes", "true")
	t.Cleanup(func() {
		_ = f.Set("branch", "")
		_ = f.Set("all-branches", "false")
		_ = f.Set("yes", "false")
	})

	if err := deleteFlow(guardrailsDeleteCmd, []string{"a/b"}); err == nil {
		t.Fatal("want a usage error")
	}
	if seen.method != "" {
		t.Errorf("a request was sent anyway: %s", seen.method)
	}
}

// Deleting for a repository still switched on leaves its checks with nothing to judge against. Not
// switched off here, because that would discard decisions recorded on open pull requests.
func TestDeleteWarnsWhenTheRepoIsStillEnabled(t *testing.T) {
	srv, _ := newStub(t, map[string]any{
		"repo": "acme/web", "deleted": []any{"main"}, "still_enabled": true,
	})
	defer srv.Close()

	f := guardrailsDeleteCmd.Flags()
	_ = f.Set("yes", "true")
	_ = f.Set("output", "table")
	t.Cleanup(func() { _ = f.Set("yes", "false"); _ = f.Set("output", "") })

	out := captureStdout(t, func() {
		if err := deleteFlow(guardrailsDeleteCmd, []string{"acme/web"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "still switched on") {
		t.Errorf("no warning:\n%s", out)
	}
}

// --- scanning many repositories ----------------------------------------------------------------

func TestBulkAllSendsNoRepoList(t *testing.T) {
	srv, seen := newStub(t, map[string]any{"rows": []any{}, "queued": 0.0})
	defer srv.Close()

	f := guardrailsScanCmd.Flags()
	_ = f.Set("all", "true")
	_ = f.Set("output", "table")
	t.Cleanup(func() { _ = f.Set("all", "false"); _ = f.Set("output", "") })

	_ = captureStdout(t, func() {
		if err := scanFlow(guardrailsScanCmd, nil); err != nil {
			t.Fatalf("scanFlow: %v", err)
		}
	})
	if want := guardrailsAPI + "/dashboard/baselines"; seen.path != want {
		t.Errorf("path = %q, want %q", seen.path, want)
	}
	// Omitted, not empty: [] would ask the server to act on nothing.
	if _, present := seen.body["repos"]; present {
		t.Errorf("a repo list was sent for --all: %v", seen.body)
	}
}

func TestBulkRemoteSendsTheNamedRepos(t *testing.T) {
	srv, seen := newStub(t, map[string]any{
		"rows": []any{
			map[string]any{"repo": "acme/web", "outcome": "queued", "job_id": "j1"},
			map[string]any{"repo": "acme/api", "outcome": "queued", "job_id": "j2"},
		},
		"queued": 2.0,
	})
	defer srv.Close()

	f := guardrailsScanCmd.Flags()
	_ = f.Set("remote", "acme/web")
	_ = f.Set("remote", "acme/api")
	_ = f.Set("output", "table")
	t.Cleanup(func() {
		clearRepeatable(f.Lookup("remote"))
		_ = f.Set("output", "")
	})

	out := captureStdout(t, func() {
		if err := scanFlow(guardrailsScanCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if got := seen.body["repos"].([]any); len(got) != 2 {
		t.Fatalf("repos = %v, want two", got)
	}
	for _, want := range []string{"acme/web", "acme/api"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s missing from output:\n%s", want, out)
		}
	}
}

// A path means "bundle this checkout" and --remote means "Konvu fetches it instead"; picking one
// would quietly ignore half the command.
func TestBulkRefusesAPathAtTheSameTime(t *testing.T) {
	srv, seen := newStub(t, map[string]any{"rows": []any{}})
	defer srv.Close()

	f := guardrailsScanCmd.Flags()
	_ = f.Set("remote", "acme/web")
	t.Cleanup(func() { clearRepeatable(f.Lookup("remote")) })

	if err := scanFlow(guardrailsScanCmd, []string{"../web"}); err == nil {
		t.Fatal("want a usage error")
	}
	if seen.method != "" {
		t.Errorf("a request was sent anyway: %s", seen.method)
	}
}

// Every repository the server answered for is reported, and the outcomes are not collapsed: each
// needs different action, and a short list would read as "all done".
func TestBulkReportsEveryRepoItCouldNotQueue(t *testing.T) {
	srv, _ := newStub(t, map[string]any{
		"rows": []any{
			map[string]any{"repo": "acme/web", "outcome": "queued", "job_id": "j1"},
			map[string]any{"repo": "acme/api", "outcome": "already_queued"},
			map[string]any{"repo": "acme/secret", "outcome": "not_visible"},
			map[string]any{"repo": "acme/old", "outcome": "not_allowed"},
			map[string]any{"repo": "acme/maybe", "outcome": "unknown"},
		},
		"queued":      1.0,
		"unreachable": []any{"othercorp"},
	})
	defer srv.Close()

	f := guardrailsScanCmd.Flags()
	_ = f.Set("all", "true")
	_ = f.Set("output", "table")
	t.Cleanup(func() { _ = f.Set("all", "false"); _ = f.Set("output", "") })

	out := captureStdout(t, func() {
		if err := scanFlow(guardrailsScanCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"acme/web", "acme/api", "acme/secret", "acme/old", "acme/maybe", "othercorp",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%s missing from output:\n%s", want, out)
		}
	}
	// "Could not tell" must not be reported as "your repository is missing": that sends someone to
	// fix a repository selection which is already correct.
	i := strings.Index(out, "Konvu cannot see:")
	if i < 0 {
		t.Fatalf("no invisible-repo line:\n%s", out)
	}
	if strings.Contains(strings.SplitN(out[i:], "\n", 2)[0], "acme/maybe") {
		t.Errorf("an unknown repo was reported as invisible:\n%s", out)
	}
}

func TestBulkRemoteThatNamesNothingRefuses(t *testing.T) {
	srv, seen := newStub(t, map[string]any{"rows": []any{}})
	defer srv.Close()

	f := guardrailsScanCmd.Flags()
	_ = f.Set("remote", "  ")
	t.Cleanup(func() { clearRepeatable(f.Lookup("remote")) })

	if err := scanFlow(guardrailsScanCmd, nil); err == nil {
		t.Fatal("want a usage error")
	}
	if seen.method != "" {
		t.Errorf("a request was sent anyway: %s", seen.method)
	}
}

// --wait follows every queued scan to a terminal state; one failing must not hide the others.
func TestBulkWaitFollowsEveryScanAndReportsEachOutcome(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/jobs/j-web"):
			_ = json.NewEncoder(w).Encode(map[string]any{"job_id": "j-web", "status": "done"})
		case strings.HasSuffix(r.URL.Path, "/jobs/j-api"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"job_id": "j-api", "status": "error", "error": "no access checks found",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"rows": []any{
					map[string]any{"repo": "acme/web", "outcome": "queued", "job_id": "j-web"},
					map[string]any{"repo": "acme/api", "outcome": "queued", "job_id": "j-api"},
				},
				"queued": 2.0,
			})
		}
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	old := bulkPollEvery
	bulkPollEvery = time.Millisecond
	t.Cleanup(func() { bulkPollEvery = old })

	f := guardrailsScanCmd.Flags()
	_ = f.Set("all", "true")
	_ = f.Set("wait", "true")
	_ = f.Set("output", "table")
	t.Cleanup(func() {
		_ = f.Set("all", "false")
		_ = f.Set("wait", "false")
		_ = f.Set("output", "")
	})

	var err error
	out := captureStdout(t, func() { err = scanFlow(guardrailsScanCmd, nil) })

	if !strings.Contains(out, "acme/web: done") {
		t.Errorf("the finished scan was not reported:\n%s", out)
	}
	if !strings.Contains(out, "acme/api: failed - no access checks found") {
		t.Errorf("the failed scan was not reported:\n%s", out)
	}
	// A scan that failed must not exit 0: printing it and returning nil would hand CI a green run
	// over work that produced nothing.
	if err == nil {
		t.Fatal("want a non-zero exit when a scan failed")
	}
	if !strings.Contains(err.Error(), "acme/api") {
		t.Errorf("error = %q, want it to name the failed repository", err)
	}
}

// --branch and --repo describe the one local checkout. Accepting either alongside --all/--remote
// and then dropping it would record somewhere the caller did not ask for.
func TestBulkRefusesLocalOnlyScopeFlags(t *testing.T) {
	for _, tc := range []struct{ flag, value string }{{"branch", "release-2.3"}, {"repo", "other/name"}} {
		t.Run(tc.flag, func(t *testing.T) {
			srv, seen := newStub(t, map[string]any{"rows": []any{}})
			defer srv.Close()

			f := guardrailsScanCmd.Flags()
			_ = f.Set("all", "true")
			_ = f.Set(tc.flag, tc.value)
			t.Cleanup(func() { _ = f.Set("all", "false"); _ = f.Set(tc.flag, "") })

			if err := scanFlow(guardrailsScanCmd, nil); err == nil {
				t.Fatalf("--%s alongside --all should be refused", tc.flag)
			}
			if seen.method != "" {
				t.Errorf("a request was sent anyway: %s", seen.method)
			}
		})
	}
}

// Asked to wait and got no confirmation: not a success, matching the single-repo path beside it.
func TestBulkWaitTimingOutIsNotASuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/jobs/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"job_id": "j1", "status": "running"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rows":   []any{map[string]any{"repo": "acme/web", "outcome": "queued", "job_id": "j1"}},
			"queued": 1.0,
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	old := bulkPollEvery
	bulkPollEvery = time.Millisecond
	t.Cleanup(func() { bulkPollEvery = old })

	f := guardrailsScanCmd.Flags()
	_ = f.Set("all", "true")
	_ = f.Set("wait", "true")
	_ = f.Set("output", "table")
	_ = f.Set("timeout", "1ns")
	t.Cleanup(func() {
		_ = f.Set("all", "false")
		_ = f.Set("wait", "false")
		_ = f.Set("output", "")
		_ = f.Set("timeout", "30m")
	})

	var err error
	_ = captureStdout(t, func() { err = scanFlow(guardrailsScanCmd, nil) })
	if err == nil {
		t.Fatal("want a non-zero exit when the wait times out")
	}
	if !strings.Contains(err.Error(), "acme/web") {
		t.Errorf("error = %q, want it to name what is still running", err)
	}
}

// The confirmation used to vanish whenever the output format resolved to JSON, which
// DetectOutputFormat answers for ANY pipe -- so `delete <repo> | tee log` deleted with no consent.
// With no terminal to ask at, refuse rather than proceed.
func TestDeleteWithoutATerminalRefusesInsteadOfDeleting(t *testing.T) {
	srv, seen := newStub(t, map[string]any{"repo": "acme/web", "deleted": []any{"main"}})
	defer srv.Close()

	// stdin is not a terminal under `go test`, which is the case this guards.
	f := guardrailsDeleteCmd.Flags()
	_ = f.Set("output", "json")
	t.Cleanup(func() { _ = f.Set("output", "") })

	err := deleteFlow(guardrailsDeleteCmd, []string{"acme/web"})
	if err == nil {
		t.Fatal("want a refusal rather than an unconfirmed delete")
	}
	if seen.method != "" {
		t.Errorf("it deleted anyway: %s %s", seen.method, seen.path)
	}
	if !strings.Contains(err.Error(), "--yes") && !strings.Contains(errSuggestion(err), "--yes") {
		t.Errorf("error does not point at --yes: %v", err)
	}
}

func errSuggestion(err error) string {
	if e, ok := err.(*clierrors.CLIError); ok {
		return e.Suggestion
	}
	return ""
}

func TestBulkWithoutWaitDoesNotPollAtAll(t *testing.T) {
	var polled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/jobs/") {
			polled = true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rows":   []any{map[string]any{"repo": "acme/web", "outcome": "queued", "job_id": "j1"}},
			"queued": 1.0,
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	f := guardrailsScanCmd.Flags()
	_ = f.Set("all", "true")
	_ = f.Set("output", "table")
	t.Cleanup(func() { _ = f.Set("all", "false"); _ = f.Set("output", "") })

	_ = captureStdout(t, func() {
		if err := scanFlow(guardrailsScanCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if polled {
		t.Error("polled for progress without --wait")
	}
}

// --- show lists the keys, list says why a repository is not watched ----------------------------

// The key is what the reader passes to --rule, so it is printed one per line and untruncated: a
// table column would truncate the one value they need to copy.
func TestShowListsTheKeysOfRulesNotApprovedYet(t *testing.T) {
	srv, _ := newStub(t, map[string]any{
		"repo": "acme/web", "branch": "main", "ratified": true, "fingerprint": "abc",
		"n_paths": 4.0, "n_guarded": 3.0, "n_unguarded": 1.0,
		"policy": []any{
			map[string]any{
				"key": "USER|read|Document||02d8a853", "role": "USER", "action": "read",
				"resource": "Document", "route": "", "condition": "owns(USER, Document)",
				"ratified": true,
			},
			map[string]any{
				"key": "ADMIN|delete|Document|GET /docs/{id}|7c1e9f04", "role": "ADMIN", "action": "delete",
				"resource": "Document", "route": "GET /docs/{id}", "condition": "true",
				"ratified": false,
			},
		},
	})
	defer srv.Close()
	_ = guardrailsShowCmd.Flags().Set("output", "table")
	t.Cleanup(func() { _ = guardrailsShowCmd.Flags().Set("output", "") })

	out := captureStdout(t, func() {
		if err := showFlow(guardrailsShowCmd, []string{"acme/web"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "ADMIN|delete|Document|GET /docs/{id}|7c1e9f04") {
		t.Errorf("the unapproved rule's key is not copyable:\n%s", out)
	}
	i := strings.Index(out, "not approved yet")
	if i < 0 {
		t.Fatalf("no pending-rules block:\n%s", out)
	}
	if strings.Contains(out[i:], "USER|read|Document||02d8a853\n") {
		t.Errorf("an approved rule was listed as pending:\n%s", out)
	}
	if !strings.Contains(out, "--rule") {
		t.Errorf("no hint how to use the key:\n%s", out)
	}
}

// The reasons call for opposite responses, and one deleted on purpose must not read as
// "nothing found here".
func TestListSaysWhyEachRepoIsNotWatched(t *testing.T) {
	srv, _ := newStub(t, map[string]any{
		"baselines": []any{},
		"skipped":   []any{"acme/gone", "acme/empty", "acme/broken"},
		"skipped_reasons": map[string]any{
			"acme/gone":   "deleted-by-owner",
			"acme/empty":  "no-authz-surface",
			"acme/broken": "abstain:unknown-stack",
		},
	})
	defer srv.Close()
	_ = guardrailsListCmd.Flags().Set("output", "table")
	t.Cleanup(func() { _ = guardrailsListCmd.Flags().Set("output", "") })

	out := captureStdout(t, func() {
		if err := runGuardrailsList(guardrailsListCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	for repo, want := range map[string]string{
		"acme/gone":   "scan the repository again",
		"acme/empty":  "nothing to judge",
		"acme/broken": "on our side",
	} {
		var line string
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, repo) {
				line = l
			}
		}
		if line == "" {
			t.Errorf("%s not reported at all:\n%s", repo, out)
			continue
		}
		if !strings.Contains(line, want) {
			t.Errorf("%s: line %q does not say %q", repo, line, want)
		}
	}
}

func TestAnUnknownReasonIsShownRatherThanSwallowed(t *testing.T) {
	if got := notWatchedReason("something-new"); got != "something-new" {
		t.Errorf("notWatchedReason dropped an unknown code: %q", got)
	}
	if got := notWatchedReason(""); !strings.Contains(got, "nothing to judge") {
		t.Errorf("notWatchedReason(%q) = %q", "", got)
	}
}

// A 422 is the caller's own request being wrong, and the server's detail says which. The default
// arm suggests checking your session, the wrong remedy for a mistyped argument.
func TestAnInvalidRequestDoesNotBlameTheSession(t *testing.T) {
	cliErr := guardrailsCLIError(&api.APIError{
		Message:    `API error: {"detail":"no rule matches: USER|read|Typo|"}`,
		StatusCode: http.StatusUnprocessableEntity,
	})
	if strings.Contains(cliErr.Suggestion, "whoami") || strings.Contains(cliErr.Suggestion, "session") {
		t.Errorf("suggestion blames the session: %q", cliErr.Suggestion)
	}
	if !strings.Contains(cliErr.Message, "USER|read|Typo|") {
		t.Errorf("message lost the server's detail: %q", cliErr.Message)
	}
	if cliErr.ExitCode != clierrors.ExitUsageError {
		t.Errorf("exit code = %d, want %d", cliErr.ExitCode, clierrors.ExitUsageError)
	}
}

// --wait must mean wait whatever the output format. Keying it off the format left the false success
// reachable through --output json -- and DetectOutputFormat answers JSON for any pipe, so
// `scan --all --wait | tee log` would never have waited at all.
func TestBulkWaitStillWaitsAndFailsInJSON(t *testing.T) {
	var polled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/jobs/") {
			polled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"job_id": "j1", "status": "error", "error": "no access checks found",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rows":   []any{map[string]any{"repo": "acme/web", "outcome": "queued", "job_id": "j1"}},
			"queued": 1.0,
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	old := bulkPollEvery
	bulkPollEvery = time.Millisecond
	t.Cleanup(func() { bulkPollEvery = old })

	f := guardrailsScanCmd.Flags()
	_ = f.Set("all", "true")
	_ = f.Set("wait", "true")
	_ = f.Set("output", "json")
	t.Cleanup(func() {
		_ = f.Set("all", "false")
		_ = f.Set("wait", "false")
		_ = f.Set("output", "")
	})

	var err error
	out := captureStdout(t, func() { err = scanFlow(guardrailsScanCmd, nil) })

	if !polled {
		t.Error("--wait was ignored because the output format was JSON")
	}
	if err == nil {
		t.Fatal("want a non-zero exit when the scan failed")
	}
	// The machine reader gets the final state too, not just the queue receipt.
	if !strings.Contains(out, "results") || !strings.Contains(out, "no access checks found") {
		t.Errorf("JSON does not carry the outcome:\n%s", out)
	}
}

// The prompt is interactive UI. On stdout it sat in front of the JSON document and broke any
// reader parsing it.
func TestTheDeletePromptDoesNotLandInTheJSON(t *testing.T) {
	srv, _ := newStub(t, map[string]any{"repo": "acme/web", "deleted": []any{"main"}})
	defer srv.Close()

	f := guardrailsDeleteCmd.Flags()
	_ = f.Set("output", "json")
	_ = f.Set("yes", "true") // no terminal under `go test`; --yes is how a script gets here
	t.Cleanup(func() { _ = f.Set("output", ""); _ = f.Set("yes", "false") })

	out := captureStdout(t, func() {
		if err := deleteFlow(guardrailsDeleteCmd, []string{"acme/web"}); err != nil {
			t.Fatal(err)
		}
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("stdout is not parseable JSON (%v):\n%s", err, out)
	}
	if parsed["repo"] != "acme/web" {
		t.Errorf("unexpected document: %v", parsed)
	}
}

// A scan already running is one the caller asked about. Excluding it from the polling set let
// --wait exit 0 without observing whether it finished, failed, or is still going.
func TestBulkWaitFollowsAScanSomeoneElseStarted(t *testing.T) {
	var polledFor []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if i := strings.LastIndex(r.URL.Path, "/jobs/"); i >= 0 {
			id := r.URL.Path[i+len("/jobs/"):]
			polledFor = append(polledFor, id)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"job_id": id, "status": "error", "error": "no access checks found",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rows": []any{
				// Not queued by this call, but the server hands back the in-flight job.
				map[string]any{"repo": "acme/web", "outcome": "already_queued", "job_id": "j-live"},
			},
			"queued": 0.0,
		})
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	old := bulkPollEvery
	bulkPollEvery = time.Millisecond
	t.Cleanup(func() { bulkPollEvery = old })

	f := guardrailsScanCmd.Flags()
	_ = f.Set("all", "true")
	_ = f.Set("wait", "true")
	_ = f.Set("output", "table")
	t.Cleanup(func() {
		_ = f.Set("all", "false")
		_ = f.Set("wait", "false")
		_ = f.Set("output", "")
	})

	var err error
	_ = captureStdout(t, func() { err = scanFlow(guardrailsScanCmd, nil) })

	if len(polledFor) == 0 || polledFor[0] != "j-live" {
		t.Errorf("the already-running scan was never followed: polled %v", polledFor)
	}
	if err == nil {
		t.Fatal("want a non-zero exit: the scan it waited on failed")
	}
}

// The same repository with no job to poll: it finished between the queue attempt and the lookup,
// so --wait cannot confirm anything and must not call that success.
func TestBulkWaitReportsAnAlreadyRunningScanItCannotFollow(t *testing.T) {
	srv, _ := newStub(t, map[string]any{
		"rows":   []any{map[string]any{"repo": "acme/web", "outcome": "already_queued"}},
		"queued": 0.0,
	})
	defer srv.Close()

	f := guardrailsScanCmd.Flags()
	_ = f.Set("all", "true")
	_ = f.Set("wait", "true")
	_ = f.Set("output", "table")
	t.Cleanup(func() {
		_ = f.Set("all", "false")
		_ = f.Set("wait", "false")
		_ = f.Set("output", "")
	})

	var err error
	_ = captureStdout(t, func() { err = scanFlow(guardrailsScanCmd, nil) })
	if err == nil {
		t.Fatal("want a non-zero exit when the scan could not be observed")
	}
	if !strings.Contains(err.Error(), "acme/web") {
		t.Errorf("error = %q, want it to name the repository", err)
	}
}

// Declining is a path a JSON caller can reach: with a terminal to prompt at, --output json used to
// answer a sentence. Every reachable path owes the format that was asked for.
func TestDecliningTheDeleteStillAnswersInJSON(t *testing.T) {
	srv, seen := newStub(t, map[string]any{"repo": "acme/web", "deleted": []any{"main"}})
	defer srv.Close()

	// Stand in for an interactive session that answers "n".
	orig := confirmDelete
	confirmDelete = func(repo, branch string, all bool) (bool, error) { return false, nil }
	t.Cleanup(func() { confirmDelete = orig })

	f := guardrailsDeleteCmd.Flags()
	_ = f.Set("output", "json")
	t.Cleanup(func() { _ = f.Set("output", "") })

	out := captureStdout(t, func() {
		if err := deleteFlow(guardrailsDeleteCmd, []string{"acme/web"}); err != nil {
			t.Fatal(err)
		}
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("declining emitted unparseable output (%v):\n%s", err, out)
	}
	if parsed["cancelled"] != true {
		t.Errorf("document does not say it was cancelled: %v", parsed)
	}
	if seen.method != "" {
		t.Errorf("it deleted anyway: %s", seen.method)
	}
}

// pflag records no entry for `--rule ""`, so testing the slice length let an unexpanded shell
// variable read as "--rule was never passed" — and approve with no --rule approves every rule.
func TestAnEmptyRuleIsNotEveryRule(t *testing.T) {
	for _, name := range []string{"approve", "unapprove"} {
		cmd := guardrailsApproveCmd
		if name == "unapprove" {
			cmd = guardrailsUnapproveCmd
		}
		if err := cmd.Flags().Set("rule", ""); err != nil {
			t.Fatalf("set --rule: %v", err)
		}
		body, err := rulesBody(cmd)
		if err == nil {
			t.Errorf("%s --rule \"\" produced %v, want a refusal rather than every rule", name, body)
		}
		clearRepeatable(cmd.Flags().Lookup("rule"))
	}
}

// ...and the near-miss: no --rule at all still means every rule.
func TestNoRuleFlagStillMeansEveryRule(t *testing.T) {
	cmd := guardrailsApproveCmd
	clearRepeatable(cmd.Flags().Lookup("rule"))
	body, err := rulesBody(cmd)
	if err != nil {
		t.Fatalf("no --rule should be allowed: %v", err)
	}
	if _, named := body["clauses"]; named {
		t.Errorf("body = %v, want no clauses key so the server acts on all of them", body)
	}
}

func explainFinding(category, route string) map[string]any {
	return map[string]any{
		"route": route, "method": "GET", "role": "USER", "action": "read",
		"resource": "Document", "current_guard": "NONE", "category": category,
		"reason": "coverage gap: nothing covers this yet",
	}
}

// The two kinds call for opposite responses, so they are grouped and labelled rather than printed
// as one list the reader has to sort by hand.
func TestExplainGroupsTheTwoKindsOfFinding(t *testing.T) {
	out := captureStdout(t, func() {
		printExplain(map[string]any{
			"repo": "a/b", "pr_number": float64(1), "flagged": []any{
				explainFinding("coverage_gap", "GET /b"),
				explainFinding("breach", "GET /a"),
			},
		})
	})
	broken := strings.Index(out, "BREAKS A RULE YOU APPROVED")
	needs := strings.Index(out, "NEEDS YOUR APPROVAL")
	if broken < 0 || needs < 0 {
		t.Fatalf("output = %q, want both headings", out)
	}
	if broken > needs {
		t.Error("a rule already broken should come before access merely awaiting approval")
	}
	if !strings.Contains(out, "GET /a") || !strings.Contains(out, "GET /b") {
		t.Errorf("output = %q, want both routes", out)
	}
}

// A server that does not send the kind yet must still print the finding, unlabelled. Guessing a
// label would be worse than not saying.
func TestExplainStillPrintsAFindingWithNoKind(t *testing.T) {
	f := explainFinding("", "GET /c")
	delete(f, "category")
	out := captureStdout(t, func() {
		printExplain(map[string]any{"repo": "a/b", "pr_number": float64(1), "flagged": []any{f}})
	})
	if !strings.Contains(out, "GET /c") {
		t.Errorf("output = %q, want the finding printed", out)
	}
	if strings.Contains(out, "BREAKS A RULE") || strings.Contains(out, "NEEDS YOUR APPROVAL") {
		t.Errorf("output = %q, want no guessed heading", out)
	}
	// With no heading, the prefix in the reason is the only thing naming the kind.
	if !strings.Contains(out, "coverage gap:") {
		t.Errorf("output = %q, want the kind kept in the reason when nothing else states it", out)
	}
}

// Stripping the kind from the reason is cosmetic: the heading states it. An unrecognised wording
// must survive whole rather than being mangled.
func TestTheKindPrefixIsOnlyTrimmedWhenRecognised(t *testing.T) {
	if got := withoutKindPrefix("breach: lost its check"); got != "lost its check" {
		t.Errorf("got %q, want the prefix gone", got)
	}
	for _, in := range []string{"something else entirely", "breaches: not the prefix", ""} {
		if got := withoutKindPrefix(in); got != in {
			t.Errorf("withoutKindPrefix(%q) = %q, want it unchanged", in, got)
		}
	}
}
