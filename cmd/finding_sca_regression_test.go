package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func captureStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = original }()

	runErr := run()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), runErr
}

func serveSCAFinding(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	t.Setenv("KONVU_ACCESS_TOKEN", "test-token")
	return server
}

func TestSCAListSelectedFieldsRenderInTables(t *testing.T) {
	serveSCAFinding(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{
			"id": "finding-1",
			"source": map[string]any{
				"state":                             "dismissed",
				"dismissed_comment":                 "Handled internally",
				"dismissed_external_reference_code": "SEC-1234",
			},
			"assessment": map[string]any{"result": "false_positive"},
		}}})
	})

	for _, groupBy := range []string{"", "assessment"} {
		name := "ungrouped"
		if groupBy != "" {
			name = "grouped"
		}
		t.Run(name, func(t *testing.T) {
			command := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: scaListCmd.RunE}
			addSCAListFlags(command)
			args := []string{
				"--fields", "id,dismissed_comment,dismissed_external_reference_code",
				"--output", "table",
			}
			if groupBy != "" {
				args = append(args, "--group-by", groupBy)
			}
			command.SetArgs(args)

			out, err := captureStdout(t, command.Execute)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Id", "Dismissed Comment", "Dismissed External Reference Code", "finding-1", "Handled internally", "SEC-1234"} {
				if !strings.Contains(out, want) {
					t.Fatalf("output missing %q:\n%s", want, out)
				}
			}
			if strings.Contains(out, "<nil>") || strings.Contains(out, "Cve") {
				t.Fatalf("output contains default-column artifacts:\n%s", out)
			}
		})
	}
}

func TestSCARateUsesAssessmentID(t *testing.T) {
	posted := make(chan string, 1)
	serveSCAFinding(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/sca_findings/finding-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"assessment": map[string]any{"id": "assessment-1", "result": "false_positive"},
			})
		case r.Method == http.MethodPost:
			posted <- r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{"helpful": true})
		default:
			http.NotFound(w, r)
		}
	})

	command := &cobra.Command{Use: "rate", Args: cobra.ExactArgs(2), RunE: scaRateCmd.RunE}
	command.Flags().StringP("comment", "c", "", "")
	command.Flags().String("recommendation-id", "", "")
	command.Flags().StringP("output", "o", "", "")
	command.SetArgs([]string{"finding-1", "agree", "--output", "json"})

	if _, err := captureStdout(t, command.Execute); err != nil {
		t.Fatal(err)
	}
	want := "/recommendation_decision_history/assessment-1/integration_issue/finding-1/scoring"
	if got := <-posted; got != want {
		t.Fatalf("scoring path = %q, want %q", got, want)
	}
}
