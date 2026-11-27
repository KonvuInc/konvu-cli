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
	triggered := `{"status":"triggered"}`
	for _, tc := range []struct {
		name, resource, format string
		status, exitCode       int
		errorCode              string
		body                   string
		retryable              bool
	}{
		{"sca", "sca_findings", "json", 200, 0, "", triggered, false},
		{"sast", "detections", "json", 200, 0, "", triggered, false},
		{"table", "sca_findings", "table", 200, 0, "", triggered, false},
		{"authentication", "sca_findings", "json", 401, 4, "AUTH_FAILED", triggered, false},
		{"ineligible", "detections", "json", 422, 1, "NOT_ASSESSABLE", `{"detail":"Finding is not open (current state: fixed)."}`, false},
		// What Konvu said of a repository it does not assess, before and after it named coverage.
		{"disabled", "sca_findings", "json", 422, 1, "NOT_COVERED", `{"detail":"Assessment is disabled."}`, false},
		{"not covered", "sca_findings", "json", 422, 1, "NOT_COVERED", `{"detail":"No coverage scope runs analysis on this repository."}`, false},
		{"server error", "sca_findings", "json", 502, 1, "API_ERROR", `{"detail":"upstream"}`, true},
		{"typo", "sca_findings", "jsno", 200, 2, "", triggered, false},
		{"unsupported", "detections", "csv", 200, 2, "", triggered, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/"+tc.resource+"/test-id/trigger_assessment" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
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
					answer := getMap(response, "error")
					if getStr(answer, "code") != tc.errorCode || answer["retryable"] != tc.retryable {
						t.Fatalf("wrong error: %s", out)
					}
					if tc.errorCode == "NOT_COVERED" && !strings.Contains(getStr(answer, "suggestion"), "konvu coverage") {
						t.Fatalf("no coverage suggestion: %s", out)
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
