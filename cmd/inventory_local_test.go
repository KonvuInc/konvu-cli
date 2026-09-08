package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

func TestInventoryLocalEntriesKeepLatestRunPerPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo")
	runs := []baseline.RunEntry{
		inventoryRun("new", path, baseline.StatusCompleted, "abcdef123", true),
		inventoryRun("old", path, baseline.StatusCompleted, "000000000", false),
	}

	entries := inventoryLocalEntries(runs)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	entry := entries[0].(map[string]any)
	graph := getMap(getMap(entry, "local"), "graph")
	if got := getStr(graph, "run_id"); got != "new" {
		t.Fatalf("run_id = %q, want new", got)
	}
	if got := getStr(graph, "status"); got != "ready" {
		t.Errorf("status = %q, want ready", got)
	}
	if dirty, _ := getBool(graph, "dirty"); !dirty {
		t.Error("dirty = false, want true")
	}
}

func TestInventoryLocalEntriesFallbackToLastSuccessfulGraph(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo")
	failed := inventoryRun("failed-new", path, baseline.StatusFailed, "failed123", false)
	failed.Problem = "model request failed"
	success := inventoryRun("successful-old", path, baseline.StatusCompleted, "success123", false)

	entries := inventoryLocalEntries([]baseline.RunEntry{failed, success})
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	entry := entries[0].(map[string]any)
	local := getMap(entry, "local")
	graph := getMap(local, "graph")
	latest := getMap(local, "latest_run")
	if got := getStr(graph, "run_id"); got != "successful-old" {
		t.Errorf("graph run = %q, want successful-old", got)
	}
	if got := getStr(latest, "run_id"); got != "failed-new" {
		t.Errorf("latest run = %q, want failed-new", got)
	}
	if got := getStr(latest, "status"); got != "failed" {
		t.Errorf("latest status = %q, want failed", got)
	}
	if got := getStr(latest, "problem"); got != "model request failed" {
		t.Errorf("latest problem = %q", got)
	}
}

func TestInventoryLocalEntriesKeepRepositoryWithoutSuccessfulGraph(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo")
	failed := inventoryRun("failed-only", path, baseline.StatusFailed, "failed123", false)

	entries := inventoryLocalEntries([]baseline.RunEntry{failed})
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	local := getMap(entries[0].(map[string]any), "local")
	if graph := getMap(local, "graph"); len(graph) != 0 {
		t.Errorf("failed run unexpectedly became graph: %v", graph)
	}
	if got := getStr(getMap(local, "latest_run"), "run_id"); got != "failed-only" {
		t.Errorf("latest run = %q, want failed-only", got)
	}
}

func TestInventoryLocalDetailReportsFallbackAndLatestError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo")
	failed := inventoryRun("failed-new", path, baseline.StatusFailed, "failed123", false)
	failed.Problem = "model request failed"
	success := inventoryRun("successful-old", path, baseline.StatusCompleted, "success123", false)
	entry := inventoryLocalEntries([]baseline.RunEntry{failed, success})[0].(map[string]any)

	var writer bytes.Buffer
	if err := writeInventoryLocalDetail(&writer, entry); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"successful-old", "Latest mapping attempt: failed (failed-new)", "Error: model request failed"} {
		if !strings.Contains(writer.String(), want) {
			t.Errorf("detail missing %q:\n%s", want, writer.String())
		}
	}
}

func TestRunInventoryListNeedsNoHostedAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo")
	fetched := false
	deps := inventoryListDependencies{
		listLocal: func() ([]baseline.RunEntry, error) {
			return []baseline.RunEntry{inventoryRun("run-1", path, baseline.StatusCompleted, "abcdef123", false)}, nil
		},
		hostedConfigured: func() bool { return false },
		fetchHosted: func() (map[string]any, map[string]any, error) {
			fetched = true
			return nil, nil, nil
		},
	}
	command, stdout, _ := inventoryTestCommand()
	if err := runInventoryList(command, deps, output.JSON, false); err != nil {
		t.Fatal(err)
	}
	if fetched {
		t.Fatal("hosted API was called without configured credentials")
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	sources := getMap(payload, "sources")
	if got := getStr(sources, "hosted"); got != "not_configured" {
		t.Errorf("hosted source = %q, want not_configured", got)
	}
	if got := len(getSlice(payload, "repositories")); got != 1 {
		t.Errorf("repositories = %d, want 1", got)
	}
}

