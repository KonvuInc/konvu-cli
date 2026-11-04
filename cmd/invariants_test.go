package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/skills"
	"github.com/spf13/cobra"
)

func invariantsServer(t *testing.T, posted *map[string]any, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != invariantsPath {
			t.Fatalf("path = %q", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode([]any{map[string]any{
				"rank": 1, "statement": "Tokens never reach logs.", "property": "confidentiality",
				"status": "validated", "controls": []any{map[string]any{}, map[string]any{}},
			}})
		case http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(posted); err != nil {
				t.Fatal(err)
			}
			if status != http.StatusCreated {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]any{"detail": "rank 2 is held by a validated invariant"})
				return
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"dry_run": (*posted)["dry_run"],
				"invariants": []any{map[string]any{
					"rank": 3, "result": "created", "statement": "Tokens never reach logs.",
				}},
			})
		default:
			t.Fatalf("method = %s", r.Method)
		}
	}))
}

func invariantsEnv(t *testing.T, server *httptest.Server) {
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "test-token")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
}

func runInvariants(c *cobra.Command, args ...string) (string, error) {
	out := &bytes.Buffer{}
	c.SetOut(out)
	c.SetArgs(args)
	c.SilenceUsage, c.SilenceErrors = true, true
	err := c.Execute()
	return out.String(), err
}

func writeInvariants(t *testing.T, body string) string {
	path := filepath.Join(t.TempDir(), "invariants.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInvariantsPushSendsTheFileAndDryRun(t *testing.T) {
	var posted map[string]any
	server := invariantsServer(t, &posted, http.StatusCreated)
	defer server.Close()
	invariantsEnv(t, server)

	file := writeInvariants(t, `{"invariants": [{"statement": "Tokens never reach logs.", "why_it_matters": "x", "property": "confidentiality"}]}`)
	out, err := runInvariants(newInvariantsPushCmd(), "--file", file, "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if posted["dry_run"] != true || len(posted["invariants"].([]any)) != 1 {
		t.Fatalf("posted = %#v", posted)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not JSON: %q", out)
	}
	if resp["invariants"].([]any)[0].(map[string]any)["result"] != "created" {
		t.Fatalf("resp = %#v", resp)
	}
}

func TestInvariantsPushAcceptsABareArray(t *testing.T) {
	var posted map[string]any
	server := invariantsServer(t, &posted, http.StatusCreated)
	defer server.Close()
	invariantsEnv(t, server)

	file := writeInvariants(t, `[{"statement": "A.", "why_it_matters": "x", "property": "integrity"}]`)
	if _, err := runInvariants(newInvariantsPushCmd(), "--file", file); err != nil {
		t.Fatal(err)
	}
	if posted["dry_run"] != false {
		t.Fatalf("posted = %#v", posted)
	}
}

func TestInvariantsPushExplainsATakenRank(t *testing.T) {
	var posted map[string]any
	server := invariantsServer(t, &posted, http.StatusConflict)
	defer server.Close()
	invariantsEnv(t, server)

	file := writeInvariants(t, `[{"statement": "A.", "why_it_matters": "x", "property": "integrity", "rank": 2}]`)
	_, err := runInvariants(newInvariantsPushCmd(), "--file", file)
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != "INVARIANT_RANK_UNAVAILABLE" {
		t.Fatalf("err = %#v", err)
	}
	if cliErr.Message != "rank 2 is held by a validated invariant" {
		t.Fatalf("message = %q", cliErr.Message)
	}
}

func TestInvariantsPushRefusesAnEmptyFile(t *testing.T) {
	file := writeInvariants(t, `[]`)
	_, err := runInvariants(newInvariantsPushCmd(), "--file", file)
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.ExitCode != clierrors.ExitUsageError {
		t.Fatalf("err = %#v", err)
	}
}

func TestInvariantsList(t *testing.T) {
	var posted map[string]any
	server := invariantsServer(t, &posted, http.StatusCreated)
	defer server.Close()
	invariantsEnv(t, server)

	out, err := runInvariants(newInvariantsListCmd(), "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || items[0]["statement"] != "Tokens never reach logs." {
		t.Fatalf("out = %q", out)
	}
}

func TestInvariantsListPrintsATable(t *testing.T) {
	var posted map[string]any
	server := invariantsServer(t, &posted, http.StatusCreated)
	defer server.Close()
	invariantsEnv(t, server)

	out, err := runInvariants(newInvariantsListCmd(), "--output", "table")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Tokens never reach logs.") || json.Valid([]byte(out)) {
		t.Fatalf("out = %q", out)
	}
}

