package cmd

import "testing"

// sampleFindingDetail mirrors the real /sca_findings/{id} response shape:
// evidence is nested under assessment.details.*, NOT a top-level "analyses".
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

// getSlice must accept the []map[string]any slices the result map is built
// from, not just []any, or the table renderer reads back an empty checklist.
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

// With evidence, the checklist, proofs, investigation steps, carto stack entry,
// and runtime reachability must all be pulled from assessment.details.*.
// This fails if the extraction reads a top-level "analyses" object.
func TestBuildFindingResultEvidence(t *testing.T) {
	result := buildFindingResult(sampleFindingDetail(), true)

	assessment := getMap(result, "assessment")
	checklist := getSlice(assessment, "checklist")
	// carto stack entry is prepended, so: [stack, ai_assessment item]
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

// Without evidence, the checklist entry carries no proofs/steps and there is
// no reachability block, but the base sections still populate.
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
