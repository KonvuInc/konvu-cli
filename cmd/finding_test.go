package cmd

import (
	"strings"
	"testing"
)

func TestChecklistItemText(t *testing.T) {
	item := map[string]any{
		"status":              "completed",
		"description":         "Vulnerable function invoked",
		"conclusion":          "reachable",
		"investigation_steps": []any{"step one", "step two"},
		"proofs": []any{
			map[string]any{
				"file":    "sinks.py",
				"line":    float64(32),
				"code":    "def f():\n    return open(p)",
				"comment": "tainted",
			},
		},
	}
	out := checklistItemText(item)

	for _, s := range []string{"Investigation steps:", "Proofs:", "- step one", "sinks.py:32"} {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q\n---\n%s", s, out)
		}
	}
	if strings.Index(out, "Investigation steps:") > strings.Index(out, "Proofs:") {
		t.Error("investigation steps should render before proofs")
	}
	if !strings.Contains(out, "\n        def f():\n") {
		t.Errorf("first code line not indented to 8 spaces\n---\n%s", out)
	}
	if !strings.Contains(out, "\n            return open(p)\n") {
		t.Errorf("continuation code line missing the 8-space block indent\n---\n%s", out)
	}
}

func TestRuntimeReachabilityText(t *testing.T) {
	cases := []struct {
		name        string
		reach       map[string]any
		wantContain []string
		wantAbsent  []string
		wantEmpty   bool
	}{
		{
			name:      "empty map renders nothing",
			reach:     map[string]any{},
			wantEmpty: true,
		},
		{
			name:        "completed with observation",
			reach:       map[string]any{"status": "completed", "has_findings": true, "findings": map[string]any{"function": map[string]any{"last": map[string]any{"name": "pillow", "version": "8.1.0", "call_site": "sinks.py:32"}}}},
			wantContain: []string{"observed at runtime: yes", "Function observed: pillow@8.1.0 (sinks.py:32)"},
		},
		{
			name:        "completed without observation",
			reach:       map[string]any{"status": "completed", "has_findings": false},
			wantContain: []string{"observed at runtime: no"},
			wantAbsent:  []string{"observed at runtime: yes"},
		},
		{
			name:        "not installed",
			reach:       map[string]any{"status": "not_installed"},
			wantContain: []string{"sensor not installed"},
			wantAbsent:  []string{"observed at runtime"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runtimeReachabilityText(tc.reach)
			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("want empty, got %q", got)
				}
				return
			}
			for _, s := range tc.wantContain {
				if !strings.Contains(got, s) {
					t.Errorf("output missing %q\n---\n%s", s, got)
				}
			}
			for _, s := range tc.wantAbsent {
				if strings.Contains(got, s) {
					t.Errorf("output should not contain %q\n---\n%s", s, got)
				}
			}
		})
	}
}

func sampleFindingDetail() map[string]any {
	return map[string]any{
		"id":            "fid-1",
		"vulnerability": map[string]any{"id": "GHSA-x", "severity": "HIGH", "summary": "vuln"},
		"manifest_location": map[string]any{
			"vcs_repository_url": "https://github.com/acme/web",
			"location":           "package-lock.json",
		},
		"dependency": map[string]any{"name": "lodash"},
		"source":     map[string]any{"source_name": "dependabot", "identifier": "GHSA-x"},
		"assessment": map[string]any{
			"result":  "exploitable",
			"summary": "confirmed",
			"details": map[string]any{
				"ai_assessment": map[string]any{
					"checklist": map[string]any{
						"items": []any{
							map[string]any{
								"description":         "Vulnerable function is invoked",
								"status":              "confirmed",
								"check_conclusion":    "sink is reachable",
								"investigation_steps": []any{"step one", "step two"},
								"proofs": []any{
									map[string]any{
										"file":    "src/merge.ts",
										"line":    float64(42),
										"code":    "zipObjectDeep(k, req.body)",
										"comment": "tainted input",
									},
								},
							},
						},
					},
				},
				"environment_analysis": map[string]any{
					"evidence": map[string]any{"applicable": true, "summary": "Node 18 stack"},
				},
				"runtime_reachability": map[string]any{
					"status":  "completed",
					"summary": "observed executing in production",
				},
			},
		},
	}
}

func TestGetSliceAcceptsMapSlice(t *testing.T) {
	m := map[string]any{
		"anyslice": []any{1, 2, 3},
		"mapslice": []map[string]any{{"a": 1}, {"b": 2}},
		"missing":  nil,
	}
	if got := getSlice(m, "anyslice"); len(got) != 3 {
		t.Errorf("[]any: got %d, want 3", len(got))
	}
	if got := getSlice(m, "mapslice"); len(got) != 2 {
		t.Errorf("[]map[string]any: got %d, want 2", len(got))
	}
	if got := getSlice(m, "missing"); got != nil {
		t.Errorf("missing key: got %v, want nil", got)
	}
}

