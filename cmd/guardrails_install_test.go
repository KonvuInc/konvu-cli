package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInstallRegistered(t *testing.T) {
	for _, c := range guardrailsCmd.Commands() {
		if c.Name() == "install" {
			return
		}
	}
	t.Error("guardrails missing subcommand: install")
}

// installServer records what the command sent and replies with the given payloads in order,
// repeating the last one once they run out.
func installServer(t *testing.T, gotPath, gotBody *string, replies ...map[string]any) *httptest.Server {
	t.Helper()
	var n atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		*gotBody = string(b)
		i := int(n.Add(1)) - 1
		if i >= len(replies) {
			i = len(replies) - 1
		}
		_ = json.NewEncoder(w).Encode(replies[i])
	}))
}

func setInstallFlags(t *testing.T, org string, wait time.Duration) {
	t.Helper()
	prevOrg, prevWait := inOrg, inWait
	inOrg, inWait = org, wait
	t.Cleanup(func() { inOrg, inWait = prevOrg, prevWait })
}

func TestInstallSendsTheOrganizationAndReportsLinked(t *testing.T) {
	var gotPath, gotBody string
	srv := installServer(t, &gotPath, &gotBody, map[string]any{
		"linked": true, "account": "acme", "detail": "acme is linked to your Konvu company",
	})
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	setInstallFlags(t, "acme", 0)

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

func TestInstallDoesNotWaitWhenWaitIsZero(t *testing.T) {
	// --wait 0 is the CI shape: print the link and exit rather than block a pipeline.
	var gotPath, gotBody string
	srv := installServer(t, &gotPath, &gotBody, map[string]any{
		"linked": false, "install_url": "https://github.com/apps/x/installations/new",
		"detail": "not installed",
	})
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	setInstallFlags(t, "acme", 0)

	done := make(chan error, 1)
	go func() { done <- installFlow(guardrailsInstallCmd, nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("installFlow: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("installFlow waited despite --wait 0")
	}
}

func TestInstallStopsPollingOnceLinked(t *testing.T) {
	var gotPath, gotBody string
	srv := installServer(t, &gotPath, &gotBody,
		map[string]any{"linked": false, "install_url": "https://github.com/apps/x/installations/new"},
		map[string]any{"linked": true, "account": "acme"},
	)
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	setInstallFlags(t, "acme", time.Minute)

	done := make(chan error, 1)
	go func() { done <- installFlow(guardrailsInstallCmd, nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("installFlow: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("installFlow kept polling after the installation appeared")
	}
}

func TestGithubOwnerReturnsNothingWithoutARemote(t *testing.T) {
	// A directory name is not a GitHub organization. Returning it would send a wrong name to
	// the server; the command asks for --org instead.
	if got := githubOwner(t.TempDir()); got != "" {
		t.Errorf("githubOwner = %q, want empty so the command asks for --org", got)
	}
}
