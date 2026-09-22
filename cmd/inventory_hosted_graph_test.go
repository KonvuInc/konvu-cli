package cmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/api"
)

func notMappedError() error {
	return &api.APIError{StatusCode: 404, Message: `API error: {"detail":"no mapping for this repository"}`}
}

func TestInventoryGraphNotMappedErrorIsRecognized(t *testing.T) {
	if !inventoryIsGraphNotMappedError(notMappedError()) {
		t.Error("guardrails' no-mapping 404 was not recognized")
	}
	for name, err := range map[string]error{
		"unregistered service 404": &api.APIError{StatusCode: 404, Message: `API error: {"detail":"Not Found"}`},
		"server error":             &api.APIError{StatusCode: 502, Message: "no mapping for this repository"},
		"plain error":              errors.New("no mapping for this repository"),
	} {
		if inventoryIsGraphNotMappedError(err) {
			t.Errorf("%s was mistaken for a not-mapped repository", name)
		}
	}
}

func TestFetchInventoryHostedGraphsSkipsUnmappedAndFlagsUnavailable(t *testing.T) {
	coverage := map[string]any{"repositories": []any{
		map[string]any{"id": "mapped"},
		map[string]any{"id": "running"},
		map[string]any{"id": "not-mapped"},
	}}
	graphs := fetchInventoryHostedGraphs(coverage, func(id string) (map[string]any, error) {
		switch id {
		case "mapped":
			return map[string]any{"job_id": "job-1", "status": "succeeded", "started_at": "2026-09-21T10:00:00Z"}, nil
		case "running":
			return map[string]any{"job_id": "job-2", "status": "running", "queued_at": "2026-09-22T09:00:00Z"}, nil
		case "not-mapped":
			return nil, notMappedError()
		default:
			return nil, fmt.Errorf("unexpected fetch for %q", id)
		}
	})
	byID := getMap(graphs, "graphs")
	if len(byID) != 2 {
		t.Fatalf("graphs = %d, want 2 (not-mapped skipped): %v", len(byID), byID)
	}
	if _, ok := byID["not-mapped"]; ok {
		t.Error("not-mapped repository was kept")
	}
	if unavailable, _ := getBool(graphs, "unavailable"); unavailable {
		t.Error("graph source flagged unavailable without a real failure")
	}

	broken := fetchInventoryHostedGraphs(coverage, func(string) (map[string]any, error) {
		return nil, &api.APIError{StatusCode: 502, Message: "upstream unavailable"}
	})
	if unavailable, _ := getBool(broken, "unavailable"); !unavailable {
		t.Error("graph source not flagged unavailable after upstream failures")
	}
	if len(getMap(broken, "graphs")) != 0 {
		t.Error("failed fetches produced graph facets")
	}
}

func TestInventoryHostedEntriesCarryGraphFacet(t *testing.T) {
	coverage := map[string]any{"repositories": []any{
		map[string]any{"id": "mapped", "url": "github:acme/api"},
		map[string]any{"id": "running", "url": "github:acme/web"},
		map[string]any{"id": "bare", "url": "github:acme/docs"},
	}}
	graphs := map[string]any{"graphs": map[string]any{
		"mapped":  map[string]any{"job_id": "job-1", "status": "succeeded", "started_at": "2026-09-21T10:00:00Z"},
		"running": map[string]any{"job_id": "job-2", "status": "running", "queued_at": "2026-09-22T09:00:00Z"},
	}}
	entries := inventoryHostedEntries(coverage, map[string]any{}, graphs)
	hostedByID := make(map[string]map[string]any)
	for _, value := range entries {
		entry := value.(map[string]any)
		hostedByID[getStr(getMap(entry, "identity"), "hosted_repository_id")] = getMap(entry, "hosted")
	}

	mapped := hostedByID["mapped"]
	if got := getStr(getMap(mapped, "graph"), "status"); got != "ready" {
		t.Errorf("mapped graph status = %q, want ready", got)
	}
	if got := getStr(getMap(mapped, "graph"), "mapped_at"); got != "2026-09-21T10:00:00Z" {
		t.Errorf("mapped_at = %q", got)
	}
	if got := inventoryHostedGraphDisplay(mapped); got != "ready" {
		t.Errorf("mapped display = %q, want ready", got)
	}

	running := hostedByID["running"]
	if _, ok := running["graph"]; ok {
		t.Error("a running job must not be presented as a graph")
	}
	if got := getStr(getMap(running, "latest_run"), "status"); got != "running" {
		t.Errorf("running latest_run status = %q", got)
	}
	if got := inventoryHostedGraphDisplay(running); got != "running" {
		t.Errorf("running display = %q, want running", got)
	}

	if got := inventoryHostedGraphDisplay(hostedByID["bare"]); got != "Not mapped" {
		t.Errorf("bare display = %q, want Not mapped", got)
	}
}

