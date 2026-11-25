package cmd

import (
	"bytes"
	"encoding/json"
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

// A path longer than the table's budget for its other columns: it must still print whole.
const knowledgeLongPath = "products/payroll-api-for-european-mid-size-companies/apps/payroll-api-gateway-and-partner-portal/threat-model.md"

const knowledgePagesList = `{"items":[
	{"path":"company.md","title":"Company","kind":"chapter","written_by":"konvu","owner":null,"revision":0,"updated_at":"2026-11-18T09:12:04Z","stale":true},
	{"path":"` + knowledgeLongPath + `","title":"Ops \u001b[31mred","kind":"custom","written_by":"person","owner":null,"revision":3,"updated_at":"2026-11-20T10:00:00Z","stale":false}
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
	for _, want := range []string{"company.md", "Company", "konvu", "2026-11-18", "yes", knowledgeLongPath, "person", "3"} {
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
		{"page on an older server", runKnowledgeCat, []string{"company.md"}, 404, "KNOWLEDGE_PAGES_UNAVAILABLE", clierrors.ExitNotFound},
		{"bad path", runKnowledgeCat, []string{"company.md"}, 422, "INVALID_ARGUMENTS", clierrors.ExitUsageError},
		{"forbidden", runKnowledgeLs, nil, 403, "FORBIDDEN", clierrors.ExitAuthFailed},
		{"logged out", runKnowledgeCat, []string{"company.md"}, 401, "AUTH_FAILED", clierrors.ExitAuthFailed},
		{"server error", runKnowledgeLs, nil, 500, "API_ERROR", clierrors.ExitGeneralError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
				if test.name == "missing page" && r.URL.Path == knowledgePagesPath {
					_, _ = w.Write([]byte(`{"items":[]}`))
					return
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"detail":"the server's\u001b[2J reason"}`))
			})
			_, err := runKnowledgePages(test.run, "", test.args...)
			cliErr, ok := err.(*clierrors.CLIError)
			if !ok || cliErr.Code != test.code || cliErr.ExitCode != test.exit {
				t.Fatalf("error = %#v, want %s exit %d", err, test.code, test.exit)
			}
			if strings.ContainsRune(cliErr.Message, '\x1b') {
				t.Errorf("message kept a control character: %q", cliErr.Message)
			}
			if test.status == 422 && cliErr.Message != "the server's [2J reason" {
				t.Errorf("422 message = %q", cliErr.Message)
			}
		})
	}
}

func TestKnowledgeLsShowsPagesNotWritten(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[
			{"path":"company.md","title":"Company","kind":"chapter","written_by":"konvu","owner":null,"revision":2,"updated_at":"2026-11-18T09:12:04Z","stale":false,"written":true},
			{"path":"infrastructure.md","title":"Infrastructure","kind":"chapter","written_by":"konvu","owner":null,"revision":0,"updated_at":null,"stale":false,"written":false}
		]}`))
	})
	out, err := runKnowledgePages(runKnowledgeLs, "table")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.Contains(line, "infrastructure.md") && !strings.Contains(line, "not written") {
			t.Errorf("unwritten page not marked: %q", line)
		}
		if strings.Contains(line, "company.md") && (strings.Contains(line, "not written") || !strings.Contains(line, "2026-11-18")) {
			t.Errorf("written page misread: %q", line)
		}
	}
	if strings.Count(out, "not written") != 1 {
		t.Errorf("table:\n%s", out)
	}
}

// knowledgeEditServer serves one page for edits: page is the GET's answer (empty for a 404), and
// put answers the PUT, after the test checks what was sent.
type knowledgeEditServer struct {
	page    string
	list    int
	put     func(t *testing.T, body map[string]any) (int, string)
	puts    int
	gotPuts []map[string]any
}

func (s *knowledgeEditServer) start(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == knowledgePagesPath:
			if s.list != 0 {
				w.WriteHeader(s.list)
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.Method == "GET":
			if s.page == "" {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"detail":"Page not found"}`))
				return
			}
			_, _ = w.Write([]byte(s.page))
		case r.Method == "PUT":
			s.puts++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			s.gotPuts = append(s.gotPuts, body)
			status, answer := s.put(t, body)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(answer))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
}

func savedAt(revision int) func(t *testing.T, body map[string]any) (int, string) {
	return func(t *testing.T, body map[string]any) (int, string) {
		return 200, fmt.Sprintf(`{"path":"ops.md","revision":%d,"written_by":"person"}`, revision)
	}
}