func TestInvariantsRefuseAnUnsupportedOutput(t *testing.T) {
	_, err := runInvariants(newInvariantsListCmd(), "-o", "csv")
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.ExitCode != clierrors.ExitUsageError {
		t.Fatalf("err = %#v", err)
	}
}

func TestInvariantsPushServerFailureClaimsNoSave(t *testing.T) {
	var posted map[string]any
	server := invariantsServer(t, &posted, http.StatusBadGateway)
	defer server.Close()
	invariantsEnv(t, server)

	file := writeInvariants(t, `[{"statement": "A.", "why_it_matters": "x", "property": "integrity"}]`)
	_, err := runInvariants(newInvariantsPushCmd(), "--file", file)
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != "API_ERROR" || !cliErr.Retryable {
		t.Fatalf("err = %#v", err)
	}
	if strings.Contains(strings.ToLower(cliErr.Suggestion), "saved") {
		t.Fatalf("suggestion claims a save: %q", cliErr.Suggestion)
	}
}

func TestInvariantsPushNotFoundIsARepositoryOnlyWhenOneWasNamed(t *testing.T) {
	var posted map[string]any
	server := invariantsServer(t, &posted, http.StatusNotFound)
	defer server.Close()
	invariantsEnv(t, server)

	for body, want := range map[string]string{
		`[{"statement": "A.", "why_it_matters": "x", "property": "integrity"}]`:                        "API_ERROR",
		`[{"statement": "A.", "why_it_matters": "x", "property": "integrity", "repository_id": "r1"}]`: "REPOSITORY_NOT_FOUND",
	} {
		_, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, body))
		var cliErr *clierrors.CLIError
		if !errors.As(err, &cliErr) || cliErr.Code != want {
			t.Fatalf("%s: err = %#v, want %s", body, err, want)
		}
	}
}

const testGuide = "# A good security rule\n\nOne sentence about the product that must always be true."

