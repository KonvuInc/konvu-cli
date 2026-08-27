package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	baselinemodel "github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
)

func TestGuardrailsBaselineSelectorRejectsConflictingSelectors(t *testing.T) {
	_, err := guardrailsBaselineSelector("run-one", "payments-api")
	assertGuardrailsBaselineCLIError(t, err, "INVALID_ARGUMENTS", clierrors.ExitUsageError)
}

func TestWriteGuardrailsBaselineListRunsKeepsInvalidAndIncompleteRuns(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: "payments-api--aaaaaaa--000001", status: "completed", repository: "payments-api",
		path: "/code/payments-api", started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:00:12Z",
	})
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: "orders-api--bbbbbbb--000002", status: "failed", repository: "orders-api",
		path: "/code/orders-api", started: "2026-08-27T11:00:00Z", completed: "2026-08-27T11:00:05Z",
	})
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: "payments-api--ddddddd--000004", status: "cancelled", repository: "payments-api",
		path: "/code/payments-api", started: "2026-08-27T12:00:00Z", completed: "2026-08-27T12:00:03Z",
	})
	invalidDir := filepath.Join(store.Root, "broken--ccccccc--000003")
	if err := os.MkdirAll(invalidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidDir, "baseline.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidDir, "run.log"), []byte("broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var jsonOutput bytes.Buffer
	err := writeGuardrailsBaselineList(
		&jsonOutput,
		store,
		"runs",
		baselinemodel.Selector{},
		output.JSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Runs []map[string]any `json:"runs"`
	}
	if err := json.Unmarshal(jsonOutput.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Runs) != 4 {
		t.Fatalf("runs = %d, want 4: %s", len(payload.Runs), jsonOutput.String())
	}
	statuses := make(map[string]bool)
	for _, run := range payload.Runs {
		statuses[fmt.Sprint(run["status"])] = true
	}
	for _, status := range []string{"completed", "failed", "cancelled", "invalid"} {
		if !statuses[status] {
			t.Errorf("missing %s run: %s", status, jsonOutput.String())
		}
	}

	var repositoryOutput bytes.Buffer
	if err := writeGuardrailsBaselineList(
		&repositoryOutput,
		store,
		"runs",
		baselinemodel.Selector{Repository: "payments-api"},
		output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	payload.Runs = nil
	if err := json.Unmarshal(repositoryOutput.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Runs) != 2 {
		t.Fatalf("payments-api runs = %d, want completed and cancelled: %s", len(payload.Runs), repositoryOutput.String())
	}

	var tableOutput bytes.Buffer
	if err := writeGuardrailsBaselineList(
		&tableOutput,
		store,
		"runs",
		baselinemodel.Selector{},
		output.Table,
	); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"payments-api", "orders-api", "inval", "Assets", "Implementations"} {
		if !strings.Contains(tableOutput.String(), expected) {
			t.Errorf("run table missing %q:\n%s", expected, tableOutput.String())
		}
	}
}

func TestWriteGuardrailsBaselineListCollectionUsesCanonicalData(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	runID := "payments-api--aaaaaaa--000001"
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: runID, status: "completed", repository: "payments-api", path: "/code/payments-api",
		started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:00:12Z",
	})

	outside := t.TempDir()
	oldDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDirectory) })

	var jsonOutput bytes.Buffer
	if err := writeGuardrailsBaselineList(
		&jsonOutput,
		store,
		"assets",
		baselinemodel.Selector{RunID: runID},
		output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(jsonOutput.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	var assets []map[string]any
	if err := json.Unmarshal(payload["assets"], &assets); err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 || assets[0]["id"] != "asset:user" {
		t.Fatalf("assets = %#v", assets)
	}
	if _, ok := assets[0]["controls"]; !ok {
		t.Fatalf("asset record is not lossless: %#v", assets[0])
	}

	var tableOutput bytes.Buffer
	if err := writeGuardrailsBaselineList(
		&tableOutput,
		store,
		"controls",
		baselinemodel.Selector{Repository: "/code/payments-api"},
		output.Table,
	); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"control:authorize-user-read", "authorization", "Implementations"} {
		if !strings.Contains(tableOutput.String(), expected) {
			t.Errorf("control table missing %q:\n%s", expected, tableOutput.String())
		}
	}
}