// editKnowledgePage runs edit and returns what it printed to stdout and to stderr.
func editKnowledgePage(t *testing.T, stdin string, args []string, flags map[string]string) (string, string, error) {
	t.Helper()
	command := &cobra.Command{}
	addKnowledgeEditFlags(command)
	for name, value := range flags {
		if err := command.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	command.SetIn(strings.NewReader(stdin))
	out, warnings := new(bytes.Buffer), new(bytes.Buffer)
	command.SetOut(out)
	command.SetErr(warnings)
	err := runKnowledgeEdit(command, args)
	return out.String(), warnings.String(), err
}

func knowledgeEditFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "page.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const knowledgeTeamPageRead = `{"path":"ops.md","title":"Operations","written_by":"person","revision":0,"updated_at":"2026-11-18T09:12:04Z","body_md":"Old.[^1]","sources":[{"n":1,"url":"https://acme.example/about","quote":"Payroll"}]}`

func TestKnowledgeEditWithBaseSendsTheSourcesRead(t *testing.T) {
	server := &knowledgeEditServer{
		page: knowledgeTeamPageRead,
		put: func(t *testing.T, body map[string]any) (int, string) {
			return 200, `{"path":"ops.md","revision":1,"written_by":"person\u001b[2J","body_md":"New."}`
		},
	}
	server.start(t)
	file := knowledgeEditFile(t, "## Deploys\r\n\r\nDaily.[^1]\r\n")
	out, warnings, err := editKnowledgePage(t, "", []string{"/ops.md"}, map[string]string{"file": file, "base": knowledgeEditFile(t, knowledgeTeamPageRead), "output": "table"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Saved ops.md at revision 1, written by person [2J.") || strings.Contains(out, "\x1b") {
		t.Errorf("out = %q", out)
	}
	if warnings != "" {
		t.Errorf("warned with a base: %q", warnings)
	}
	sent, _ := json.Marshal(server.gotPuts[0])
	want := `{"body_md":"## Deploys\n\nDaily.[^1]\n","revision":0,"sources":[{"n":1,"quote":"Payroll","url":"https://acme.example/about"}]}`
	if string(sent) != want {
		t.Errorf("PUT body = %s, want %s", sent, want)
	}
}

func TestKnowledgeEditWithoutBaseWarns(t *testing.T) {
	server := &knowledgeEditServer{page: knowledgeTeamPageRead, put: savedAt(1)}
	server.start(t)
	_, warnings, err := editKnowledgePage(t, "New.[^1]", []string{"ops.md"}, map[string]string{"file": "-"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warnings, "no --base") || !strings.Contains(warnings, "revision 0") {
		t.Errorf("warnings = %q", warnings)
	}
	sent, _ := json.Marshal(server.gotPuts[0]["sources"])
	if string(sent) != `[{"n":1,"quote":"Payroll","url":"https://acme.example/about"}]` {
		t.Errorf("sources = %s", sent)
	}
}

func TestKnowledgeEditNewPage(t *testing.T) {
	server := &knowledgeEditServer{put: savedAt(1)}
	server.start(t)
	if _, _, err := editKnowledgePage(t, "We deploy daily.", []string{"ops.md"}, map[string]string{"file": "-"}); knowledgeExitCode(t, err) != clierrors.ExitUsageError {
		t.Errorf("new page with no title: %v", err)
	}
	if server.puts != 0 {
		t.Fatalf("a new page with no title was sent")
	}
	out, warnings, err := editKnowledgePage(t, "We deploy daily.", []string{"ops.md"}, map[string]string{"file": "-", "title": " Operations ", "output": "json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"revision": 1`) || warnings != "" {
		t.Errorf("out = %s, warnings = %q", out, warnings)
	}
	sent, _ := json.Marshal(server.gotPuts[0])
	if want := `{"body_md":"We deploy daily.","revision":0,"sources":[],"title":"Operations"}`; string(sent) != want {
		t.Errorf("PUT body = %s, want %s", sent, want)
	}
}

// Konvu writes its own pages from the company's data: their text is never sent, and the error names
// the command that edits that data.
func TestKnowledgeEditRefusesKonvuPages(t *testing.T) {
	calls := knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request sent: %s %s", r.Method, r.URL.String())
	})
	for path, command := range map[string]string{
		"company.md":           "konvu company apply",
		"/infrastructure.md":   "konvu company apply",
		"threat-model.md":      "konvu threat-model apply",
		"context/ownership.md": "konvu knowledge note add --label ownership",
	} {
		// The file is not even read: a missing one still gets the refusal.
		_, _, err := editKnowledgePage(t, "", []string{path}, map[string]string{"file": filepath.Join(t.TempDir(), "missing.md")})
		cliErr, ok := err.(*clierrors.CLIError)
		if !ok || cliErr.Code != "KONVU_PAGE" || cliErr.ExitCode != clierrors.ExitUsageError || !strings.Contains(cliErr.Suggestion, command) {
			t.Errorf("%s: error = %#v", path, err)
		}
	}
	if *calls != 0 {
		t.Errorf("%d requests sent", *calls)
	}
}

func TestKnowledgeEditRefusesAMovedPageBeforeAnyPut(t *testing.T) {
	for _, test := range []struct{ name, page string }{
		{"edited by a person", strings.Replace(knowledgeTeamPageRead, `"revision":0`, `"revision":2`, 1)},
		// A page can be rewritten without moving its revision.
		{"rewritten at the same revision", strings.Replace(knowledgeTeamPageRead, "2026-11-18T09:12:04Z", "2026-11-23T08:00:00Z", 1)},
		{"deleted since", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := &knowledgeEditServer{page: test.page, put: savedAt(1)}
			server.start(t)
			_, _, err := editKnowledgePage(t, "Text.", []string{"ops.md"}, map[string]string{"file": "-", "base": knowledgeEditFile(t, knowledgeTeamPageRead)})
			cliErr, ok := err.(*clierrors.CLIError)
			if !ok || cliErr.Code != "PAGE_CHANGED" || cliErr.ExitCode != clierrors.ExitGeneralError || !strings.Contains(cliErr.Suggestion, "cat ops.md -o json") {
				t.Fatalf("error = %#v", err)
			}
			if server.puts != 0 {
				t.Errorf("%d PUTs sent", server.puts)
			}
		})
	}
}

func TestKnowledgeEditRefusesABaseOfAnotherPage(t *testing.T) {
	calls := knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request sent: %s %s", r.Method, r.URL.String())
	})
	for name, base := range map[string]string{
		"another page":  strings.Replace(knowledgeTeamPageRead, `"ops.md"`, `"runbook.md"`, 1),
		"no sources":    `{"path":"ops.md","revision":0}`,
		"no revision":   `{"path":"ops.md","sources":[]}`,
		"not JSON":      "# Company",
		"the list":      `{"items":[]}`,
		"null sources":  `{"path":"ops.md","revision":0,"sources":null}`,
		"string number": `{"path":"ops.md","revision":"0","sources":[]}`,
	} {
		_, _, err := editKnowledgePage(t, "Text.", []string{"ops.md"}, map[string]string{"file": "-", "base": knowledgeEditFile(t, base)})
		if knowledgeExitCode(t, err) != clierrors.ExitUsageError {
			t.Errorf("%s: exit %d", name, knowledgeExitCode(t, err))
		}
	}
	if _, _, err := editKnowledgePage(t, "Text.", []string{"ops.md"}, map[string]string{"file": "-", "base": filepath.Join(t.TempDir(), "missing.json")}); knowledgeExitCode(t, err) != clierrors.ExitUsageError {
		t.Errorf("missing base: exit %d", knowledgeExitCode(t, err))
	}
	if *calls != 0 {
		t.Errorf("%d requests sent", *calls)
	}
}

