package cmd

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

const testAppID = "0190aaaabbbbccccddddeeeeffff0001"

func catalogFixture() map[string]any {
	return map[string]any{
		"products": []any{map[string]any{"name": "Acme", "modules": []any{"Billing"}}},
		"modules":  []any{},
		"apps": []any{map[string]any{
			"id": testAppID, "name": "Billing", "product": "Acme", "module": "Billing",
			"components": []any{"acme/api"}, "origin": "agent",
		}},
		"unassigned": []any{"acme/job"},
	}
}

func TestCatalogGetPrintsJSON(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		"GET /inventory/catalog": {{http.StatusOK, catalogFixture()}},
	})
	out, err := runInvariants(newInventoryCatalogCmd(), "get", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"unassigned"`) || len(calls()) != 1 {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestCatalogGetPrintsATable(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /inventory/catalog": {{http.StatusOK, catalogFixture()}},
	})
	out, err := runInvariants(newInventoryCatalogCmd(), "get", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Acme\n  Billing\n", testAppID, "1 components in no app"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestCatalogApplySendsTheFileAndDryRun(t *testing.T) {
	var query string
	var method string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, query = r.Method+" "+r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"applied": false, "changes": ["Rename app Billing to Payments"]}`))
	}))
	defer server.Close()
	invariantsEnv(t, server)
	file := writeInvariants(t, `{"revision": "r1", "products": [{"name": "Acme"}], "modules": [], "apps": []}`)
	out, err := runInvariants(newInventoryCatalogCmd(), "apply", "--file", file, "--dry-run", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if method != "PUT /inventory/catalog" || query != "dry_run=true" {
		t.Errorf("called %s ?%s", method, query)
	}
	if !strings.Contains(out, "Would apply 1 changes (dry run, nothing written):\n  Rename app Billing to Payments") {
		t.Errorf("unexpected output %q", out)
	}
}

func TestCatalogApplyListsEveryProblem(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"PUT /inventory/catalog": {{http.StatusUnprocessableEntity, map[string]any{
			"detail": []any{"App 'X': product 'Nope' is not listed", "App 'X' is listed twice"},
		}}},
	})
	file := writeInvariants(t, `{"revision": "r1", "products": [], "modules": [], "apps": [{"name": "X"}]}`)
	_, err := runInvariants(newInventoryCatalogCmd(), "apply", "--file", file)
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.ExitCode != clierrors.ExitUsageError {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(cliErr.Message, "product 'Nope' is not listed\n  App 'X' is listed twice") {
		t.Errorf("unexpected message %q", cliErr.Message)
	}
}

func TestCatalogApplyRefusesAFileThatIsNotAnObject(t *testing.T) {
	file := writeInvariants(t, `[1, 2]`)
	_, err := runInvariants(newInventoryCatalogCmd(), "apply", "--file", file)
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != "INVALID_CATALOG" {
		t.Fatalf("got %v", err)
	}
}

func TestCatalogNotEnabledIsAnAuthRefusal(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /inventory/catalog": {{http.StatusForbidden, map[string]any{"detail": "The inventory catalog is not enabled for this company"}}},
	})
	_, err := runInvariants(newInventoryCatalogCmd(), "get", "-o", "json")
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.ExitCode != clierrors.ExitAuthFailed {
		t.Fatalf("got %v", err)
	}
}

func TestAppsShowFindsTheAppByName(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		"GET /inventory/catalog": {{http.StatusOK, catalogFixture()}},
		"GET /inventory/apps/" + testAppID: {{http.StatusOK, map[string]any{
			"app":     map[string]any{"name": "Billing", "product": "Acme", "origin": "agent"},
			"members": []any{map[string]any{"name": "acme/api", "kind": "api"}},
			"edges":   []any{},
		}}},
	})
	out, err := runInvariants(newInventoryAppsCmd(), "show", "Billing", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "acme/api") || len(calls()) != 2 {
		t.Errorf("unexpected output %q", out)
	}
}

func TestAppsShowUnknownAppIsNotFound(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /inventory/catalog": {{http.StatusOK, catalogFixture()}},
	})
	_, err := runInvariants(newInventoryAppsCmd(), "show", "Nope")
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.ExitCode != clierrors.ExitNotFound {
		t.Fatalf("got %v", err)
	}
}

func TestComponentsListPagesThroughEveryComponent(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("after") == "" {
			_, _ = w.Write([]byte(`{"items": [{"id": "a1", "name": "acme/api", "kind": "api"}], "next": "a1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"items": [{"id": "b2", "name": "acme/job", "kind": "worker"}], "next": null}`))
	}))
	defer server.Close()
	invariantsEnv(t, server)
	out, err := runInvariants(newInventoryComponentsCmd(), "list", "--unassigned", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || !strings.Contains(queries[0], "unassigned=true") || !strings.Contains(out, "acme/job") {
		t.Errorf("queries %v, output %q", queries, out)
	}
}

