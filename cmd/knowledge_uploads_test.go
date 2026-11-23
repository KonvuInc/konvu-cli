package cmd

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/spf13/cobra"
)

func knowledgeUploadFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func uploadKnowledgeFiles(t *testing.T, format string, files ...string) (string, error) {
	t.Helper()
	command := &cobra.Command{}
	command.Flags().StringP("output", "o", "", "")
	_ = command.Flags().Set("output", format)
	out := new(bytes.Buffer)
	command.SetOut(out)
	err := runKnowledgeUpload(command, files)
	return out.String(), err
}

// knowledgeUploadParts reads a multipart upload as "field|filename|content" per part.
func knowledgeUploadParts(t *testing.T, r *http.Request) []string {
	t.Helper()
	reader, err := r.MultipartReader()
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return parts
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(part)
		parts = append(parts, part.FormName()+"|"+part.FileName()+"|"+string(data))
	}
}

func knowledgeUploadAnswer(statuses ...string) string {
	items := make([]string, len(statuses))
	for i, status := range statuses {
		errorText := "null"
		if status == "failed" || status == "unsupported" {
			errorText = `"cannot read\u001b[2J this file"`
		}
		items[i] = fmt.Sprintf(`{"id":"u%d","filename":"f%d","content_type":"text/markdown","size_bytes":2048,"status":%q,"error":%s,"uploaded_by":"ana@acme.com","created_at":"2026-11-23T10:00:00Z"}`, i, i, status, errorText)
	}
	return `{"items":[` + strings.Join(items, ",") + `]}`
}

func TestKnowledgeUploadSendsMultipart(t *testing.T) {
	dir := knowledgeUploadFiles(t, map[string]string{"notes.md": "# Notes", "policy.pdf": "%PDF-1.7"})
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != knowledgeUploadsPath {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		parts := knowledgeUploadParts(t, r)
		if want := []string{"file|notes.md|# Notes", "file|policy.pdf|%PDF-1.7"}; strings.Join(parts, "\n") != strings.Join(want, "\n") {
			t.Errorf("parts = %q, want %q", parts, want)
		}
		_, _ = w.Write([]byte(knowledgeUploadAnswer("pending", "ready")))
	})
	out, err := uploadKnowledgeFiles(t, "table", filepath.Join(dir, "notes.md"), filepath.Join(dir, "policy.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"notes.md", "policy.pdf", "pending", "ready", "2.0 KiB"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestKnowledgeUploadRefusesFilesLocally(t *testing.T) {
	dir := knowledgeUploadFiles(t, map[string]string{"empty.md": ""})
	big := filepath.Join(dir, "big.pdf")
	file, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(knowledgeUploadMaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	calls := knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request sent: %s %s", r.Method, r.URL.Path)
	})
	out, err := uploadKnowledgeFiles(t, "table", big, dir, filepath.Join(dir, "missing.md"), filepath.Join(dir, "empty.md"))
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok || cliErr.Code != "UPLOAD_INCOMPLETE" || cliErr.ExitCode != clierrors.ExitGeneralError {
		t.Fatalf("error = %#v", err)
	}
	for _, want := range []string{"big.pdf (over 20 MiB)", "(a directory)", "missing.md (cannot be read)", "empty.md (empty)", "4 of 4"} {
		if !strings.Contains(cliErr.Message, want) {
			t.Errorf("message lacks %q: %s", want, cliErr.Message)
		}
	}
	if strings.Count(out, "refused") != 4 {
		t.Errorf("table:\n%s", out)
	}
	if *calls != 0 {
		t.Errorf("%d requests sent", *calls)
	}
}

func TestKnowledgeUploadMixedStatusesExitOne(t *testing.T) {
	dir := knowledgeUploadFiles(t, map[string]string{"a.md": "a", "b.bin": "b", "c.docx": "c"})
	calls := knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		if parts := knowledgeUploadParts(t, r); len(parts) != 3 {
			t.Errorf("parts = %q", parts)
		}
		_, _ = w.Write([]byte(knowledgeUploadAnswer("ready", "unsupported", "failed")))
	})
	files := []string{filepath.Join(dir, "a.md"), filepath.Join(dir, "b.bin"), filepath.Join(dir, "c.docx")}
	out, err := uploadKnowledgeFiles(t, "table", files...)
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok || cliErr.Code != "UPLOAD_INCOMPLETE" || cliErr.ExitCode != clierrors.ExitGeneralError {
		t.Fatalf("error = %#v", err)
	}
	for _, want := range []string{"2 of 3", "b.bin (unsupported)", "c.docx (failed)"} {
		if !strings.Contains(cliErr.Message, want) {
			t.Errorf("message lacks %q: %s", want, cliErr.Message)
		}
	}
	if strings.Contains(cliErr.Message, "a.md") {
		t.Errorf("message names a file that went through: %s", cliErr.Message)
	}
	if !strings.Contains(out, "cannot read [2J this file") || strings.Contains(out, "\x1b") {
		t.Errorf("table:\n%q", out)
	}
	if *calls != 1 {
		t.Errorf("%d requests sent", *calls)
	}

	out, _ = uploadKnowledgeFiles(t, "json", files...)
	if !strings.Contains(out, `"errors": []`) || !strings.Contains(out, `"status": "unsupported"`) {
		t.Errorf("json = %s", out)
	}
}

