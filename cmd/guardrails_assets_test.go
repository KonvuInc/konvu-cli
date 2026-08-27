package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

type guardrailsAssetsFixture struct {
	repoPath      string
	blueprintName string
	graphRepo     string
	commit        string
	complete      bool
}

func writeGuardrailsAssetsJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", filepath.Base(path), err)
	}
}

func guardrailsAssetsBlueprintFixture(name string) map[string]any {
	repo := map[string]any{
		"name":    name,
		"layout":  "single_component",
		"summary": "Synthetic service fixture.",
	}
	return map[string]any{
		"format_version": 1,
		"repo":           repo,
		"metrics":        map[string]any{"source_files": 1, "source_lines": 10},
		"languages":      []any{},
		"components":     []any{},
		"frameworks":     []any{},
		"databases":      []any{},
		"orms":           []any{},
		"unknowns":       []any{},
	}
}

func buildGuardrailsAssetsStore(t *testing.T, fixtures []guardrailsAssetsFixture) string {
	t.Helper()
	root := t.TempDir()
	records := make([]map[string]any, 0, len(fixtures))
	for i, fixture := range fixtures {
		runDir := filepath.Join(root, "runs", string(rune('a'+i)))
		if err := os.MkdirAll(runDir, 0o700); err != nil {
			t.Fatalf("mkdir run: %v", err)
		}
		records = append(records, map[string]any{
			"repo_path":        fixture.repoPath,
			"run_dir":          runDir,
			"last_scanned":     "2026-08-20",
			"resources_count":  1,
			"mechanisms_count": 1,
		})
		if !fixture.complete {
			continue
		}
		writeGuardrailsAssetsJSON(t, filepath.Join(runDir, "run.json"), []map[string]any{
			{"stage": "normalize", "duration_seconds": 1.2, "summary": "complete"},
		})
		writeGuardrailsAssetsJSON(
			t,
			filepath.Join(runDir, "blueprint.json"),
			guardrailsAssetsBlueprintFixture(fixture.blueprintName),
		)
		source := map[string]any{
			"kind": "verified-mechanism-catalog", "observation_count": 1, "residual_asset_count": 0,
		}
		if fixture.commit != "" {
			source["commit"] = fixture.commit
		}
		writeGuardrailsAssetsJSON(t, filepath.Join(runDir, "protections.json"), map[string]any{
			"format_version": 1,
			"repo":           fixture.graphRepo,
			"source":         source,
			"assets": []any{map[string]any{
				"id": "ep:accounts", "kind": "endpoint", "name": "Account endpoints",
				"decl": "routes.go#routes", "origin": "resources", "source_ids": []any{"ep:accounts"},
				"routes": []any{map[string]any{
					"method": "GET", "path": "/accounts", "handler": "listAccounts",
					"decl": "routes.go#listAccounts", "line": 10,
				}},
			}},
			"controls": []any{map[string]any{
				"id": "ctrl:owner", "name": "Owner check", "property": "authorization",
				"description": "Requests are scoped to the account owner.",
				"asvs":        []any{"V4"}, "source_observation_ids": []any{"mech:owner"},
			}},
			"implementations": []any{map[string]any{
				"id": "impl:owner", "name": "Owner predicate", "kind": "predicate",
				"description":            "A predicate checks the active account owner.",
				"anchors":                []any{map[string]any{"decl": "auth.go#isOwner", "quote": "account.OwnerID == user.ID"}},
				"source_observation_ids": []any{"mech:owner"},
			}},
			"protections": []any{map[string]any{
				"id": "prot:accounts-owner", "asset_id": "ep:accounts", "control_id": "ctrl:owner",
				"presence": "partial", "implementation_ids": []any{"impl:owner"},
				"description": "Most account routes require the owner predicate.",
				"evidence":    []any{map[string]any{"decl": "routes.go#listAccounts", "quote": "isOwner(account, user)"}},
				"checked":     []any{}, "source_observation_ids": []any{"mech:owner"},
			}},
			"unresolved": []any{},
		})
	}
	writeGuardrailsAssetsJSON(t, filepath.Join(root, "registry.json"), records)
	return root
}

