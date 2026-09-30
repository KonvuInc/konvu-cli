package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/spf13/cobra"
)

func instructionTestCommand(args ...string) (*cobra.Command, *bytes.Buffer) {
	command := newTriageInstructionsCommand()
	out := new(bytes.Buffer)
	command.SetOut(out)
	command.SetErr(new(bytes.Buffer))
	command.SetArgs(args)
	return command, out
}

func instructionServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing auth")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == vulnerabilityReportsBase+"/programs" {
			_, _ = w.Write([]byte(`{"items":[{"id":"target-1","source_code_repos":["acme/web"]}]}`))
			return
		}
		handler(w, r)
	}))
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "test-token")
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
	t.Cleanup(server.Close)
	return server
}

func TestInstructionsCreateAndUpdate(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "replace"}[replacement], func(t *testing.T) {
			calls := 0
			instructionServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				path, method := vulnerabilityReportsBase+"/programs/target-1/policy/files", "POST"
				if replacement {
					path += "/file-1"
					method = "PUT"
				}
				if r.URL.Path != path || r.Method != method {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload["content"] != "# Private instructions\n" || payload["path"] != "/scope.md" || payload["client"] != "cli" {
					t.Errorf("payload = %#v", payload)
				}
				if replacement && payload["base_sha256"] != "revision-1" {
					t.Error("missing base revision")
				}
				if !replacement && payload["base_sha256"] != nil {
					t.Error("unexpected base revision")
				}
				_, _ = w.Write([]byte(`{"id":"file-1","path":"/scope.md","sha256":"revision-2","content":"Private instructions"}`))
			})
			args := []string{"upload", "--repo", "https://github.com/acme/web.git", "--file", "-", "--path", "scope.md", "-o", "json"}
			if replacement {
				args = append(args, "--file-id", "file-1", "--base-sha256", "revision-1")
			}
			command, out := instructionTestCommand(args...)
			command.SetIn(strings.NewReader("# Private instructions\r\n"))
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Errorf("writes=%d", calls)
			}
			if strings.Contains(out.String(), "Private instructions") || strings.Contains(out.String(), "content") {
				t.Errorf("upload printed content: %s", out.String())
			}
		})
	}
}

