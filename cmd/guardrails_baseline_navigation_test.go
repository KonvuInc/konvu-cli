package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	baselinemodel "github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
)

func TestWriteGuardrailsBaselineRunListSupportsFiltersPagingAndQuiet(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	for _, run := range []guardrailsBaselineCommandRun{
		{id: "payments--aaaaaaaa--000001", status: "completed", repository: "payments", path: "/code/payments", started: "2026-08-26T10:00:00Z", completed: "2026-08-26T10:01:00Z"},
		{id: "payments--bbbbbbbb--000002", status: "completed", repository: "payments", path: "/code/payments", started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:01:00Z"},
		{id: "payments--cccccccc--000003", status: "failed", repository: "payments", path: "/code/payments", started: "2026-08-28T10:00:00Z", completed: "2026-08-28T10:01:00Z"},
	} {
		writeGuardrailsBaselineCommandRun(t, store, run)
	}
	var outputBuffer bytes.Buffer
	err := writeGuardrailsBaselineRunList(
		&outputBuffer,
		store,
		baselinemodel.Selector{Repository: "payments"},
		guardrailsBaselineRunListOptions{
			Statuses: []string{"completed"},
			Limit:    1,
			Offset:   0,
			Sort:     "scanned",
			Order:    "desc",
			Quiet:    true,
		},
		output.Table,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(outputBuffer.String()); got != "payments--bbbbbbbb--000002" {
		t.Fatalf("quiet run list = %q", got)
	}
}

func TestBaselineRecordsNavigationSearchListCountsAndExplain(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	runID := "payments--aaaaaaaa--000001"
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: runID, status: "completed", repository: "payments", path: "/code/payments",
		started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:01:00Z",
	})
	selector := baselinemodel.Selector{RunID: runID}

	var search bytes.Buffer
	if err := writeGuardrailsBaselineRecordsSearch(
		&search,
		store,
		selector,
		"account owner",
		guardrailsBaselineSearchOptions{Limit: 50},
		output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	var searchPayload struct {
		Matches []struct {
			Collection string         `json:"collection"`
			Record     map[string]any `json:"record"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(search.Bytes(), &searchPayload); err != nil {
		t.Fatal(err)
	}
	foundControl := false
	for _, match := range searchPayload.Matches {
		if match.Collection == "controls" && match.Record["id"] == "control:account-owner" {
			foundControl = true
		}
	}
	if !foundControl {
		t.Fatalf("search did not find account-owner Control: %s", search.String())
	}

	var records bytes.Buffer
	if err := writeGuardrailsBaselineRecordsList(
		&records,
		store,
		selector,
		guardrailsBaselineRecordListOptions{
			Collection:  "assets",
			Kind:        "endpoint",
			HasControls: true,
			Limit:       25,
			Sort:        "id",
			Order:       "asc",
		},
		output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	var recordPayload struct {
		Assets []map[string]any `json:"assets"`
	}
	if err := json.Unmarshal(records.Bytes(), &recordPayload); err != nil {
		t.Fatal(err)
	}
	if len(recordPayload.Assets) != 1 || recordPayload.Assets[0]["id"] != "asset:endpoint:accounts" {
		t.Fatalf("filtered assets = %#v", recordPayload.Assets)
	}

	var counts bytes.Buffer
	if err := writeGuardrailsBaselineCounts(&counts, store, selector, "collection", output.JSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(counts.String(), `"group": "controls"`) || !strings.Contains(counts.String(), `"count": 1`) {
		t.Fatalf("collection counts missing Controls: %s", counts.String())
	}

	var explained bytes.Buffer
	if err := writeGuardrailsBaselineExplainDepth(
		&explained,
		store,
		"control:account-owner",
		selector,
		"",
		2,
		output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	var explainPayload struct {
		Related []struct {
			Depth int `json:"depth"`
		} `json:"related"`
	}
	if err := json.Unmarshal(explained.Bytes(), &explainPayload); err != nil {
		t.Fatal(err)
	}
	foundSecondHop := false
	for _, related := range explainPayload.Related {
		if related.Depth == 2 {
			foundSecondHop = true
		}
	}
	if !foundSecondHop {
		t.Fatalf("depth-2 explain returned no second-hop records: %s", explained.String())
	}
}

func TestBaselineGetIncludesAndDiff(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	baseID := "payments--aaaaaaaa--000001"
	headID := "payments--bbbbbbbb--000002"
	for _, run := range []guardrailsBaselineCommandRun{
		{id: baseID, status: "completed", repository: "payments", path: "/code/payments", started: "2026-08-26T10:00:00Z", completed: "2026-08-26T10:01:00Z"},
		{id: headID, status: "completed", repository: "payments", path: "/code/payments", started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:01:00Z"},
	} {
		writeGuardrailsBaselineCommandRun(t, store, run)
	}
	mutateGuardrailsBaselineArtifact(t, store, headID, func(raw map[string]any) {
		controls := raw["controls"].([]any)
		controls[0].(map[string]any)["name"] = "Renamed account owner"
	})

	var getOutput bytes.Buffer
	if err := writeGuardrailsBaselineGet(
		&getOutput,
		store,
		headID,
		[]string{"architecture,counts,stages"},
		output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(getOutput.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"run", "architecture", "counts", "stages"} {
		if _, found := payload[key]; !found {
			t.Errorf("get payload missing %q: %s", key, getOutput.String())
		}
	}

	var diff bytes.Buffer
	if err := writeGuardrailsBaselineDiff(&diff, store, baseID, headID, "controls", output.JSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.String(), `"changed": 1`) ||
		!strings.Contains(diff.String(), `"control:account-owner"`) {
		t.Fatalf("diff did not report changed Control: %s", diff.String())
	}
}

func TestBaselineCommandsExcludeAbsentControls(t *testing.T) {
	store := newGuardrailsBaselineCommandStore(t)
	runID := "payments--aaaaaaaa--000001"
	writeGuardrailsBaselineCommandRun(t, store, guardrailsBaselineCommandRun{
		id: runID, status: "completed", repository: "payments", path: "/code/payments",
		started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:01:00Z",
	})
	mutateGuardrailsBaselineArtifact(t, store, runID, func(raw map[string]any) {
		assets := raw["assets"].([]any)
		links := assets[1].(map[string]any)["controls"].([]any)
		links[0].(map[string]any)["status"] = "absent"
		observations := raw["observations"].(map[string]any)["controls"].([]any)
		observations[0].(map[string]any)["status"] = "absent"
	})
	selector := baselinemodel.Selector{RunID: runID}

	var search bytes.Buffer
	if err := writeGuardrailsBaselineRecordsSearch(
		&search,
		store,
		selector,
		"account-owner",
		guardrailsBaselineSearchOptions{Limit: 50},
		output.JSON,
	); err != nil {
		t.Fatal(err)
	}
	var searchPayload struct {
		Matches []struct {
			Collection string `json:"collection"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(search.Bytes(), &searchPayload); err != nil {
		t.Fatal(err)
	}
	for _, match := range searchPayload.Matches {
		switch match.Collection {
		case "controls", "implementations", "control-observations":
			t.Fatalf("search returned absent control records: %s", search.String())
		}
	}

	var getOutput bytes.Buffer
	if err := writeGuardrailsBaselineGet(&getOutput, store, runID, nil, output.JSON); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(getOutput.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw["controls"].([]any)) != 0 || len(raw["implementations"].([]any)) != 0 {
		t.Fatalf("get returned absent-only records: %s", getOutput.String())
	}
	assets := raw["assets"].([]any)
	if links := assets[1].(map[string]any)["controls"].([]any); len(links) != 0 {
		t.Fatalf("get returned absent links: %#v", links)
	}
}

func mutateGuardrailsBaselineArtifact(
	t *testing.T,
	store baselinemodel.Store,
	runID string,
	mutate func(map[string]any),
) {
	t.Helper()
	path := filepath.Join(store.Root, runID, "baseline.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	mutate(raw)
	data, err = json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