func assertGuardrailsAssetsCLIError(t *testing.T, err error, code string) *clierrors.CLIError {
	t.Helper()
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("error type = %T, want *errors.CLIError: %v", err, err)
	}
	if cliErr.Code != code {
		t.Errorf("error code = %q, want %q", cliErr.Code, code)
	}
	if cliErr.Suggestion == "" {
		t.Error("suggestion must not be empty")
	}
	switch code {
	case "GUARDRAILS_BASELINE_NOT_FOUND", "GUARDRAILS_BASELINE_UNREADABLE",
		"GUARDRAILS_BASELINE_INVALID", "GUARDRAILS_BASELINE_INCOMPLETE":
		if !strings.Contains(cliErr.Suggestion, "guardrails baseline scan") {
			t.Errorf("suggestion = %q, want baseline scan guidance", cliErr.Suggestion)
		}
	}
	return cliErr
}

func TestGuardrailsAssetsRepositoryIDsPreferParentNameAndDisambiguate(t *testing.T) {
	entries := []guardrailsAssetsEntry{
		{repoPath: "/tmp/one/acme/api"},
		{repoPath: "/tmp/two/acme/api"},
		{repoPath: "/tmp/acme/web"},
	}
	if err := assignGuardrailsAssetsRepositoryIDs(entries); err != nil {
		t.Fatalf("assign IDs: %v", err)
	}
	got := []string{entries[0].id, entries[1].id, entries[2].id}
	want := []string{"one/acme/api", "two/acme/api", "acme/web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
}

func TestGuardrailsAssetsRegistryErrorsAreActionable(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, err := loadGuardrailsAssetsRegistry(t.TempDir())
		assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_NOT_FOUND")
	})

	t.Run("empty", func(t *testing.T) {
		root := t.TempDir()
		writeGuardrailsAssetsJSON(t, filepath.Join(root, "registry.json"), []any{})
		_, err := loadGuardrailsAssetsRegistry(root)
		assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_NOT_FOUND")
	})

	t.Run("corrupt", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "registry.json"), []byte("{invalid"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := loadGuardrailsAssetsRegistry(root)
		assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_INVALID")
	})

	t.Run("missing contract field", func(t *testing.T) {
		root := t.TempDir()
		writeGuardrailsAssetsJSON(t, filepath.Join(root, "registry.json"), []any{
			map[string]any{"repo_path": "/tmp/acme/api"},
		})
		_, err := loadGuardrailsAssetsRegistry(root)
		assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_INVALID")
	})
}

func TestGuardrailsAssetsBaselineRequiresCompletionAndBothArtifacts(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{
		{repoPath: "/tmp/acme/api", complete: false},
	})
	entries, err := loadGuardrailsAssetsRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = loadGuardrailsAssetsBaseline(entries[0])
	cliErr := assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_INCOMPLETE")
	if !strings.Contains(cliErr.Message, "run.json") {
		t.Errorf("message = %q, want run.json", cliErr.Message)
	}

	runDir := entries[0].runDir
	writeGuardrailsAssetsJSON(t, filepath.Join(runDir, "run.json"), []any{})
	writeGuardrailsAssetsJSON(
		t,
		filepath.Join(runDir, "blueprint.json"),
		guardrailsAssetsBlueprintFixture("api"),
	)
	_, err = loadGuardrailsAssetsBaseline(entries[0])
	cliErr = assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_INCOMPLETE")
	if !strings.Contains(cliErr.Message, "protections.json") {
		t.Errorf("message = %q, want protections.json", cliErr.Message)
	}
}

