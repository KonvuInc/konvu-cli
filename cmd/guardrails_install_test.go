package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInstallRegistered(t *testing.T) {
	for _, c := range guardrailsCmd.Commands() {
		if c.Name() == "install" {
			return
		}
	}
	t.Error("guardrails missing subcommand: install")
}

// installServer records what the command sent, counts requests, and replies with one payload.
func installServer(t *testing.T, gotPath, gotBody *string, calls *atomic.Int32, reply map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		*gotBody = string(b)
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(reply)
	}))
}

func setInstallOrg(t *testing.T, org string) {
	t.Helper()
	prev := inOrg
	inOrg = org
	t.Cleanup(func() { inOrg = prev })
}

func TestInstallSendsTheOrganizationAndReportsLinked(t *testing.T) {
	var gotPath, gotBody string
	var calls atomic.Int32
	srv := installServer(t, &gotPath, &gotBody, &calls, map[string]any{
		"linked": true, "account": "acme", "detail": "acme is linked to your Konvu company",
	})
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	setInstallOrg(t, "acme")

	if err := installFlow(guardrailsInstallCmd, nil); err != nil {
		t.Fatalf("installFlow: %v", err)
	}
	if want := guardrailsAPI + "/dashboard/install"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if !strings.Contains(gotBody, `"account":"acme"`) {
		t.Errorf("body = %q, want the organization", gotBody)
	}
}

func TestInstallShowsTheRepositorySelectionWhenAlreadyConnected(t *testing.T) {
	// Connecting an organization is not the same as giving Konvu a repository, and the selection
	// is changed on the same page, so the link has to appear on every run and not only the first.
	var gotPath, gotBody string
	var calls atomic.Int32
	manage := "https://github.com/organizations/acme/settings/installations/42"
	srv := installServer(t, &gotPath, &gotBody, &calls, map[string]any{
		"linked": true, "account": "acme", "manage_url": manage,
		"detail": "acme is linked to your Konvu company",
	})
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	setInstallOrg(t, "acme")
	// Forced, because captureStdout makes stdout a pipe and the format auto-detects to JSON
	// there. Without this the assertion passes on the JSON dump of the whole payload and says
	// nothing about the human output, which is the thing being tested.
	if err := guardrailsInstallCmd.Flags().Set("output", "table"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guardrailsInstallCmd.Flags().Set("output", "") })

	out := captureStdout(t, func() {
		if err := installFlow(guardrailsInstallCmd, nil); err != nil {
			t.Fatalf("installFlow: %v", err)
		}
	})
	if !strings.Contains(out, "Connected acme") {
		t.Fatalf("not the human output path:\n%s", out)
	}
	if !strings.Contains(out, manage) {
		t.Errorf("output does not offer the repository selection:\n%s", out)
	}
}

func TestInstallAsksOnceAndReturnsWhenNotInstalled(t *testing.T) {
	// The command reports state and exits; you install and run it again. A version that waited
	// would keep asking here, so the request count is what pins the behaviour.
	var gotPath, gotBody string
	var calls atomic.Int32
	srv := installServer(t, &gotPath, &gotBody, &calls, map[string]any{
		"linked": false, "install_url": "https://github.com/apps/x/installations/new",
		"detail": "not installed",
	})
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	setInstallOrg(t, "acme")

	if err := installFlow(guardrailsInstallCmd, nil); err != nil {
		t.Fatalf("installFlow: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("asked %d times, want exactly 1", n)
	}
}

func TestGithubOwnerReturnsNothingWithoutARemote(t *testing.T) {
	// A directory name is not a GitHub organization. Returning it would send a wrong name to
	// the server; the command asks for --org instead.
	if got := githubOwner(t.TempDir()); got != "" {
		t.Errorf("githubOwner = %q, want empty so the command asks for --org", got)
	}
}