func TestBuildFindingResultEvidence(t *testing.T) {
	result := buildFindingResult(sampleFindingDetail(), true)

	assessment := getMap(result, "assessment")
	checklist := getSlice(assessment, "checklist")
	if len(checklist) != 2 {
		t.Fatalf("checklist items: got %d, want 2", len(checklist))
	}

	stack, _ := checklist[0].(map[string]any)
	if getStr(stack, "description") != "Vulnerability applicable to dependency stack" {
		t.Errorf("first checklist entry should be the carto stack entry, got %q", getStr(stack, "description"))
	}

	item, _ := checklist[1].(map[string]any)
	if getStr(item, "description") != "Vulnerable function is invoked" {
		t.Errorf("checklist item description = %q", getStr(item, "description"))
	}
	if steps := getSlice(item, "investigation_steps"); len(steps) != 2 {
		t.Errorf("investigation_steps: got %d, want 2", len(steps))
	}
	proofs := getSlice(item, "proofs")
	if len(proofs) != 1 {
		t.Fatalf("proofs: got %d, want 1", len(proofs))
	}
	proof, _ := proofs[0].(map[string]any)
	if getStr(proof, "file") != "src/merge.ts" || getStr(proof, "code") == "" {
		t.Errorf("proof not extracted: %+v", proof)
	}

	reach := getMap(assessment, "reachability")
	if getStr(reach, "status") != "completed" || getStr(reach, "summary") == "" {
		t.Errorf("runtime reachability not extracted: %+v", reach)
	}
}

func TestBuildFindingResultDismissal(t *testing.T) {
	detail := sampleFindingDetail()
	detail["source"] = map[string]any{
		"source_name":            "dependabot",
		"identifier":             "GHSA-x",
		"dismissed_at":           "2026-01-01T00:00:00Z",
		"dismissed_reason":       "false_positive",
		"dismissed_comment":      "not exploitable in our context",
		"dismissible_from_konvu": true,
	}

	f := getMap(buildFindingResult(detail, false), "finding")
	if getStr(f, "dismissed_at") != "2026-01-01T00:00:00Z" {
		t.Errorf("dismissed_at = %q", getStr(f, "dismissed_at"))
	}
	if getStr(f, "dismissed_reason") != "false_positive" {
		t.Errorf("dismissed_reason = %q", getStr(f, "dismissed_reason"))
	}
	if getStr(f, "dismissed_comment") != "not exploitable in our context" {
		t.Errorf("dismissed_comment = %q", getStr(f, "dismissed_comment"))
	}
	if v, _ := getBool(f, "dismissible_from_konvu"); !v {
		t.Error("dismissible_from_konvu = false, want true")
	}
}

func TestBuildFindingResultNoEvidence(t *testing.T) {
	result := buildFindingResult(sampleFindingDetail(), false)

	assessment := getMap(result, "assessment")
	checklist := getSlice(assessment, "checklist")
	if len(checklist) != 2 {
		t.Fatalf("checklist items: got %d, want 2", len(checklist))
	}
	item, _ := checklist[1].(map[string]any)
	if _, ok := item["proofs"]; ok {
		t.Error("proofs should be absent without --include evidence")
	}
	if _, ok := item["investigation_steps"]; ok {
		t.Error("investigation_steps should be absent without --include evidence")
	}
	if _, ok := assessment["reachability"]; ok {
		t.Error("reachability should be absent without --include evidence")
	}

	finding := getMap(result, "finding")
	if getStr(finding, "dependency") != "lodash" {
		t.Errorf("finding.dependency = %q, want lodash", getStr(finding, "dependency"))
	}
	vuln := getMap(result, "vulnerability")
	if getStr(vuln, "cve") != "GHSA-x" {
		t.Errorf("vulnerability.cve = %q, want GHSA-x", getStr(vuln, "cve"))
	}
}

func TestTransformFindingScanner(t *testing.T) {
	cases := []struct {
		source map[string]any
		want   string
	}{
		{map[string]any{"scanner": "vendor-sca", "source_name": "api"}, "vendor-sca"},
		{map[string]any{"source_name": "dependabot"}, "dependabot"},
	}
	for _, tc := range cases {
		got := getStr(transformFinding(map[string]any{"source": tc.source}), "scanner")
		if got != tc.want {
			t.Errorf("scanner = %q, want %q", got, tc.want)
		}
	}
}

func TestTransformFindingDismissalFields(t *testing.T) {
	got := transformFinding(map[string]any{
		"source": map[string]any{
			"dismissed_comment":      "Tracked in SEC-1234",
			"dismissible_from_konvu": true,
		},
	})

	if getStr(got, "dismissed_comment") != "Tracked in SEC-1234" {
		t.Errorf("dismissed_comment = %q", getStr(got, "dismissed_comment"))
	}
	if dismissible, _ := getBool(got, "dismissible_from_konvu"); !dismissible {
		t.Error("dismissible_from_konvu = false, want true")
	}
}

func TestParseFindingListFields(t *testing.T) {
	fields, err := parseFindingListFields("cve, dismissed_comment, dismissible_from_konvu")
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields[1] != "dismissed_comment" || fields[2] != "dismissible_from_konvu" {
		t.Fatalf("fields = %#v", fields)
	}

	if _, err := parseFindingListFields("cve,does_not_exist"); err == nil {
		t.Fatal("unknown field should return an error")
	}
}
