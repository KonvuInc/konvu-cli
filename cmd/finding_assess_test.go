package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

const assessmentTestID = "019d4f3bfd1e7af1bef48476eb7a620d"

func TestAssessmentTriggerAndWatch(t *testing.T) {
	for _, kind := range []string{"sca", "sast"} {
		t.Run(kind, func(t *testing.T) {
			posts, gets := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				prefix := "/sca_findings/"
				if kind == "sast" {
					prefix = "/detections/"
				}
				if !strings.HasPrefix(r.URL.Path, prefix+assessmentTestID) {
					t.Errorf("wrong path: %s", r.URL.Path)
				}
				status := "queued"
				if r.Method == "POST" {
					posts++
				} else {
					gets++
					status = "assessing"
					if gets > 1 {
						status = "done"
					}
				}
				state := map[string]any{"status": status, "message": "Assessment " + status}
				if kind == "sca" && r.Method == "POST" {
					state = map[string]any{"status": "triggered", "execution": state}
				}
				_ = json.NewEncoder(w).Encode(state)
			}))
			defer server.Close()
			t.Setenv("KONVU_API_URL", server.URL)
			t.Setenv("KONVU_ACCESS_TOKEN", "test")
			t.Setenv("KONVU_ZITADEL_CLIENT_ID", "local-test")
			cmd := newFindingAssessmentCommand(kind, true)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs([]string{assessmentTestID, "--watch", "--interval", "1ms", "-o", "json"})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			var state map[string]any
			if err := json.Unmarshal(out.Bytes(), &state); err != nil {
				t.Fatalf("not one JSON result: %s", out.String())
			}
			if posts != 1 || gets != 2 || state["status"] != "done" {
				t.Fatalf("posts=%d gets=%d result=%v", posts, gets, state)
			}
		})
	}
}

func TestAssessmentWatchCancelsStalledRequests(t *testing.T) {
	for _, stage := range []string{"initial_get", "initial_post", "reference", "poll", "body", "cancel"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 && (stage == "poll" || stage == "body" || stage == "cancel") {
					_, _ = w.Write([]byte(`{"status":"queued"}`))
					return
				}
				if stage == "cancel" {
					cancel()
				}
				if stage == "body" {
					_, _ = w.Write([]byte(`{"status":`))
					w.(http.Flusher).Flush()
				}
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Second):
				}
			}))
			defer server.Close()
			t.Setenv("KONVU_API_URL", server.URL)
			t.Setenv("KONVU_ACCESS_TOKEN", "test")
			t.Setenv("KONVU_ZITADEL_CLIENT_ID", "local-test")
			cmd := newFindingAssessmentCommand("sca", stage == "initial_post")
			cmd.SetContext(ctx)
			var out bytes.Buffer
			cmd.SetOut(&out)
			reference := assessmentTestID
			if stage == "reference" {
				reference = "https://github.com/demo/app/security/dependabot/1"
			}
			cmd.SetArgs([]string{reference, "--watch", "--timeout", "50ms", "--interval", "1ms", "-o", "json"})
			started := time.Now()
			err := cmd.Execute()
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("stalled request overran timeout: %s", elapsed)
			}
			wantCode := "ASSESSMENT_TIMEOUT"
			if stage == "cancel" {
				wantCode = "ASSESSMENT_CANCELED"
			}
			var cliErr *clierrors.CLIError
			if !errors.As(err, &cliErr) || cliErr.Code != wantCode {
				t.Fatalf("error = %v, want %s", err, wantCode)
			}
			if stage == "poll" || stage == "body" || stage == "cancel" {
				var state map[string]any
				if err := json.Unmarshal(out.Bytes(), &state); err != nil || state["status"] != "queued" {
					t.Fatalf("lost last known state: %s", out.String())
				}
			} else if out.Len() != 0 {
				t.Fatalf("invented state before initial response: %s", out.String())
			}
		})
	}
}

