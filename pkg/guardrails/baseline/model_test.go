package baseline

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseCanonicalBaseline(t *testing.T) {
	document := canonicalDocument(t)
	if document.SchemaVersion != 1 || document.Run.ID != "payments-api--a17c2e99--000042" {
		t.Fatalf("identity = version %d, run %q", document.SchemaVersion, document.Run.ID)
	}
	if document.Run.Status != StatusCompleted || document.Run.DurationSeconds != 12.5 {
		t.Fatalf("run = %#v", document.Run)
	}
	if document.Codebase.Name != "payments-api" || document.Codebase.Path != "/workspace/payments-api" {
		t.Fatalf("codebase = %#v", document.Codebase)
	}
	if document.Codebase.Git.Commit != "a17c2e9987654321" || document.Codebase.Git.Dirty {
		t.Fatalf("git = %#v", document.Codebase.Git)
	}
	wantCounts := Counts{
		Classes:             1,
		Routes:              1,
		Resources:           1,
		Roles:               1,
		AssetObservations:   1,
		ControlObservations: 2,
		Assets:              2,
		Controls:            1,
		Implementations:     1,
		Unresolved:          1,
	}
	if !reflect.DeepEqual(document.Counts, wantCounts) {
		t.Fatalf("counts = %#v, want %#v", document.Counts, wantCounts)
	}

	raw := document.Raw()
	if raw["extension"].(map[string]any)["kept"] != true {
		t.Fatal("unknown top-level extension was dropped")
	}
	raw["extension"].(map[string]any)["kept"] = false
	if document.Raw()["extension"].(map[string]any)["kept"] != true {
		t.Fatal("Raw exposed mutable document state")
	}
	index := document.Index()
	index["version"] = json.Number("99")
	if document.Index()["version"] == json.Number("99") {
		t.Fatal("Index exposed mutable document state")
	}
	assets, err := document.Section(CollectionAssets)
	if err != nil || len(assets) != 2 {
		t.Fatalf("assets = %#v, error = %v", assets, err)
	}
	assets[0]["name"] = "mutated"
	again, _ := document.Section(CollectionAssets)
	if again[0]["name"] == "mutated" {
		t.Fatal("Section exposed mutable document state")
	}
	if _, err := document.Section("not-a-section"); err == nil {
		t.Fatal("unknown section was accepted")
	}
}