func TestGuardrailsAssetsCompletionMarkerRejectsIdenticalReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.json")
	raw := []byte(`[{"stage":"normalize","duration_seconds":1.2,"summary":"complete"}]`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	marker, err := openGuardrailsAssetsCompletionMarker(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = marker.file.Close() }()
	if err := marker.verifyUnchanged(path); err != nil {
		t.Fatalf("unchanged marker rejected: %v", err)
	}

	replacement := filepath.Join(dir, "run.replacement.json")
	if err := os.WriteFile(replacement, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err := marker.verifyUnchanged(path); err == nil ||
		!strings.Contains(err.Error(), "completion marker changed") {
		t.Fatalf("identically replaced marker error = %v", err)
	}
}

func TestGuardrailsAssetsBaselineRejectsMalformedBlueprint(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{{
		repoPath:      "/tmp/acme/api",
		blueprintName: "api",
		graphRepo:     "acme/api",
		complete:      true,
	}})
	entries, err := loadGuardrailsAssetsRegistry(root)
	if err != nil {
		t.Fatal(err)
	}

	blueprint := guardrailsAssetsBlueprintFixture("api")
	blueprint["languages"] = []any{map[string]any{
		"name": "Go", "files": 1, "lines": -1,
	}}
	writeGuardrailsAssetsJSON(
		t,
		filepath.Join(entries[0].runDir, "blueprint.json"),
		blueprint,
	)

	_, err = loadGuardrailsAssetsBaseline(entries[0])
	cliErr := assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_INCOMPLETE")
	if !strings.Contains(cliErr.Message, "blueprint.json") ||
		!strings.Contains(cliErr.Message, "languages[0].lines") {
		t.Fatalf("message = %q, want malformed blueprint field context", cliErr.Message)
	}
}

func TestGuardrailsAssetsBaselineRejectsBlueprintExtensionOutsideV1Schema(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{{
		repoPath:      "/tmp/acme/api",
		blueprintName: "api",
		graphRepo:     "acme/api",
		complete:      true,
	}})
	entries, err := loadGuardrailsAssetsRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	blueprint := guardrailsAssetsBlueprintFixture("api")
	blueprint["repo"].(map[string]any)["commit_sha"] = "synthetic-commit"
	writeGuardrailsAssetsJSON(t, filepath.Join(entries[0].runDir, "blueprint.json"), blueprint)

	_, err = loadGuardrailsAssetsBaseline(entries[0])
	cliErr := assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_INCOMPLETE")
	if !strings.Contains(cliErr.Message, "repo.commit_sha") {
		t.Fatalf("message = %q, want unsupported blueprint field", cliErr.Message)
	}
}

func TestGuardrailsAssetsBaselineRejectsInvalidProtectionPresence(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{{
		repoPath:      "/tmp/acme/api",
		blueprintName: "api",
		graphRepo:     "acme/api",
		complete:      true,
	}})
	entries, err := loadGuardrailsAssetsRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(entries[0].runDir, "protections.json")
	var graph map[string]any
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatal(err)
	}
	graph["protections"].([]any)[0].(map[string]any)["presence"] = "unknown"
	writeGuardrailsAssetsJSON(t, path, graph)

	_, err = loadGuardrailsAssetsBaseline(entries[0])
	cliErr := assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_INCOMPLETE")
	if !strings.Contains(cliErr.Message, "presence") || !strings.Contains(cliErr.Message, "unknown") {
		t.Fatalf("message = %q, want invalid presence context", cliErr.Message)
	}
}

func TestGuardrailsAssetsBaselinePreservesRoutesAndPresence(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{
		{
			repoPath:      "/tmp/acme/api",
			blueprintName: "api",
			graphRepo:     "acme/api",
			complete:      true,
		},
	})
	entries, err := loadGuardrailsAssetsRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := loadGuardrailsAssetsBaseline(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := baseline.protections["protections"].([]any)[0].(map[string]any)["presence"]; got != "partial" {
		t.Errorf("presence = %v, want partial", got)
	}
	routes := baseline.protections["assets"].([]any)[0].(map[string]any)["routes"].([]any)
	if got := routes[0].(map[string]any)["path"]; got != "/accounts" {
		t.Errorf("route path = %v, want /accounts", got)
	}
}

