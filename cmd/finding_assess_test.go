package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindingAssess(t *testing.T) {
	for _, resource := range []string{"sca_findings", "detections"} {
		for _, status := range []int{http.StatusOK, http.StatusUnprocessableEntity} {
			t.Run(resource+"/"+http.StatusText(status), func(t *testing.T) {
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if r.Method != http.MethodPost || r.URL.Path != "/"+resource+"/test-id/trigger_assessment" {
						t.Errorf("request = %s %s", r.Method, r.URL.Path)
					}
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"status":"triggered"}`))
				}))
				defer server.Close()
				t.Setenv("KONVU_API_URL", server.URL)
				t.Setenv("KONVU_ACCESS_TOKEN", "test")
				t.Setenv("KONVU_ZITADEL_CLIENT_ID", "local-test")
				cmd := newFindingAssessCommand(resource, resource, "finding-id")
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetArgs([]string{"test-id", "-o", "json"})
				err := cmd.Execute()
				if (err != nil) != (status != http.StatusOK) {
					t.Fatalf("status=%d error=%v", status, err)
				}
				if requests != 1 {
					t.Fatalf("sent %d requests, want one POST", requests)
				}
				if status == http.StatusOK && out.String() != "{\n  \"status\": \"triggered\"\n}\n" {
					t.Fatalf("response = %s", out.String())
				}
			})
		}
	}
}