func TestParseRejectsInvalidShapeIDsAndReferences(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]any)
		message string
		code    ErrorCode
	}{
		{
			name: "unsupported schema",
			mutate: func(raw map[string]any) {
				raw["schema_version"] = json.Number("2")
			},
			message: "unsupported schema_version 2",
			code:    ErrorUnsupportedSchema,
		},
		{
			name: "missing section",
			mutate: func(raw map[string]any) {
				delete(raw, "routes")
			},
			message: "baseline.routes: is required",
		},
		{
			name: "unsafe run id",
			mutate: func(raw map[string]any) {
				raw["run"].(map[string]any)["id"] = "../run"
			},
			message: "must match",
		},
		{
			name: "run id whitespace is not normalized",
			mutate: func(raw map[string]any) {
				raw["run"].(map[string]any)["id"] = " payments-api--a17c2e99--000042\n"
			},
			message: "must match",
		},
		{
			name: "invalid status",
			mutate: func(raw map[string]any) {
				raw["run"].(map[string]any)["status"] = "ready"
			},
			message: `unsupported value "ready"`,
		},
		{
			name: "completed timestamp required",
			mutate: func(raw map[string]any) {
				raw["run"].(map[string]any)["completed_at"] = nil
			},
			message: "is required for a completed run",
		},
		{
			name: "duration required",
			mutate: func(raw map[string]any) {
				delete(raw["run"].(map[string]any), "duration_seconds")
			},
			message: "duration_seconds: is required",
		},
		{
			name: "negative duration",
			mutate: func(raw map[string]any) {
				raw["run"].(map[string]any)["duration_seconds"] = json.Number("-0.1")
			},
			message: "must be a non-negative number",
		},
		{
			name: "relative codebase path",
			mutate: func(raw map[string]any) {
				raw["codebase"].(map[string]any)["path"] = "payments-api"
			},
			message: "must be absolute",
		},
		{
			name: "class prefix",
			mutate: func(raw map[string]any) {
				array(raw, "classes")[0].(map[string]any)["id"] = "UserService"
			},
			message: `must start with "class:"`,
		},
		{
			name: "route prefix",
			mutate: func(raw map[string]any) {
				array(raw, "routes")[0].(map[string]any)["id"] = "get-user"
			},
			message: `must start with "route:"`,
		},
		{
			name: "resource prefix",
			mutate: func(raw map[string]any) {
				array(raw, "resources")[0].(map[string]any)["id"] = "user"
			},
			message: `must start with "resource:"`,
		},
		{
			name: "role prefix",
			mutate: func(raw map[string]any) {
				array(raw, "roles")[0].(map[string]any)["id"] = "admin"
			},
			message: `must start with "role:"`,
		},
		{
			name: "asset prefix",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[0].(map[string]any)["id"] = "user"
			},
			message: `must start with "asset:"`,
		},
		{
			name: "duplicate public id",
			mutate: func(raw map[string]any) {
				assets := array(raw, "assets")
				assets[1].(map[string]any)["id"] = assets[0].(map[string]any)["id"]
			},
			message: "duplicates",
		},
		{
			name: "invalid relationship status",
			mutate: func(raw map[string]any) {
				assetLink(raw)["status"] = "unknown"
			},
			message: `unsupported value "unknown"`,
		},
		{
			name: "unknown control reference",
			mutate: func(raw map[string]any) {
				assetLink(raw)["control_id"] = "control:missing"
			},
			message: "references unknown control",
		},
		{
			name: "unknown implementation reference",
			mutate: func(raw map[string]any) {
				assetLink(raw)["implementation_ids"] = []any{"implementation:missing"}
			},
			message: "references unknown implementation",
		},
		{
			name: "unknown relationship observation",
			mutate: func(raw map[string]any) {
				assetLink(raw)["source_control_observation_ids"] = []any{"control-observation:missing"}
			},
			message: "references unknown control observation",
		},
		{
			name: "unknown control observation",
			mutate: func(raw map[string]any) {
				array(raw, "controls")[0].(map[string]any)["source_control_observation_ids"] =
					[]any{"control-observation:missing"}
			},
			message: "references unknown control observation",
		},
		{
			name: "unknown parent",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[1].(map[string]any)["parent"] = "asset:missing"
			},
			message: "references unknown asset",
		},
		{
			name: "unknown resource parent",
			mutate: func(raw map[string]any) {
				array(raw, "resources")[0].(map[string]any)["parent"] = "resource:missing"
			},
			message: "references unknown resource",
		},
		{
			name: "unknown asset source",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[0].(map[string]any)["source_ids"] = []any{"resource:missing"}
			},
			message: "references unknown Asset observation or Resource",
		},
		{
			name: "observation missing description",
			mutate: func(raw map[string]any) {
				observation := raw["observations"].(map[string]any)["controls"].([]any)[0].(map[string]any)
				delete(observation, "description")
			},
			message: "description: is required",
		},
		{
			name: "unresolved field name is canonical",
			mutate: func(raw map[string]any) {
				unresolved := array(raw, "unresolved")[0].(map[string]any)
				unresolved["observation_id"] = unresolved["control_observation_id"]
				delete(unresolved, "control_observation_id")
			},
			message: "control_observation_id: is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := canonicalRaw(t)
			test.mutate(raw)
			encoded, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Parse(encoded)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want message containing %q", err, test.message)
			}
			var baselineErr *Error
			if !errors.As(err, &baselineErr) {
				t.Fatalf("error type = %T, want *Error", err)
			}
			wantCode := test.code
			if wantCode == "" {
				wantCode = ErrorInvalidArtifact
			}
			if baselineErr.Code != wantCode {
				t.Fatalf("error code = %q, want %q", baselineErr.Code, wantCode)
			}
		})
	}
}