func TestAssessmentProcessExitCodes(t *testing.T) {
	const helperEnv = "KONVU_TEST_ASSESSMENT_ARGS"
	if args := os.Getenv(helperEnv); args != "" {
		rootCmd.SetArgs(strings.Fields(args))
		Execute()
		os.Exit(0)
	}
	for _, tc := range []struct {
		name   string
		args   string
		status int
		state  string
		code   int
	}{
		{"invalid duration", assessmentTestID + " --timeout 0s", 200, "done", 2},
		{"malformed duration", assessmentTestID + " --timeout nope", 200, "done", 2},
		{"missing argument", "", 200, "done", 2},
		{"invalid output", assessmentTestID + " -o csv", 200, "done", 2},
		{"authentication", assessmentTestID, 401, "", 4},
		{"not found", assessmentTestID, 404, "", 3},
		{"failed watch", assessmentTestID + " --watch -o json", 200, "failed", 1},
		{"timeout", assessmentTestID + " --watch --timeout 30ms -o json", 200, "queued", 1},
		{"done", assessmentTestID + " --watch -o json", 200, "done", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"status": tc.state})
			}))
			defer server.Close()
			t.Setenv("KONVU_API_URL", server.URL)
			t.Setenv("KONVU_ACCESS_TOKEN", "test")
			t.Setenv("KONVU_ZITADEL_CLIENT_ID", "local-test")
			command := exec.Command(os.Args[0], "-test.run=^TestAssessmentProcessExitCodes$")
			command.Env = append(os.Environ(), helperEnv+"=finding sast status "+tc.args)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			code := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatal(err)
				}
				code = exitErr.ExitCode()
			}
			if code != tc.code {
				t.Fatalf("exit=%d, want %d; stderr=%s", code, tc.code, stderr.String())
			}
			if strings.Contains(tc.args, "-o json") {
				var state map[string]any
				if err := json.Unmarshal(stdout.Bytes(), &state); err != nil || state["status"] != tc.state {
					t.Fatalf("invalid stdout: %s", stdout.String())
				}
			}
		})
	}
}

func TestAssessmentExistingRunIsNotTriggeredAgain(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"detail":"Already in progress"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"queued","retrying":true,"message":"Previous attempt failed; retry queued."}`))
	}))
	defer server.Close()
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "test")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "local-test")
	cmd := newFindingAssessmentCommand("sca", true)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{assessmentTestID, "-o", "table"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if posts != 1 || !strings.Contains(out.String(), "queued: Previous attempt failed") {
		t.Fatal(out.String())
	}
}

func TestAssessmentStatusNeverPosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("status mutated: %s", r.Method)
		}
		_, _ = w.Write([]byte(`{"status":"failed","message":"Assessment could not finish."}`))
	}))
	defer server.Close()
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "test")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "local-test")
	cmd := newFindingAssessmentCommand("sast", false)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{assessmentTestID, "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"status": "failed"`) {
		t.Fatal(out.String())
	}
}

func TestAssessmentFailureAndInvalidArguments(t *testing.T) {
	for _, status := range []int{402, 403, 404, 422, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"detail":"Not available"}`))
			}))
			defer server.Close()
			t.Setenv("KONVU_API_URL", server.URL)
			t.Setenv("KONVU_ACCESS_TOKEN", "test")
			t.Setenv("KONVU_ZITADEL_CLIENT_ID", "local-test")
			cmd := newFindingAssessmentCommand("sast", true)
			cmd.SetArgs([]string{assessmentTestID})
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected actionable error")
			}
		})
	}
}

func TestAssessmentWatchFailureAndTimeoutPreserveJSON(t *testing.T) {
	for _, state := range []string{"failed", "queued"} {
		t.Run(state, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("watch status mutated: %s", r.Method)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"status": state, "message": "Execution state"})
			}))
			defer server.Close()
			t.Setenv("KONVU_API_URL", server.URL)
			t.Setenv("KONVU_ACCESS_TOKEN", "test")
			t.Setenv("KONVU_ZITADEL_CLIENT_ID", "local-test")
			cmd := newFindingAssessmentCommand("sast", false)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs([]string{assessmentTestID, "--watch", "--interval", "1ms", "--timeout", "5ms", "-o", "json"})
			if err := cmd.Execute(); err == nil {
				t.Fatal("failure and timeout must return a nonzero result")
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatalf("not one JSON result: %s", out.String())
			}
			if result["status"] != state {
				t.Fatalf("lost last known state: %v", result)
			}
		})
	}
}
