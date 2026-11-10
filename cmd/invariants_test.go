package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
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

func TestInvariantsGuideAuthFailure(t *testing.T) {
	guideServer(t, http.StatusUnauthorized, map[string]any{"detail": "token expired"})

	_, err := runInvariants(newInvariantsGuideCmd())
	if e := cliErrorOf(t, err); e.ExitCode != clierrors.ExitAuthFailed || !strings.Contains(e.Suggestion, "konvu login") {
		t.Fatalf("err = %#v", e)
	}
}

func TestInvariantsGuideRefusedCaller(t *testing.T) {
	guideServer(t, http.StatusForbidden, map[string]any{"detail": "no company for this login"})

	_, err := runInvariants(newInvariantsGuideCmd())
	if e := cliErrorOf(t, err); e.ExitCode != clierrors.ExitAuthFailed || e.Message != "no company for this login" {
		t.Fatalf("err = %#v", e)
	}
}

func TestInvariantsGuideNeedsALogin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("a call reached the server without a token: %s", r.URL.Path)
	}))
	defer server.Close()
	invariantsEnv(t, server)
	t.Setenv("KONVU_ACCESS_TOKEN", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	_, err := runInvariants(newInvariantsGuideCmd())
	if e := cliErrorOf(t, err); e.ExitCode != clierrors.ExitAuthFailed || !strings.Contains(e.Suggestion, "konvu login") {
		t.Fatalf("err = %#v", e)
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

const (
	testOnboardingCode = "R4WN8HZT"
	testOnboardingHint = "R4…"
)

// onboardingRequest is what the onboarding route received.
type onboardingRequest struct {
	hits int
	auth []string
	code string
	body map[string]any
}

// onboardingServer answers the onboarding route with status and body, and records each request.
// Any call to another route fails the test: an onboarding push never touches the login routes.
func onboardingServer(t *testing.T, status int, body any) *onboardingRequest {
	got := &onboardingRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != onboardingRulesPath {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		got.hits++
		got.auth = r.Header.Values("Authorization")
		got.code = r.Header.Get(onboardingCodeHeader)
		if err := json.NewDecoder(r.Body).Decode(&got.body); err != nil {
			t.Errorf("body: %v", err)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	invariantsEnv(t, server)
	return got
}

// onboardingResult is what the onboarding route answers: the onboarding's company domain, and one
// result per invariant sent, in order.
func onboardingResult(results ...string) map[string]any {
	items := make([]any, 0, len(results))
	for _, result := range results {
		items = append(items, map[string]any{"result": result})
	}
	return map[string]any{"domain": "acme.com", "invariants": items}
}

// guardrailsResult is what the logged-in push route answers for one invariant.
func guardrailsResult(result string) map[string]any {
	return map[string]any{"dry_run": false, "invariants": []any{map[string]any{
		"statement": "Tokens never reach logs.", "rank": 1, "result": result,
	}}}
}

const onboardingInvariants = `{"invariants": [{"statement": "Tokens never reach logs.", "why_it_matters": "x", "property": "confidentiality"}]}`

func TestInvariantsPushWithOnboardingCodeSavesWithoutALogin(t *testing.T) {
	got := onboardingServer(t, http.StatusOK, onboardingResult("saved"))

	out, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", testOnboardingCode)
	if err != nil {
		t.Fatal(err)
	}
	if got.hits != 1 || got.code != testOnboardingCode {
		t.Fatalf("hits = %d, code = %q", got.hits, got.code)
	}
	// KONVU_ACCESS_TOKEN is set: the push must still not send it.
	if len(got.auth) != 0 {
		t.Fatalf("Authorization sent: %q", got.auth)
	}
	if got.body["dry_run"] != false || len(got.body["invariants"].([]any)) != 1 {
		t.Fatalf("body = %#v", got.body)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not JSON: %q", out)
	}
	// The route answers results only: the statement comes from the file.
	row := resp["invariants"].([]any)[0].(map[string]any)
	if row["result"] != "saved" || row["statement"] != "Tokens never reach logs." || row["rank"] != nil || resp["dry_run"] != false || resp["domain"] != "acme.com" {
		t.Fatalf("resp = %#v", resp)
	}
}

func TestInvariantsPushWithOnboardingCodeDryRun(t *testing.T) {
	got := onboardingServer(t, http.StatusOK, onboardingResult("valid"))

	out, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", testOnboardingCode, "--dry-run", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if got.body["dry_run"] != true {
		t.Fatalf("body = %#v", got.body)
	}
	if !strings.Contains(out, "valid") || !strings.Contains(out, "Tokens never reach logs.") {
		t.Fatalf("out = %q", out)
	}
	if !strings.HasPrefix(out, "Saves to the Konvu onboarding for acme.com\n") {
		t.Fatalf("the dry run does not say where the rules go: %q", out)
	}
	// The rank is null until the account exists: it must not print as rank 0.
	if strings.Contains(out, "0 ") {
		t.Fatalf("a null rank printed as 0: %q", out)
	}
}

func TestInvariantsPushWithOnboardingCodeWorksLoggedOut(t *testing.T) {
	got := onboardingServer(t, http.StatusOK, onboardingResult("saved"))
	t.Setenv("KONVU_ACCESS_TOKEN", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	if _, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", testOnboardingCode); err != nil {
		t.Fatal(err)
	}
	if got.hits != 1 || len(got.auth) != 0 {
		t.Fatalf("hits = %d, auth = %q", got.hits, got.auth)
	}
}

// The code only ever comes from --onboarding: a KONVU_ONBOARDING_CODE left in the environment
// (after a 409, say) must never turn a logged-in push into an onboarding one.
func TestInvariantsPushIgnoresAnOnboardingCodeInTheEnvironment(t *testing.T) {
	var path, auth, code string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth, code = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get(onboardingCodeHeader)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(guardrailsResult("created"))
	}))
	defer server.Close()
	invariantsEnv(t, server)
	t.Setenv("KONVU_ONBOARDING_CODE", testOnboardingCode)

	if _, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants)); err != nil {
		t.Fatal(err)
	}
	if path != invariantsPath || auth != "Bearer test-token" || code != "" {
		t.Fatalf("path = %q, auth = %q, code = %q", path, auth, code)
	}
}

