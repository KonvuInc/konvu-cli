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

// Guardrails refuses fields it does not know, so a logged-in push drops repository, which only
// the onboarding route takes, and keeps everything else.
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
