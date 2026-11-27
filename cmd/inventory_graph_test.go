package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

const graphDocument = `{"revision":"g1","level":"component",
 "nodes":[{"id":"component:web","type":"component","name":"acme/web","parent_ids":["app:billing"]}],
 "edges":[{"id":"e1","source":"component:web","target":"resource:api.acme.test","kind":"calls",
   "evidence":[12345678901234567890],"via":["host:api.acme.test"],"protocol":"http",
   "exposure":"internet","confidence":0.9,"origin":"agent","provenance":"detected","count":1,"component_edge_ids":["e1"]}],
 "rejected_edges":[{"source":"component:web","target":"resource:old.acme.test","kind":"calls"}]}`

func TestInventoryGraphIsDiscoverableWithoutLogin(t *testing.T) {
	t.Setenv("KONVU_ACCESS_TOKEN", "")
	out, err := runInvariants(newInventoryGraphCmd(), "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"component", "app", "product", "revision", "rejected_edges", "resource:<name>", "source, target", "survive", "inventory catalog", "untrusted"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
	found, _, err := rootCmd.Find([]string{"inventory", "graph", "apply"})
	if err != nil || found.Name() != "apply" || found.Parent().Name() != "graph" {
		t.Fatalf("inventory graph apply not registered: %v, %v", found, err)
	}
}

func TestInventoryGraphGetKeepsTheWholeDocument(t *testing.T) {
	for _, level := range []string{"component", "app", "product"} {
		t.Run(level, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != inventoryGraphPath || r.URL.Query().Get("level") != level {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				_, _ = w.Write([]byte(strings.Replace(graphDocument, `"level":"component"`, `"level":"`+level+`"`, 1)))
			}))
			defer server.Close()
			invariantsEnv(t, server)
			out, err := runInvariants(newInventoryGraphCmd(), "get", "--level", level)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`"nodes"`, `"rejected_edges"`, `"origin"`, "12345678901234567890", `"level": "` + level + `"`} {
				if !strings.Contains(out, want) {
					t.Errorf("get dropped %q: %s", want, out)
				}
			}
		})
	}
}

func TestInventoryGraphGetDefaultsToComponents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("level") != "component" {
			t.Errorf("level = %q", r.URL.Query().Get("level"))
		}
		_, _ = w.Write([]byte(graphDocument))
	}))
	defer server.Close()
	invariantsEnv(t, server)
	if _, err := runInvariants(newInventoryGraphCmd(), "get"); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryGraphApplyPreservesScopeAndStripsReadOnlyMetadata(t *testing.T) {
	for _, level := range []string{"component", "app", "product"} {
		t.Run(level, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != inventoryGraphPath || r.URL.RawQuery != "dry_run=true" {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if len(body) != 3 || string(body["level"]) != `"`+level+`"` || string(body["revision"]) != `"g1"` {
					t.Errorf("body = %#v", body)
				}
				var edges []map[string]json.RawMessage
				if err := json.Unmarshal(body["edges"], &edges); err != nil || len(edges) != 1 {
					t.Fatalf("edges = %s, %v", body["edges"], err)
				}
				for _, key := range []string{"id", "origin", "provenance", "count", "component_edge_ids"} {
					if _, ok := edges[0][key]; ok {
						t.Errorf("read-only edge field %s was sent", key)
					}
				}
				if string(edges[0]["evidence"]) != `[12345678901234567890]` || string(edges[0]["confidence"]) != `0.9` {
					t.Errorf("metadata changed: %#v", edges[0])
				}
				_, _ = w.Write([]byte(`{"applied":false,"changes":["Add connection"],"revision":"g1"}`))
			}))
			defer server.Close()
			invariantsEnv(t, server)
			cmd := newInventoryGraphCmd()
			cmd.SetIn(strings.NewReader(strings.Replace(graphDocument, `"level":"component"`, `"level":"`+level+`"`, 1)))
			out, err := runInvariants(cmd, "apply", "--file", "-", "--dry-run", "-o", "table")
			if err != nil || !strings.Contains(out, "Would apply 1 change (dry run, nothing written)") {
				t.Fatalf("out = %q, err = %v", out, err)
			}
		})
	}
}