func TestGuardrailsAssetsResolveAcceptsOnlyExactStoredID(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{
		{repoPath: "/tmp/acme/api", blueprintName: "service-api", graphRepo: "catalog-api", complete: true},
		{repoPath: "/tmp/acme/web", blueprintName: "service-web", graphRepo: "acme/web", complete: true},
	})
	entries, err := loadGuardrailsAssetsRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	loadCalls := 0
	load := func(entry guardrailsAssetsEntry) (*guardrailsAssetsBaseline, error) {
		loadCalls++
		return loadGuardrailsAssetsBaseline(entry)
	}
	entry, baseline, err := resolveGuardrailsAssetsEntry(entries, "acme/api", load)
	if err != nil {
		t.Fatalf("resolve exact ID: %v", err)
	}
	if entry.repoPath != "/tmp/acme/api" || baseline == nil || loadCalls != 1 {
		t.Fatalf("resolved exact ID to %#v after %d loads", entry, loadCalls)
	}

	for _, requested := range []string{"/tmp/acme/api", "api", "service-api", "catalog-api"} {
		t.Run(strings.ReplaceAll(requested, "/", "_"), func(t *testing.T) {
			_, _, err := resolveGuardrailsAssetsEntry(entries, requested, load)
			cliErr := assertGuardrailsAssetsCLIError(
				t,
				err,
				"GUARDRAILS_BASELINE_REPOSITORY_REQUIRED",
			)
			if got := cliErr.Context["available_repositories"]; !reflect.DeepEqual(
				got,
				[]string{"acme/api", "acme/web"},
			) {
				t.Fatalf("available_repositories = %#v", got)
			}
		})
	}
	if loadCalls != 1 {
		t.Fatalf("unknown selectors loaded artifacts %d times, want only the exact-ID load", loadCalls)
	}
}

func TestGuardrailsAssetsUnknownSelectorDoesNotLoadUnrelatedArtifacts(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{
		{repoPath: "/tmp/acme/api", blueprintName: "shared", graphRepo: "catalog-api", complete: true},
		{repoPath: "/tmp/acme/web", blueprintName: "shared", graphRepo: "catalog-web", complete: true},
	})
	entries, err := loadGuardrailsAssetsRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = resolveGuardrailsAssetsEntry(
		entries,
		"shared",
		func(guardrailsAssetsEntry) (*guardrailsAssetsBaseline, error) {
			t.Fatal("an unknown exact ID must not load any stored artifact")
			return nil, nil
		},
	)
	assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_REPOSITORY_REQUIRED")
}

func TestGuardrailsAssetsResolveRequiresRepoForMultipleAndListsIDs(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{
		{repoPath: "/tmp/acme/web", blueprintName: "web", graphRepo: "acme/web", complete: true},
		{repoPath: "/tmp/acme/api", blueprintName: "api", graphRepo: "acme/api", complete: true},
	})
	entries, err := loadGuardrailsAssetsRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = resolveGuardrailsAssetsEntry(entries, "", loadGuardrailsAssetsBaseline)
	cliErr := assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_REPOSITORY_REQUIRED")
	if !strings.Contains(cliErr.Message, "acme/api, acme/web") {
		t.Errorf("available IDs are not sorted in %q", cliErr.Message)
	}
	if got := cliErr.Context["available_repositories"]; !reflect.DeepEqual(got, []string{"acme/api", "acme/web"}) {
		t.Errorf("available_repositories = %#v", got)
	}
}

func TestGuardrailsAssetsResolveUnknownRepoListsAvailableIDs(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{
		{repoPath: "/tmp/acme/api", blueprintName: "api", graphRepo: "acme/api", complete: true},
		{repoPath: "/tmp/acme/web", blueprintName: "web", graphRepo: "acme/web", complete: true},
	})
	entries, err := loadGuardrailsAssetsRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = resolveGuardrailsAssetsEntry(entries, "acme/missing", loadGuardrailsAssetsBaseline)
	cliErr := assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_REPOSITORY_REQUIRED")
	if got := cliErr.Context["available_repositories"]; !reflect.DeepEqual(got, []string{"acme/api", "acme/web"}) {
		t.Fatalf("available_repositories = %#v", got)
	}
}

