package output

import (
	"encoding/json"
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
	for kind, want := range map[string]string{
		"endpoint": "asset:endpoint:accounts",
		"object":   "asset:object:account",
		"field":    "asset:field:account.owner_id",
		"code":     "asset:code:audit-log",
	} {
		assets, listErr := workspace.baselineAssets(kind)
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(assets) != 1 || assets[0].ID != want {
			t.Fatalf("%s Assets = %#v, want %s", kind, assets, want)
		}
	}
	for kind, got := range workspace.baselineAssetCounts() {
		if got != 1 {
			t.Fatalf("%s Asset count = %d, want 1", kind, got)
		}
	}
	fields := workspace.controlledBaselineFields("asset:object:account")
	if len(fields) != 1 || fields[0].ID != "asset:field:account.owner_id" {
		t.Fatalf("object child fields = %#v", fields)
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
	state = initialBaselineWorkspaceState()
	for range 3 {
		state, _, err = workspace.reduceBaselineState(state, baselineKey{kind: baselineKeyDown})
		if err != nil {
			t.Fatal(err)
		}
	}
	state, _, err = workspace.reduceBaselineState(state, baselineKey{kind: baselineKeyEnter})
	if err != nil {
		t.Fatal(err)
	}
	frame = workspace.renderBaselineFrame(state, 120, 32)
	if !strings.Contains(frame, "Audit log") {
		t.Fatalf("uncontrolled Code Asset is not explorable:\n%s", frame)
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

func TestBaselineWorkspaceV1ExcludesAbsentControls(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(
		"..", "guardrails", "baseline", "testdata", "baseline-v1.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	assets := raw["assets"].([]any)
	links := assets[1].(map[string]any)["controls"].([]any)
	links[0].(map[string]any)["status"] = "absent"
	observations := raw["observations"].(map[string]any)["controls"].([]any)
	observations[0].(map[string]any)["status"] = "absent"
	data, err = json.Marshal(raw)
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
	if _, found := workspace.catalog.Control("control:account-owner"); found {
		t.Fatal("TUI catalog retained an absent-only control")
	}
	if _, found := workspace.catalog.Implementation("implementation:account-owner"); found {
		t.Fatal("TUI catalog retained an absent-only implementation")
	}
	if links := workspace.catalog.ProtectionsForAsset("asset:endpoint:accounts"); len(links) != 0 {
		t.Fatalf("TUI catalog retained absent links: %#v", links)
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
	for _, want := range []string{"1 endpoint group", "1 object", "1 field", "1 code asset"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary is missing %q:\n%s", want, summary)
		}
	}
}
