package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
)

func TestBaselineWorkspaceV1ExposesCodeAssets(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(
		"..", "guardrails", "baseline", "testdata", "baseline-v1.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		t.Fatal(err)
	}
	assets := raw["assets"].([]any)
	assets[0].(map[string]any)["kind"] = "code"
	assets[0].(map[string]any)["id"] = "asset:code:user"
	raw["assets"] = assets[:1]
	observations := raw["observations"].(map[string]any)["controls"].([]any)
	for _, observation := range observations {
		observation.(map[string]any)["asset_id"] = "asset:code:user"
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	document, err := baseline.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := NewBaselineWorkspaceV1(document)
	if err != nil {
		t.Fatal(err)
	}
	if got := workspace.catalog.ReviewableAssetCounts()["code"]; got != 1 {
		t.Fatalf("reviewable Code Assets = %d, want 1", got)
	}

	state := initialBaselineWorkspaceState()
	for range 3 {
		state, _, err = workspace.reduceBaselineState(state, baselineKey{kind: baselineKeyDown})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := baselineAssetKinds[state.kindIndex]; got != "code" {
		t.Fatalf("selected Asset kind = %q, want code", got)
	}
	state, _, err = workspace.reduceBaselineState(state, baselineKey{kind: baselineKeyEnter})
	if err != nil {
		t.Fatal(err)
	}
	frame := workspace.renderBaselineFrame(state, 120, 32)
	for _, want := range []string{"Code", "User"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("Code Asset frame is missing %q:\n%s", want, frame)
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
	if _, ok := workspace.catalog.Control("control:authorize-user-read"); !ok {
		t.Fatal("canonical Control ID was not indexed")
	}
	if _, ok := workspace.catalog.Implementation("implementation:authorize-user-read"); !ok {
		t.Fatal("canonical Implementation ID was not indexed")
	}
	links := workspace.catalog.ProtectionsForAsset("asset:user")
	if len(links) != 1 || links[0].ControlID != "control:authorize-user-read" {
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