// guideServer answers the guide route with status and body, and records the auth header it got.
func guideServer(t *testing.T, status int, body any) *string {
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != invariantsGuidePath {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	invariantsEnv(t, server)
	return &auth
}

func TestInvariantsGuidePrintsTheMarkdownEvenWhenPiped(t *testing.T) {
	auth := guideServer(t, http.StatusOK, map[string]any{"guide": testGuide, "version": "0123456789ab"})

	out, err := runInvariants(newInvariantsGuideCmd())
	if err != nil {
		t.Fatal(err)
	}
	if out != testGuide+"\n" {
		t.Fatalf("out = %q", out)
	}
	if *auth != "Bearer test-token" {
		t.Fatalf("auth = %q", *auth)
	}
}

func TestInvariantsGuideDropsTerminalControlCharacters(t *testing.T) {
	guideServer(t, http.StatusOK, map[string]any{"guide": "# Rule\r\n\x1b[2Jkeep\ttabs\n", "version": "0123456789ab"})

	out, err := runInvariants(newInvariantsGuideCmd(), "-o", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	if out != "# Rule\n[2Jkeep\ttabs\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestInvariantsGuideJSON(t *testing.T) {
	guideServer(t, http.StatusOK, map[string]any{"guide": testGuide, "version": "0123456789ab"})

	out, err := runInvariants(newInvariantsGuideCmd(), "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not JSON: %q", out)
	}
	if resp["guide"] != testGuide || resp["version"] != "0123456789ab" {
		t.Fatalf("resp = %#v", resp)
	}
}

// An expired login reads the bundled guide like no login at all: the guide holds no company data.
func TestInvariantsGuideWithAnExpiredLoginPrintsTheBundledGuide(t *testing.T) {
	guideServer(t, http.StatusUnauthorized, map[string]any{"detail": "token expired"})

	out, err := runInvariants(newInvariantsGuideCmd())
	if err != nil {
		t.Fatal(err)
	}
	assertBundledGuide(t, out)
}

func TestInvariantsGuideRefusedCaller(t *testing.T) {
	guideServer(t, http.StatusForbidden, map[string]any{"detail": "no company for this login"})

	_, err := runInvariants(newInvariantsGuideCmd())
	if e := cliErrorOf(t, err); e.ExitCode != clierrors.ExitAuthFailed || e.Message != "no company for this login" {
		t.Fatalf("err = %#v", e)
	}
}

// Writing a rule needs no account: without a login, the guide is the copy bundled with the CLI.
func TestInvariantsGuideWithoutALoginPrintsTheBundledGuide(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("a call reached the server without a token: %s", r.URL.Path)
	}))
	defer server.Close()
	invariantsEnv(t, server)
	t.Setenv("KONVU_ACCESS_TOKEN", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	c := newInvariantsGuideCmd()
	stderr := &bytes.Buffer{}
	c.SetErr(stderr)
	out, err := runInvariants(c)
	if err != nil {
		t.Fatal(err)
	}
	assertBundledGuide(t, out)
	if !strings.Contains(stderr.String(), "bundled with this CLI") {
		t.Fatalf("stderr = %q, want it to say the guide is the bundled copy", stderr.String())
	}

	out, err = runInvariants(newInvariantsGuideCmd(), "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not JSON: %q", out)
	}
	bundled, _ := skills.BundledGuide()
	sum := sha256.Sum256([]byte(bundled))
	if resp["guide"] != bundled || resp["version"] != hex.EncodeToString(sum[:])[:12] {
		t.Fatalf("resp = %#v", resp)
	}
}

func assertBundledGuide(t *testing.T, out string) {
	t.Helper()
	bundled, err := skills.BundledGuide()
	if err != nil {
		t.Fatal(err)
	}
	if out != guideText(bundled) || !strings.Contains(out, "## What a security rule is") {
		t.Fatalf("out = %q, want the bundled guide", out)
	}
}

func TestInvariantsGuideOnAServerWithoutIt(t *testing.T) {
	// 422 is what an older server answers: the path reaches its one-invariant route.
	for _, status := range []int{http.StatusNotFound, http.StatusUnprocessableEntity} {
		guideServer(t, status, map[string]any{"detail": "Not Found"})

		_, err := runInvariants(newInvariantsGuideCmd())
		e := cliErrorOf(t, err)
		if e.Code != "GUIDE_NOT_AVAILABLE" || e.ExitCode != clierrors.ExitGeneralError || !strings.Contains(e.Suggestion, "newer Konvu server") {
			t.Fatalf("%d: err = %#v", status, e)
		}
	}
}

func TestInvariantsGuideRefusesAnEmptyGuide(t *testing.T) {
	for _, body := range []any{map[string]any{"guide": "  \n", "version": "0123456789ab"}, map[string]any{"version": "0123456789ab"}} {
		guideServer(t, http.StatusOK, body)

		out, err := runInvariants(newInvariantsGuideCmd(), "-o", "json")
		if e := cliErrorOf(t, err); e.Code != "GUIDE_EMPTY" || e.ExitCode != clierrors.ExitGeneralError || out != "" {
			t.Fatalf("%#v: err = %#v, out = %q", body, e, out)
		}
	}
}

func TestInvariantsGuideRefusesAnUnsupportedOutput(t *testing.T) {
	_, err := runInvariants(newInvariantsGuideCmd(), "-o", "table")
	if e := cliErrorOf(t, err); e.ExitCode != clierrors.ExitUsageError {
		t.Fatalf("err = %#v", e)
	}
}
