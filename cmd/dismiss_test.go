package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	"github.com/spf13/cobra"
)

func TestPreviewDismissalsReportsSkippedIDs(t *testing.T) {
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responses := map[string]map[string]any{
			"/sca_findings/dismissible": {
				"source": map[string]any{"state": "open", "dismissible_from_konvu": true},
			},
			"/sca_findings/blocked": {
				"source": map[string]any{"state": "open", "dismissible_from_konvu": false},
			},
			"/sca_findings/closed": {
				"source": map[string]any{"state": "dismissed", "dismissible_from_konvu": true},
			},
		}
		response, ok := responses[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "token")
	defer client.Close()
	wouldDismiss, skipped, err := previewDismissals(
		client,
		[]string{"dismissible", "blocked", "closed", "missing"},
		map[string]map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if wouldDismiss != 1 {
		t.Fatalf("would dismiss %d, want 1", wouldDismiss)
	}
	want := []dismissalSkip{
		{FindingID: "blocked", Reason: "not_dismissible_from_konvu"},
		{FindingID: "closed", Reason: "not_open"},
		{FindingID: "missing", Reason: "not_found"},
	}
	if len(skipped) != len(want) {
		t.Fatalf("skipped = %#v", skipped)
	}
	for i := range want {
		if skipped[i] != want[i] {
			t.Errorf("skipped[%d] = %#v, want %#v", i, skipped[i], want[i])
		}
	}
}

func TestResponseSkippedFindings(t *testing.T) {
	got := responseSkippedFindings(map[string]any{
		"skipped": []any{
			map[string]any{
				"finding_id": "finding-1",
				"reason":     "not_dismissible_from_konvu",
			},
		},
	})
	if len(got) != 1 || got[0].FindingID != "finding-1" || got[0].Reason != "not_dismissible_from_konvu" {
		t.Fatalf("skipped = %#v", got)
	}
}

func TestDismissSendsExternalReference(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sca_findings/bulk_dismiss" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"dismissed_count": 1, "skipped_count": 0})
	}))
	defer server.Close()
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	t.Setenv("KONVU_ACCESS_TOKEN", "test-token")

	command := &cobra.Command{Use: "dismiss", RunE: runDismiss}
	out := &bytes.Buffer{}
	command.SetOut(out)
	command.SetErr(&bytes.Buffer{})
	addDismissFlags(command)
	command.SetArgs([]string{
		"--issues", "finding-1",
		"--reason", "Tracked externally",
		"--comment", "Handled internally",
		"--external-reference", " SEC-1234 ",
		"--output", "json",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	if body["external_reference_code"] != "SEC-1234" {
		t.Fatalf("body = %#v", body)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["external_reference_code"] != "SEC-1234" {
		t.Fatalf("output = %#v", result)
	}
}

func TestDismissDryRunPrintsExternalReference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sca_findings/finding-1" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"source": map[string]any{"state": "open", "dismissible_from_konvu": true},
		})
	}))
	defer server.Close()
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	t.Setenv("KONVU_ACCESS_TOKEN", "test-token")

	command := &cobra.Command{Use: "dismiss", RunE: runDismiss}
	out := &bytes.Buffer{}
	command.SetOut(out)
	command.SetErr(&bytes.Buffer{})
	addDismissFlags(command)
	command.SetArgs([]string{
		"--issues", "finding-1",
		"--reason", "Tracked externally",
		"--external-reference", " SEC-1234 ",
		"--dry-run",
		"--output", "json",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["external_reference_code"] != "SEC-1234" {
		t.Fatalf("output = %#v", result)
	}
}
