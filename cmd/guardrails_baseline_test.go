package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/api"
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