// Konvu reads a code in any case, with spaces and dashes ignored: the header carries it normalized.
func TestInvariantsPushNormalizesTheOnboardingCode(t *testing.T) {
	for _, given := range []string{"R4WN8HZT", "r4wn8hzt", "R4WN-8HZT", "r4wn-8hzt", " r4wn 8hzt\n", "R4-WN-8H-ZT"} {
		got := onboardingServer(t, http.StatusOK, onboardingResult("saved"))

		if _, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", given); err != nil {
			t.Fatalf("%q: %v", given, err)
		}
		if got.code != testOnboardingCode {
			t.Fatalf("%q: header = %q", given, got.code)
		}
	}
}

func TestInvariantsPushRefusesAMalformedOnboardingCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a malformed code reached the server: %s", r.URL.Path)
	}))
	defer server.Close()
	invariantsEnv(t, server)

	for _, flag := range []string{
		"", "  ", "-",
		"R4WN8HZ", "R4WN8HZTX", // 7 and 9 characters
		"R4WN8HZ0", "R4WN8HZO", "R4WN8HZ1", "R4WN8HZI", "R4WN8HZL", "r4wn8hzl", // look-alikes Konvu never issues
		"R4WN_8HZT", "R4WN.8HZT", "R4WN8HZſ", // ſ uppercases to S outside ASCII
		"onb_Ab3dEf6hIj9kLm2nOp5qRs8tUv1wXy4z", // the old format
		testOnboardingCode + "\r\nX-Evil: 1",
	} {
		_, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", flag)
		e := cliErrorOf(t, err)
		if e.Code != "INVALID_ONBOARDING_CODE" || e.ExitCode != clierrors.ExitUsageError {
			t.Fatalf("%q: err = %#v", flag, e)
		}
		if len(strings.TrimSpace(flag)) >= 7 && strings.Contains(e.Message+e.Suggestion, strings.TrimSpace(flag)) {
			t.Fatalf("%q: the error echoes the code: %#v", flag, e)
		}
	}
}

func TestInvariantsPushOnboardingErrors(t *testing.T) {
	for status, want := range map[int]struct {
		code      string
		exit      int
		retryable bool
		login     bool
	}{
		http.StatusNotFound:            {"ONBOARDING_CODE_NOT_FOUND", clierrors.ExitNotFound, false, true},
		http.StatusConflict:            {"ONBOARDING_HAS_ACCOUNT", clierrors.ExitAuthFailed, false, true},
		http.StatusUnprocessableEntity: {"INVALID_INVARIANTS", clierrors.ExitUsageError, false, false},
		http.StatusTooManyRequests:     {"RATE_LIMITED", clierrors.ExitGeneralError, true, false},
		http.StatusUnauthorized:        {"ONBOARDING_CODE_REFUSED", clierrors.ExitAuthFailed, false, true},
		http.StatusBadGateway:          {"API_ERROR", clierrors.ExitGeneralError, true, false},
	} {
		// The server echoes the code, as a FastAPI validation error does with a header's input.
		onboardingServer(t, status, map[string]any{"detail": "bad header " + testOnboardingCode + " (r4wn-8hzt)"})

		_, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", "r4wn-8hzt")
		e := cliErrorOf(t, err)
		if e.Code != want.code || e.ExitCode != want.exit || e.Retryable != want.retryable {
			t.Fatalf("%d: err = %#v", status, e)
		}
		if strings.Contains(e.Message+e.Suggestion, testOnboardingCode) || strings.Contains(e.Message+e.Suggestion, "r4wn-8hzt") {
			t.Fatalf("%d: the error carries the whole code: %#v", status, e)
		}
		if login := strings.Contains(e.Suggestion, "Run 'konvu login', then push again without --onboarding.") ||
			strings.Contains(e.Suggestion, "run 'konvu login', then push again without --onboarding."); login != want.login {
			t.Fatalf("%d: suggestion = %q", status, e.Suggestion)
		}
	}
}

