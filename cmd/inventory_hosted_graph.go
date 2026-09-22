package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
)

// Hosted Security Context Graphs are produced and served by Guardrails, not core.
// Core forwards the call opaquely through its /services/guardrails proxy (the same
// path the dashboard uses), so these are Guardrails' own /v1 routes under that prefix.
const guardrailsGraphPath = "/services/guardrails/v1/graph/"

// inventoryHostedGraphFetcher reads one hosted repository's mapping state. The
// list surface uses the light job route; show uses the full graph route.
type inventoryHostedGraphFetcher func(repositoryID string) (map[string]any, error)

// fetchInventoryHostedGraphJob returns the newest mapping run for the repository,
// whatever its status. Guardrails answers 404 both for "never mapped" and for a
// repository another tenant owns, so a not-mapped 404 is a normal outcome here.
func fetchInventoryHostedGraphJob(client *api.Client, repositoryID string) (map[string]any, error) {
	return client.Get(guardrailsGraphPath+repositoryID+"/job", nil)
}

// fetchInventoryHostedGraph returns the repository's current Security Context
// Graph: {commit_sha, engine_version, content_digest, graph}. nil, nil when Konvu
// holds no mapping for it.
func fetchInventoryHostedGraph(client *api.Client, repositoryID string) (map[string]any, error) {
	data, err := client.Get(guardrailsGraphPath+repositoryID, nil)
	if err != nil {
		if inventoryIsGraphNotMappedError(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// inventoryIsGraphNotMappedError distinguishes Guardrails' "no mapping" 404 from
// core's 404 for an unregistered service: the first means the repository is not
// mapped, the second means hosted graphs are unavailable, and the list must not
// print "Not mapped" for the latter.
func inventoryIsGraphNotMappedError(err error) bool {
	var apiError *api.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != 404 {
		return false
	}
	message := strings.ToLower(apiError.Message)
	return strings.Contains(message, "no mapping") || strings.Contains(message, "no mapping run")
}

type inventoryGraphFetchResult struct {
	repositoryID string
	job          map[string]any
	err          error
}

// fetchInventoryHostedGraphs reads the mapping run of every hosted repository in
// coverage, eight at a time like the Threat Profile fetch. Not-mapped repositories
// are simply absent from the result. Any other failure marks the whole graph source
// unavailable rather than failing the listing: Threat Profiles come from core and
// stay readable when Guardrails is not.
func fetchInventoryHostedGraphs(coverage map[string]any, fetch inventoryHostedGraphFetcher) map[string]any {
	var ids []string
	for _, value := range getSlice(coverage, inventoryRepositoriesKey) {
		repository, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if id := getStr(repository, "id"); id != "" {
			ids = append(ids, id)
		}
	}

	jobs := make(chan string, len(ids))
	results := make(chan inventoryGraphFetchResult, len(ids))
	for _, id := range ids {
		jobs <- id
	}
	close(jobs)
	for range min(8, len(ids)) {
		go func() {
			for id := range jobs {
				job, err := fetch(id)
				results <- inventoryGraphFetchResult{repositoryID: id, job: job, err: err}
			}
		}()
	}

	graphs := make(map[string]any)
	unavailable := false
	for range ids {
		result := <-results
		if result.err != nil {
			if inventoryIsGraphNotMappedError(result.err) {
				continue
			}
			unavailable = true
			continue
		}
		if result.job == nil {
			continue
		}
		graphs[result.repositoryID] = result.job
	}
	value := map[string]any{"graphs": graphs}
	if unavailable {
		value["unavailable"] = true
	}
	return value
}

// inventoryHostedGraphFacet turns a Guardrails mapping run into the same
// latest_run / graph pair a local repository carries, so the table and TUI read
// both facets through one code path. graph is only present once a run succeeded:
// a queued or failed run is a latest_run, not a graph.
func inventoryHostedGraphFacet(job map[string]any) map[string]any {
	if len(job) == 0 {
		return nil
	}
	status := inventoryHostedGraphStatus(getStr(job, "status"))
	run := map[string]any{
		"run_id":     getStr(job, "job_id"),
		"status":     status,
		"queued_at":  getStr(job, "queued_at"),
		"started_at": getStr(job, "started_at"),
		"problem":    "",
	}
	facet := map[string]any{"latest_run": run}
	if status == "ready" {
		facet["graph"] = map[string]any{
			"run_id":    getStr(job, "job_id"),
			"status":    "ready",
			"mapped_at": inventoryRunTimestamp(run),
		}
	}
	return facet
}

// inventoryHostedGraphStatus maps Guardrails job states onto the local vocabulary
// ("ready" is what a completed local run reports).
func inventoryHostedGraphStatus(status string) string {
	switch status {
	case "succeeded", "completed":
		return "ready"
	case "":
		return "—"
	}
	return status
}

// inventoryHostedGraphValue is the `security_context_graph` value of `inventory
// show` for a hosted repository: provenance and counts first, then the served
// graph itself (assets, controls, implementations, unresolved) so -o json is the
// full artifact. An unmapped repository says so explicitly instead of returning
// nothing, so absence is never mistaken for an empty graph.
func inventoryHostedGraphValue(response map[string]any) map[string]any {
	if len(response) == 0 {
		return map[string]any{"status": "not_mapped"}
	}
	graph := getMap(response, "graph")
	run := getMap(graph, "run")
	value := map[string]any{
		"status":         "ready",
		"run_id":         getStr(run, "id"),
		"commit":         getStr(response, "commit_sha"),
		"engine_version": getStr(response, "engine_version"),
		"content_digest": getStr(response, "content_digest"),
		"mapped_at":      inventoryRunTimestamp(run),
		"counts": map[string]any{
			"assets":          len(getSlice(graph, "assets")),
			"controls":        len(getSlice(graph, "controls")),
			"implementations": len(getSlice(graph, "implementations")),
			"unresolved":      len(getSlice(graph, "unresolved")),
		},
		"graph": graph,
	}
	return value
}

// inventoryHostedGraphDetailText renders the hosted graph block shown under a
// Threat Profile by `inventory show` and the TUI detail view. It mirrors the local
// detail's fields; the graph body is JSON-only.
func inventoryHostedGraphDetailText(value map[string]any) string {
	var text strings.Builder
	text.WriteString("\nSECURITY CONTEXT GRAPH · HOSTED\n\n")
	if getStr(value, "status") != "ready" {
		text.WriteString("This repository hasn't been mapped in Konvu yet.\n")
		text.WriteString("To inspect a local checkout in the meantime:\n\n")
		text.WriteString("  konvu inventory map <local-path>\n")
		return text.String()
	}
	commit := getStr(value, "commit")
	if len(commit) > 12 {
		commit = commit[:12]
	}
	counts := getMap(value, "counts")
	fmt.Fprintf(&text, "%-18s %s\n", "Status", "ready")
	fmt.Fprintf(&text, "%-18s %s\n", "Commit", orDefault(commit, "—"))
	fmt.Fprintf(&text, "%-18s %s\n", "Engine", orDefault(getStr(value, "engine_version"), "—"))
	mapped := getStr(value, "mapped_at")
	if mapped != "" {
		mapped = formatGuardrailsBaselineMapped(mapped)
	}
	fmt.Fprintf(&text, "%-18s %s\n", "Mapped", orDefault(mapped, "—"))
	fmt.Fprintf(
		&text,
		"%-18s %d assets · %d controls · %d relationships · %d unresolved\n",
		"Graph",
		intOf(counts["assets"]),
		intOf(counts["controls"]),
		intOf(counts["implementations"]),
		intOf(counts["unresolved"]),
	)
	text.WriteString("\nRun with -o json to get the full graph (assets, controls, implementations).\n")
	return text.String()
}
