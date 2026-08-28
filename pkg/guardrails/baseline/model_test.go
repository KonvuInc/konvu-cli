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
	if document.SchemaVersion != 1 || document.Run.ID != "payments-api--a17c2e9--000042" {
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
			message: "must be a single safe path component",
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

func TestPartialRunKeepsDiagnosticsButCannotBuildCatalog(t *testing.T) {
	for _, status := range []Status{StatusRunning, StatusFailed, StatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			raw := canonicalRaw(t)
			run := raw["run"].(map[string]any)
			run["status"] = string(status)
			if status == StatusRunning {
				run["completed_at"] = nil
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
