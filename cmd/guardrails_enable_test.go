package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func enableServer(t *testing.T, body *string, reply map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := guardrailsAPI + "/dashboard/enablement"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		b, _ := io.ReadAll(r.Body)
		*body = string(b)
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	t.Cleanup(func() { _ = guardrailsEnableCmd.Flags().Set("off", "false") })
	return srv
}

// A bare `enable` omits the list rather than sending an empty one: omitted asks for every
// approved repository, `[]` would ask for none and report success for it.
func TestBareEnableOmitsTheRepoList(t *testing.T) {
	var body string
	srv := enableServer(t, &body, map[string]any{"rows": []any{}})
	defer srv.Close()

	if err := enableFlow(guardrailsEnableCmd, nil); err != nil {
		t.Fatalf("enableFlow: %v", err)
	}
	if strings.Contains(body, `"repos"`) {
		t.Errorf("body = %q, want no repos key", body)
	}
	if !strings.Contains(body, `"enabled":true`) {
		t.Errorf("body = %q, want enabled true", body)
	}
}

func TestNamedReposAreSent(t *testing.T) {
	var body string
	srv := enableServer(t, &body, map[string]any{
		"rows": []any{map[string]any{"repo": "acme/web", "outcome": "enabled"}},
	})
	defer srv.Close()

	if err := enableFlow(guardrailsEnableCmd, []string{"acme/web", "acme/api"}); err != nil {
		t.Fatalf("enableFlow: %v", err)
	}
	if !strings.Contains(body, `"acme/web"`) || !strings.Contains(body, `"acme/api"`) {
		t.Errorf("body = %q, want both repos", body)
	}
}

func TestOffSendsEnabledFalse(t *testing.T) {
	var body string
	srv := enableServer(t, &body, map[string]any{
		"rows": []any{map[string]any{"repo": "acme/web", "outcome": "disabled"}},
	})
	defer srv.Close()

	_ = guardrailsEnableCmd.Flags().Set("off", "true")
	if err := enableFlow(guardrailsEnableCmd, []string{"acme/web"}); err != nil {
		t.Fatalf("enableFlow: %v", err)
	}
	if !strings.Contains(body, `"enabled":false`) {
		t.Errorf("body = %q, want enabled false", body)
	}
}

// A repository the server refused has to reach the reader; dropping it leaves a short list that
// reads as "all done" for a repository nothing is checking.
func TestARefusedRepoIsPrinted(t *testing.T) {
	out := captureStdout(t, func() {
		printEnablement(map[string]any{"rows": []any{
			map[string]any{"repo": "acme/web", "outcome": "enabled"},
			map[string]any{"repo": "acme/draft", "outcome": "no_ratified_baseline"},
		}}, false)
	})
	if !strings.Contains(out, "acme/draft") || !strings.Contains(out, "no approved rules") {
		t.Errorf("output = %q, want the refused repo and the reason", out)
	}
}

func TestDisablingReportsStrandedDecisions(t *testing.T) {
	out := captureStdout(t, func() {
		printEnablement(map[string]any{"rows": []any{
			map[string]any{"repo": "acme/web", "outcome": "disabled", "stranded_approvals": float64(3)},
		}}, true)
	})
	if !strings.Contains(out, "3 open pull request") {
		t.Errorf("output = %q, want the stranded count", out)
	}
}

// The command must not imply it blocks merges, since it cannot.
func TestEnablingSaysItDoesNotBlockMerges(t *testing.T) {
	out := captureStdout(t, func() {
		printEnablement(map[string]any{"rows": []any{
			map[string]any{"repo": "acme/web", "outcome": "enabled"},
		}}, false)
	})
	if !strings.Contains(out, "do not block merges") || !strings.Contains(out, "next push") {
		t.Errorf("output = %q, want the non-blocking note and the open-PR caveat", out)
	}
}

func TestNothingApprovedYetTellsYouWhatToDo(t *testing.T) {
	out := captureStdout(t, func() {
		printEnablement(map[string]any{"rows": []any{}}, false)
	})
	if !strings.Contains(out, "guardrails scan") {
		t.Errorf("output = %q, want the next step", out)
	}
}
