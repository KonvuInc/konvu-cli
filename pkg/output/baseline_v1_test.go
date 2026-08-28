package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
)

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
