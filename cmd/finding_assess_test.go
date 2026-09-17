package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFindingAssess(t *testing.T) {
	// The existing finding error handler exits the process, so exercise the
	// command in a subprocess to verify structured output and exit status.
	if resource := os.Getenv("KONVU_TEST_ASSESS_RESOURCE"); resource != "" {
		cmd := newFindingAssessCommand(resource, resource, "finding-id")
		cmd.SetArgs([]string{"test-id", "-o", os.Getenv("KONVU_TEST_ASSESS_FORMAT")})
		if err := cmd.Execute(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	for _, tc := range []struct {
		name, resource, format string
		status, exitCode       int
		errorCode              string
	}{
		{"sca", "sca_findings", "json", 200, 0, ""},
		{"sast", "detections", "json", 200, 0, ""},
		{"table", "sca_findings", "table", 200, 0, ""},
		{"authentication", "sca_findings", "json", 401, 4, "AUTH_FAILED"},
		{"ineligible", "detections", "json", 422, 1, "API_ERROR"},
		{"typo", "sca_findings", "jsno", 200, 2, ""},
		{"unsupported", "detections", "csv", 200, 2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/"+tc.resource+"/test-id/trigger_assessment" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"status":"triggered"}`))
			}))
			defer server.Close()
			t.Setenv("KONVU_API_URL", server.URL)
			t.Setenv("KONVU_ACCESS_TOKEN", "test")
			t.Setenv("KONVU_ZITADEL_CLIENT_ID", "local-test")
			command := exec.Command(os.Args[0], "-test.run=^TestFindingAssess$")
			command.Env = append(os.Environ(), "KONVU_TEST_ASSESS_RESOURCE="+tc.resource, "KONVU_TEST_ASSESS_FORMAT="+tc.format)
			out, err := command.Output()
			if command.ProcessState == nil {
				t.Fatal(err)
			}
			if code := command.ProcessState.ExitCode(); code != tc.exitCode {
				t.Fatalf("exit=%d want=%d; output=%s", code, tc.exitCode, out)
			}
			wantRequests := int32(1)
			if tc.exitCode == 2 {
				wantRequests = 0
			}
			if requests.Load() != wantRequests {
				t.Fatalf("sent %d requests, want %d", requests.Load(), wantRequests)
			}
			if tc.format == "json" {
				var response map[string]any
				if err := json.Unmarshal(out, &response); err != nil {
					t.Fatalf("invalid JSON: %s", out)
				}
				if tc.errorCode != "" {
					if getStr(getMap(response, "error"), "code") != tc.errorCode {
						t.Fatalf("wrong error: %s", out)
					}
				} else if getStr(response, "status") != "triggered" {
					t.Fatalf("wrong response: %s", out)
				}
			} else if tc.exitCode == 0 && strings.TrimSpace(string(out)) != "Assessment requested." {
				t.Fatalf("wrong confirmation: %s", out)
			}
		})
	}
}