func TestWriteGuardrailsBaselineListValidatesCollectionAndRunLifecycle(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	failedID := "payments-api--aaaaaaa--000001"
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: failedID, status: "failed", repository: "payments-api", path: "/code/payments-api",
		started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:00:12Z",
	})

	err := writeGuardrailsBaselineList(
		&bytes.Buffer{}, store, "mechanisms", baselinemodel.Selector{}, output.JSON,
	)
	assertGuardrailsBaselineCLIError(t, err, "INVALID_ARGUMENTS", clierrors.ExitUsageError)
	err = writeGuardrailsBaselineList(
		&bytes.Buffer{}, store, "assets", baselinemodel.Selector{RunID: failedID}, output.JSON,
	)
	assertGuardrailsBaselineCLIError(t, err, "GUARDRAILS_BASELINE_INCOMPLETE", clierrors.ExitGeneralError)
	var filtered bytes.Buffer
	err = writeGuardrailsBaselineList(
		&filtered, store, "runs", baselinemodel.Selector{RunID: failedID}, output.JSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filtered.String(), failedID) || !strings.Contains(filtered.String(), `"status": "failed"`) {
		t.Fatalf("filtered runs = %s", filtered.String())
	}
	filtered.Reset()
	err = writeGuardrailsBaselineList(
		&filtered, store, "runs", baselinemodel.Selector{Repository: "payments-api"}, output.JSON,
	)
	if err != nil || !strings.Contains(filtered.String(), failedID) {
		t.Fatalf("repository-filtered runs = %s, error = %v", filtered.String(), err)
	}
	err = writeGuardrailsBaselineList(
		&bytes.Buffer{}, store, "runs", baselinemodel.Selector{RunID: "missing"}, output.JSON,
	)
	assertGuardrailsBaselineCLIError(t, err, "GUARDRAILS_BASELINE_NOT_FOUND", clierrors.ExitNotFound)
}

func TestWriteGuardrailsBaselineListAssetObservationsDoesNotChangeAssetLookup(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	runID := "payments-api--aaaaaaa--000001"
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: runID, status: "completed", repository: "payments-api", path: "/code/payments-api",
		started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:00:12Z",
	})

	var observationsJSON bytes.Buffer
	if err := writeGuardrailsBaselineList(
		&observationsJSON,
		store,
		"asset-observations",
		baselinemodel.Selector{RunID: runID},
		output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		AssetObservations []map[string]any `json:"asset_observations"`
	}
	if err := json.Unmarshal(observationsJSON.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.AssetObservations) != 1 || payload.AssetObservations[0]["id"] != "asset:user" {
		t.Fatalf("asset observations = %#v", payload.AssetObservations)
	}

	var assetJSON bytes.Buffer
	if err := writeGuardrailsBaselineShow(
		&assetJSON,
		store,
		"asset:user",
		baselinemodel.Selector{RunID: runID},
		false,
		output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	var asset map[string]any
	if err := json.Unmarshal(assetJSON.Bytes(), &asset); err != nil {
		t.Fatal(err)
	}
	if asset["kind"] != "object" || asset["controls"] == nil {
		t.Fatalf("lookup returned observation instead of normalized asset: %#v", asset)
	}
}