func TestInstructionsLocalFileDefaultsPath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "scope.md")
	if err := os.WriteFile(file, []byte("Instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	instructionServer(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["path"] != "/scope.md" {
			t.Errorf("path=%v", payload["path"])
		}
		_, _ = w.Write([]byte(`{"id":"file-1"}`))
	})
	command, _ := instructionTestCommand("upload", "--repo", "acme/web", "--file", file)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestInstructionsReadsAndDelete(t *testing.T) {
	for _, operation := range []string{"list", "get", "delete"} {
		t.Run(operation, func(t *testing.T) {
			instructionServer(t, func(w http.ResponseWriter, r *http.Request) {
				if operation == "delete" {
					if r.Method != "DELETE" || r.URL.Query().Get("base_sha256") != "revision-1" || r.URL.Query().Get("client") != "cli" {
						t.Errorf("request=%s %s", r.Method, r.URL)
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method != "GET" {
					t.Errorf("method=%s", r.Method)
				}
				if operation == "list" {
					_, _ = w.Write([]byte(`{"files":[{"id":"file-1","path":"/scope.md","sha256":"revision-1"}]}`))
				} else {
					_, _ = w.Write([]byte(`{"id":"file-1","content":"Instructions","sha256":"revision-1"}`))
				}
			})
			args := []string{operation}
			if operation != "list" {
				args = append(args, "file-1")
			}
			args = append(args, "--repo", "github:acme/web", "-o", "json")
			if operation == "delete" {
				args = append(args, "--base-sha256", "revision-1")
			}
			command, out := instructionTestCommand(args...)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if operation == "get" && result["content"] != "Instructions" {
				t.Error("content missing")
			}
			if operation == "delete" && result["deleted"] != true {
				t.Error("delete not rendered")
			}
		})
	}
}

func TestInstructionsRejectInvalidInputBeforeNetwork(t *testing.T) {
	t.Setenv("KONVU_API_URL", "http://127.0.0.1:1")
	for _, tc := range []struct {
		args []string
		body string
	}{
		{[]string{"upload", "--file", "-", "--path", "scope.md"}, "Instructions"},
		{[]string{"upload", "--repo", "acme/web", "--file", "-"}, "Instructions"},
		{[]string{"upload", "--repo", "acme/web", "--file", "-", "--path", "scope.md", "--file-id", "file-1"}, "Instructions"},
		{[]string{"upload", "--repo", "acme/web", "--file", "-", "--path", "scope.md", "--base-sha256", "rev"}, "Instructions"},
		{[]string{"upload", "--repo", "acme/web", "--file", "-", "--path", "scope.md"}, " "},
		{[]string{"upload", "--repo", "acme/web", "--file", "-", "--path", "scope.md"}, string([]byte{0xff, 0})},
		{[]string{"delete", "file-1", "--repo", "acme/web"}, ""},
		{[]string{"get", "../bad", "--repo", "acme/web"}, ""},
		{[]string{"list", "--repo", "acme/web", "-o", "csv"}, ""},
	} {
		command, _ := instructionTestCommand(tc.args...)
		command.SetIn(strings.NewReader(tc.body))
		err := command.Execute()
		cliErr, ok := err.(*clierrors.CLIError)
		if !ok || cliErr.ExitCode != clierrors.ExitUsageError || cliErr.Suggestion == "" {
			t.Errorf("args=%v error=%v", tc.args, err)
		}
	}
}

func TestInstructionsConflictDoesNotRetryOrExposeBody(t *testing.T) {
	calls := 0
	instructionServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"detail":"private server detail"}`))
	})
	command, out := instructionTestCommand("upload", "--repo", "acme/web", "--file", "-", "--path", "scope.md", "--file-id", "file-1", "--base-sha256", "old")
	command.SetIn(strings.NewReader("Private instructions"))
	err := command.Execute()
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok || cliErr.Code != "INSTRUCTIONS_CONFLICT" || !strings.Contains(cliErr.Suggestion, "sha256") {
		t.Fatalf("error=%v", err)
	}
	if calls != 1 || strings.Contains(err.Error(), "private") || out.Len() != 0 {
		t.Errorf("calls=%d error=%v output=%s", calls, err, out)
	}
}

func TestInstructionsRepositoryResolution(t *testing.T) {
	for _, items := range []string{`[]`, `[{"id":"one","source_code_repos":["acme/web"]},{"id":"two","source_code_repos":["acme/web"]}]`, `[{"id":"one","source_code_repos":["other/web"]}]`} {
		writes := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != vulnerabilityReportsBase+"/programs" {
				writes++
			}
			_, _ = w.Write([]byte(`{"items":` + items + `}`))
		}))
		t.Setenv("KONVU_API_URL", server.URL)
		t.Setenv("KONVU_ACCESS_TOKEN", "test-token")
		t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
		command, _ := instructionTestCommand("upload", "--repo", "acme/web", "--file", "-", "--path", "scope.md")
		command.SetIn(strings.NewReader("Instructions"))
		err := command.Execute()
		server.Close()
		if err == nil || writes != 0 {
			t.Errorf("error=%v writes=%d", err, writes)
		}
	}
}

func TestInstructionsPreserveMarkdownWhitespace(t *testing.T) {
	instructionServer(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["content"] != "    Indented code\nText with hard break  " {
			t.Errorf("content=%q", payload["content"])
		}
		_, _ = w.Write([]byte(`{"id":"file-1"}`))
	})
	command, _ := instructionTestCommand("upload", "--repo", "acme/web", "--file", "-", "--path", "scope.md")
	command.SetIn(strings.NewReader("    Indented code\nText with hard break  "))
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestInstructionsAuthenticationErrorIsRedacted(t *testing.T) {
	instructionServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"private authentication detail"}`))
	})
	command, _ := instructionTestCommand("get", "file-1", "--repo", "acme/web")
	err := command.Execute()
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok || cliErr.ExitCode != clierrors.ExitAuthFailed || strings.Contains(err.Error(), "private") {
		t.Fatalf("error=%v", err)
	}
	out := new(bytes.Buffer)
	if code := writeExecutionError(out, err); code != clierrors.ExitAuthFailed {
		t.Errorf("exit=%d", code)
	}
	if strings.Contains(out.String(), "private") || !strings.Contains(out.String(), "konvu login") {
		t.Errorf("output=%s", out)
	}
}
