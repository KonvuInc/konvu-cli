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

func TestTransformDetectionIncludesDismissalMetadata(t *testing.T) {
	row := transformDetection(map[string]any{
		"id":                                "detection-1",
		"dismissed_at":                      "2026-09-25T10:00:00Z",
		"dismissed_reason":                  "Accepted risk",
		"dismissed_comment":                 "Handled internally",
		"dismissed_external_reference_code": "SEC-1234",
		"supports_dismissal":                true,
	})
	if row["dismissed_comment"] != "Handled internally" ||
		row["dismissed_external_reference_code"] != "SEC-1234" ||
		row["supports_dismissal"] != true {
		t.Fatalf("row = %#v", row)
	}
}

func TestSastListSelectsDismissalFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/detections" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("state"); got != "dismissed" {
			t.Fatalf("state = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{
			"id":                                "detection-1",
			"title":                             "SQL injection",
			"state":                             "dismissed",
			"dismissed_comment":                 "Handled internally",
			"dismissed_external_reference_code": "SEC-1234",
		}}})
	}))
	defer server.Close()
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "test-token")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	command := &cobra.Command{Use: "list", RunE: runSastList}
	addSastListFlags(command)
	out := &bytes.Buffer{}
	command.SetOut(out)
	command.SetArgs([]string{
		"--state", "dismissed",
		"--fields", "detection_id,dismissed_comment,dismissed_external_reference_code",
		"--output", "table",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Detection Id", "Dismissed Comment", "SEC-1234", "Handled internally"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "SQL injection") {
		t.Fatalf("unselected title printed:\n%s", out.String())
	}
}

func TestSastListRejectsUnknownField(t *testing.T) {
	_, err := parseSastListFields("detection_id,does_not_exist")
	if err == nil || !strings.Contains(err.Error(), "does_not_exist") {
		t.Fatalf("err = %v", err)
	}
}

func TestNormalizeSastFeedbackTags(t *testing.T) {
	got, err := normalizeSastFeedbackTags([]string{"inaccurate", "not-relevant", "Other"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Inaccurate", "Not Relevant", "Other"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tags = %#v, want %#v", got, want)
	}

	if _, err := normalizeSastFeedbackTags([]string{"strong_evidence"}); err == nil {
		t.Fatal("unknown tag should return an error")
	}
}
