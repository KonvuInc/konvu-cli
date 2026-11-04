package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/config"
	"github.com/KonvuInc/konvu-cli/skills"
)

// Both login paths pitch the same skills. They used to hold a byte-identical copy each, so a
// skill added to one was invisible to whoever used the other; one function is what keeps them iso.
func TestOneSkillPitchForBothLoginPaths(t *testing.T) {
	src, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatalf("read auth.go: %v", err)
	}
	if n := strings.Count(string(src), "offerSkills()"); n != 3 { // 1 definition + 2 call sites
		t.Errorf("offerSkills appears %d times, want 1 definition and 2 call sites", n)
	}
}

// Derived from the bundled inventory, never a list repeated here: repeating it would pass while a
// newly bundled skill went unmentioned, which is the regression this is meant to catch.
func TestEveryBundledSkillIsPitchedOrMarkedSupport(t *testing.T) {
	var pitched int
	for _, sd := range skills.SkillDirs() {
		switch {
		case sd.Support && len(sd.Pitch) > 0:
			t.Errorf("%s is marked support yet carries a pitch", sd.InstallName)
		case sd.Support:
			continue
		case len(sd.Pitch) == 0:
			t.Errorf("%s is bundled but says nothing in the offer, so it installs unannounced",
				sd.InstallName)
		default:
			pitched++
			if !strings.Contains(strings.Join(sd.Pitch, "\n"), sd.InstallName) {
				t.Errorf("%s's pitch never names how to run it", sd.InstallName)
			}
		}
	}
	if pitched == 0 {
		t.Fatal("no skill is pitched, so this asserts nothing")
	}
}

// companyServer answers /companies/current with status and body, and records the auth header it got.
func companyServer(t *testing.T, status int, body any) *string {
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/companies/current" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	invariantsEnv(t, server)
	t.Setenv("KONVU_ACCESS_TOKEN", "")
	return &auth
}

// A browser login Konvu then refuses must fail, not print "Login successful!", and must not save
// the refused token over whatever login was there.
func TestBrowserLoginFailsWhenTheCompanyCannotBeLoaded(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		auth := companyServer(t, status, map[string]any{"detail": "no company for this login"})

		err := finishOAuthLogin(map[string]any{"access_token": "new-token"})
		e := cliErrorOf(t, err)
		if e.ExitCode == 0 || !strings.Contains(e.Message, "could not load your company") || !strings.Contains(e.Message, "no company for this login") {
			t.Fatalf("%d: err = %#v", status, e)
		}
		if *auth != "Bearer new-token" {
			t.Fatalf("%d: the check did not use the new login: auth = %q", status, *auth)
		}
		if _, statErr := os.Stat(config.GetCredentialsPath()); !os.IsNotExist(statErr) {
			t.Fatalf("%d: credentials were saved for a refused login: %v", status, statErr)
		}
	}
}

func TestBrowserLoginWithoutATokenFailsBeforeAnyCall(t *testing.T) {
	auth := companyServer(t, http.StatusOK, map[string]any{"name": "Acme"})

	if e := cliErrorOf(t, finishOAuthLogin(map[string]any{})); e.ExitCode == 0 {
		t.Fatalf("err = %#v", e)
	}
	if *auth != "" {
		t.Fatalf("a call reached the server: auth = %q", *auth)
	}
}

func TestBrowserLoginSavesOnceTheCompanyLoads(t *testing.T) {
	companyServer(t, http.StatusOK, map[string]any{"name": "Acme"})

	if err := finishOAuthLogin(map[string]any{"access_token": "new-token", "refresh_token": "r"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(config.GetCredentialsPath())
	if err != nil {
		t.Fatalf("credentials not saved: %v", err)
	}
	var creds map[string]any
	if err := json.Unmarshal(data, &creds); err != nil || creds["access_token"] != "new-token" {
		t.Fatalf("creds = %s (%v)", data, err)
	}
}