func TestCatalogApplyRefusesAPartialFile(t *testing.T) {
	for _, body := range []string{`{"products": [], "modules": []}`, `{"products": [], "modules": [], "app": []}`, `{"products": [], "modules": [], "apps": [], "extra": 1}`} {
		file := writeInvariants(t, body)
		_, err := runInvariants(newInventoryCatalogCmd(), "apply", "--file", file)
		var cliErr *clierrors.CLIError
		if !errors.As(err, &cliErr) || cliErr.Code != "INVALID_CATALOG" {
			t.Errorf("%s: got %v", body, err)
		}
	}
}

func TestCatalogApplyListsValidationObjects(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"PUT /inventory/catalog": {{http.StatusUnprocessableEntity, map[string]any{
			"detail": []any{map[string]any{"loc": []any{"body", "apps", 0, "name"}, "msg": "Field required"}},
		}}},
	})
	file := writeInvariants(t, `{"revision": "r1", "products": [], "modules": [], "apps": [{}]}`)
	_, err := runInvariants(newInventoryCatalogCmd(), "apply", "--file", file)
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || !strings.Contains(cliErr.Message, "Field required") {
		t.Fatalf("got %v", err)
	}
}

func TestShowTakesADashedID(t *testing.T) {
	dashed := "0190aaaa-bbbb-cccc-dddd-eeeeffff0001"
	_, calls := reviewServer(t, map[string][]invariantReply{
		"GET /inventory/apps/" + testAppID:       {{http.StatusOK, map[string]any{"app": map[string]any{"name": "Billing"}}}},
		"GET /inventory/components/" + testAppID: {{http.StatusOK, map[string]any{"component": map[string]any{"name": "acme/api"}}}},
	})
	if _, err := runInvariants(newInventoryAppsCmd(), "show", dashed, "-o", "json"); err != nil {
		t.Fatal(err)
	}
	if _, err := runInvariants(newInventoryComponentsCmd(), "show", dashed, "-o", "json"); err != nil {
		t.Fatal(err)
	}
	if len(calls()) != 2 {
		t.Errorf("expected one call each, got %v", calls())
	}
}

func TestComponentsListStopsOnAnEchoedCursor(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"items": [{"id": "a1", "name": "acme/api"}], "next": "a1"}`))
	}))
	defer server.Close()
	invariantsEnv(t, server)
	if _, err := runInvariants(newInventoryComponentsCmd(), "list", "-o", "json"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d", calls)
	}
}

func TestComponentsListBadArgumentIsNotAFileProblem(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /inventory/components": {{http.StatusUnprocessableEntity, map[string]any{"detail": "name too long"}}},
	})
	_, err := runInvariants(newInventoryComponentsCmd(), "list", "--name", "x")
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != "INVALID_ARGUMENTS" {
		t.Fatalf("got %v", err)
	}
}

func TestErrorsArePrintedAsJSONWithJSONOutput(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /inventory/catalog": {{http.StatusForbidden, map[string]any{"detail": "not enabled"}}},
	})
	out, err := runInvariants(newInventoryCatalogCmd(), "get", "-o", "json")
	if err == nil || !strings.Contains(out, `"FORBIDDEN"`) {
		t.Fatalf("got %v, output %q", err, out)
	}
}

func TestCatalogApplyListsEveryFileProblemAtOnce(t *testing.T) {
	file := writeInvariants(t, `{"products": [], "x2": 1, "x1": 1}`)
	_, err := runInvariants(newInventoryCatalogCmd(), "apply", "--file", file)
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || !strings.Contains(cliErr.Message, `no "modules" list; no "apps" list; no "revision"; unknown keys "x1", "x2"`) {
		t.Fatalf("got %v", err)
	}
}

func TestCatalogApplyOfAStaleReadSaysToReadAgain(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"PUT /inventory/catalog": {{http.StatusConflict, map[string]any{"detail": "The catalog changed since it was read"}}},
	})
	file := writeInvariants(t, `{"revision": "r1", "products": [], "modules": [], "apps": []}`)
	_, err := runInvariants(newInventoryCatalogCmd(), "apply", "--file", file)
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != "CATALOG_CHANGED" {
		t.Fatalf("got %v", err)
	}
}

func TestComponentsListPrintsAnEmptyListAsAnArray(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /inventory/components": {{http.StatusOK, map[string]any{"items": []any{}, "next": nil}}},
	})
	out, err := runInvariants(newInventoryComponentsCmd(), "list", "--unassigned", "-o", "json")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("got %v, %q", err, out)
	}
}

func TestAFailedRunPrintsNoUsage(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /inventory/catalog": {{http.StatusForbidden, map[string]any{"detail": "not enabled"}}},
	})
	c := newInventoryCatalogCmd()
	out := &bytes.Buffer{}
	c.SetOut(out)
	c.SetErr(out)
	c.SetArgs([]string{"get", "-o", "table"})
	c.SilenceErrors = true
	if err := c.Execute(); err == nil || strings.Contains(out.String(), "Usage:") {
		t.Fatalf("got %v, output %q", err, out.String())
	}
}