func TestWriteGuardrailsBaselineShowRunRecordAndLog(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	completedID := "payments-api--aaaaaaa--000001"
	failedID := "payments-api--aaaaaaa--000002"
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: completedID, status: "completed", repository: "payments-api", path: "/code/payments-api",
		started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:00:12Z",
	})
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: failedID, status: "failed", repository: "payments-api", path: "/code/payments-api",
		started: "2026-08-27T11:00:00Z", completed: "2026-08-27T11:00:05Z", log: "step: index\nerror: model failed\n",
	})

	var runJSON bytes.Buffer
	if err := writeGuardrailsBaselineShow(
		&runJSON, store, completedID, baselinemodel.Selector{}, false, output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(runJSON.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["schema_version"] != float64(1) || raw["assets"] == nil || raw["run"] == nil {
		t.Fatalf("show run did not return exact baseline object: %s", runJSON.String())
	}
	if _, wrapped := raw["baseline"]; wrapped {
		t.Fatalf("show run unexpectedly wrapped baseline: %s", runJSON.String())
	}

	var recordJSON bytes.Buffer
	if err := writeGuardrailsBaselineShow(
		&recordJSON,
		store,
		"implementation:authorize-user-read",
		baselinemodel.Selector{RunID: completedID},
		false,
		output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	var implementation map[string]any
	if err := json.Unmarshal(recordJSON.Bytes(), &implementation); err != nil {
		t.Fatal(err)
	}
	if implementation["id"] != "implementation:authorize-user-read" || implementation["locations"] == nil {
		t.Fatalf("implementation = %#v", implementation)
	}

	var runTable bytes.Buffer
	if err := writeGuardrailsBaselineShow(
		&runTable, store, failedID, baselinemodel.Selector{}, false, output.Table,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runTable.String(), "failed") || !strings.Contains(runTable.String(), failedID) {
		t.Fatalf("failed run summary:\n%s", runTable.String())
	}

	var logOutput bytes.Buffer
	if err := writeGuardrailsBaselineShow(
		&logOutput, store, failedID, baselinemodel.Selector{}, true, output.Table,
	); err != nil {
		t.Fatal(err)
	}
	if logOutput.String() != "step: index\nerror: model failed\n" {
		t.Fatalf("log output = %q", logOutput.String())
	}
}

func TestWriteGuardrailsBaselineShowRejectsIncompleteRecordAndMissingRecord(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	failedID := "payments-api--aaaaaaa--000001"
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: failedID, status: "failed", repository: "payments-api", path: "/code/payments-api",
		started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:00:12Z",
	})
	err := writeGuardrailsBaselineShow(
		&bytes.Buffer{},
		store,
		"asset:user",
		baselinemodel.Selector{RunID: failedID},
		false,
		output.JSON,
	)
	assertGuardrailsBaselineCLIError(t, err, "GUARDRAILS_BASELINE_INCOMPLETE", clierrors.ExitGeneralError)

	completedID := "payments-api--aaaaaaa--000002"
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: completedID, status: "completed", repository: "payments-api", path: "/code/payments-api",
		started: "2026-08-27T11:00:00Z", completed: "2026-08-27T11:00:12Z",
	})
	err = writeGuardrailsBaselineShow(
		&bytes.Buffer{},
		store,
		"asset:missing",
		baselinemodel.Selector{RunID: completedID},
		false,
		output.JSON,
	)
	assertGuardrailsBaselineCLIError(t, err, "GUARDRAILS_BASELINE_RECORD_NOT_FOUND", clierrors.ExitNotFound)

	err = writeGuardrailsBaselineShow(
		&bytes.Buffer{},
		store,
		failedID,
		baselinemodel.Selector{RunID: completedID},
		true,
		output.Table,
	)
	assertGuardrailsBaselineCLIError(t, err, "INVALID_ARGUMENTS", clierrors.ExitUsageError)
}