func TestGuardrailsAssetsErrorsStripTerminalControls(t *testing.T) {
	err := completedGuardrailsAssetsError(
		"acme/\x1b[31mapi",
		"protections.json",
		errors.New("bad\nartifact\x07"),
	)
	for _, character := range err.Message {
		if character < 0x20 || (character >= 0x7f && character <= 0x9f) {
			t.Fatalf("message contains control character %#U: %q", character, err.Message)
		}
	}
	if !strings.Contains(err.Message, "bad artifact") {
		t.Errorf("message = %q, want collapsed cause", err.Message)
	}
}

func TestGuardrailsAssetsJSONNeverPromptsAndPreservesRawGraph(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{
		{repoPath: "/tmp/acme/api", blueprintName: "api", graphRepo: "acme/api", complete: true},
	})
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	deps := guardrailsAssetsDependencies{
		interactive: func() bool { return true },
		pick: func([]guardrailsAssetsRepositoryOption, int) (int, bool, error) {
			t.Fatal("JSON must not prompt")
			return 0, false, nil
		},
		browse: func(*guardrailsAssetsBaseline) (guardrailsAssetsBrowseOutcome, error) {
			t.Fatal("JSON must not open the workspace")
			return guardrailsAssetsBrowseQuit, nil
		},
		static: func(*guardrailsAssetsBaseline) (string, error) {
			t.Fatal("JSON must not render the static summary")
			return "", nil
		},
	}
	if err := runGuardrailsAssets(cmd, root, "", "json", deps); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	baseline := payload["baseline"].(map[string]any)
	if got := baseline["protections"].([]any)[0].(map[string]any)["presence"]; got != "partial" {
		t.Errorf("presence = %v", got)
	}
	if got := baseline["assets"].([]any)[0].(map[string]any)["routes"].([]any)[0].(map[string]any)["method"]; got != "GET" {
		t.Errorf("route method = %v", got)
	}
}

func TestGuardrailsAssetsJSONLeavesStoredArtifactsUnchanged(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{{
		repoPath: "/tmp/acme/api", blueprintName: "api", graphRepo: "acme/api", complete: true,
	}})
	entries, err := loadGuardrailsAssetsRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(root, "registry.json"),
		filepath.Join(entries[0].runDir, "run.json"),
		filepath.Join(entries[0].runDir, "blueprint.json"),
		filepath.Join(entries[0].runDir, "protections.json"),
	}
	before := make(map[string][]byte, len(paths))
	for _, path := range paths {
		before[path], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	deps := guardrailsAssetsDependencies{
		interactive: func() bool { return true },
		pick: func([]guardrailsAssetsRepositoryOption, int) (int, bool, error) {
			t.Fatal("JSON must not prompt")
			return 0, false, nil
		},
		browse: func(*guardrailsAssetsBaseline) (guardrailsAssetsBrowseOutcome, error) {
			t.Fatal("JSON must not browse")
			return guardrailsAssetsBrowseQuit, nil
		},
		static: func(*guardrailsAssetsBaseline) (string, error) {
			t.Fatal("JSON must not render a static summary")
			return "", nil
		},
	}
	if err := runGuardrailsAssets(cmd, root, "acme/api", "json", deps); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		after, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(before[path], after) {
			t.Fatalf("stored artifact %s changed during read-only JSON output", filepath.Base(path))
		}
	}
}

func TestGuardrailsAssetsNonTTYUsesStaticSummary(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{
		{repoPath: "/tmp/acme/api", blueprintName: "api", graphRepo: "acme/api", complete: true},
	})
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	staticCalls := 0
	deps := guardrailsAssetsDependencies{
		interactive: func() bool { return false },
		pick: func([]guardrailsAssetsRepositoryOption, int) (int, bool, error) {
			t.Fatal("non-TTY output must not prompt")
			return 0, false, nil
		},
		browse: func(*guardrailsAssetsBaseline) (guardrailsAssetsBrowseOutcome, error) {
			t.Fatal("non-TTY output must not open the workspace")
			return guardrailsAssetsBrowseQuit, nil
		},
		static: func(baseline *guardrailsAssetsBaseline) (string, error) {
			staticCalls++
			return "summary for " + baseline.entry.id + "\n", nil
		},
	}
	if err := runGuardrailsAssets(cmd, root, "", "table", deps); err != nil {
		t.Fatal(err)
	}
	if staticCalls != 1 || stdout.String() != "summary for acme/api\n" {
		t.Fatalf("static calls/output = %d, %q", staticCalls, stdout.String())
	}
}

