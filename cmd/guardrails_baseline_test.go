package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

func TestGuardrailsBaselineRegistered(t *testing.T) {
	found := false
	for _, c := range guardrailsCmd.Commands() {
		if c.Name() == "baseline" {
			found = true
		}
	}
	if !found {
		t.Error("guardrails missing subcommand: baseline")
	}
}

func TestGuardrailsBaselineFlags(t *testing.T) {
	for _, flag := range []string{"policy", "branch", "repo", "timeout"} {
		if guardrailsBaselineCmd.Flags().Lookup(flag) == nil {
			t.Errorf("guardrails baseline missing flag: --%s", flag)
		}
	}
}

// The gateway path is what makes this reuse the normal login; a change here silently
// sends every guardrails call somewhere else.
func TestGuardrailsAPIPath(t *testing.T) {
	if guardrailsAPI != "/services/guardrails/v1" {
		t.Errorf("guardrailsAPI = %q", guardrailsAPI)
	}
}

// uploadBundle is the part with a decision in it: out-of-band when offered, and a real
// refusal must not be mistaken for "not offered".
func TestUploadBundlePutsWithoutAuthAndReturnsTheKey(t *testing.T) {
	var (
		gotSize string
		putAuth = "unset"
		putLen  int64
		putBody string
	)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case guardrailsAPI + "/baselines/upload-url":
			_ = r.ParseForm()
			gotSize = r.FormValue("size_bytes")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"bundle_key": "key-abc", "url": srv.URL + "/put-here", "expires_in": 900,
			})
		case "/put-here":
			putAuth = r.Header.Get("Authorization")
			putLen = r.ContentLength
			b, _ := io.ReadAll(r.Body)
			putBody = string(b)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client") // httptest is plaintext; HTTPS is only required for the production client id

	path := filepath.Join(t.TempDir(), "b.bundle")
	if err := os.WriteFile(path, []byte("bundle-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	client := newTestClient(srv.URL)
	defer client.Close()

	key, err := uploadBundle(client, path)
	if err != nil {
		t.Fatalf("uploadBundle: %v", err)
	}
	if key != "key-abc" {
		t.Errorf("key = %q", key)
	}
	if gotSize != "12" {
		t.Errorf("size_bytes = %q, want 12", gotSize)
	}
	if putAuth != "" {
		t.Errorf("Authorization sent to the upload URL = %q, want none", putAuth)
	}
	if putLen != 12 {
		t.Errorf("PUT Content-Length = %d, want 12", putLen)
	}
	if putBody != "bundle-bytes" {
		t.Errorf("PUT body = %q", putBody)
	}
}

func TestUploadBundleReportsNotOfferedOn501(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client") // httptest is plaintext; HTTPS is only required for the production client id

	path := filepath.Join(t.TempDir(), "b.bundle")
	_ = os.WriteFile(path, []byte("x"), 0o644)

	client := newTestClient(srv.URL)
	defer client.Close()

	key, err := uploadBundle(client, path)
	if err != nil {
		t.Fatalf("501 should report not-offered, not error: %v", err)
	}
	if key != "" {
		t.Errorf("key = %q, want empty", key)
	}
}

func TestUploadBundleSurfacesARealRefusal(t *testing.T) {
	// 413 (over the size cap) must not be read as "upload not offered".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"detail":"bundle too large"}`))
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client") // httptest is plaintext; HTTPS is only required for the production client id

	path := filepath.Join(t.TempDir(), "b.bundle")
	_ = os.WriteFile(path, []byte("x"), 0o644)

	client := newTestClient(srv.URL)
	defer client.Close()

	if _, err := uploadBundle(client, path); err == nil {
		t.Fatal("want an error for a 413, got nil")
	}
}

func TestUploadBundleFailsWhenThePutFails(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case guardrailsAPI + "/baselines/upload-url":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"bundle_key": "k", "url": srv.URL + "/put-here", "expires_in": 900,
			})
		case "/put-here":
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client") // httptest is plaintext; HTTPS is only required for the production client id

	path := filepath.Join(t.TempDir(), "b.bundle")
	_ = os.WriteFile(path, []byte("x"), 0o644)

	client := newTestClient(srv.URL)
	defer client.Close()

	if _, err := uploadBundle(client, path); err == nil {
		t.Fatal("want an error when the upload is rejected")
	}
}

func newTestClient(baseURL string) *api.Client {
	return api.NewClient(baseURL, "tok")
}

// Classification decides the exit code and the suggestion, so it is worth pinning per status.
func TestGuardrailsCLIErrorClassifies(t *testing.T) {
	cases := []struct {
		name     string
		in       error
		wantCode string
		wantExit int
		wantMsg  string
	}{
		{
			"403 is about the account",
			&api.APIError{Message: `API error: {"detail":"company not provisioned for guardrails"}`, StatusCode: 403},
			"NOT_AVAILABLE", clierrors.ExitGeneralError, "company not provisioned for guardrails",
		},
		{
			"404 is a missing baseline",
			&api.APIError{Message: `API error: {"detail":"no baseline for a/b@main"}`, StatusCode: 404},
			"NOT_FOUND", clierrors.ExitNotFound, "no baseline for a/b@main",
		},
		{
			"409 is a stale baseline",
			&api.APIError{Message: `API error: {"detail":"built by an older version of the analysis model; rebuild and ratify it"}`, StatusCode: 409},
			"STALE_BASELINE", clierrors.ExitGeneralError, "rebuild and ratify it",
		},
		{
			"an expired session is an auth failure",
			&api.AuthenticationError{Message: "Session expired. Run 'konvu login' again."},
			"AUTH_FAILED", clierrors.ExitAuthFailed, "Session expired",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := guardrailsCLIError(tc.in)
			if got.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", got.Code, tc.wantCode)
			}
			if got.ExitCode != tc.wantExit {
				t.Errorf("ExitCode = %d, want %d", got.ExitCode, tc.wantExit)
			}
			if !strings.Contains(got.Message, tc.wantMsg) {
				t.Errorf("Message = %q, want it to contain %q", got.Message, tc.wantMsg)
			}
			if strings.Contains(got.Message, "API error:") || strings.Contains(got.Message, `{"detail"`) {
				t.Errorf("raw wrapper leaked: %q", got.Message)
			}
			if got.Suggestion == "" {
				t.Error("every classified error should suggest a next step")
			}
		})
	}
}

func TestGuardrailsCLIErrorKeepsATransportError(t *testing.T) {
	got := guardrailsCLIError(errors.New("dial tcp: refused"))
	if !strings.Contains(got.Message, "dial tcp") {
		t.Errorf("Message = %q", got.Message)
	}
}

// os.Exit does not run deferred functions, so classifying the error inside the flow would
// leave the staged refs and the temp bundle in the user's checkout. The flow returns; only
// the wrapper exits.
func TestBaselineFlowCleansUpAfterAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail":"company not provisioned for guardrails"}`))
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "a.txt"}, {"commit", "-q", "-m", "one"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	policy := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(policy, []byte("subjects: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = guardrailsBaselineCmd.Flags().Set("policy", policy)
	defer func() { _ = guardrailsBaselineCmd.Flags().Set("policy", "") }()

	if err := baselineFlow(guardrailsBaselineCmd, []string{repo}); err == nil {
		t.Fatal("want the 403 to surface as an error")
	}

	out, err := exec.Command("git", "-C", repo, "for-each-ref", "--format=%(refname)", "refs/authzprover/").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("staged refs left behind after a failure: %q", out)
	}
}
