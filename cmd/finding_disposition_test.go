package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runDispositionCommand(
	t *testing.T, server *httptest.Server, command *cobra.Command, args ...string,
) map[string]any {
	t.Helper()
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "test-token")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	out := &bytes.Buffer{}
	command.SetOut(out)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs(append(args, "--output", "json"))
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON %q: %v", out.String(), err)
	}
	return result
}

func TestSastDismissSendsMetadata(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/detections/detection-1/dismiss" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"broker_task_id": "task-1",
			"integration_id": "integration-1",
		})
	}))
	defer server.Close()

	result := runDispositionCommand(t, server, newFindingDismissCommand("sast", "detection-id"),
		"detection-1",
		"--reason", "Tracked externally",
		"--comment", "Handled internally",
		"--external-reference", " SEC-1234 ",
	)
	if body["dismissed_comment"] != "Handled internally" || body["external_reference_code"] != "SEC-1234" {
		t.Fatalf("body = %#v", body)
	}
	if result["status"] != "queued" || result["external_reference_code"] != "SEC-1234" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSastReopen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/detections/detection-1/reopen" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"integration_id": "integration-1"})
	}))
	defer server.Close()

	result := runDispositionCommand(
		t, server, newFindingReopenCommand("sast", "detection-id"), "detection-1",
	)
	if result["status"] != "completed" || result["action"] != "reopen" {
		t.Fatalf("result = %#v", result)
	}
}

func TestScaDismissUsesFindingID(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sca_findings/bulk_dismiss" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"dismissed_count": 1, "skipped_count": 0})
	}))
	defer server.Close()

	result := runDispositionCommand(t, server, newFindingDismissCommand("sca", "finding-id"),
		"finding-1", "--external-reference", "SEC-1234",
	)
	ids, ok := body["finding_ids"].([]any)
	if !ok || len(ids) != 1 || ids[0] != "finding-1" {
		t.Fatalf("body = %#v", body)
	}
	if result["status"] != "completed" {
		t.Fatalf("result = %#v", result)
	}
}

func TestScaReopenResolvesSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/sca_findings/finding-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"source": map[string]any{"id": "issue-1", "integration_id": "integration-1"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/integrations/integration-1/issue/issue-1/reopen":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	result := runDispositionCommand(
		t, server, newFindingReopenCommand("sca", "finding-id"), "finding-1",
	)
	if result["status"] != "completed" || result["source_id"] != "issue-1" ||
		result["integration_id"] != "integration-1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestFindingDismissDryRunTable(t *testing.T) {
	command := newFindingDismissCommand("sast", "detection-id")
	out := &bytes.Buffer{}
	command.SetOut(out)
	command.SetArgs([]string{
		"detection-1", "--dry-run", "--comment", "Handled internally",
		"--external-reference", "SEC-1234", "--output", "table",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Status", "Comment", "External Reference Code", "preview", "SEC-1234"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}