func TestKnowledgeEditRefusals(t *testing.T) {
	tests := []struct {
		status int
		code   string
		exit   int
	}{
		{409, "PAGE_CHANGED", clierrors.ExitGeneralError},
		{403, "FORBIDDEN", clierrors.ExitAuthFailed},
		{422, "INVALID_ARGUMENTS", clierrors.ExitUsageError},
		{413, "REQUEST_TOO_LARGE", clierrors.ExitGeneralError},
		{405, "KNOWLEDGE_EDIT_UNAVAILABLE", clierrors.ExitNotFound},
		{401, "AUTH_FAILED", clierrors.ExitAuthFailed},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			server := &knowledgeEditServer{
				page: knowledgeTeamPageRead,
				put: func(t *testing.T, body map[string]any) (int, string) {
					return test.status, `{"detail":"the server's\u001b[2J reason"}`
				},
			}
			server.start(t)
			_, _, err := editKnowledgePage(t, "Text.", []string{"ops.md"}, map[string]string{"file": "-", "base": knowledgeEditFile(t, knowledgeTeamPageRead)})
			cliErr, ok := err.(*clierrors.CLIError)
			if !ok || cliErr.Code != test.code || cliErr.ExitCode != test.exit {
				t.Fatalf("error = %#v, want %s exit %d", err, test.code, test.exit)
			}
			if strings.ContainsRune(cliErr.Message, '\x1b') {
				t.Errorf("message kept a control character: %q", cliErr.Message)
			}
			if test.status == 422 && cliErr.Message != "the server's [2J reason" {
				t.Errorf("422 message = %q", cliErr.Message)
			}
			if test.status == 413 && strings.Contains(cliErr.Suggestion, "files") {
				t.Errorf("413 on an edit talks of files: %q", cliErr.Suggestion)
			}
		})
	}
}