func TestInventoryGraphApplyRefusesAmbiguousOrMalformedFiles(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{})
	for _, document := range []string{
		`{"revision":"g1","edges":[]}`,
		`{"revision":"g1","level":"apps","edges":[]}`,
		`{"revision":"g1","level":"app"}`,
		`{"revision":"g1","level":"app","edges":null}`,
		`{"revision":"g1","level":"app","edges":{}}`,
		`{"revision":"g1","level":"app","edges":[null]}`,
		`{"revision":"g1","level":"app","edges":[],"edegs":[]}`,
		`{"revision":"g1","level":"app","edges":[{"soruce":"a","target":"b","kind":"calls"}]}`,
		`{"revision":"g1","level":"app","edges":[]} {}`,
		`{"revision":"","level":"app","edges":[]}`,
	} {
		_, err := runInvariants(newInventoryGraphCmd(), "apply", "--file", writeInvariants(t, document))
		if cliErr := bookDataCLIError(t, err); cliErr.ExitCode != clierrors.ExitUsageError {
			t.Errorf("document %s: %v", document, err)
		}
	}
	if len(calls()) != 0 {
		t.Fatalf("invalid files made requests: %v", calls())
	}
}

func TestInventoryGraphApplyEmptyEdgesIsAnExplicitReplacement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.URL.RawQuery != "dry_run=false" || body["level"] != "app" || len(body["edges"].([]any)) != 0 {
			t.Errorf("request = %s, body = %#v", r.URL, body)
		}
		_, _ = w.Write([]byte(`{"applied":true,"changes":["Reject connection"],"revision":"g2"}`))
	}))
	defer server.Close()
	invariantsEnv(t, server)
	out, err := runInvariants(newInventoryGraphCmd(), "apply", "--file", writeInvariants(t, `{"revision":"g1","level":"app","edges":[]}`), "-o", "table")
	if err != nil || !strings.Contains(out, "Applied 1 change") || !strings.Contains(out, "revision g2") {
		t.Fatalf("out = %q, err = %v", out, err)
	}
}

func TestInventoryGraphFailuresKeepTheEditingLevel(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		code   string
	}{
		{http.StatusConflict, `{"detail":"revision changed"}`, "DATA_CHANGED"},
		{http.StatusBadGateway, `{"detail":"lost response"}`, "OUTCOME_UNKNOWN"},
		{http.StatusOK, `{"revision":"g1"}`, "UNEXPECTED_ANSWER"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(test.status)
			_, _ = w.Write([]byte(test.body))
		}))
		invariantsEnv(t, server)
		_, err := runInvariants(newInventoryGraphCmd(), "apply", "--file", writeInvariants(t, `{"revision":"g1","level":"product","edges":[]}`))
		cliErr := bookDataCLIError(t, err)
		if cliErr.Code != test.code || !strings.Contains(cliErr.Suggestion, "get --level product") || strings.Contains(cliErr.Suggestion, "history revert") {
			t.Errorf("error = %#v", cliErr)
		}
		if test.code == "OUTCOME_UNKNOWN" && !strings.Contains(cliErr.Suggestion, "history --object catalog") {
			t.Errorf("unknown write outcome does not suggest catalog history: %#v", cliErr)
		}
		server.Close()
	}
}

func TestInventoryGraphGetRejectsInvalidFlagsBeforeARequest(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{})
	for _, args := range [][]string{{"get", "--level", "apps"}, {"get", "-o", "table"}} {
		_, err := runInvariants(newInventoryGraphCmd(), args...)
		if cliErr := bookDataCLIError(t, err); cliErr.ExitCode != clierrors.ExitUsageError {
			t.Errorf("args %v: %v", args, err)
		}
	}
	if len(calls()) != 0 {
		t.Errorf("bad flags made requests: %v", calls())
	}
}
