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

func TestConnectRegistered(t *testing.T) {
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

func TestConnectSendsTheOrganizationAndReportsLinked(t *testing.T) {
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

	if err := connectFlow(guardrailsConnectCmd, nil); err != nil {
		t.Fatalf("connectFlow: %v", err)
	}
	if want := guardrailsAPI + "/dashboard/install"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if !strings.Contains(gotBody, `"account":"acme"`) {
		t.Errorf("body = %q, want the organization", gotBody)
	}
}

func TestConnectShowsTheRepositorySelectionWhenAlreadyConnected(t *testing.T) {
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
	forceTableOutput(t)

	out := captureStdout(t, func() {
		if err := connectFlow(guardrailsConnectCmd, nil); err != nil {
			t.Fatalf("connectFlow: %v", err)
		}
	})
	if !strings.Contains(out, "Connected acme") {
		t.Fatalf("not the human output path:\n%s", out)
	}
	if !strings.Contains(out, manage) {
		t.Errorf("output does not offer the repository selection:\n%s", out)
	}
}

// installSequence replies with each payload in turn, repeating the last once they run out, so a
// test can make the repository appear part-way through a wait.
func installSequence(t *testing.T, calls *atomic.Int32, replies ...map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(calls.Add(1)) - 1
		if i >= len(replies) {
			i = len(replies) - 1
		}
		_ = json.NewEncoder(w).Encode(replies[i])
	}))
}

func TestConnectWaitsUntilTheRepositoryIsVisible(t *testing.T) {
	// The wait ends on a fact Konvu checked with GitHub, which is what makes it worth waiting for
	// rather than asking the user to confirm they are done.
	manage := "https://github.com/organizations/acme/settings/installations/42"
	var calls atomic.Int32
	srv := installSequence(t, &calls,
		map[string]any{"linked": true, "account": "acme", "manage_url": manage, "repo_visible": false},
		map[string]any{"linked": true, "account": "acme", "manage_url": manage, "repo_visible": true},
	)
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	setInstallOrg(t, "acme")
	forceTableOutput(t)
	opened := stubBrowser(t)

	out := captureStdout(t, func() {
		if err := connectFlow(guardrailsConnectCmd, nil); err != nil {
			t.Fatalf("connectFlow: %v", err)
		}
	})

	if len(*opened) != 1 || (*opened)[0] != manage {
		t.Errorf("browser opened at %v, want exactly [%s]", *opened, manage)
	}
	if !strings.Contains(out, "cannot see") || !strings.Contains(out, manage) {
		t.Errorf("did not say what is missing, or where to fix it:\n%s", out)
	}
	if !strings.Contains(out, "is connected") {
		t.Errorf("never reported success once the repository appeared:\n%s", out)
	}
	if n := calls.Load(); n < 2 {
		t.Errorf("asked %d times, so it never waited", n)
	}
}

func TestConnectDoesNotWaitWhenVisibilityIsUnknown(t *testing.T) {
	// null is "could not tell" - GitHub unreachable, or no repo named. Treating it as "missing"
	// would send someone to fix a repository selection that is already correct.
	manage := "https://github.com/organizations/acme/settings/installations/42"
	var calls atomic.Int32
	srv := installSequence(t, &calls,
		map[string]any{"linked": true, "account": "acme", "manage_url": manage, "repo_visible": nil},
	)
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	setInstallOrg(t, "acme")
	forceTableOutput(t)
	opened := stubBrowser(t)

	out := captureStdout(t, func() {
		if err := connectFlow(guardrailsConnectCmd, nil); err != nil {
			t.Fatalf("connectFlow: %v", err)
		}
	})

	if len(*opened) != 0 {
		t.Errorf("opened a browser on an unknown answer: %v", *opened)
	}
	if strings.Contains(out, "cannot see") {
		t.Errorf("claimed the repository is missing on a null answer:\n%s", out)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("asked %d times, want exactly 1", n)
	}
}

func TestConnectAsksOnceAndReturnsWhenNotInstalled(t *testing.T) {
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

	if err := connectFlow(guardrailsConnectCmd, nil); err != nil {
		t.Fatalf("connectFlow: %v", err)
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

// forceTableOutput pins the human output path. captureStdout makes stdout a pipe and the format
// auto-detects to JSON there, so without this an assertion matches the JSON dump of the whole
// payload and says nothing about what a person sees -- which is how the first version of these
// tests passed while the print it was checking had been deleted.
func forceTableOutput(t *testing.T) {
	t.Helper()
	if err := guardrailsConnectCmd.Flags().Set("output", "table"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guardrailsConnectCmd.Flags().Set("output", "") })
}

// stubBrowser records what would have been opened instead of opening it. Without this the wait
// path launches a real tab on every test run, at a URL that does not exist.
func stubBrowser(t *testing.T) *[]string {
	t.Helper()
	var opened []string
	prev := openInBrowser
	openInBrowser = func(u string) { opened = append(opened, u) }
	t.Cleanup(func() { openInBrowser = prev })
	return &opened
}