func TestRunInventoryListKeepsLocalResultsWhenHostedFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo")
	deps := inventoryListDependencies{
		listLocal: func() ([]baseline.RunEntry, error) {
			return []baseline.RunEntry{inventoryRun("run-1", path, baseline.StatusCompleted, "abcdef123", false)}, nil
		},
		hostedConfigured: func() bool { return true },
		fetchHosted: func() (map[string]any, map[string]any, error) {
			return nil, nil, errors.New("offline")
		},
	}
	command, stdout, stderr := inventoryTestCommand()
	if err := runInventoryList(command, deps, output.Table, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "repo") {
		t.Errorf("stdout missing local repository:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Hosted Inventory is unavailable") {
		t.Errorf("stderr missing hosted warning:\n%s", stderr.String())
	}
}

func TestInventoryHostedEntriesUseStableSelectorAndProfileFacet(t *testing.T) {
	coverage := map[string]any{"repositories": []any{map[string]any{
		"id": "repo-1", "url": "github:acme/api",
	}}}
	profiles := map[string]any{"profiles": []any{map[string]any{
		"vcs_repository_id": "repo-1", "threat_profile_score": float64(92), "threat_profile_tier": "crown_jewel",
	}}}
	entries := inventoryHostedEntries(coverage, profiles)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	entry := entries[0].(map[string]any)
	if got := inventoryEntrySelector(entry); got != "github:acme/api" {
		t.Errorf("selector = %q", got)
	}
	profile := getMap(getMap(entry, "hosted"), "threat_profile")
	if got := intOf(profile["score"]); got != 92 {
		t.Errorf("score = %d, want 92", got)
	}
}