func TestWriteInventoryListTableShowsHostedGraphState(t *testing.T) {
	entries := []any{map[string]any{
		"identity": map[string]any{"selector": "github:acme/api", "display_name": "github:acme/api"},
		"hosted": map[string]any{
			"graph":      map[string]any{"status": "ready", "mapped_at": "2026-09-21T10:00:00Z"},
			"latest_run": map[string]any{"status": "ready", "started_at": "2026-09-21T10:00:00Z"},
		},
	}}
	var writer strings.Builder
	if err := writeInventoryListTable(&writer, entries); err != nil {
		t.Fatal(err)
	}
	table := writer.String()
	if strings.Contains(table, "Not mapped") {
		t.Errorf("mapped hosted repository rendered as Not mapped:\n%s", table)
	}
	if !strings.Contains(table, "ready") || !strings.Contains(table, "2026-09-21T10:00:00Z") {
		t.Errorf("table missing hosted graph state:\n%s", table)
	}
}

func TestInventoryHostedGraphValueSummarizesServedGraph(t *testing.T) {
	value := inventoryHostedGraphValue(map[string]any{
		"commit_sha":     "0123456789abcdef",
		"engine_version": "0.4.2",
		"content_digest": "sha256:abc",
		"graph": map[string]any{
			"run":             map[string]any{"id": "run-7", "completed_at": "2026-09-21T14:00:00Z"},
			"assets":          []any{map[string]any{}, map[string]any{}},
			"controls":        []any{map[string]any{}, map[string]any{}, map[string]any{}},
			"implementations": []any{map[string]any{}},
			"unresolved":      []any{},
		},
	})
	if got := getStr(value, "status"); got != "ready" {
		t.Errorf("status = %q", got)
	}
	counts := getMap(value, "counts")
	for key, want := range map[string]int{"assets": 2, "controls": 3, "implementations": 1, "unresolved": 0} {
		if got := intOf(counts[key]); got != want {
			t.Errorf("counts[%s] = %d, want %d", key, got, want)
		}
	}
	if got := getStr(value, "mapped_at"); got != "2026-09-21T14:00:00Z" {
		t.Errorf("mapped_at = %q", got)
	}
	if len(getSlice(getMap(value, "graph"), "controls")) != 3 {
		t.Error("served graph body was not carried through for -o json")
	}

	unmapped := inventoryHostedGraphValue(nil)
	if got := getStr(unmapped, "status"); got != "not_mapped" {
		t.Errorf("unmapped status = %q, want not_mapped", got)
	}
	if _, ok := unmapped["graph"]; ok {
		t.Error("unmapped value must not carry an empty graph")
	}
}

func TestInventoryHostedGraphDetailText(t *testing.T) {
	ready := inventoryHostedGraphDetailText(map[string]any{
		"status":         "ready",
		"commit":         "0123456789abcdef",
		"engine_version": "0.4.2",
		"mapped_at":      "2026-09-21T14:00:00Z",
		"counts":         map[string]any{"assets": 412, "controls": 1108, "implementations": 980, "unresolved": 12},
	})
	for _, want := range []string{
		"SECURITY CONTEXT GRAPH · HOSTED", "0123456789ab", "0.4.2",
		"412 assets", "1108 controls", "980 relationships", "12 unresolved", "-o json",
	} {
		if !strings.Contains(ready, want) {
			t.Errorf("ready detail missing %q:\n%s", want, ready)
		}
	}
	if strings.Contains(ready, "0123456789abc") {
		t.Errorf("commit not shortened:\n%s", ready)
	}

	unmapped := inventoryHostedGraphDetailText(map[string]any{"status": "not_mapped"})
	if !strings.Contains(unmapped, "hasn't been mapped") || !strings.Contains(unmapped, "konvu inventory map") {
		t.Errorf("unmapped detail is not actionable:\n%s", unmapped)
	}
}

func TestInventoryHostedIdentityFallsBackToRepositoryID(t *testing.T) {
	repos := []any{map[string]any{"id": "repo-1", "url": "github:acme/api"}}
	data := inventoryHostedIdentity(repos, "repo-1")
	if getStr(data, "repo_url") != "github:acme/api" || getStr(data, "vcs_repository_id") != "repo-1" {
		t.Errorf("identity = %v", data)
	}
	if got := getStr(inventoryHostedIdentity(repos, "unknown"), "repo_url"); got != "" {
		t.Errorf("unknown repository got url %q", got)
	}
}
