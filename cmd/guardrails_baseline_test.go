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
			// 409 covers a stale baseline AND a draft that cannot be ratified, so the suggestion
			// cannot name one remedy; the server's detail already says which.
			"409 lets the server's detail speak",
			&api.APIError{Message: `API error: {"detail":"built by an older version of the analysis model; rebuild and ratify it"}`, StatusCode: 409},
			"CONFLICT", clierrors.ExitGeneralError, "rebuild and ratify it",
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
			// Not every status has one honest next step: a 409 can mean two different
			// things, so its message carries the remedy instead of a canned suggestion.
			if got.Suggestion == "" && got.Code != "CONFLICT" {
				t.Error("this error should suggest a next step")
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

// os.Exit does not run deferred functions, so ending the flow anywhere but the wrapper leaves the
// staged refs and the temp bundle in the user's checkout. One case per way the flow can end badly,
// because fixing this once for the 403 path is exactly how the other three survived.
func TestBaselineFlowCleansUpOnEveryFailurePath(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			"the account cannot use guardrails",
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"detail":"not available"}`))
			},
		},
		{
			"the server cannot issue an upload URL",
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotImplemented)
			},
		},
		{
			"the build fails",
			func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/upload-url"):
					_ = json.NewEncoder(w).Encode(map[string]any{
						"bundle_key": "k", "url": "http://" + r.Host + "/put", "expires_in": 900,
					})
				case r.URL.Path == "/put":
					w.WriteHeader(http.StatusOK)
				case strings.Contains(r.URL.Path, "/jobs/"):
					_ = json.NewEncoder(w).Encode(map[string]any{
						"job_id": "j1", "status": "error", "error": "no routes found",
					})
				default:
					w.WriteHeader(http.StatusAccepted)
					_ = json.NewEncoder(w).Encode(map[string]any{"job_id": "j1", "status": "pending"})
				}
			},
		},
		{
			"the build outlasts the timeout",
			func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/upload-url"):
					_ = json.NewEncoder(w).Encode(map[string]any{
						"bundle_key": "k", "url": "http://" + r.Host + "/put", "expires_in": 900,
					})
				case r.URL.Path == "/put":
					w.WriteHeader(http.StatusOK)
				case strings.Contains(r.URL.Path, "/jobs/"):
					_ = json.NewEncoder(w).Encode(map[string]any{"job_id": "j1", "status": "running"})
				default:
					w.WriteHeader(http.StatusAccepted)
					_ = json.NewEncoder(w).Encode(map[string]any{"job_id": "j1", "status": "pending"})
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			t.Setenv("KONVU_API_URL", srv.URL)
			t.Setenv("KONVU_ACCESS_TOKEN", "tok")
			t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

			repo := newRepo(t)
			// A timeout that has already passed, so the poll loop gives up on its first look.
			_ = guardrailsBaselineCmd.Flags().Set("timeout", "1ns")
			defer func() { _ = guardrailsBaselineCmd.Flags().Set("timeout", "30m") }()

			if err := baselineFlow(guardrailsBaselineCmd, []string{repo}); err == nil {
				t.Fatal("want an error rather than an exit")
			}

			out, err := exec.Command("git", "-C", repo, "for-each-ref",
				"--format=%(refname)", "refs/authzprover/").CombinedOutput()
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(out)) != "" {
				t.Errorf("staged refs left behind: %q", out)
			}
		})
	}
}

// newRepo makes a throwaway checkout with one commit.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"add", "a.txt"}, {"commit", "-q", "-m", "one"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	return dir
}

// The server retired client-supplied policies: it proposes one from the baseline and it is
// ratified in the dashboard. Sending the field at all is a 422, so this pins that we do not.
func TestBaselineDoesNotSendAPolicy(t *testing.T) {
	var sawPolicy bool
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case guardrailsAPI + "/baselines/upload-url":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"bundle_key": "bundles/dev/baseline/k.bundle",
				"url":        srv.URL + "/put", "expires_in": 900,
			})
		case "/put":
			w.WriteHeader(http.StatusOK)
		case guardrailsAPI + "/baselines":
			_ = r.ParseForm()
			_, sawPolicy = r.Form["policy"]
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"job_id": "j1", "status": "pending"})
		case guardrailsAPI + "/baselines/jobs/j1":
			_ = json.NewEncoder(w).Encode(map[string]any{"job_id": "j1", "status": "done"})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	t.Setenv("KONVU_API_URL", srv.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "tok")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	repo := t.TempDir()
	for _, a := range [][]string{
		{"init", "-q", "-b", "main"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "t"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"add", "a.txt"}, {"commit", "-q", "-m", "one"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}

	// Even when the deprecated flag is set, the field must not go on the wire.
	_ = guardrailsBaselineCmd.Flags().Set("policy", "/does/not/matter.yaml")
	defer func() { _ = guardrailsBaselineCmd.Flags().Set("policy", "") }()

	if err := baselineFlow(guardrailsBaselineCmd, []string{repo}); err != nil {
		t.Fatalf("baselineFlow: %v", err)
	}
	if sawPolicy {
		t.Error("policy was sent; the server rejects the field outright (422)")
	}
}