// A redirect turns the PUT into a GET, answered with the page as it was: that saved nothing.
func TestKnowledgeEditRefusesAnAnswerThatSavedNothing(t *testing.T) {
	server := &knowledgeEditServer{page: knowledgeTeamPageRead, put: func(t *testing.T, body map[string]any) (int, string) {
		return 200, knowledgeTeamPageRead
	}}
	server.start(t)
	_, _, err := editKnowledgePage(t, "Text.", []string{"ops.md"}, map[string]string{"file": "-", "base": knowledgeEditFile(t, knowledgeTeamPageRead)})
	if cliErr, ok := err.(*clierrors.CLIError); !ok || cliErr.ExitCode != clierrors.ExitGeneralError || !strings.Contains(cliErr.Message, "does not show ops.md saved") {
		t.Fatalf("error = %#v", err)
	}
}

func TestKnowledgeEditOnAServerWithNoBook(t *testing.T) {
	server := &knowledgeEditServer{list: 404, put: savedAt(1)}
	server.start(t)
	_, _, err := editKnowledgePage(t, "Text.", []string{"ops.md"}, map[string]string{"file": "-"})
	if cliErr, ok := err.(*clierrors.CLIError); !ok || cliErr.Code != "KNOWLEDGE_PAGES_UNAVAILABLE" {
		t.Fatalf("error = %#v", err)
	}
	if server.puts != 0 {
		t.Errorf("%d PUTs sent", server.puts)
	}
}

func TestKnowledgeEditRefusesBadTextBeforeAnyRequest(t *testing.T) {
	calls := knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request sent: %s %s", r.Method, r.URL.String())
	})
	for name, text := range map[string]string{
		"empty":      " \r\n\t",
		"NUL":        "a\x00b",
		"not UTF-8":  "caf\xe9",
		"over limit": strings.Repeat("é", knowledgePageMaxChars+1),
		"way over":   strings.Repeat("a", 4*knowledgePageMaxChars+1),
	} {
		if _, _, err := editKnowledgePage(t, text, []string{"ops.md"}, map[string]string{"file": "-"}); knowledgeExitCode(t, err) != clierrors.ExitUsageError {
			t.Errorf("%s: exit %d", name, knowledgeExitCode(t, err))
		}
	}
	for name, flags := range map[string]map[string]string{
		"no file":      {},
		"missing file": {"file": filepath.Join(t.TempDir(), "missing.md")},
		"empty title":  {"file": "-", "title": " "},
		"empty base":   {"file": "-", "base": ""},
		"bad output":   {"file": "-", "output": "yaml"},
	} {
		if _, _, err := editKnowledgePage(t, "Text.", []string{"ops.md"}, flags); knowledgeExitCode(t, err) != clierrors.ExitUsageError {
			t.Errorf("%s: exit %d", name, knowledgeExitCode(t, err))
		}
	}
	if _, _, err := editKnowledgePage(t, "Text.", []string{"../ops.md"}, map[string]string{"file": "-"}); knowledgeExitCode(t, err) != clierrors.ExitUsageError {
		t.Errorf("bad path: exit %d", knowledgeExitCode(t, err))
	}
	if *calls != 0 {
		t.Errorf("%d requests sent", *calls)
	}
}

func TestKnowledgeEditAcceptsTheLimit(t *testing.T) {
	for name, text := range map[string]string{
		"multibyte characters": strings.Repeat("é", knowledgePageMaxChars),
		// Over the limit in raw characters, under it once CRLF is made LF: counted after.
		"CRLF line ends": strings.Repeat("\r\n", knowledgePageMaxChars*3/4) + "a",
	} {
		server := &knowledgeEditServer{page: knowledgeTeamPageRead, put: savedAt(1)}
		server.start(t)
		if _, _, err := editKnowledgePage(t, text, []string{"ops.md"}, map[string]string{"file": "-"}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if server.puts != 1 {
			t.Errorf("%s: %d PUTs sent", name, server.puts)
		}
	}
}