func TestGuardrailsAssetsInteractivePickerIsSortedAndBackPreservesSelection(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{
		{repoPath: "/tmp/acme/web", blueprintName: "service", graphRepo: "acme/web", complete: true},
		{repoPath: "/tmp/acme/api", blueprintName: "service", graphRepo: "acme/api", complete: true},
	})
	cmd := &cobra.Command{}
	pickCalls := 0
	browseCalls := 0
	deps := guardrailsAssetsDependencies{
		interactive: func() bool { return true },
		pick: func(options []guardrailsAssetsRepositoryOption, selected int) (int, bool, error) {
			pickCalls++
			if got := []string{options[0].ID, options[1].ID}; !reflect.DeepEqual(got, []string{"acme/api", "acme/web"}) {
				t.Fatalf("picker order = %v", got)
			}
			if options[0].DisplayName != "acme/api" || options[1].DisplayName != "acme/web" {
				t.Fatalf("picker labels are not unique repository IDs: %#v", options)
			}
			if pickCalls == 2 && selected != 1 {
				t.Fatalf("selection after Back = %d, want 1", selected)
			}
			return 1, true, nil
		},
		browse: func(baseline *guardrailsAssetsBaseline) (guardrailsAssetsBrowseOutcome, error) {
			browseCalls++
			if baseline.entry.id != "acme/web" {
				t.Fatalf("browsed %q, want acme/web", baseline.entry.id)
			}
			if browseCalls == 1 {
				return guardrailsAssetsBrowseBack, nil
			}
			return guardrailsAssetsBrowseQuit, nil
		},
		static: func(*guardrailsAssetsBaseline) (string, error) {
			t.Fatal("interactive output must not use static summary")
			return "", nil
		},
	}
	if err := runGuardrailsAssets(cmd, root, "", "table", deps); err != nil {
		t.Fatal(err)
	}
	if pickCalls != 2 || browseCalls != 2 {
		t.Fatalf("picker/workspace calls = %d/%d, want 2/2", pickCalls, browseCalls)
	}
}

