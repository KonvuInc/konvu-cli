package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func pushCmd() *cobra.Command {
	c := &cobra.Command{Use: "push", RunE: runInvariantsPush}
	c.Flags().String("file", "", "")
	c.Flags().Bool("dry-run", false, "")
	return c
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
	out, err := runInvariants(pushCmd(), "--file", file, "--dry-run")
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
	if _, err := runInvariants(pushCmd(), "--file", file); err != nil {
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
	_, err := runInvariants(pushCmd(), "--file", file)
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
	_, err := runInvariants(pushCmd(), "--file", file)
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

	out, err := runInvariants(&cobra.Command{Use: "list", RunE: runInvariantsList})
	if err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || items[0]["statement"] != "Tokens never reach logs." {
		t.Fatalf("out = %q", out)
	}
}
