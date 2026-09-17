package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