func TestParseRejectsBarePublicIDPrefixes(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		mutate func(map[string]any)
	}{
		{
			name: "class record",
			path: "baseline.classes[0].id",
			mutate: func(raw map[string]any) {
				array(raw, "classes")[0].(map[string]any)["id"] = "class:"
			},
		},
		{
			name: "route record",
			path: "baseline.routes[0].id",
			mutate: func(raw map[string]any) {
				array(raw, "routes")[0].(map[string]any)["id"] = "route:"
			},
		},
		{
			name: "resource record",
			path: "baseline.resources[0].id",
			mutate: func(raw map[string]any) {
				array(raw, "resources")[0].(map[string]any)["id"] = "resource:"
			},
		},
		{
			name: "role record",
			path: "baseline.roles[0].id",
			mutate: func(raw map[string]any) {
				array(raw, "roles")[0].(map[string]any)["id"] = "role:"
			},
		},
		{
			name: "asset observation record",
			path: "baseline.asset-observations[0].id",
			mutate: func(raw map[string]any) {
				raw["observations"].(map[string]any)["assets"].([]any)[0].(map[string]any)["id"] = "asset:"
			},
		},
		{
			name: "control observation record",
			path: "baseline.control-observations[0].id",
			mutate: func(raw map[string]any) {
				raw["observations"].(map[string]any)["controls"].([]any)[0].(map[string]any)["id"] =
					"control-observation:"
			},
		},
		{
			name: "asset record",
			path: "baseline.assets[0].id",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[0].(map[string]any)["id"] = "asset:"
			},
		},
		{
			name: "control record",
			path: "baseline.controls[0].id",
			mutate: func(raw map[string]any) {
				array(raw, "controls")[0].(map[string]any)["id"] = "control:"
			},
		},
		{
			name: "implementation record",
			path: "baseline.implementations[0].id",
			mutate: func(raw map[string]any) {
				array(raw, "implementations")[0].(map[string]any)["id"] = "implementation:"
			},
		},
		{
			name: "resource parent",
			path: "baseline.resources[0].parent",
			mutate: func(raw map[string]any) {
				array(raw, "resources")[0].(map[string]any)["parent"] = "resource:"
			},
		},
		{
			name: "asset parent",
			path: "baseline.assets[1].parent",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[1].(map[string]any)["parent"] = "asset:"
			},
		},
		{
			name: "asset source",
			path: "baseline.assets[0].source_ids[0]",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[0].(map[string]any)["source_ids"] = []any{"resource:"}
			},
		},
		{
			name: "control observation asset",
			path: "baseline.observations.controls[0].asset_id",
			mutate: func(raw map[string]any) {
				raw["observations"].(map[string]any)["controls"].([]any)[0].(map[string]any)["asset_id"] =
					"asset:"
			},
		},
		{
			name: "asset control",
			path: "baseline.assets[0].controls[0].control_id",
			mutate: func(raw map[string]any) {
				assetLink(raw)["control_id"] = "control:"
			},
		},
		{
			name: "asset implementation",
			path: "baseline.assets[0].controls[0].implementation_ids",
			mutate: func(raw map[string]any) {
				assetLink(raw)["implementation_ids"] = []any{"implementation:"}
			},
		},
		{
			name: "asset control observation source",
			path: "baseline.assets[0].controls[0].source_control_observation_ids",
			mutate: func(raw map[string]any) {
				assetLink(raw)["source_control_observation_ids"] = []any{"control-observation:"}
			},
		},
		{
			name: "control observation source",
			path: "baseline.controls[0].source_control_observation_ids",
			mutate: func(raw map[string]any) {
				array(raw, "controls")[0].(map[string]any)["source_control_observation_ids"] =
					[]any{"control-observation:"}
			},
		},
		{
			name: "implementation observation source",
			path: "baseline.implementations[0].source_control_observation_ids",
			mutate: func(raw map[string]any) {
				array(raw, "implementations")[0].(map[string]any)["source_control_observation_ids"] =
					[]any{"control-observation:"}
			},
		},
		{
			name: "unresolved observation",
			path: "baseline.unresolved[0].control_observation_id",
			mutate: func(raw map[string]any) {
				array(raw, "unresolved")[0].(map[string]any)["control_observation_id"] =
					"control-observation:"
			},
		},
		{
			name: "route ids",
			path: "baseline.assets[0].route_ids",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[0].(map[string]any)["route_ids"] = []any{"route:"}
			},
		},
		{
			name: "embedded route string",
			path: "baseline.assets[0].routes[0]",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[0].(map[string]any)["routes"] = []any{"route:"}
			},
		},
		{
			name: "embedded route object",
			path: "baseline.assets[0].routes[0].id",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[0].(map[string]any)["routes"] =
					[]any{map[string]any{"id": "route:"}}
			},
		},
		{
			name: "resource ids",
			path: "baseline.assets[0].resource_ids",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[0].(map[string]any)["resource_ids"] = []any{"resource:"}
			},
		},
		{
			name: "role ids",
			path: "baseline.assets[0].role_ids",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[0].(map[string]any)["role_ids"] = []any{"role:"}
			},
		},
		{
			name: "class ids",
			path: "baseline.assets[0].class_ids",
			mutate: func(raw map[string]any) {
				array(raw, "assets")[0].(map[string]any)["class_ids"] = []any{"class:"}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := canonicalRaw(t)
			test.mutate(raw)
			_, err := Parse(mustJSON(t, raw))
			if err == nil || !strings.Contains(err.Error(), test.path) ||
				!strings.Contains(err.Error(), "non-empty suffix") {
				t.Fatalf(
					"Parse error = %v, want path %q and non-empty suffix diagnostic",
					err,
					test.path,
				)
			}
		})
	}
}

