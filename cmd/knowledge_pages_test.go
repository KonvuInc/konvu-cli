package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/spf13/cobra"
)

func knowledgePagesServer(t *testing.T, handler http.HandlerFunc) *int {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing auth")
		}
		handler(w, r)
	}))
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "test-token")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	t.Cleanup(server.Close)
	return &calls
}

func runKnowledgePages(run func(*cobra.Command, []string) error, format string, args ...string) (string, error) {
	command := &cobra.Command{}
	command.Flags().StringP("output", "o", "", "")
	_ = command.Flags().Set("output", format)
	out := new(bytes.Buffer)
	command.SetOut(out)
	err := run(command, args)
	return out.String(), err
}

func knowledgeExitCode(t *testing.T, err error) int {
	t.Helper()
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok {
		t.Fatalf("error = %#v, want a CLIError", err)
	}
	return cliErr.ExitCode
}

const knowledgePagesList = `{"items":[
	{"path":"company.md","title":"Company","kind":"chapter","written_by":"konvu","owner":null,"revision":0,"updated_at":"2026-11-18T09:12:04Z","stale":true},
	{"path":"ops.md","title":"Ops \u001b[31mred","kind":"custom","written_by":"person","owner":null,"revision":3,"updated_at":"2026-11-20T10:00:00Z","stale":false}
]}`

func TestKnowledgeLsListsPages(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/knowledge/pages" || r.URL.RawQuery != "" {
			t.Errorf("request = %s", r.URL.String())
		}
		_, _ = w.Write([]byte(knowledgePagesList))
	})
	out, err := runKnowledgePages(runKnowledgeLs, "table")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"company.md", "Company", "konvu", "2026-11-18", "yes", "ops.md", "person", "3"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("table kept a control character from a title:\n%q", out)
	}
}

func TestKnowledgeLsPassesJSONThrough(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(knowledgePagesList))
	})
	out, err := runKnowledgePages(runKnowledgeLs, "json")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if items, _ := result["items"].([]any); len(items) != 2 {
		t.Errorf("items = %v", result["items"])
	}
}

func TestKnowledgeLsFolder(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("under"); got != "products/acme" {
			t.Errorf("under = %q", got)
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	})
	out, err := runKnowledgePages(runKnowledgeLs, "table", "/products/acme/")
	if err != nil {
		t.Fatal(err)
	}
	if out != "No pages under products/acme.\n" {
		t.Errorf("out = %q", out)
	}
}

func TestKnowledgeLsEmptyBook(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[]}`))
	})
	out, err := runKnowledgePages(runKnowledgeLs, "table")
	if err != nil {
		t.Fatal(err)
	}
	if out != knowledgeNoPagesMessage {
		t.Errorf("out = %q", out)
	}
}

func TestKnowledgeCatPrintsMarkdown(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/knowledge/pages/company.md" || r.URL.Query().Get("format") != "md" {
			t.Errorf("request = %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte("# Company\r\n\nAcme sells payroll.[^1]\x1b[2J\n\n## Sources\n\n[^1]: <https://acme.example>\n    > Payroll"))
	})
	out, err := runKnowledgePages(runKnowledgeCat, "", "/company.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Company\n\nAcme sells payroll.[^1][2J\n\n## Sources\n\n[^1]: <https://acme.example>\n    > Payroll\n"
	if out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
}

func TestKnowledgeCatJSON(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/knowledge/pages/products/acme/README.md" || r.URL.RawQuery != "" {
			t.Errorf("request = %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"path":"products/acme/README.md","revision":2,"body_md":"Text.","sources":[]}`))
	})
	out, err := runKnowledgePages(runKnowledgeCat, "json", "products/acme/README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"revision": 2`) || !strings.Contains(out, `"body_md": "Text."`) {
		t.Errorf("out = %s", out)
	}
}

func TestKnowledgeRefusesPathsBeforeAnyRequest(t *testing.T) {
	calls := knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request sent: %s", r.URL.String())
	})
	for _, path := range []string{"", "../admin.md", "x/../company.md", "a?b.md", "a%2f.md", "company", "Company.md", "//company.md", "products/acme/", strings.Repeat("a", 253) + ".md"} {
		if _, err := runKnowledgePages(runKnowledgeCat, "", path); knowledgeExitCode(t, err) != clierrors.ExitUsageError {
			t.Errorf("cat %q: exit %d, want %d", path, knowledgeExitCode(t, err), clierrors.ExitUsageError)
		}
	}
	for _, folder := range []string{"..", "company.md", "products/../admin", "a?b", "a/b/c/d/e/f/g/h"} {
		if _, err := runKnowledgePages(runKnowledgeLs, "table", folder); knowledgeExitCode(t, err) != clierrors.ExitUsageError {
			t.Errorf("ls %q: exit %d, want %d", folder, knowledgeExitCode(t, err), clierrors.ExitUsageError)
		}
	}
	if _, err := runKnowledgePages(runKnowledgeCat, "table", "company.md"); knowledgeExitCode(t, err) != clierrors.ExitUsageError {
		t.Errorf("cat -o table: exit %d", knowledgeExitCode(t, err))
	}
	if *calls != 0 {
		t.Errorf("%d requests sent", *calls)
	}
}

func TestKnowledgeErrors(t *testing.T) {
	tests := []struct {
		name   string
		run    func(*cobra.Command, []string) error
		args   []string
		status int
		code   string
		exit   int
	}{
		{"missing page", runKnowledgeCat, []string{"company.md"}, 404, "PAGE_NOT_FOUND", clierrors.ExitNotFound},
		{"older server", runKnowledgeLs, nil, 404, "KNOWLEDGE_PAGES_UNAVAILABLE", clierrors.ExitNotFound},
		{"bad path", runKnowledgeCat, []string{"company.md"}, 422, "INVALID_ARGUMENTS", clierrors.ExitUsageError},
		{"forbidden", runKnowledgeLs, nil, 403, "FORBIDDEN", clierrors.ExitAuthFailed},
		{"logged out", runKnowledgeCat, []string{"company.md"}, 401, "AUTH_FAILED", clierrors.ExitAuthFailed},
		{"server error", runKnowledgeLs, nil, 500, "API_ERROR", clierrors.ExitGeneralError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"detail":"the server's reason"}`))
			})
			_, err := runKnowledgePages(test.run, "", test.args...)
			cliErr, ok := err.(*clierrors.CLIError)
			if !ok || cliErr.Code != test.code || cliErr.ExitCode != test.exit {
				t.Fatalf("error = %#v, want %s exit %d", err, test.code, test.exit)
			}
			if test.status == 422 && cliErr.Message != "the server's reason" {
				t.Errorf("422 message = %q", cliErr.Message)
			}
		})
	}
}