// The server leaves out an upload deleted since it was stored, so a short answer can't be matched
// to the files by position: every file of that request is unknown, none reads as uploaded.
func TestKnowledgeUploadShortAnswerMatchesNoFile(t *testing.T) {
	dir := knowledgeUploadFiles(t, map[string]string{"a.md": "a", "b.md": "b", "c.md": "c"})
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(knowledgeUploadAnswer("ready", "ready")))
	})
	files := []string{filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md"), filepath.Join(dir, "c.md")}
	out, err := uploadKnowledgeFiles(t, "table", files...)
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok || cliErr.Code != "UPLOAD_INCOMPLETE" || !strings.Contains(cliErr.Message, "3 of 3") {
		t.Fatalf("error = %#v", err)
	}
	if strings.Contains(out, "ready") || strings.Count(out, "unknown") != 3 {
		t.Errorf("table:\n%s", out)
	}
	out, _ = uploadKnowledgeFiles(t, "json", files...)
	if !strings.Contains(out, `"status": "unknown"`) {
		t.Errorf("json = %s", out)
	}
}

// A failed request keeps the names of the files that did not go through before it.
func TestKnowledgeUploadRequestErrorNamesEveryFile(t *testing.T) {
	files := map[string]string{}
	args := []string{}
	for i := 0; i < knowledgeUploadBatch+1; i++ {
		files[fmt.Sprintf("f%02d.md", i)] = "text"
	}
	dir := knowledgeUploadFiles(t, files)
	for i := 0; i < knowledgeUploadBatch+1; i++ {
		args = append(args, filepath.Join(dir, fmt.Sprintf("f%02d.md", i)))
	}
	requests := 0
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		parts := knowledgeUploadParts(t, r)
		if requests == 2 {
			w.WriteHeader(500)
			return
		}
		statuses := make([]string, len(parts))
		for i := range statuses {
			statuses[i] = "pending"
		}
		statuses[0] = "unsupported"
		_, _ = w.Write([]byte(knowledgeUploadAnswer(statuses...)))
	})
	_, err := uploadKnowledgeFiles(t, "table", args...)
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok || cliErr.Code != "API_ERROR" {
		t.Fatalf("error = %#v", err)
	}
	for _, want := range []string{"f00.md (unsupported)", "f20.md (the request failed", "2 of 21"} {
		if !strings.Contains(cliErr.Message, want) {
			t.Errorf("message lacks %q: %s", want, cliErr.Message)
		}
	}
}

func TestKnowledgeUploadSendsTwentyFilesARequest(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < knowledgeUploadBatch+1; i++ {
		files[fmt.Sprintf("f%02d.md", i)] = "text"
	}
	dir := knowledgeUploadFiles(t, files)
	var sizes []int
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		parts := knowledgeUploadParts(t, r)
		sizes = append(sizes, len(parts))
		statuses := make([]string, len(parts))
		for i := range statuses {
			statuses[i] = "pending"
		}
		_, _ = w.Write([]byte(knowledgeUploadAnswer(statuses...)))
	})
	args := make([]string, 0, len(files))
	for i := 0; i < knowledgeUploadBatch+1; i++ {
		args = append(args, filepath.Join(dir, fmt.Sprintf("f%02d.md", i)))
	}
	if _, err := uploadKnowledgeFiles(t, "json", args...); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sizes) != "[20 1]" {
		t.Errorf("request sizes = %v, want [20 1]", sizes)
	}
}