func TestParseRejectsBlankCanonicalAssetFields(t *testing.T) {
	for _, field := range []string{"kind", "name", "description", "origin"} {
		t.Run(field, func(t *testing.T) {
			raw := canonicalRaw(t)
			array(raw, "assets")[0].(map[string]any)[field] = " \t\n "
			_, err := Parse(mustJSON(t, raw))
			path := "baseline.assets[0]." + field
			if err == nil || !strings.Contains(err.Error(), path) ||
				!strings.Contains(err.Error(), "non-empty string") {
				t.Fatalf("Parse error = %v, want blank field diagnostic for %q", err, path)
			}
		})
	}
}

func TestPartialRunKeepsDiagnosticsButCannotBuildCatalog(t *testing.T) {
	for _, status := range []Status{StatusRunning, StatusFailed, StatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			raw := canonicalRaw(t)
			run := raw["run"].(map[string]any)
			run["status"] = string(status)
			if status == StatusRunning {
				run["completed_at"] = nil
			} else if status == StatusFailed {
				run["error"] = "fixture failed"
			}
			assetLink(raw)["control_id"] = "control:not-produced-yet"
			encoded, _ := json.Marshal(raw)
			document, err := Parse(encoded)
			if err != nil {
				t.Fatalf("partial Parse() error = %v", err)
			}
			_, err = NewCatalog(document)
			var baselineErr *Error
			if !errors.As(err, &baselineErr) || baselineErr.Code != ErrorRunIncomplete {
				t.Fatalf("NewCatalog error = %#v", err)
			}
		})
	}
}

func TestCompletedV1RequiresFullEnvelopeAndEveryCollection(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		field  string
	}{
		{name: "run model", mutate: deleteNested("run", "model"), field: "model"},
		{name: "run estimate", mutate: deleteNested("run", "estimate"), field: "estimate"},
		{name: "run cost", mutate: deleteNested("run", "cost"), field: "cost"},
		{name: "run stages", mutate: deleteNested("run", "stages"), field: "stages"},
		{name: "run usage", mutate: deleteNested("run", "usage"), field: "usage"},
		{name: "run error", mutate: deleteNested("run", "error"), field: "error"},
		{name: "codebase summary", mutate: deleteNested("codebase", "summary"), field: "summary"},
		{name: "codebase layout", mutate: deleteNested("codebase", "layout"), field: "layout"},
		{name: "codebase git", mutate: deleteNested("codebase", "git"), field: "git"},
		{name: "codebase fingerprint", mutate: deleteNested("codebase", "source_fingerprint"), field: "source_fingerprint"},
		{name: "codebase metrics", mutate: deleteNested("codebase", "metrics"), field: "metrics"},
	}
	for _, field := range []string{"languages", "components", "frameworks", "databases", "orms", "unknowns"} {
		tests = append(tests, struct {
			name   string
			mutate func(map[string]any)
			field  string
		}{name: "codebase " + field, mutate: deleteNested("codebase", field), field: field})
	}
	for _, field := range []string{
		"index", "classes", "routes", "resources", "roles", "assets", "controls",
		"implementations", "unresolved",
	} {
		field := field
		tests = append(tests, struct {
			name   string
			mutate func(map[string]any)
			field  string
		}{
			name: "top-level " + field,
			mutate: func(raw map[string]any) {
				delete(raw, field)
			},
			field: field,
		})
	}
	for _, field := range []string{"assets", "controls"} {
		field := field
		tests = append(tests, struct {
			name   string
			mutate func(map[string]any)
			field  string
		}{
			name: "observations " + field,
			mutate: func(raw map[string]any) {
				delete(raw["observations"].(map[string]any), field)
			},
			field: field,
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := canonicalRaw(t)
			test.mutate(raw)
			assertInvalidArtifactContains(t, raw, test.field)
		})
	}
}