func TestGuardrailsAssetsRepositoryOptionsUseStoredCommitAndSurfaceCorruption(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{{
		repoPath:      "/tmp/acme/api",
		blueprintName: "api",
		graphRepo:     "acme/api",
		commit:        "0123456789abcdef",
		complete:      true,
	}})
	entries, err := loadGuardrailsAssetsRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	options, err := guardrailsAssetsRepositoryOptions(entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 || options[0].Commit != "0123456789abcdef" {
		t.Fatalf("repository options = %#v", options)
	}

	if err := os.WriteFile(
		filepath.Join(entries[0].runDir, "protections.json"),
		[]byte("{invalid"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	_, err = guardrailsAssetsRepositoryOptions(entries)
	assertGuardrailsAssetsCLIError(t, err, "GUARDRAILS_BASELINE_INCOMPLETE")
}

func TestGuardrailsAssetsInteractiveLaunchAlwaysStartsWithPicker(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{{
		repoPath: "/tmp/acme/api", blueprintName: "api", graphRepo: "acme/api", complete: true,
	}})
	cmd := &cobra.Command{}
	picked := 0
	deps := guardrailsAssetsDependencies{
		interactive: func() bool { return true },
		pick: func(options []guardrailsAssetsRepositoryOption, selected int) (int, bool, error) {
			picked++
			if len(options) != 1 || selected != 0 || options[0].ID != "acme/api" {
				t.Fatalf("picker state = (%#v, %d)", options, selected)
			}
			return 0, false, nil
		},
		browse: func(*guardrailsAssetsBaseline) (guardrailsAssetsBrowseOutcome, error) {
			t.Fatal("exiting the first picker must not open a workspace")
			return guardrailsAssetsBrowseQuit, nil
		},
		static: func(*guardrailsAssetsBaseline) (string, error) {
			t.Fatal("interactive output must not render a static summary")
			return "", nil
		},
	}
	if err := runGuardrailsAssets(cmd, root, "", "table", deps); err != nil {
		t.Fatal(err)
	}
	if picked != 1 {
		t.Fatalf("picker calls = %d, want 1", picked)
	}
}

func TestGuardrailsAssetsInteractiveCancellationIsClean(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{{
		repoPath: "/tmp/acme/api", blueprintName: "api", graphRepo: "acme/api", complete: true,
	}})

	t.Run("repository picker", func(t *testing.T) {
		cmd := &cobra.Command{}
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		deps := guardrailsAssetsDependencies{
			interactive: func() bool { return true },
			pick: func([]guardrailsAssetsRepositoryOption, int) (int, bool, error) {
				return 0, false, output.ErrBaselineCancelled
			},
			browse: func(*guardrailsAssetsBaseline) (guardrailsAssetsBrowseOutcome, error) {
				t.Fatal("picker cancellation must not open a workspace")
				return guardrailsAssetsBrowseQuit, nil
			},
			static: func(*guardrailsAssetsBaseline) (string, error) {
				t.Fatal("interactive cancellation must not render a static summary")
				return "", nil
			},
		}
		if err := runGuardrailsAssets(cmd, root, "", "table", deps); err != nil {
			t.Fatal(err)
		}
		if stdout.String() != "Cancelled.\n" {
			t.Fatalf("cancellation output = %q", stdout.String())
		}
	})

	for _, requestedRepository := range []string{"", "acme/api"} {
		name := "workspace after picker"
		if requestedRepository != "" {
			name = "workspace with explicit repo"
		}
		t.Run(name, func(t *testing.T) {
			cmd := &cobra.Command{}
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			deps := guardrailsAssetsDependencies{
				interactive: func() bool { return true },
				pick: func([]guardrailsAssetsRepositoryOption, int) (int, bool, error) {
					if requestedRepository != "" {
						t.Fatal("--repo must bypass the picker")
					}
					return 0, true, nil
				},
				browse: func(*guardrailsAssetsBaseline) (guardrailsAssetsBrowseOutcome, error) {
					return guardrailsAssetsBrowseCancelled, nil
				},
				static: func(*guardrailsAssetsBaseline) (string, error) {
					t.Fatal("interactive cancellation must not render a static summary")
					return "", nil
				},
			}
			if err := runGuardrailsAssets(cmd, root, requestedRepository, "table", deps); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != "Cancelled.\n" {
				t.Fatalf("cancellation output = %q", stdout.String())
			}
		})
	}
}

func TestGuardrailsAssetsExplicitRepoBypassesInteractivePicker(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{
		{repoPath: "/tmp/acme/api", blueprintName: "api", graphRepo: "acme/api", complete: true},
		{repoPath: "/tmp/acme/web", blueprintName: "web", graphRepo: "acme/web", complete: true},
	})
	cmd := &cobra.Command{}
	browsed := ""
	deps := guardrailsAssetsDependencies{
		interactive: func() bool { return true },
		pick: func([]guardrailsAssetsRepositoryOption, int) (int, bool, error) {
			t.Fatal("--repo must bypass the picker")
			return 0, false, nil
		},
		browse: func(baseline *guardrailsAssetsBaseline) (guardrailsAssetsBrowseOutcome, error) {
			browsed = baseline.entry.id
			return guardrailsAssetsBrowseBack, nil
		},
		static: func(*guardrailsAssetsBaseline) (string, error) { return "", nil },
	}
	if err := runGuardrailsAssets(cmd, root, "acme/web", "table", deps); err != nil {
		t.Fatal(err)
	}
	if browsed != "acme/web" {
		t.Fatalf("browsed = %q, want acme/web", browsed)
	}
}

func TestGuardrailsAssetsCommitUsesOnlyStoredArtifactMetadata(t *testing.T) {
	baseline := &guardrailsAssetsBaseline{
		protections: map[string]any{
			"source": map[string]any{"commit": "0123456789abcdef"},
		},
		blueprint: map[string]any{},
	}
	if got := guardrailsAssetsCommit(baseline); got != "0123456789abcdef" {
		t.Fatalf("commit = %q", got)
	}
	baseline.protections = map[string]any{}
	if got := guardrailsAssetsCommit(baseline); got != "" {
		t.Fatalf("commit without stored metadata = %q, want empty", got)
	}
}

