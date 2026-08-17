package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/api"
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