func TestCompletedV1RequiresExactCostUsageStageAndAssetControlFields(t *testing.T) {
	for _, field := range []string{"nanodollars", "display", "unpriced_calls"} {
		t.Run("cost "+field, func(t *testing.T) {
			raw := canonicalRaw(t)
			delete(raw["run"].(map[string]any)["cost"].(map[string]any), field)
			assertInvalidArtifactContains(t, raw, field)
		})
	}
	for _, field := range runUsageFields {
		t.Run("usage "+field, func(t *testing.T) {
			raw := canonicalRaw(t)
			delete(raw["run"].(map[string]any)["usage"].(map[string]any), field)
			assertInvalidArtifactContains(t, raw, field)
		})
	}
	for _, field := range []string{"name", "status", "summary", "duration_seconds"} {
		t.Run("stage "+field, func(t *testing.T) {
			raw := canonicalRaw(t)
			stage := raw["run"].(map[string]any)["stages"].([]any)[0].(map[string]any)
			delete(stage, field)
			assertInvalidArtifactContains(t, raw, field)
		})
	}
	for _, field := range []string{
		"control_id", "status", "description", "implementation_ids", "evidence", "checked",
		"source_control_observation_ids",
	} {
		t.Run("asset control "+field, func(t *testing.T) {
			raw := canonicalRaw(t)
			delete(assetLink(raw), field)
			assertInvalidArtifactContains(t, raw, field)
		})
	}
}

func TestCompletedV1RejectsDuplicateRelationshipsAndAllowsUnknownFields(t *testing.T) {
	raw := canonicalRaw(t)
	raw["future"] = map[string]any{"kept": true}
	raw["run"].(map[string]any)["future"] = "kept"
	if _, err := Parse(mustJSON(t, raw)); err != nil {
		t.Fatalf("additive fields were rejected: %v", err)
	}

	for _, mutate := range []func(map[string]any){
		func(value map[string]any) {
			array(value, "assets")[0].(map[string]any)["source_ids"] = []any{"resource:user", "resource:user"}
		},
		func(value map[string]any) {
			unresolved := array(value, "unresolved")[0].(map[string]any)
			value["unresolved"] = []any{unresolved, cloneMap(unresolved)}
		},
	} {
		raw = canonicalRaw(t)
		mutate(raw)
		assertInvalidArtifactContains(t, raw, "duplicate")
	}
}

func TestParseRejectsTrailingJSONAndLoadRejectsSymlink(t *testing.T) {
	data := canonicalData(t)
	if _, err := Parse(append(data, []byte("\n{}")...)); err == nil {
		t.Fatal("Parse accepted trailing JSON")
	}

	dir := t.TempDir()
	realPath := dir + "/real.json"
	linkPath := dir + "/baseline.json"
	if err := os.WriteFile(realPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Load(linkPath); err == nil {
		t.Fatal("Load accepted symlinked artifact")
	}
}

func canonicalDocument(t *testing.T) *Document {
	t.Helper()
	document, err := Parse(canonicalData(t))
	if err != nil {
		t.Fatalf("Parse canonical fixture: %v", err)
	}
	return document
}

func canonicalData(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/baseline-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func canonicalRaw(t *testing.T) map[string]any {
	t.Helper()
	return canonicalDocument(t).Raw()
}

func array(raw map[string]any, key string) []any {
	return raw[key].([]any)
}

func assetLink(raw map[string]any) map[string]any {
	return array(raw, "assets")[0].(map[string]any)["controls"].([]any)[0].(map[string]any)
}

func deleteNested(owner, field string) func(map[string]any) {
	return func(raw map[string]any) {
		delete(raw[owner].(map[string]any), field)
	}
}

func assertInvalidArtifactContains(t *testing.T, raw map[string]any, fragment string) {
	t.Helper()
	_, err := Parse(mustJSON(t, raw))
	if err == nil || !strings.Contains(err.Error(), fragment) {
		t.Fatalf("Parse error = %v, want fragment %q", err, fragment)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