func TestWriteGuardrailsBaselineShowInvalidRunDiagnosticsAndSafeLog(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	invalidID := "payments-api--aaaaaaa--000001"
	directory := filepath.Join(store.Root, invalidID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "baseline.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "run.log"), []byte("artifact parse failed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var tableOutput bytes.Buffer
	if err := writeGuardrailsBaselineShow(
		&tableOutput, store, invalidID, baselinemodel.Selector{}, false, output.Table,
	); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{invalidID, "invalid", "problem", "is required"} {
		if !strings.Contains(tableOutput.String(), expected) {
			t.Errorf("invalid diagnostic missing %q:\n%s", expected, tableOutput.String())
		}
	}

	var logOutput bytes.Buffer
	if err := writeGuardrailsBaselineShow(
		&logOutput, store, invalidID, baselinemodel.Selector{}, true, output.Table,
	); err != nil {
		t.Fatal(err)
	}
	if logOutput.String() != "artifact parse failed\n" {
		t.Fatalf("invalid log = %q", logOutput.String())
	}

	err := writeGuardrailsBaselineShow(
		&bytes.Buffer{}, store, invalidID, baselinemodel.Selector{}, false, output.JSON,
	)
	assertGuardrailsBaselineCLIError(t, err, "GUARDRAILS_BASELINE_INVALID", clierrors.ExitGeneralError)

	outsideLog := filepath.Join(t.TempDir(), "outside.log")
	if err := os.WriteFile(outsideLog, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unsafeDir := t.TempDir()
	unsafeLog := filepath.Join(unsafeDir, "run.log")
	if err := os.Symlink(outsideLog, unsafeLog); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err = readGuardrailsBaselineLog(baselinemodel.RunEntry{Dir: unsafeDir, LogPath: unsafeLog})
	if err == nil || !strings.Contains(err.Error(), "non-symlinked") {
		t.Fatalf("symlinked log error = %v", err)
	}
}

func TestGuardrailsBaselineLocationProducerShapes(t *testing.T) {
	tests := []struct {
		name  string
		value map[string]any
		want  string
	}{
		{name: "decl string", value: map[string]any{"decl": "src/auth.rs:12"}, want: "src/auth.rs:12"},
		{
			name: "location object",
			value: map[string]any{
				"location": map[string]any{"path": "internal/auth.go", "line": json.Number("42")},
			},
			want: "internal/auth.go:42",
		},
		{
			name:  "decl object",
			value: map[string]any{"decl": map[string]any{"path": "app/models.py", "line": float64(7)}},
			want:  "app/models.py:7",
		},
		{
			name:  "module and line",
			value: map[string]any{"module": "payments.auth", "line": json.Number("9")},
			want:  "payments.auth:9",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := guardrailsBaselineLocation(test.value); got != test.want {
				t.Fatalf("location = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWriteGuardrailsBaselineExplainReturnsRelationships(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	runID := "payments-api--aaaaaaa--000001"
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: runID, status: "completed", repository: "payments-api", path: "/code/payments-api",
		started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:00:12Z",
	})

	var jsonOutput bytes.Buffer
	if err := writeGuardrailsBaselineExplain(
		&jsonOutput,
		store,
		"asset:user",
		baselinemodel.Selector{Repository: "payments-api"},
		output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Record struct {
			Collection string         `json:"collection"`
			Data       map[string]any `json:"data"`
		} `json:"record"`
		Related []struct {
			Collection string         `json:"collection"`
			Record     map[string]any `json:"record"`
		} `json:"related"`
	}
	if err := json.Unmarshal(jsonOutput.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Record.Collection != "assets" || payload.Record.Data["id"] != "asset:user" {
		t.Fatalf("record = %#v", payload.Record)
	}
	relatedIDs := make(map[string]bool)
	for _, related := range payload.Related {
		relatedIDs[fmt.Sprint(related.Record["id"])] = true
	}
	for _, id := range []string{
		"asset:user-email",
		"resource:user",
		"control:authorize-user-read",
		"implementation:authorize-user-read",
		"control-observation:authorize-user-read",
	} {
		if !relatedIDs[id] {
			t.Errorf("explain missing related %s: %s", id, jsonOutput.String())
		}
	}

	var tableOutput bytes.Buffer
	if err := writeGuardrailsBaselineExplain(
		&tableOutput,
		store,
		"control:authorize-user-read",
		baselinemodel.Selector{RunID: runID},
		output.Table,
	); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Control control:authorize-user-read", "Related", "asset:user", "implementation:authorize-user-read"} {
		if !strings.Contains(tableOutput.String(), expected) {
			t.Errorf("explain table missing %q:\n%s", expected, tableOutput.String())
		}
	}
}

func TestWriteGuardrailsBaselineOutputReturnsCLIError(t *testing.T) {
	err := writeGuardrailsBaselineOutput(failingGuardrailsBaselineWriter{}, "value")
	assertGuardrailsBaselineCLIError(t, err, "GUARDRAILS_BASELINE_OUTPUT_FAILED", clierrors.ExitGeneralError)
}

type failingGuardrailsBaselineWriter struct{}

func (failingGuardrailsBaselineWriter) Write([]byte) (int, error) {
	return 0, errors.New("fixture write failed")
}

type guardrailsBaselineCommandRun struct {
	id         string
	status     string
	repository string
	path       string
	started    string
	completed  string
	log        string
}

func newGuardrailsBaselineCommandStore(t *testing.T) baselinemodel.Store {
	t.Helper()
	root := filepath.Join(t.TempDir(), "baselines")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return baselinemodel.Store{Root: root}
}

func writeGuardrailsBaselineCommandRun(
	t *testing.T,
	store baselinemodel.Store,
	run guardrailsBaselineCommandRun,
) {
	t.Helper()
	fixturePath := filepath.Join("..", "pkg", "guardrails", "baseline", "testdata", "baseline-v1.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var artifact map[string]any
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	runValue := artifact["run"].(map[string]any)
	runValue["id"] = run.id
	runValue["status"] = run.status
	runValue["started_at"] = run.started
	if run.completed == "" {
		runValue["completed_at"] = nil
	} else {
		runValue["completed_at"] = run.completed
	}
	codebase := artifact["codebase"].(map[string]any)
	codebase["name"] = run.repository
	codebase["path"] = run.path

	directory := filepath.Join(store.Root, run.id)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "baseline.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	log := run.log
	if log == "" {
		log = "step: complete\n"
	}
	if err := os.WriteFile(filepath.Join(directory, "run.log"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertGuardrailsBaselineCLIError(
	t *testing.T,
	err error,
	code string,
	exitCode int,
) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", code)
	}
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("error type = %T, want *errors.CLIError: %v", err, err)
	}
	if cliErr.Code != code || cliErr.ExitCode != exitCode {
		t.Fatalf("error = %#v, want code=%s exit=%d", cliErr, code, exitCode)
	}
}