func TestKnowledgeUploadRequestErrors(t *testing.T) {
	tests := []struct {
		status int
		code   string
		exit   int
		row    string
	}{
		{404, "KNOWLEDGE_UPLOADS_UNAVAILABLE", clierrors.ExitNotFound, "refused"},
		{405, "KNOWLEDGE_UPLOADS_UNAVAILABLE", clierrors.ExitNotFound, "refused"},
		{403, "FORBIDDEN", clierrors.ExitAuthFailed, "refused"},
		{401, "AUTH_FAILED", clierrors.ExitAuthFailed, "refused"},
		{422, "INVALID_ARGUMENTS", clierrors.ExitUsageError, "refused"},
		{413, "REQUEST_TOO_LARGE", clierrors.ExitGeneralError, "refused"},
		// The server may have stored the files before failing: the table must not say otherwise.
		{500, "API_ERROR", clierrors.ExitGeneralError, "unknown"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			dir := knowledgeUploadFiles(t, map[string]string{"a.md": "a"})
			knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"detail":"the server's\u001b[2J reason"}`))
			})
			out, err := uploadKnowledgeFiles(t, "table", filepath.Join(dir, "a.md"))
			cliErr, ok := err.(*clierrors.CLIError)
			if !ok || cliErr.Code != test.code || cliErr.ExitCode != test.exit {
				t.Fatalf("error = %#v, want %s exit %d", err, test.code, test.exit)
			}
			if strings.ContainsRune(cliErr.Message, '\x1b') {
				t.Errorf("message kept a control character: %q", cliErr.Message)
			}
			if !strings.Contains(out, test.row) {
				t.Errorf("table lacks %q:\n%s", test.row, out)
			}
		})
	}
}

func TestKnowledgeUploadsLists(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != knowledgeUploadsPath || r.URL.Query().Get("limit") != "2" {
			t.Errorf("request = %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"items":[
			{"id":"u1","filename":"arch\u001b[2J.pdf","size_bytes":3145728,"status":"ready","error":null,"uploaded_by":"ana@acme.com","created_at":"2026-11-23T10:00:00Z"},
			{"id":"u2","filename":"tool.exe","size_bytes":12,"status":"unsupported","error":"not a document","uploaded_by":"bob@acme.com [API]","created_at":"2026-11-22T10:00:00Z"}
		],"total":5}`))
	})
	command := &cobra.Command{}
	command.Flags().StringP("output", "o", "", "")
	command.Flags().Int("limit", 0, "")
	_ = command.Flags().Set("output", "table")
	_ = command.Flags().Set("limit", "2")
	out := new(bytes.Buffer)
	command.SetOut(out)
	if err := runKnowledgeUploads(command, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"arch [2J.pdf", "3.0 MiB", "ready", "ana@acme.com", "2026-11-23", "tool.exe", "unsupported", "not a document", "Showing uploads 1 to 2 of 5", "--offset 2"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Errorf("table kept a control character:\n%q", out)
	}
}

func TestKnowledgeUploadsOnAnOlderServer(t *testing.T) {
	knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	})
	command := &cobra.Command{}
	command.Flags().StringP("output", "o", "", "")
	command.Flags().Int("limit", 0, "")
	command.SetOut(new(bytes.Buffer))
	err := runKnowledgeUploads(command, nil)
	if cliErr, ok := err.(*clierrors.CLIError); !ok || cliErr.Code != "KNOWLEDGE_UPLOADS_UNAVAILABLE" || cliErr.ExitCode != clierrors.ExitNotFound {
		t.Fatalf("error = %#v", err)
	}
}

func TestKnowledgeUploadsPages(t *testing.T) {
	calls := knowledgePagesServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "200" || r.URL.Query().Get("limit") != "" {
			t.Errorf("request = %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"items":[],"total":120}`))
	})
	list := func(flags map[string]string) (string, error) {
		command := &cobra.Command{}
		command.Flags().StringP("output", "o", "", "")
		command.Flags().Int("limit", 0, "")
		command.Flags().Int("offset", 0, "")
		_ = command.Flags().Set("output", "table")
		for name, value := range flags {
			_ = command.Flags().Set(name, value)
		}
		out := new(bytes.Buffer)
		command.SetOut(out)
		err := runKnowledgeUploads(command, nil)
		return out.String(), err
	}
	for _, flags := range []map[string]string{{"limit": "0"}, {"limit": "201"}, {"offset": "-1"}} {
		if _, err := list(flags); knowledgeExitCode(t, err) != clierrors.ExitUsageError {
			t.Errorf("%v: exit %d", flags, knowledgeExitCode(t, err))
		}
	}
	if *calls != 0 {
		t.Fatalf("%d requests sent for invalid flags", *calls)
	}
	out, err := list(map[string]string{"offset": "200"})
	if err != nil || out != "No uploads past the newest 200; there are 120.\n" {
		t.Errorf("out = %q, err = %v", out, err)
	}
}