func TestGuardrailsAssetsRejectsUnknownFormat(t *testing.T) {
	_, err := guardrailsAssetsOutputFormat("yaml")
	assertGuardrailsAssetsCLIError(t, err, "INVALID_ARGUMENTS")
}

func TestGuardrailsAssetsCommandContract(t *testing.T) {
	if guardrailsAssetsCmd.Short != "Browse the assets and security controls in a stored baseline" {
		t.Errorf("Short = %q", guardrailsAssetsCmd.Short)
	}
	if guardrailsAssetsCmd.Use != "assets" || guardrailsAssetsCmd.Run == nil {
		t.Errorf("command must be a no-argument runnable command: Use=%q", guardrailsAssetsCmd.Use)
	}
	for _, name := range []string{"repo", "output"} {
		if guardrailsAssetsCmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s", name)
		}
	}
	if shorthand := guardrailsAssetsCmd.Flags().ShorthandLookup("o"); shorthand == nil || shorthand.Name != "output" {
		t.Error("missing -o shorthand for --output")
	}
}

func TestGuardrailsAssetsCommandReportsStructuredErrorsAndExitCode(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().String("output", "json", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	code := executeGuardrailsAssetsCommand(
		cmd,
		[]string{"unexpected"},
		func() (string, error) {
			t.Fatal("invalid arguments must fail before resolving the store")
			return "", nil
		},
		guardrailsAssetsDependencies{},
	)
	if code != clierrors.ExitUsageError {
		t.Fatalf("exit code = %d, want %d", code, clierrors.ExitUsageError)
	}
	if stderr.Len() != 0 {
		t.Fatalf("JSON error wrote to stderr: %q", stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode structured error: %v\n%s", err, stdout.String())
	}
	errorObject := payload["error"].(map[string]any)
	if errorObject["code"] != "INVALID_ARGUMENTS" || errorObject["suggestion"] == "" {
		t.Errorf("structured error = %#v", errorObject)
	}
}

func TestGuardrailsAssetsCommandReportsHumanBaselineSuggestion(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().String("output", "table", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	code := executeGuardrailsAssetsCommand(
		cmd,
		nil,
		func() (string, error) { return t.TempDir(), nil },
		guardrailsAssetsDependencies{},
	)
	if code != clierrors.ExitNotFound {
		t.Fatalf("exit code = %d, want %d", code, clierrors.ExitNotFound)
	}
	if stdout.Len() != 0 {
		t.Fatalf("human error wrote to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Error: no stored guardrails baselines were found") ||
		!strings.Contains(stderr.String(), guardrailsAssetsScanSuggestion) {
		t.Errorf("human error = %q", stderr.String())
	}
}

func TestGuardrailsAssetsCommandIncludesAvailableRepositoriesInJSONError(t *testing.T) {
	root := buildGuardrailsAssetsStore(t, []guardrailsAssetsFixture{
		{repoPath: "/tmp/acme/web", blueprintName: "web", graphRepo: "acme/web", complete: true},
		{repoPath: "/tmp/acme/api", blueprintName: "api", graphRepo: "acme/api", complete: true},
	})
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", "", "")
	cmd.Flags().String("output", "json", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	code := executeGuardrailsAssetsCommand(
		cmd,
		nil,
		func() (string, error) { return root, nil },
		guardrailsAssetsDependencies{},
	)
	if code != clierrors.ExitUsageError {
		t.Fatalf("exit code = %d, want %d", code, clierrors.ExitUsageError)
	}
	if stderr.Len() != 0 {
		t.Fatalf("JSON error wrote to stderr: %q", stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode structured error: %v\n%s", err, stdout.String())
	}
	errorObject := payload["error"].(map[string]any)
	contextObject := errorObject["context"].(map[string]any)
	if got := contextObject["available_repositories"]; !reflect.DeepEqual(got, []any{"acme/api", "acme/web"}) {
		t.Errorf("available_repositories = %#v", got)
	}
}
