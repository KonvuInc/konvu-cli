package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
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
	t.Setenv(onboardingCodeEnv, "")
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
	testOnboardingCode = "onb_Ab3dEf6hIj9kLm2nOp5qRs8tUv1wXy4z"
	testOnboardingHint = "onb_Ab…"
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

func onboardingResult(result string) map[string]any {
	return map[string]any{"invariants": []any{map[string]any{
		"statement": "Tokens never reach logs.", "rank": nil, "result": result,
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
	if resp["invariants"].([]any)[0].(map[string]any)["result"] != "saved" {
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

func TestInvariantsPushOnboardingCodeFromFlagOrEnv(t *testing.T) {
	const envCode = "onb_EnvEnvEnvEnvEnvEnvEnvEnvEnvEnvEn"
	for name, tc := range map[string]struct {
		env  string
		args []string
		want string
	}{
		"env only":           {env: envCode, want: envCode},
		"flag only":          {args: []string{"--onboarding", testOnboardingCode}, want: testOnboardingCode},
		"flag wins over env": {env: envCode, args: []string{"--onboarding", testOnboardingCode}, want: testOnboardingCode},
		"env padded":         {env: "  " + envCode + "\n", want: envCode},
	} {
		got := onboardingServer(t, http.StatusOK, onboardingResult("saved"))
		t.Setenv(onboardingCodeEnv, tc.env)

		args := append([]string{"--file", writeInvariants(t, onboardingInvariants)}, tc.args...)
		if _, err := runInvariants(newInvariantsPushCmd(), args...); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.code != tc.want || len(got.auth) != 0 {
			t.Fatalf("%s: code = %q, auth = %q", name, got.code, got.auth)
		}
	}
}

func TestInvariantsPushWithoutOnboardingCodeStillLogsIn(t *testing.T) {
	var posted map[string]any
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != invariantsPath {
			t.Errorf("path = %q", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&posted)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(onboardingResult("created"))
	}))
	defer server.Close()
	invariantsEnv(t, server)
	t.Setenv(onboardingCodeEnv, "   ")

	if _, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants)); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer test-token" {
		t.Fatalf("auth = %q", auth)
	}
}

func TestInvariantsPushRefusesAMalformedOnboardingCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a malformed code reached the server: %s", r.URL.Path)
	}))
	defer server.Close()
	invariantsEnv(t, server)

	for _, tc := range []struct{ env, flag, source string }{
		{flag: "", source: "--onboarding"},
		{flag: "  ", source: "--onboarding"},
		{flag: "onb_short", source: "--onboarding"},
		{flag: testOnboardingCode + "\r\nX-Evil: 1", source: "--onboarding"},
		{env: "abc_Ab3dEf6hIj9kLm2nOp5qRs8tUv1wXy4z", source: onboardingCodeEnv},
	} {
		t.Setenv(onboardingCodeEnv, tc.env)
		args := []string{"--file", writeInvariants(t, onboardingInvariants)}
		if tc.env == "" {
			args = append(args, "--onboarding", tc.flag)
		}
		_, err := runInvariants(newInvariantsPushCmd(), args...)
		e := cliErrorOf(t, err)
		if e.Code != "INVALID_ONBOARDING_CODE" || e.ExitCode != clierrors.ExitUsageError || !strings.HasPrefix(e.Message, tc.source) {
			t.Fatalf("%+v: err = %#v", tc, e)
		}
		if strings.TrimSpace(tc.flag) != "" && strings.Contains(e.Message+e.Suggestion, strings.TrimSpace(tc.flag)) {
			t.Fatalf("%+v: the error echoes the code: %#v", tc, e)
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
		onboardingServer(t, status, map[string]any{"detail": "bad header " + testOnboardingCode})

		_, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants), "--onboarding", testOnboardingCode)
		e := cliErrorOf(t, err)
		if e.Code != want.code || e.ExitCode != want.exit || e.Retryable != want.retryable {
			t.Fatalf("%d: err = %#v", status, e)
		}
		if strings.Contains(e.Message+e.Suggestion, testOnboardingCode) {
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

func TestInvariantsPushOnboardingAccountFromEnvSaysToUnsetIt(t *testing.T) {
	onboardingServer(t, http.StatusConflict, map[string]any{"detail": "already has an account"})
	t.Setenv(onboardingCodeEnv, testOnboardingCode)

	_, err := runInvariants(newInvariantsPushCmd(), "--file", writeInvariants(t, onboardingInvariants))
	e := cliErrorOf(t, err)
	if e.Message != "this onboarding already has a Konvu account" || e.Suggestion != "Run 'konvu login', then unset KONVU_ONBOARDING_CODE and push again." {
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
