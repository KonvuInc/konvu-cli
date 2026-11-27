package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const bookPagesList = `{"items":[
	{"path":"README.md","title":"Acme","kind":"generated","written_by":"konvu","revision":0,"updated_at":"2026-11-18T09:12:04Z","stale":false},
	{"path":"company.md","title":"Company","kind":"chapter","written_by":"konvu","revision":0,"updated_at":"2026-11-18T09:12:04Z","stale":false},
	{"path":"infrastructure.md","title":"Infrastructure","kind":"chapter","written_by":"konvu","revision":0,"updated_at":null,"stale":false,"written":false},
	{"path":"threat-model.md","title":"Threat model","kind":"chapter","written_by":"konvu","revision":0,"updated_at":"2026-11-18T09:12:04Z","stale":true},
	{"path":"security-principles.md","title":"Principles","kind":"generated","written_by":"konvu","revision":0,"updated_at":"2026-11-18T09:12:04Z","stale":false},
	{"path":"products/acme/apps/billing/threat-model.md","title":"Billing","kind":"app","written_by":"konvu","revision":0,"updated_at":"2026-11-18T09:12:04Z","stale":false},
	{"path":"ops.md","title":"Ops \u001b[31mred","kind":"custom","written_by":"person","revision":3,"updated_at":"2026-11-20T10:00:00Z","stale":false}
]}`

var bookPagesEditWith = map[string]string{
	"README.md":              "read-only",
	"company.md":             "konvu company apply",
	"infrastructure.md":      "konvu company apply",
	"threat-model.md":        "konvu threat-model apply",
	"security-principles.md": "konvu principles",
	"products/acme/apps/billing/threat-model.md": "konvu inventory catalog apply",
	"ops.md": "konvu knowledge note add",
}

func TestBookLsShowsTheCommandThatEditsEachPage(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /knowledge/pages": {{http.StatusOK, json.RawMessage(bookPagesList)}},
	})
	out, err := runInvariants(newBookCmd(), "ls", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "EDIT WITH") || strings.Contains(out, "\x1b") {
		t.Fatalf("table:\n%q", out)
	}
	for path, command := range bookPagesEditWith {
		found := false
		for _, line := range strings.Split(out, "\n") {
			if strings.HasSuffix(line, "  "+path) {
				found = true
				if !strings.Contains(line, command) {
					t.Errorf("%s: line %q lacks %q", path, line, command)
				}
			}
		}
		if !found {
			t.Errorf("%s not listed:\n%s", path, out)
		}
	}
	if !strings.Contains(out, "not written") {
		t.Errorf("unwritten page not marked:\n%s", out)
	}
}

func TestBookLsJSONAddsEditWith(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /knowledge/pages": {{http.StatusOK, json.RawMessage(bookPagesList)}},
	})
	out, err := runInvariants(newBookCmd(), "ls", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(result.Items) != len(bookPagesEditWith) {
		t.Fatalf("items = %v", result.Items)
	}
	for _, item := range result.Items {
		path, _ := item["path"].(string)
		if item["edit_with"] != bookPagesEditWith[path] {
			t.Errorf("%s: edit_with = %v, want %q", path, item["edit_with"], bookPagesEditWith[path])
		}
	}
}

// knowledge ls keeps its table and JSON as they were: no edit_with.
func TestKnowledgeLsHasNoEditWith(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bookPagesList))
	})
	for _, format := range []string{"table", "json"} {
		out, err := runKnowledgePages(runKnowledgeLs, format)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(out), "edit") {
			t.Errorf("%s output names an edit command:\n%s", format, out)
		}
	}
}

func TestBookEditWith(t *testing.T) {
	for _, test := range []struct{ path, kind, want string }{
		// Only the three root pages are built from the company's data, as the server has it.
		{"company/README.md", "custom", "konvu knowledge note add"},
		{"company/README.md", "generated", "read-only"},
		{"products/README.md", "generated", "konvu inventory catalog apply"},
		{"products/acme/security-principles.md", "generated", "konvu principles"},
		{"products/acme/notes.md", "custom", "konvu knowledge note add"},
		{"runbooks/README.md", "custom", "konvu knowledge note add"},
		{"repositories.md", "generated", "read-only"},
		{"inventory/endpoints.md", "generated", "read-only"},
		{"manual-feedback.md", "", "read-only"},
		// Konvu writes a page under context/ for each topic of the notes, with any slug.
		{"context/deployment.md", "generated", "konvu knowledge note list"},
		{"context/on-call-rota.md", "generated", "konvu knowledge note list"},
		{"context/runbook.md", "custom", "konvu knowledge note list"},
		{"context/team/deployment.md", "generated", "read-only"},
		{"docs/context/deployment.md", "generated", "read-only"},
	} {
		if got := bookEditWith(test.path, test.kind); got != test.want {
			t.Errorf("bookEditWith(%q, %q) = %q, want %q", test.path, test.kind, got, test.want)
		}
	}
}

func TestBookReadPrintsThePage(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/knowledge/pages/company.md" || r.URL.Query().Get("format") != "md" {
			t.Errorf("request = %s", r.URL.String())
		}
		_, _ = w.Write([]byte("# Company\n\nPayroll.\u001b[2J\n"))
	})
	out, err := runInvariants(newBookCmd(), "read", "/company.md")
	if err != nil {
		t.Fatal(err)
	}
	if out != "# Company\n\nPayroll.[2J\n" {
		t.Errorf("out = %q", out)
	}
}