func TestFetchInventoryHostedProfilesIncludesProfilesOutsideRanking(t *testing.T) {
	coverage := map[string]any{"repositories": []any{
		map[string]any{"id": "ranked"},
		map[string]any{"id": "outside-ranking"},
		map[string]any{"id": "not-mapped"},
	}}
	summary := map[string]any{"top_repos": []any{map[string]any{
		"vcs_repository_id": "ranked", "threat_profile_score": float64(92),
	}}}
	profileData, err := fetchInventoryHostedProfiles(coverage, summary, func(id string) (map[string]any, error) {
		switch id {
		case "outside-ranking":
			return map[string]any{"threat_profile_score": float64(71)}, nil
		case "not-mapped":
			return nil, &api.APIError{StatusCode: 404, Message: "No threat profile for this repository"}
		default:
			return nil, fmt.Errorf("unexpected profile fetch for %q", id)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := inventoryHostedEntries(coverage, profileData)
	if got := len(entries); got != 3 {
		t.Fatalf("entries = %d, want 3", got)
	}
	profilesByID := make(map[string]map[string]any)
	for _, value := range entries {
		entry := value.(map[string]any)
		id := getStr(getMap(entry, "identity"), "hosted_repository_id")
		profilesByID[id] = getMap(getMap(entry, "hosted"), "threat_profile")
	}
	if got := intOf(profilesByID["outside-ranking"]["score"]); got != 71 {
		t.Errorf("outside-ranking score = %d, want 71", got)
	}
	if len(profilesByID["not-mapped"]) != 0 {
		t.Errorf("not-mapped repository unexpectedly has a profile: %v", profilesByID["not-mapped"])
	}
}

func TestInventoryHostedDetailIncludesProfileEvidence(t *testing.T) {
	detail := inventoryHostedDetailText(map[string]any{
		"repo_name":                 "payments-api",
		"repo_url":                  "github:acme/payments-api",
		"threat_profile_score":      float64(92),
		"threat_profile_tier":       "crown_jewel",
		"classification_category":   "internal_service",
		"classification_confidence": 0.97,
		"is_production":             true,
		"surface":                   "web",
		"updated_at":                "2026-09-07T12:00:00Z",
		"threat_profile_summary":    "Handles customer payments.",
		"domains":                   []any{"payments.example.com"},
		"attributes": map[string]any{
			"customer_data": map[string]any{
				"value":      true,
				"provenance": "konvu_sensor",
				"confidence": 0.95,
				"evidence":   []any{"Stores billing details"},
			},
		},
		"threat_profile_factors": map[string]any{"internet_exposure": float64(20)},
		"source":                 "konvu_sensor",
	})
	for _, want := range []string{
		"THREAT PROFILE · HOSTED", "Score", "92 / 100", "Crown jewel",
		"SUMMARY", "Handles customer payments.", "DOMAINS",
		"SECURITY SIGNALS", "Customer data", "Stores billing details",
		"SCORE FACTORS", "Internet exposure", "Source",
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail missing %q:\n%s", want, detail)
		}
	}
	scoreFactors := strings.Index(detail, "SCORE FACTORS")
	securitySignals := strings.Index(detail, "SECURITY SIGNALS")
	if scoreFactors < 0 || securitySignals < 0 || scoreFactors > securitySignals {
		t.Errorf("score factors should precede security signals:\n%s", detail)
	}
}

func TestInventoryLocalDirectory(t *testing.T) {
	directory := t.TempDir()
	got, err := inventoryLocalDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(directory) {
		t.Errorf("path = %q, want %q", got, filepath.Clean(directory))
	}
	if _, err := inventoryLocalDirectory("github:acme/api"); err == nil {
		t.Fatal("hosted selector unexpectedly accepted")
	}
}

func TestInventoryLooksLocalTargetKeepsMissingRelativePathsLocal(t *testing.T) {
	for _, target := range []string{"./removed-repo", "../removed-repo", `.\removed-repo`, `..\removed-repo`} {
		if !inventoryLooksLocalTarget(target) {
			t.Errorf("%q was not recognized as a local target", target)
		}
	}
	for _, target := range []string{"github:acme/api", "acme/api"} {
		if inventoryLooksLocalTarget(target) {
			t.Errorf("hosted selector %q was recognized as local", target)
		}
	}
}

func TestInventoryCommandSurface(t *testing.T) {
	children := make(map[string]bool)
	for _, child := range inventoryCmd.Commands() {
		children[child.Name()] = true
	}
	for _, name := range []string{"list", "map", "show"} {
		if !children[name] {
			t.Errorf("inventory command missing %q", name)
		}
	}
	if err := inventoryCmd.Args(inventoryCmd, []string{"unknown"}); err == nil {
		t.Error("inventory parent accepted an unexpected argument")
	}
}

func TestInventoryHelpDescribesLocalHostedAndOutputModes(t *testing.T) {
	for _, want := range []string{"Security Context Graphs", "Threat Profiles", "without a", "interactive terminal", "When piped"} {
		if !strings.Contains(inventoryCmd.Long, want) {
			t.Errorf("inventory help missing %q:\n%s", want, inventoryCmd.Long)
		}
	}
	if inventoryShowCmd.Use != "show <target>" {
		t.Errorf("inventory show usage = %q", inventoryShowCmd.Use)
	}
	list := newInventoryListCmd()
	if !strings.Contains(list.Long, "Local results never require") || !strings.Contains(list.Long, "-q") {
		t.Errorf("inventory list help is incomplete:\n%s", list.Long)
	}
	mapping := newInventoryMapCmd()
	if !strings.Contains(mapping.Long, "does not require a Konvu account") || !strings.Contains(mapping.Long, "OPENAI_API_KEY") {
		t.Errorf("inventory map help is incomplete:\n%s", mapping.Long)
	}
}

func TestShouldOpenInventoryTUI(t *testing.T) {
	if !shouldOpenInventoryTUI(true, false, false) {
		t.Error("interactive bare inventory did not select the TUI")
	}
	for _, test := range []struct {
		interactive, outputChanged, quiet bool
	}{
		{interactive: false},
		{interactive: true, outputChanged: true},
		{interactive: true, quiet: true},
	} {
		if shouldOpenInventoryTUI(test.interactive, test.outputChanged, test.quiet) {
			t.Errorf("unexpected TUI for %+v", test)
		}
	}
}

func TestInventoryTUIUsesInventoryEmptyState(t *testing.T) {
	command, stdout, _ := inventoryTestCommand()
	err := executeGuardrailsBaselineTUI(command, "", guardrailsBaselineTUIDependencies{
		list:       func() ([]baseline.RunEntry, error) { return nil, nil },
		emptyState: inventoryEmptyState,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != inventoryEmptyState {
		t.Errorf("empty state = %q, want %q", got, inventoryEmptyState)
	}
}

func TestInventoryTUIIncludesAndOpensHostedRepository(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local-repo")
	openedHosted := ""
	picks := 0
	command, _, _ := inventoryTestCommand()
	err := executeInventoryTUI(command, inventoryTUIDependencies{
		list: inventoryListDependencies{
			listLocal: func() ([]baseline.RunEntry, error) {
				return []baseline.RunEntry{inventoryRun("local-run", path, baseline.StatusCompleted, "abcdef123", false)}, nil
			},
			hostedConfigured: func() bool { return true },
			fetchHosted: func() (map[string]any, map[string]any, error) {
				return map[string]any{"repositories": []any{map[string]any{
					"id": "hosted-repo", "url": "github:acme/api",
				}}}, map[string]any{}, nil
			},
		},
		pick: func(options []output.InventoryRepositoryOption, _ int) (int, bool, error) {
			picks++
			if len(options) != 2 {
				t.Fatalf("picker options = %d, want 2", len(options))
			}
			if picks > 1 {
				return 0, false, nil
			}
			for index, option := range options {
				if option.Repository == "github:acme/api" {
					return index, true, nil
				}
			}
			t.Fatal("hosted repository missing from picker")
			return 0, false, nil
		},
		openLocal: func(string) (output.BaselineWorkspaceOutcome, error) {
			t.Fatal("opened local repository instead of hosted repository")
			return output.BaselineWorkspaceQuit, nil
		},
		openHosted: func(repositoryID, _ string) (output.BaselineWorkspaceOutcome, error) {
			openedHosted = repositoryID
			return output.BaselineWorkspaceBack, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if openedHosted != "hosted-repo" {
		t.Errorf("opened hosted repository %q, want hosted-repo", openedHosted)
	}
	if picks != 2 {
		t.Errorf("picker opened %d times, want 2 after returning from detail", picks)
	}
}

func TestInventoryNoThreatProfileErrorIsRecognized(t *testing.T) {
	apiErrors := []*api.APIError{
		{StatusCode: 404, Message: "API error: {\"detail\":\"No threat profile for this repository\"}"},
		{StatusCode: 400, Message: "No Threat Profile for this repository"},
	}
	for _, apiError := range apiErrors {
		if !inventoryIsNoThreatProfileError(apiError) {
			t.Errorf("error was not recognized: %v", apiError)
		}
	}
	if inventoryIsNoThreatProfileError(&api.APIError{
		StatusCode: 500,
		Message:    "database unavailable",
	}) {
		t.Error("unrelated API error was recognized as missing Threat Profile")
	}
}

func TestInventoryNoThreatProfileDetailIsActionable(t *testing.T) {
	detail := inventoryNoThreatProfileDetailText("github:acme/api")
	for _, want := range []string{
		"github:acme/api",
		"This repository hasn't been mapped in Konvu yet.",
		"Its Threat Profile will appear after Konvu maps it.",
		"konvu inventory map <local-path>",
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("empty detail missing %q:\n%s", want, detail)
		}
	}
	cliError := inventoryNoThreatProfileError()
	if cliError.Code != "REPOSITORY_NOT_MAPPED" || cliError.ExitCode != clierrors.ExitNotFound {
		t.Errorf("unexpected CLI error: %#v", cliError)
	}
}

func inventoryRun(id, path string, status baseline.Status, commit string, dirty bool) baseline.RunEntry {
	return baseline.RunEntry{
		ID:    id,
		Valid: true,
		Run: baseline.RunMetadata{
			ID:          id,
			Status:      status,
			StartedAt:   "2026-09-07T10:00:00Z",
			CompletedAt: "2026-09-07T10:01:00Z",
		},
		Codebase: baseline.CodebaseMetadata{
			Name: "repo",
			Path: path,
			Git:  baseline.GitMetadata{Commit: commit, Branch: "main", Dirty: dirty},
		},
		Counts: baseline.Counts{Assets: 3, Controls: 2, Implementations: 1},
	}
}

func inventoryTestCommand() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	command := &cobra.Command{}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command.SetOut(stdout)
	command.SetErr(stderr)
	return command, stdout, stderr
}
