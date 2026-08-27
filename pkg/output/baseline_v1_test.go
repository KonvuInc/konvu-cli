package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
)

func TestBaselineWorkspaceV1ConsumesProducerAssetKinds(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(
		"..", "guardrails", "baseline", "testdata", "baseline-v1.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	document, err := baseline.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := NewBaselineWorkspaceV1(document)
	if err != nil {
		t.Fatal(err)
	}
	codeAssets, err := workspace.catalog.Discoveries("code")
	if err != nil {
		t.Fatal(err)
	}
	if len(codeAssets) != 1 || codeAssets[0].ID != "asset:code:audit-log" {
		t.Fatalf("Code Assets = %#v", codeAssets)
	}
	if got := workspace.catalog.ReviewableAssetCounts()["code"]; got != 0 {
		t.Fatalf("reviewable Code Assets = %d, want unresolved Asset excluded", got)
	}

	state := initialBaselineWorkspaceState()
	if got := baselineAssetKinds[state.kindIndex]; got != "endpoint" {
		t.Fatalf("selected Asset kind = %q, want endpoint", got)
	}
	state, _, err = workspace.reduceBaselineState(state, baselineKey{kind: baselineKeyEnter})
	if err != nil {
		t.Fatal(err)
	}
	frame := workspace.renderBaselineFrame(state, 120, 32)
	for _, want := range []string{"Endpoint", "Accounts API"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("producer Asset frame is missing %q:\n%s", want, frame)
		}
	}
}

func TestBaselineWorkspaceV1UsesCanonicalIDsAndDocument(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(
		"..", "guardrails", "baseline", "testdata", "baseline-v1.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	document, err := baseline.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := NewBaselineWorkspaceV1(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := workspace.catalog.Control("control:account-owner"); !ok {
		t.Fatal("canonical Control ID was not indexed")
	}
	if _, ok := workspace.catalog.Implementation("implementation:account-owner"); !ok {
		t.Fatal("canonical Implementation ID was not indexed")
	}
	links := workspace.catalog.ProtectionsForAsset("asset:endpoint:accounts")
	if len(links) != 1 || links[0].ControlID != "control:account-owner" {
		t.Fatalf("asset Control links = %#v", links)
	}
	if _, legacy := workspace.catalog.Raw()["protections"]; legacy {
		t.Fatal("workspace Raw reintroduced a legacy top-level collection")
	}
	if workspace.catalog.Raw()["schema_version"] == nil {
		t.Fatal("workspace did not retain the canonical document")
	}
}

func TestBaselineWorkspaceV1StaticSummaryUsesPublicVocabulary(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(
		"..", "guardrails", "baseline", "testdata", "baseline-v1.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	document, err := baseline.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := NewBaselineWorkspaceV1(document)
	if err != nil {
		t.Fatal(err)
	}
	summary := strings.ToLower(workspace.StaticSummary())
	for _, legacy := range []string{"mechanism", "protection", "ctrl:", "impl:", "prot:"} {
		if strings.Contains(summary, legacy) {
			t.Fatalf("summary contains legacy vocabulary %q:\n%s", legacy, summary)
		}
	}
}