func TestInvariantsPushOnboardingErrorsNameTheCode(t *testing.T) {
	onboardingServer(t, http.StatusNotFound, map[string]any{"detail": "Not Found"})

	_, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", testOnboardingCode)
	if e := cliErrorOf(t, err); !strings.Contains(e.Message, testOnboardingHint) || !strings.Contains(e.Suggestion, "/start") {
		t.Fatalf("err = %#v", e)
	}
}

func TestInvariantsPushOnboardingWithAnAccountSaysToLogIn(t *testing.T) {
	onboardingServer(t, http.StatusConflict, map[string]any{"detail": "already has an account"})

	_, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", testOnboardingCode)
	e := cliErrorOf(t, err)
	if e.Message != "this onboarding already has a Konvu account" || e.Suggestion != "Run 'konvu login', then push again without --onboarding." {
		t.Fatalf("err = %#v", e)
	}
}

// konvu-upload-invariants tells the errors apart by these messages, so they must stay in sync.
func TestUploadSkillKnowsTheOnboardingErrors(t *testing.T) {
	skill, err := os.ReadFile(filepath.Join("..", "skills", "konvu-upload-invariants", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for status, phrase := range map[int]string{
		http.StatusConflict: "this onboarding already has a Konvu account",
		http.StatusNotFound: "unknown or has expired",
	} {
		onboardingServer(t, status, map[string]any{"detail": "x"})
		_, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", testOnboardingCode)
		if e := cliErrorOf(t, err); !strings.Contains(e.Message, phrase) {
			t.Errorf("%d: message %q does not say %q", status, e.Message, phrase)
		}
		if !strings.Contains(string(skill), `"`+phrase+`"`) {
			t.Errorf("konvu-upload-invariants does not handle %q", phrase)
		}
	}
}

func TestInvariantsPushOnboardingPairsResultsWithTheFile(t *testing.T) {
	onboardingServer(t, http.StatusOK, onboardingResult("saved", "unchanged"))
	file := writeInvariants(t, `[
		{"statement": "  Tokens never reach logs. ", "why_it_matters": "x", "property": "confidentiality", "rank": 2},
		{"statement": "Only admins delete projects.", "why_it_matters": "y", "property": "authorization"}
	]`)

	out, err := runInvariants(newInvariantsPushCmd(), "--file", file, "--onboarding", testOnboardingCode, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Invariants []map[string]any `json:"invariants"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not JSON: %q", out)
	}
	if len(resp.Invariants) != 2 ||
		resp.Invariants[0]["statement"] != "Tokens never reach logs." || resp.Invariants[0]["result"] != "saved" || resp.Invariants[0]["rank"] != float64(2) ||
		resp.Invariants[1]["statement"] != "Only admins delete projects." || resp.Invariants[1]["result"] != "unchanged" {
		t.Fatalf("resp = %#v", resp.Invariants)
	}

	table, err := runInvariants(newInvariantsPushCmd(), "--file", file, "--onboarding", testOnboardingCode, "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table, "Only admins delete projects.") || !strings.Contains(table, "unchanged") {
		t.Fatalf("table = %q", table)
	}
}

func TestInvariantsPushOnboardingRefusesResultsThatDoNotLineUp(t *testing.T) {
	file := writeInvariants(t, `[
		{"statement": "A.", "why_it_matters": "x", "property": "integrity"},
		{"statement": "B.", "why_it_matters": "x", "property": "integrity"}
	]`)
	for _, answer := range []map[string]any{
		onboardingResult("saved"),
		onboardingResult("saved", "saved", "saved"),
		onboardingResult("saved", ""),
		{"invariants": []any{map[string]any{"result": "saved"}, "saved"}},
	} {
		onboardingServer(t, http.StatusOK, answer)

		out, err := runInvariants(newInvariantsPushCmd(), "--file", file, "--onboarding", testOnboardingCode)
		if e := cliErrorOf(t, err); e.Code != "API_ERROR" || !e.Retryable || out != "" {
			t.Fatalf("%#v: err = %#v, out = %q", answer, e, out)
		}
	}
}

// Konvu stores an onboarding push unread, so the CLI refuses what Guardrails would refuse after
// sign-up, and sends nothing.
func TestInvariantsPushOnboardingChecksTheInvariantsFirst(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("invalid invariants reached the server: %s", r.URL.Path)
	}))
	defer server.Close()
	invariantsEnv(t, server)

	long := func(n int) string { return strings.Repeat("a", n) }
	rule := func(extra string) string {
		return `[{"statement": "A.", "why_it_matters": "x", "property": "integrity"` + extra + `}]`
	}
	assets := func(n int) string {
		items := make([]string, n)
		for i := range items {
			items[i] = `{"kind": "credential", "name": "token"}`
		}
		return `, "protected_assets": [` + strings.Join(items, ", ") + `]`
	}
	ten := make([]string, 11)
	for i := range ten {
		ten[i] = fmt.Sprintf(`{"statement": "Rule %d.", "why_it_matters": "x", "property": "integrity"}`, i)
	}
	for body, want := range map[string]string{
		`[{"why_it_matters": "x", "property": "integrity"}]`:                                    "invariant 1: statement is required",
		`[{"statement": "  ", "why_it_matters": "x", "property": "integrity"}]`:                 "invariant 1: statement is empty",
		`[{"statement": 3, "why_it_matters": "x", "property": "integrity"}]`:                    "invariant 1: statement must be text",
		`[{"statement": "` + long(201) + `", "why_it_matters": "x", "property": "integrity"}]`:  "statement is longer than 200 characters",
		`[{"statement": "A.", "property": "integrity"}]`:                                        "invariant 1: why_it_matters is required",
		`[{"statement": "A.", "why_it_matters": "` + long(501) + `", "property": "integrity"}]`: "why_it_matters is longer than 500 characters",
		`[{"statement": "A.", "why_it_matters": "x\u0000", "property": "integrity"}]`:           "why_it_matters must not contain NUL",
		`[{"statement": "A.", "why_it_matters": "x"}]`:                                          "property must be one of",
		`[{"statement": "A.", "why_it_matters": "x", "property": "Integrity"}]`:                 "property must be one of",
		rule(`, "rank": 0`):                     "rank must be a whole number from 1 to 32767",
		rule(`, "rank": 1.5`):                   "rank must be a whole number",
		rule(`, "rank": "2"`):                   "rank must be a whole number",
		rule(`, "rank": 32768`):                 "rank must be a whole number",
		rule(assets(21)):                        "protected_assets has 21 items, at most 20",
		rule(`, "protected_assets": {}`):        "protected_assets must be a list",
		rule(`, "protected_assets": null`):      "protected_assets must be a list",
		rule(`, "protected_assets": ["token"]`): `protected_assets[0] must be {"kind", "name"}`,
		rule(`, "protected_assets": [{"kind": "` + long(65) + `", "name": "t"}]`):  "protected_assets[0]: kind is longer than 64 characters",
		rule(`, "protected_assets": [{"kind": "k", "name": "` + long(201) + `"}]`): "protected_assets[0]: name is longer than 200 characters",
		rule(`, "protected_assets": [{"kind": "k"}]`):                              "protected_assets[0]: name is required",
		rule(`, "protected_assets": [{"kind": "k", "name": "t", "why": "w"}]`):     `protected_assets[0]: unknown field "why"`,
		rule(`, "repository_id": "6f1c0a6e-0000-4000-8000-000000000000"`):          "repository_id cannot be used with --onboarding",
		rule(`, "repository": "api"`):                                              "repository must be the repository's name as owner/name",
		rule(`, "repository": "https://github.com/acme/api"`):                      "repository must be the repository's name as owner/name",
		rule(`, "repository": null`):                                               "repository must be the repository's name as owner/name",
		rule(`, "evidence": ["a"]`):                                                `invariant 1: unknown field "evidence"`,
		`[` + strings.Join(ten, ", ") + `]`:                                        "11 invariants, at most 10 per push",
		`[{"statement": "A  b.", "why_it_matters": "x", "property": "integrity"}, {"statement": "a B.", "why_it_matters": "x", "property": "integrity"}]`:                  "invariant 2: same statement as invariant 1",
		`[{"statement": "A.", "why_it_matters": "x", "property": "integrity", "rank": 1}, {"statement": "B.", "why_it_matters": "x", "property": "integrity", "rank": 1}]`: "invariant 2: same rank as invariant 1",
		`["A."]`: "invariant 1 is not an object",
	} {
		_, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, body), "--onboarding", testOnboardingCode)
		e := cliErrorOf(t, err)
		if e.Code != "INVALID_INVARIANTS" || e.ExitCode != clierrors.ExitUsageError || !strings.Contains(e.Message, want) {
			t.Errorf("%.80s: err = %#v, want %q", body, e, want)
		}
	}
}

func TestInvariantsPushOnboardingSendsTheRepositoryAsIs(t *testing.T) {
	got := onboardingServer(t, http.StatusOK, onboardingResult("saved"))
	file := writeInvariants(t, `[{"statement": "A.", "why_it_matters": "x", "property": "integrity", "rank": 1, "repository": "acme/api",
		"protected_assets": [{"kind": "credential", "name": "Claude login token"}]}]`)

	if _, err := runInvariants(newInvariantsPushCmd(), "--file", file, "--onboarding", testOnboardingCode); err != nil {
		t.Fatal(err)
	}
	sent := got.body["invariants"].([]any)[0].(map[string]any)
	if sent["repository"] != "acme/api" || sent["rank"] != float64(1) || len(sent["protected_assets"].([]any)) != 1 {
		t.Fatalf("sent = %#v", sent)
	}
}

// An explicit repository_id wins: a logged-in push drops repository, which Guardrails refuses,
// looks nothing up (invariantsServer fails on any other route) and keeps everything else.
func TestInvariantsPushWithoutOnboardingDropsTheRepository(t *testing.T) {
	var posted map[string]any
	server := invariantsServer(t, &posted, http.StatusCreated)
	defer server.Close()
	invariantsEnv(t, server)

	file := writeInvariants(t, `[{"statement": "A.", "why_it_matters": "x", "property": "integrity", "repository": "acme/api", "repository_id": "r1"}]`)
	if _, err := runInvariants(newInvariantsPushCmd(), "--file", file); err != nil {
		t.Fatal(err)
	}
	sent := posted["invariants"].([]any)[0].(map[string]any)
	if _, has := sent["repository"]; has || sent["repository_id"] != "r1" || sent["statement"] != "A." {
		t.Fatalf("sent = %#v", sent)
	}
}

// The onboarding code must never reach another server: push --onboarding follows no redirect,
// which Go would otherwise follow with the code header, and with the body on a 307 or 308.
func TestInvariantsPushOnboardingFollowsNoRedirect(t *testing.T) {
	var elsewhere int
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere++
		t.Errorf("the redirect was followed: code %q", r.Header.Get(onboardingCodeHeader))
	}))
	defer other.Close()

	for _, status := range []int{
		http.StatusMultipleChoices, http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusNotModified, http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", other.URL+r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(onboardingResult("saved"))
		}))
		invariantsEnv(t, server)

		out, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", testOnboardingCode)
		server.Close()
		e := cliErrorOf(t, err)
		if e.Code != "REDIRECT_REFUSED" || e.Retryable || e.ExitCode != clierrors.ExitGeneralError || out != "" {
			t.Fatalf("%d: err = %#v, out = %q", status, e, out)
		}
		if strings.Contains(e.Message+e.Suggestion, testOnboardingCode) {
			t.Fatalf("%d: the error carries the code: %#v", status, e)
		}
	}
	if elsewhere != 0 {
		t.Fatalf("the other server got %d requests", elsewhere)
	}
}

// The dry run is how the user checks the code is theirs: without a company domain, or with text
// that is not one, it refuses to go on. A save already made still reports its results.
func TestInvariantsPushOnboardingDryRunNeedsTheDomain(t *testing.T) {
	for _, domain := range []any{nil, "", "acme", "acme.com (yours)", "acme.com\x1b[2J", 3} {
		answer := onboardingResult("valid")
		answer["domain"] = domain
		onboardingServer(t, http.StatusOK, answer)

		out, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", testOnboardingCode, "--dry-run")
		if e := cliErrorOf(t, err); e.Code != "ONBOARDING_DOMAIN_MISSING" || e.Retryable || out != "" {
			t.Fatalf("%#v: err = %#v, out = %q", domain, e, out)
		}
	}

	answer := onboardingResult("saved")
	delete(answer, "domain")
	onboardingServer(t, http.StatusOK, answer)
	out, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", testOnboardingCode, "-o", "table")
	if err != nil || strings.Contains(out, "Konvu onboarding for") || !strings.Contains(out, "saved") {
		t.Fatalf("err = %v, out = %q", err, out)
	}
}

func TestInvariantsPushOnboardingShowsTheDomainAndRepository(t *testing.T) {
	answer := onboardingResult("valid")
	answer["domain"] = " Acme.COM "
	onboardingServer(t, http.StatusOK, answer)
	file := writeInvariants(t, `[{"statement": "A.", "why_it_matters": "x", "property": "integrity", "repository": "acme/api"}]`)

	out, err := runInvariants(newInvariantsPushCmd(), "--file", file, "--onboarding", testOnboardingCode, "--dry-run", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not JSON: %q", out)
	}
	row := resp["invariants"].([]any)[0].(map[string]any)
	if resp["domain"] != "acme.com" || resp["dry_run"] != true || row["repository"] != "acme/api" || row["result"] != "valid" {
		t.Fatalf("resp = %#v", resp)
	}

	table, err := runInvariants(newInvariantsPushCmd(), "--file", file, "--onboarding", testOnboardingCode, "--dry-run", "-o", "table")
	if err != nil || !strings.HasPrefix(table, "Saves to the Konvu onboarding for acme.com\n") || !strings.Contains(table, "acme/api") {
		t.Fatalf("err = %v, table = %q", err, table)
	}
}

const (
	apiRepoID   = "6f1c0a6e-0000-4000-8000-000000000001"
	webRepoID   = "6f1c0a6e-0000-4000-8000-000000000002"
	otherRepoID = "6f1c0a6e-0000-4000-8000-000000000003"
	givenRepoID = "6f1c0a6e-0000-4000-8000-000000000004"
)

// pushRequests is what a logged-in push sent: how many repository lookups, and each body posted.
type pushRequests struct {
	lookups int
	posted  []map[string]any
}

// sent is invariant i of the nth body posted.
func (p *pushRequests) sent(t *testing.T, n, i int) map[string]any {
	t.Helper()
	if n >= len(p.posted) {
		t.Fatalf("%d pushes, want push %d", len(p.posted), n+1)
	}
	return p.posted[n]["invariants"].([]any)[i].(map[string]any)
}

// pushServer answers the repository lookup with lookupStatus and lookup, and each push with the
// next of statuses (201 once they run out). Any other call fails the test.
func pushServer(t *testing.T, lookupStatus int, lookup any, statuses ...int) *pushRequests {
	got := &pushRequests{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == coverageConfigPath:
			got.lookups++
			w.WriteHeader(lookupStatus)
			_ = json.NewEncoder(w).Encode(lookup)
		case r.Method == http.MethodPost && r.URL.Path == invariantsPath:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("body: %v", err)
			}
			got.posted = append(got.posted, body)
			status := http.StatusCreated
			if n := len(got.posted); n <= len(statuses) {
				status = statuses[n-1]
			}
			w.WriteHeader(status)
			if status != http.StatusCreated {
				_ = json.NewEncoder(w).Encode(map[string]any{"detail": "no such repository for this company"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"dry_run": body["dry_run"], "invariants": []any{
				map[string]any{"rank": 1, "result": "created", "statement": "A."},
			}})
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(server.Close)
	invariantsEnv(t, server)
	return got
}

// konvuRepositories is the repository lookup's answer, one {id, url} per pair.
func konvuRepositories(idsAndURLs ...string) map[string]any {
	repos := []any{}
	for i := 0; i+1 < len(idsAndURLs); i += 2 {
		repos = append(repos, map[string]any{"id": idsAndURLs[i], "url": idsAndURLs[i+1]})
	}
	return map[string]any{"repositories": repos, "default_severities": nil}
}

// runPush runs a push and returns its stdout and stderr apart.
func runPush(args ...string) (string, string, error) {
	c := newInvariantsPushCmd()
	stderr := &bytes.Buffer{}
	c.SetErr(stderr)
	stdout, err := runInvariants(c, args...)
	return stdout, stderr.String(), err
}

// rulesFor is a push file with one invariant per repository field given, as raw JSON.
func rulesFor(repositories ...string) string {
	rules := make([]string, len(repositories))
	for i, repository := range repositories {
		rules[i] = fmt.Sprintf(`{"statement": "Rule %d.", "why_it_matters": "x", "property": "integrity", "repository": %s}`, i+1, repository)
	}
	return "[" + strings.Join(rules, ", ") + "]"
}

func assertJSON(t *testing.T, stdout string) {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("stdout is not JSON: %q", stdout)
	}
}

// Logged in, a rule's repository (owner/name) goes as the id of the Konvu repository of that
// name, matched exactly and whatever its case, looked up once per push, dry run included.
func TestInvariantsPushLinksTheRepositoryLoggedIn(t *testing.T) {
	file := writeInvariants(t, rulesFor(`"acme/api"`, `" ACME/team/Web "`, `"Acme/API"`))
	for _, dryRun := range []bool{false, true} {
		got := pushServer(t, http.StatusOK, konvuRepositories(
			otherRepoID, "github:acme/api-legacy",
			apiRepoID, "github:Acme/API",
			webRepoID, "https://gitlab.com/acme/team/web.git",
		))
		args := []string{"--file", file, "-o", "json"}
		if dryRun {
			args = append(args, "--dry-run")
		}
		stdout, stderr, err := runPush(args...)
		if err != nil {
			t.Fatal(err)
		}
		if got.lookups != 1 || len(got.posted) != 1 || got.posted[0]["dry_run"] != dryRun {
			t.Fatalf("dry run %v: lookups = %d, posted = %#v", dryRun, got.lookups, got.posted)
		}
		for i, want := range []string{apiRepoID, webRepoID, apiRepoID} {
			sent := got.sent(t, 0, i)
			if _, has := sent["repository"]; has || sent["repository_id"] != want {
				t.Errorf("dry run %v: invariant %d sent %#v, want repository_id %s", dryRun, i+1, sent, want)
			}
		}
		if stderr != "" {
			t.Errorf("dry run %v: stderr = %q", dryRun, stderr)
		}
		assertJSON(t, stdout)
	}
}

// A name that matches no repository, or several, or a failed lookup sends the rule without a
// repository, as before: one warning per name on stderr, and the push still succeeds.
func TestInvariantsPushWarnsWhenTheRepositoryIsNotFound(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		lookup any
		want   []string
	}{
		{"no match, not even a longer name", http.StatusOK, konvuRepositories(apiRepoID, "github:acme/api-legacy", webRepoID, "github:other/acme/api"),
			[]string{"none of your Konvu repositories is acme/api"}},
		{"ambiguous", http.StatusOK, konvuRepositories(webRepoID, "gitlab:ACME/api", apiRepoID, "github:acme/api"),
			[]string{"2 Konvu repositories are acme/api (github:acme/api (id " + apiRepoID + "), gitlab:ACME/api (id " + webRepoID + "))", "set repository_id"}},
		{"unusable entries", http.StatusOK, konvuRepositories("", "github:acme/api", "r1", "github:acme/api", apiRepoID, "acme/api", webRepoID, "github:"),
			[]string{"none of your Konvu repositories is acme/api"}},
		{"lookup fails", http.StatusInternalServerError, map[string]any{"detail": "boom"},
			[]string{"could not look up Konvu repository acme/api (Konvu answered 500)"}},
		{"no repository list", http.StatusOK, map[string]any{},
			[]string{"could not look up Konvu repository acme/api (Konvu sent no repository list)"}},
	} {
		got := pushServer(t, c.status, c.lookup)
		stdout, stderr, err := runPush("--file", writeInvariants(t, rulesFor(`"acme/api"`, `"Acme/API"`)), "-o", "json")
		if err != nil {
			t.Fatalf("%s: the push failed: %v", c.name, err)
		}
		if got.lookups != 1 || len(got.posted) != 1 {
			t.Fatalf("%s: lookups = %d, pushes = %d", c.name, got.lookups, len(got.posted))
		}
		for i := range 2 {
			sent := got.sent(t, 0, i)
			_, hasName := sent["repository"]
			if _, hasID := sent["repository_id"]; hasName || hasID {
				t.Errorf("%s: invariant %d sent %#v", c.name, i+1, sent)
			}
		}
		if strings.Count(stderr, "Warning: ") != 1 || !strings.Contains(stderr, "so its invariants go without a repository") {
			t.Errorf("%s: want one warning, stderr = %q", c.name, stderr)
		}
		for _, want := range c.want {
			if !strings.Contains(stderr, want) {
				t.Errorf("%s: stderr %q does not say %q", c.name, stderr, want)
			}
		}
		assertJSON(t, stdout)
	}
}

// A repository that is not owner/name is never looked up: the rule goes without one, with a
// warning naming the invariant, and a null repository names none.
func TestInvariantsPushWarnsOnAMalformedRepository(t *testing.T) {
	got := pushServer(t, http.StatusOK, konvuRepositories(apiRepoID, "github:acme/api"))
	stdout, stderr, err := runPush("--file", writeInvariants(t, rulesFor(`"api"`, `"https://github.com/acme/api"`, `""`, `3`, `null`)), "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if got.lookups != 0 {
		t.Fatalf("looked up %d times", got.lookups)
	}
	for i := range 5 {
		sent := got.sent(t, 0, i)
		_, hasName := sent["repository"]
		if _, hasID := sent["repository_id"]; hasName || hasID {
			t.Errorf("invariant %d sent %#v", i+1, sent)
		}
	}
	for n := 1; n <= 4; n++ {
		if !strings.Contains(stderr, fmt.Sprintf("Warning: invariant %d's repository is not owner/name (like acme/api), so it goes without a repository.\n", n)) {
			t.Errorf("no warning for invariant %d: %q", n, stderr)
		}
	}
	if strings.Count(stderr, "Warning: ") != 4 {
		t.Errorf("stderr = %q", stderr)
	}
	assertJSON(t, stdout)
}

// An explicit repository_id is sent untouched, even beside a repository naming another one.
func TestInvariantsPushKeepsAnExplicitRepositoryID(t *testing.T) {
	got := pushServer(t, http.StatusOK, konvuRepositories(apiRepoID, "github:acme/api", otherRepoID, "github:acme/other"))
	file := writeInvariants(t, `[
		{"statement": "A.", "why_it_matters": "x", "property": "integrity", "repository": "acme/other", "repository_id": "`+givenRepoID+`"},
		{"statement": "B.", "why_it_matters": "x", "property": "integrity", "repository": "acme/api"}]`)
	if _, stderr, err := runPush("--file", file); err != nil || stderr != "" {
		t.Fatalf("err = %v, stderr = %q", err, stderr)
	}
	first, second := got.sent(t, 0, 0), got.sent(t, 0, 1)
	if _, has := first["repository"]; has || first["repository_id"] != givenRepoID {
		t.Errorf("explicit invariant sent %#v", first)
	}
	if second["repository_id"] != apiRepoID {
		t.Errorf("looked-up invariant sent %#v", second)
	}
}

// Guardrails answers 404 for a repository it has not mapped yet, before writing anything: the
// push goes once more without the ids it looked up, keeping the explicit ones, and warns.
func TestInvariantsPushRetriesWithoutAnUnmappedRepository(t *testing.T) {
	file := writeInvariants(t, `[
		{"statement": "A.", "why_it_matters": "x", "property": "integrity", "repository": "acme/api"},
		{"statement": "B.", "why_it_matters": "x", "property": "integrity", "repository_id": "`+givenRepoID+`"}]`)
	lookup := konvuRepositories(apiRepoID, "github:acme/api")

	got := pushServer(t, http.StatusOK, lookup, http.StatusNotFound)
	stdout, stderr, err := runPush("--file", file, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.posted) != 2 || got.sent(t, 0, 0)["repository_id"] != apiRepoID {
		t.Fatalf("posted = %#v", got.posted)
	}
	if _, has := got.sent(t, 1, 0)["repository_id"]; has || got.sent(t, 1, 1)["repository_id"] != givenRepoID {
		t.Fatalf("retry sent %#v", got.posted[1])
	}
	if stderr != "Warning: Konvu has not mapped repository acme/api yet, so its invariants go without a repository.\n" {
		t.Errorf("stderr = %q", stderr)
	}
	assertJSON(t, stdout)

	// The explicit id is the unknown one: one retry only, then the usual exit 3, and no warning.
	got = pushServer(t, http.StatusOK, lookup, http.StatusNotFound, http.StatusNotFound)
	_, stderr, err = runPush("--file", file)
	if e := cliErrorOf(t, err); e.Code != "REPOSITORY_NOT_FOUND" || e.ExitCode != clierrors.ExitNotFound || len(got.posted) != 2 || stderr != "" {
		t.Fatalf("err = %#v, pushes = %d, stderr = %q", e, len(got.posted), stderr)
	}

	// Nothing looked up: no retry.
	got = pushServer(t, http.StatusOK, lookup, http.StatusNotFound)
	_, _, err = runPush("--file", writeInvariants(t, `[{"statement": "B.", "why_it_matters": "x", "property": "integrity", "repository_id": "`+givenRepoID+`"}]`))
	if e := cliErrorOf(t, err); e.Code != "REPOSITORY_NOT_FOUND" || len(got.posted) != 1 || got.lookups != 0 {
		t.Fatalf("err = %#v, pushes = %d, lookups = %d", e, len(got.posted), got.lookups)
	}
}

// A stalled lookup gives up after its timeout, so it never holds the push.
func TestKonvuRepositoriesLookupIsBounded(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(server.Close)
	defer close(release)
	invariantsEnv(t, server)

	_, err := konvuRepositoriesByName(api.NewClient("", ""), 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "no answer within") {
		t.Fatalf("err = %v", err)
	}
}

func TestKonvuRepositoryName(t *testing.T) {
	for repoURL, want := range map[string][2]string{
		"github:tolmohq/repo-1":                      {"tolmohq/repo-1", "github:tolmohq/repo-1"},
		"GitHub:Acme/API.git":                        {"Acme/API", "github:Acme/API"},
		"gitlab:acme/team/api":                       {"acme/team/api", "gitlab:acme/team/api"},
		"https://gitlab.example.com/acme/api.git/":   {"acme/api", "gitlab.example.com/acme/api"},
		"https://user:secret@bitbucket.org/acme/api": {"acme/api", "bitbucket.org/acme/api"},
		"ssh://git@github.com/acme/api.git":          {"acme/api", "github.com/acme/api"},
		"https://github.com/acme%2Fapi":              {"", ""},
		"git@github.com:acme/api.git":                {"", ""},
		"github:":                                    {"", ""},
		"github:acme":                                {"", ""},
		"https://github.com/acme":                    {"", ""},
		"https://github.com":                         {"", ""},
		"acme/api":                                   {"", ""},
		"":                                           {"", ""},
	} {
		name, label := konvuRepositoryName(repoURL)
		if name != want[0] || label != want[1] {
			t.Errorf("%q: got (%q, %q), want %q", repoURL, name, label, want)
		}
	}
}
