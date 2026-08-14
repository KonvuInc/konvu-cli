package cmd

import (
	"testing"
)

func TestRemediateHasListSubcommand(t *testing.T) {
	found := false
	for _, c := range remediateCmd.Commands() {
		if c.Name() == "list" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("remediate command missing subcommand: list")
	}
}

func TestRemediateListFlags(t *testing.T) {
	for _, name := range []string{"kind", "status", "grouping", "repo-scope", "limit", "output", "quiet"} {
		if remediateListCmd.Flags().Lookup(name) == nil {
			t.Errorf("remediate list missing flag: --%s", name)
		}
	}
}

func TestFilterPlans(t *testing.T) {
	items := []any{
		map[string]any{"id": "a", "kind": "sca", "status": "ready"},
		map[string]any{"id": "b", "kind": "sast", "status": "in_progress"},
		map[string]any{"id": "c", "kind": "sca", "status": "draft"},
	}

	all := filterPlans(items, "all", "")
	if len(all) != 3 {
		t.Errorf("kind=all: want 3, got %d", len(all))
	}

	sca := filterPlans(items, "sca", "")
	if len(sca) != 2 {
		t.Errorf("kind=sca: want 2, got %d", len(sca))
	}

	ready := filterPlans(items, "all", "ready")
	if len(ready) != 1 || ready[0].(map[string]any)["id"] != "a" {
		t.Errorf("status=ready: unexpected result %v", ready)
	}

	scaReady := filterPlans(items, "sca", "ready")
	if len(scaReady) != 1 || scaReady[0].(map[string]any)["id"] != "a" {
		t.Errorf("kind=sca status=ready: unexpected result %v", scaReady)
	}
}

func TestTransformPlanSCA(t *testing.T) {
	row := transformPlan(map[string]any{
		"id":     "plan-1",
		"kind":   "sca",
		"status": "ready",
		"packages": []any{
			map[string]any{"name": "lodash", "version": "4.17.21"},
			map[string]any{"name": "axios", "version": "1.6.0"},
		},
		"findings":          []any{map[string]any{}, map[string]any{}},
		"manifest_location": map[string]any{"vcs_repository_url": "github.com/org/repo"},
	})
	if row["target"] != "lodash@4.17.21 (+1)" {
		t.Errorf("sca target: got %q", row["target"])
	}
	if row["repository"] != "github.com/org/repo" {
		t.Errorf("sca repository: got %q", row["repository"])
	}
	if row["findings"] != "2" {
		t.Errorf("sca findings: got %q", row["findings"])
	}
}

func TestTransformPlanSAST(t *testing.T) {
	row := transformPlan(map[string]any{
		"id":                       "plan-2",
		"kind":                     "sast",
		"status":                   "ready",
		"detection_title":          "SQL Injection",
		"detection_repository_url": "github.com/org/api",
		"findings":                 []any{map[string]any{}},
	})
	if row["target"] != "SQL Injection" {
		t.Errorf("sast target: got %q", row["target"])
	}
	if row["repository"] != "github.com/org/api" {
		t.Errorf("sast repository: got %q", row["repository"])
	}
}
