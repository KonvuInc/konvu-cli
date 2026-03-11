# Konvu CLI Go Rewrite Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port konvu-cli from Python to Go for single-binary distribution with bundled Claude Code skills.

**Architecture:** Cobra CLI with stdlib HTTP client. Internal packages for config, errors, mapping, output formatting, API client, and OAuth device flow. Commands in `cmd/` call into `internal/` packages. Distribution via goreleaser → npm + GitHub Releases.

**Tech Stack:** Go 1.22+, github.com/spf13/cobra, golang.org/x/term, net/http, text/tabwriter

**Spec:** `docs/superpowers/specs/2026-03-11-go-rewrite-and-distribution-design.md`

**Python source (reference):** `konvu_cli/` — read these files for exact behavior to replicate.

---

## Chunk 1: Go Scaffold & Internal Packages

### Task 1: Initialize Go module and entry point

**Files:**
- Create: `go.mod`
- Create: `main.go`

- [ ] **Step 1: Initialize Go module**

Run: `go mod init github.com/KonvuTeam/konvu-cli`

- [ ] **Step 2: Create main.go**

```go
package main

import "github.com/KonvuTeam/konvu-cli/cmd"

func main() {
	cmd.Execute()
}
```

- [ ] **Step 3: Create cmd/root.go stub**

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "konvu",
	Short: "Konvu CLI - Security vulnerability management",
	Long:  "Konvu CLI for security vulnerability management from your terminal.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Add cobra dependency and verify build**

Run: `go get github.com/spf13/cobra && go build ./...`
Expected: Clean build, `konvu` binary produced

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum main.go cmd/root.go
git commit -m "feat: initialize Go module with cobra root command"
```

---

### Task 2: Config package

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

Port `konvu_cli/config.py` (63 lines). Platform-specific config dirs + env var helpers.

- [ ] **Step 1: Write failing test**

```go
package config

import (
	"os"
	"testing"
)

func TestGetAPIBaseURL_Default(t *testing.T) {
	os.Unsetenv("KONVU_API_URL")
	got := GetAPIBaseURL()
	if got != "https://api.konvu.com" {
		t.Errorf("GetAPIBaseURL() = %q, want %q", got, "https://api.konvu.com")
	}
}

func TestGetAPIBaseURL_Override(t *testing.T) {
	t.Setenv("KONVU_API_URL", "https://custom.api.com")
	got := GetAPIBaseURL()
	if got != "https://custom.api.com" {
		t.Errorf("GetAPIBaseURL() = %q, want %q", got, "https://custom.api.com")
	}
}

func TestGetZitadelDomain_Fallback(t *testing.T) {
	os.Unsetenv("KONVU_ZITADEL_DOMAIN")
	t.Setenv("ZITADEL_DOMAIN", "https://fallback.example.com")
	got := GetZitadelDomain()
	if got != "https://fallback.example.com" {
		t.Errorf("GetZitadelDomain() = %q, want %q", got, "https://fallback.example.com")
	}
}

func TestGetZitadelClientID_Primary(t *testing.T) {
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "primary-id")
	t.Setenv("ZITADEL_CLI_CLIENT_ID", "fallback-id")
	got := GetZitadelClientID()
	if got != "primary-id" {
		t.Errorf("GetZitadelClientID() = %q, want %q", got, "primary-id")
	}
}

func TestGetConfigDir(t *testing.T) {
	dir := GetConfigDir()
	if dir == "" {
		t.Error("GetConfigDir() returned empty string")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement config package**

```go
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const AppName = "konvu"

const (
	DefaultAPIBaseURL      = "https://api.konvu.com"
	DefaultZitadelDomain   = "https://auth.konvu.com"
	DefaultZitadelClientID = ""
)

func GetConfigDir() string {
	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, AppName)
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, AppName)
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", AppName)
	default: // Linux/Unix — XDG
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, AppName)
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", AppName)
	}
}

func GetCredentialsPath() string {
	return filepath.Join(GetConfigDir(), "credentials.json")
}

func GetAPIBaseURL() string {
	if v := os.Getenv("KONVU_API_URL"); v != "" {
		return v
	}
	return DefaultAPIBaseURL
}

func GetZitadelDomain() string {
	if v := os.Getenv("KONVU_ZITADEL_DOMAIN"); v != "" {
		return v
	}
	if v := os.Getenv("ZITADEL_DOMAIN"); v != "" {
		return v
	}
	return DefaultZitadelDomain
}

func GetZitadelClientID() string {
	if v := os.Getenv("KONVU_ZITADEL_CLIENT_ID"); v != "" {
		return v
	}
	if v := os.Getenv("ZITADEL_CLI_CLIENT_ID"); v != "" {
		return v
	}
	return DefaultZitadelClientID
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add config package with platform-specific dirs and env vars"
```

---

### Task 3: Errors package

**Files:**
- Create: `internal/errors/errors.go`
- Create: `internal/errors/errors_test.go`

Port `konvu_cli/errors.py`. Exit codes + CLIError struct + JSON formatting.

- [ ] **Step 1: Write failing test**

```go
package errors

import (
	"encoding/json"
	"testing"
)

func TestCLIError_Error(t *testing.T) {
	err := &CLIError{
		Code:     "AUTH_FAILED",
		Message:  "Session expired",
		ExitCode: ExitAuthFailed,
	}
	if err.Error() != "Session expired" {
		t.Errorf("Error() = %q, want %q", err.Error(), "Session expired")
	}
}

func TestFormatErrorJSON(t *testing.T) {
	err := &CLIError{
		Code:       "NOT_FOUND",
		Message:    "Finding not found",
		Suggestion: "Run 'konvu finding list'",
		Retryable:  false,
		ExitCode:   ExitNotFound,
	}
	got := FormatErrorJSON(err)
	var parsed map[string]any
	if jsonErr := json.Unmarshal([]byte(got), &parsed); jsonErr != nil {
		t.Fatalf("FormatErrorJSON() returned invalid JSON: %v", jsonErr)
	}
	errObj, ok := parsed["error"].(map[string]any)
	if !ok {
		t.Fatal("missing 'error' key in JSON output")
	}
	if errObj["code"] != "NOT_FOUND" {
		t.Errorf("code = %v, want NOT_FOUND", errObj["code"])
	}
}

func TestExitCodes(t *testing.T) {
	if ExitGeneralError != 1 {
		t.Errorf("ExitGeneralError = %d, want 1", ExitGeneralError)
	}
	if ExitUsageError != 2 {
		t.Errorf("ExitUsageError = %d, want 2", ExitUsageError)
	}
	if ExitNotFound != 3 {
		t.Errorf("ExitNotFound = %d, want 3", ExitNotFound)
	}
	if ExitAuthFailed != 4 {
		t.Errorf("ExitAuthFailed = %d, want 4", ExitAuthFailed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/errors/ -v`
Expected: FAIL

- [ ] **Step 3: Implement errors package**

```go
package errors

import (
	"encoding/json"
	"fmt"
)

const (
	ExitGeneralError = 1
	ExitUsageError   = 2
	ExitNotFound     = 3
	ExitAuthFailed   = 4
)

type CLIError struct {
	Code       string
	Message    string
	Suggestion string
	Retryable  bool
	ExitCode   int
}

func (e *CLIError) Error() string {
	return e.Message
}

func FormatErrorJSON(err *CLIError) string {
	obj := map[string]any{
		"error": map[string]any{
			"code":       err.Code,
			"message":    err.Message,
			"suggestion": err.Suggestion,
			"retryable":  err.Retryable,
		},
	}
	b, _ := json.MarshalIndent(obj, "", "  ")
	return string(b)
}

// Convenience constructors matching Python patterns.

func NewAuthError(msg string) *CLIError {
	return &CLIError{
		Code:       "AUTH_FAILED",
		Message:    msg,
		Suggestion: "Run 'konvu login' to authenticate.",
		ExitCode:   ExitAuthFailed,
	}
}

func NewAPIError(msg string) *CLIError {
	return &CLIError{
		Code:       "API_ERROR",
		Message:    msg,
		Suggestion: "Check 'konvu whoami' to verify your session.",
		Retryable:  true,
		ExitCode:   ExitGeneralError,
	}
}

func NewNotFoundError(resource, id string) *CLIError {
	return &CLIError{
		Code:       fmt.Sprintf("%s_NOT_FOUND", resource),
		Message:    fmt.Sprintf("%s '%s' not found", resource, id),
		Suggestion: "Run 'konvu finding list' to see available findings.",
		ExitCode:   ExitNotFound,
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/errors/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/errors/
git commit -m "feat: add errors package with CLIError, exit codes, and JSON formatting"
```

---

### Task 4: Mapping package

**Files:**
- Create: `internal/mapping/mapping.go`
- Create: `internal/mapping/mapping_test.go`

Port `konvu_cli/mapping.py`. Assessment status enum, recommendation-to-assessment mapping, colors, summaries.

- [ ] **Step 1: Write failing test**

```go
package mapping

import "testing"

func TestRecommendationToAssessment(t *testing.T) {
	tests := []struct {
		rec  string
		want AssessmentStatus
	}{
		{"to_fix", Exploitable},
		{"to_dismiss", FalsePositive},
		{"no_qualification", NotAssessed},
		{"monitoring", Inconclusive},
		{"install_runtime", Inconclusive},
		{"install_github", Inconclusive},
		{"no_recommendation", Inconclusive},
		{"", Inconclusive},
	}
	for _, tt := range tests {
		got := RecommendationToAssessment(tt.rec)
		if got != tt.want {
			t.Errorf("RecommendationToAssessment(%q) = %q, want %q", tt.rec, got, tt.want)
		}
	}
}

func TestAssessmentToRecommendation(t *testing.T) {
	recs := AssessmentToRecommendation(Exploitable)
	if len(recs) != 1 || recs[0] != "to_fix" {
		t.Errorf("AssessmentToRecommendation(Exploitable) = %v, want [to_fix]", recs)
	}

	recs = AssessmentToRecommendation(NotAssessed)
	if len(recs) != 2 {
		t.Errorf("AssessmentToRecommendation(NotAssessed) = %v, want 2 items", recs)
	}
}

func TestGetAssessmentColor(t *testing.T) {
	if c := GetAssessmentColor(string(Exploitable)); c == "" {
		t.Error("GetAssessmentColor(exploitable) returned empty string")
	}
}

func TestGetAssessmentSummary(t *testing.T) {
	summary, nextSteps := GetAssessmentSummary(Exploitable)
	if summary == "" || nextSteps == "" {
		t.Error("GetAssessmentSummary(Exploitable) returned empty strings")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mapping/ -v`
Expected: FAIL

- [ ] **Step 3: Implement mapping package**

```go
package mapping

type AssessmentStatus string

const (
	Exploitable  AssessmentStatus = "exploitable"
	FalsePositive AssessmentStatus = "false-positive"
	Inconclusive AssessmentStatus = "inconclusive"
	NotAssessed  AssessmentStatus = "not-assessed"
)

// AllStatuses in display order.
var AllStatuses = []AssessmentStatus{Exploitable, FalsePositive, Inconclusive, NotAssessed}

// Backend recommendation values.
const (
	RecToFix              = "to_fix"
	RecToDismiss          = "to_dismiss"
	RecMonitoring         = "monitoring"
	RecInstallRuntime     = "install_runtime"
	RecInstallGithub      = "install_github"
	RecNoRecommendation   = "no_recommendation"
	RecNoQualification    = "no_qualification"
)

func RecommendationToAssessment(rec string) AssessmentStatus {
	switch rec {
	case RecToFix:
		return Exploitable
	case RecToDismiss:
		return FalsePositive
	case RecNoQualification:
		return NotAssessed
	default:
		return Inconclusive
	}
}

func AssessmentToRecommendation(a AssessmentStatus) []string {
	switch a {
	case Exploitable:
		return []string{RecToFix}
	case FalsePositive:
		return []string{RecToDismiss}
	case NotAssessed:
		return []string{RecNoQualification, RecNoRecommendation}
	default: // Inconclusive
		return []string{RecMonitoring, RecInstallRuntime, RecInstallGithub}
	}
}

// ANSI color codes for terminal output.
var assessmentColors = map[AssessmentStatus]string{
	Exploitable:  "\033[1;31m", // bold red
	FalsePositive: "\033[32m",  // green
	Inconclusive: "\033[33m",   // yellow
	NotAssessed:  "\033[2m",    // dim
}

const colorReset = "\033[0m"

func GetAssessmentColor(status string) string {
	return assessmentColors[AssessmentStatus(status)]
}

func ColorReset() string {
	return colorReset
}

func Colorize(text string, status AssessmentStatus) string {
	c := assessmentColors[status]
	if c == "" {
		return text
	}
	return c + text + colorReset
}

func GetAssessmentSummary(a AssessmentStatus) (summary, nextSteps string) {
	switch a {
	case Exploitable:
		return "A vulnerable function is being executed in your application.",
			"Prioritize remediation of this vulnerability."
	case FalsePositive:
		return "Not exploitable in your context.",
			"You may deprioritize remediation of this vulnerability."
	case NotAssessed:
		return "This vulnerability has not been assessed yet.",
			"Additional analysis may be required."
	default:
		return "Unable to determine exploitability with high confidence.",
			"Review the exploitability conditions manually."
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mapping/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mapping/
git commit -m "feat: add mapping package with assessment/recommendation mappings"
```

---

### Task 5: Output formatting package

**Files:**
- Create: `internal/output/format.go`
- Create: `internal/output/table.go`
- Create: `internal/output/format_test.go`

Port `konvu_cli/output/detection.py` and `konvu_cli/output/formatters.py`.

- [ ] **Step 1: Write failing test**

```go
package output

import (
	"strings"
	"testing"
)

func TestDetectOutputFormat_Explicit(t *testing.T) {
	if f := DetectOutputFormat("json"); f != JSON {
		t.Errorf("got %v, want JSON", f)
	}
	if f := DetectOutputFormat("TABLE"); f != Table {
		t.Errorf("got %v, want Table", f)
	}
	if f := DetectOutputFormat("csv"); f != CSV {
		t.Errorf("got %v, want CSV", f)
	}
}

func TestDetectOutputFormat_Empty(t *testing.T) {
	// When no explicit format, behavior depends on isatty.
	// In test context stdout is not a TTY, so default is JSON.
	f := DetectOutputFormat("")
	if f != JSON {
		t.Errorf("got %v, want JSON (non-TTY default)", f)
	}
}

func TestFormatJSON(t *testing.T) {
	data := map[string]any{"key": "value", "num": 42}
	got := FormatJSON(data)
	if !strings.Contains(got, `"key": "value"`) {
		t.Errorf("FormatJSON missing expected content: %s", got)
	}
}

func TestFormatCSV(t *testing.T) {
	data := map[string]any{
		"items": []any{
			map[string]any{"id": "1", "name": "foo"},
			map[string]any{"id": "2", "name": "bar"},
		},
	}
	got := FormatCSV(data, []string{"id", "name"}, "items")
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 { // header + 2 rows
		t.Errorf("FormatCSV got %d lines, want 3", len(lines))
	}
}

func TestFormatQuiet(t *testing.T) {
	items := []map[string]any{
		{"id": "abc-1"},
		{"id": "abc-2"},
	}
	got := FormatQuiet(items, "id")
	if got != "abc-1\nabc-2" {
		t.Errorf("FormatQuiet = %q, want %q", got, "abc-1\nabc-2")
	}
}

func TestFilterFields(t *testing.T) {
	data := map[string]any{"a": 1, "b": 2, "c": 3}
	got := FilterFields(data, []string{"a", "c"})
	if len(got) != 2 {
		t.Errorf("FilterFields returned %d fields, want 2", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/output/ -v`
Expected: FAIL

- [ ] **Step 3: Implement format.go**

```go
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

type OutputFormat int

const (
	JSON OutputFormat = iota
	Table
	CSV
)

func DetectOutputFormat(explicit string) OutputFormat {
	switch strings.ToLower(explicit) {
	case "json":
		return JSON
	case "table":
		return Table
	case "csv":
		return CSV
	}
	// Auto-detect: table for TTY, JSON for pipe
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return Table
	}
	return JSON
}

func FormatJSON(data any) string {
	b, _ := json.MarshalIndent(data, "", "  ")
	return string(b)
}

func FormatCSV(data map[string]any, columns []string, listKey string) string {
	var sb strings.Builder
	w := csv.NewWriter(&sb)

	// Header
	w.Write(columns)

	// Rows
	items, _ := data[listKey].([]any)
	for _, item := range items {
		row, _ := item.(map[string]any)
		record := make([]string, len(columns))
		for i, col := range columns {
			record[i] = fmt.Sprintf("%v", row[col])
		}
		w.Write(record)
	}
	w.Flush()
	return sb.String()
}

func FormatQuiet(items []map[string]any, idField string) string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, fmt.Sprintf("%v", item[idField]))
	}
	return strings.Join(ids, "\n")
}

func FilterFields(data map[string]any, fields []string) map[string]any {
	if fields == nil {
		return data
	}
	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}
	result := make(map[string]any)
	for k, v := range data {
		if fieldSet[k] {
			result[k] = v
		}
	}
	return result
}
```

- [ ] **Step 4: Implement table.go**

```go
package output

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/KonvuTeam/konvu-cli/internal/mapping"
)

// StyleCellFunc optionally styles a cell value. Return value is printed as-is.
type StyleCellFunc func(column, value string) string

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func FormatTable(data map[string]any, columns []string, listKey string, styleCell StyleCellFunc) string {
	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 4, 2, ' ', 0)

	// Header
	headers := make([]string, len(columns))
	for i, col := range columns {
		headers[i] = titleCase(strings.ReplaceAll(col, "_", " "))
	}
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	// Rows
	items, _ := data[listKey].([]any)
	for _, item := range items {
		row, _ := item.(map[string]any)
		cells := make([]string, len(columns))
		for i, col := range columns {
			val := fmt.Sprintf("%v", row[col])
			if styleCell != nil {
				val = styleCell(col, val)
			}
			cells[i] = val
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	w.Flush()
	return sb.String()
}

// DefaultStyleCell applies color to assessment columns.
func DefaultStyleCell(column, value string) string {
	if column == "assessment" {
		return mapping.Colorize(value, mapping.AssessmentStatus(value))
	}
	return value
}

// PrintStderr prints a message to stderr.
func PrintStderr(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}
```

- [ ] **Step 5: Add x/term dependency and run tests**

Run: `go get golang.org/x/term && go test ./internal/output/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/output/ go.mod go.sum
git commit -m "feat: add output package with JSON/table/CSV formatting and TTY detection"
```

---

### Task 6: API client package

**Files:**
- Create: `internal/api/client.go`
- Create: `internal/api/client_test.go`

Port `konvu_cli/api/client.py`. HTTP client with Bearer token auth, GET/POST, error handling.

- [ ] **Step 1: Write failing test**

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestClient_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing or wrong Authorization header")
		}
		if r.URL.Path != "/sca_findings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()

	data, err := client.Get("/sca_findings", nil)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if data["total"] != float64(0) {
		t.Errorf("total = %v, want 0", data["total"])
	}
}

func TestClient_Get_401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte("unauthorized"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "bad-token")
	defer client.Close()

	_, err := client.Get("/test", nil)
	if err == nil {
		t.Fatal("expected AuthenticationError, got nil")
	}
	if _, ok := err.(*AuthenticationError); !ok {
		t.Errorf("expected *AuthenticationError, got %T", err)
	}
}

func TestClient_Post(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()

	data, err := client.Post("/test", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	if data["status"] != "ok" {
		t.Errorf("status = %v, want ok", data["status"])
	}
}

func TestClient_Post_204(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()

	data, err := client.Post("/test", nil)
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil body for 204, got %v", data)
	}
}

func TestClient_TokenFromEnv(t *testing.T) {
	t.Setenv("KONVU_ACCESS_TOKEN", "env-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer env-token" {
			t.Error("expected env token in Authorization header")
		}
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	defer client.Close()

	_, err := client.Get("/test", nil)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -v`
Expected: FAIL

- [ ] **Step 3: Implement client.go**

```go
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/KonvuTeam/konvu-cli/internal/config"
)

type AuthenticationError struct {
	Message string
}

func (e *AuthenticationError) Error() string { return e.Message }

type APIError struct {
	Message    string
	StatusCode int
}

func (e *APIError) Error() string { return e.Message }

type Client struct {
	baseURL       string
	explicitToken string
	httpClient    *http.Client
}

func NewClient(baseURL, accessToken string) *Client {
	if baseURL == "" {
		baseURL = config.GetAPIBaseURL()
	}
	return &Client{
		baseURL:       baseURL,
		explicitToken: accessToken,
		httpClient:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) Close() {
	c.httpClient.CloseIdleConnections()
}

func (c *Client) getToken() (string, error) {
	if c.explicitToken != "" {
		return c.explicitToken, nil
	}
	if envToken := os.Getenv("KONVU_ACCESS_TOKEN"); envToken != "" {
		return envToken, nil
	}
	return readTokenFromFile()
}

func readTokenFromFile() (string, error) {
	credsPath := config.GetCredentialsPath()
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return "", &AuthenticationError{Message: "Not logged in. Run 'konvu login' first."}
	}
	var creds map[string]any
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", &AuthenticationError{Message: "Corrupted credentials. Run 'konvu login' again."}
	}
	token, ok := creds["access_token"].(string)
	if !ok || token == "" {
		return "", &AuthenticationError{Message: "Invalid credentials. Run 'konvu login' again."}
	}
	return token, nil
}

func (c *Client) authHeader() (string, error) {
	token, err := c.getToken()
	if err != nil {
		return "", err
	}
	return "Bearer " + token, nil
}

func (c *Client) checkResponse(resp *http.Response) error {
	if resp.StatusCode == 401 {
		return &AuthenticationError{Message: "Session expired. Run 'konvu login' again."}
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			Message:    fmt.Sprintf("API error: %s", string(body)),
			StatusCode: resp.StatusCode,
		}
	}
	return nil
}

func (c *Client) Get(path string, params map[string]any) (map[string]any, error) {
	reqURL := c.baseURL + path
	if len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			switch val := v.(type) {
			case []string:
				for _, s := range val {
					values.Add(k, s)
				}
			case string:
				values.Set(k, val)
			default:
				values.Set(k, fmt.Sprintf("%v", val))
			}
		}
		reqURL += "?" + values.Encode()
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	auth, err := c.authHeader()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Post(path string, data map[string]any) (map[string]any, error) {
	reqURL := c.baseURL + path

	var body io.Reader
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest("POST", reqURL, body)
	if err != nil {
		return nil, err
	}

	auth, err := c.authHeader()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	if resp.StatusCode == 204 {
		return nil, nil
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/
git commit -m "feat: add API client with Bearer auth, GET/POST, and error handling"
```

---

### Task 7: Auth device flow package

**Files:**
- Create: `internal/auth/device_flow.go`
- Create: `internal/auth/credentials.go`
- Create: `internal/auth/device_flow_test.go`

Port `konvu_cli/auth/oauth.py`. RFC 8628 device flow + credential file management.

- [ ] **Step 1: Write failing test**

```go
package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPerformDeviceFlowLogin(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/v2/device_authorization":
			json.NewEncoder(w).Encode(map[string]any{
				"device_code":               "test-device-code",
				"user_code":                 "ABCD-1234",
				"verification_uri":          "https://example.com/verify",
				"verification_uri_complete": "https://example.com/verify?code=ABCD-1234",
				"interval":                  1,
				"expires_in":                300,
			})
		case "/oauth/v2/token":
			callCount++
			if callCount == 1 {
				// First poll: pending
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
			} else {
				// Second poll: success
				json.NewEncoder(w).Encode(map[string]any{
					"access_token": "test-access-token",
					"token_type":   "Bearer",
					"expires_in":   3600,
				})
			}
		}
	}))
	defer server.Close()

	noop := func(string) {}
	result, err := PerformDeviceFlowLogin(server.URL, "test-client-id", 10, noop)
	if err != nil {
		t.Fatalf("PerformDeviceFlowLogin error: %v", err)
	}
	if result["access_token"] != "test-access-token" {
		t.Errorf("access_token = %v, want test-access-token", result["access_token"])
	}
}

func TestSaveCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")

	err := SaveCredentials(path, map[string]any{
		"access_token": "my-token",
		"expires_in":   float64(3600),
	})
	if err != nil {
		t.Fatalf("SaveCredentials error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var creds map[string]any
	json.Unmarshal(data, &creds)
	if creds["access_token"] != "my-token" {
		t.Errorf("access_token = %v, want my-token", creds["access_token"])
	}

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file permissions = %o, want 600", info.Mode().Perm())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -v`
Expected: FAIL

- [ ] **Step 3: Implement credentials.go**

```go
package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

func SaveCredentials(path string, tokenData map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	creds := map[string]any{
		"access_token": tokenData["access_token"],
	}

	if expiresIn, ok := tokenData["expires_in"].(float64); ok {
		creds["expires_at"] = int(time.Now().Unix()) + int(expiresIn)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer fd.Close()

	_, err = fd.Write(data)
	return err
}
```

- [ ] **Step 4: Implement device_flow.go**

```go
package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultLoginTimeout = 300
	DefaultPollInterval = 5
)

func PerformDeviceFlowLogin(zitadelDomain, clientID string, timeout float64, echo func(string)) (map[string]any, error) {
	if clientID == "" {
		return nil, fmt.Errorf("Zitadel client ID not configured. Set KONVU_ZITADEL_CLIENT_ID.")
	}

	// Step 1: Request device code
	deviceAuthURL := zitadelDomain + "/oauth/v2/device_authorization"
	resp, err := http.PostForm(deviceAuthURL, url.Values{
		"client_id": {clientID},
		"scope":     {"openid profile email"},
	})
	if err != nil {
		return nil, fmt.Errorf("device authorization request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device authorization failed: %s", string(body))
	}

	var deviceData map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&deviceData); err != nil {
		return nil, err
	}

	deviceCode := deviceData["device_code"].(string)
	userCode := deviceData["user_code"].(string)
	verificationURI := deviceData["verification_uri"].(string)
	verificationURIComplete, _ := deviceData["verification_uri_complete"].(string)
	pollInterval := DefaultPollInterval
	if v, ok := deviceData["interval"].(float64); ok {
		pollInterval = int(v)
	}
	expiresIn := timeout
	if v, ok := deviceData["expires_in"].(float64); ok && v < timeout {
		expiresIn = v
	}

	// Step 2: Display instructions
	echo(fmt.Sprintf("\nTo authenticate, visit: %s", verificationURI))
	echo(fmt.Sprintf("And enter code: %s\n", userCode))

	openURL := verificationURIComplete
	if openURL == "" {
		openURL = verificationURI
	}
	openBrowser(openURL)

	echo(fmt.Sprintf("Waiting for authentication (timeout: %ds)...", int(expiresIn)))

	// Step 3: Poll for token
	return pollForToken(zitadelDomain, clientID, deviceCode, pollInterval, expiresIn, echo)
}

func pollForToken(zitadelDomain, clientID, deviceCode string, pollInterval int, timeout float64, echo func(string)) (map[string]any, error) {
	tokenURL := zitadelDomain + "/oauth/v2/token"
	start := time.Now()

	for time.Since(start).Seconds() < timeout {
		resp, err := http.PostForm(tokenURL, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {deviceCode},
			"client_id":   {clientID},
		})
		if err != nil {
			return nil, err
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 200 {
			var tokenData map[string]any
			if err := json.Unmarshal(body, &tokenData); err != nil {
				return nil, err
			}
			if _, ok := tokenData["access_token"]; !ok {
				return nil, fmt.Errorf("token response missing access_token")
			}
			return map[string]any{
				"access_token": tokenData["access_token"],
				"token_type":   tokenData["token_type"],
				"expires_in":   tokenData["expires_in"],
			}, nil
		}

		var errData map[string]string
		json.Unmarshal(body, &errData)
		errCode := errData["error"]

		switch errCode {
		case "authorization_pending":
			time.Sleep(time.Duration(pollInterval) * time.Second)
		case "slow_down":
			pollInterval += 5
			time.Sleep(time.Duration(pollInterval) * time.Second)
		case "expired_token":
			return nil, fmt.Errorf("device code expired. Please try again.")
		case "access_denied":
			return nil, fmt.Errorf("authentication was denied by the user.")
		default:
			desc := errData["error_description"]
			if desc == "" {
				desc = errCode
			}
			if desc == "" {
				desc = string(body)
			}
			return nil, fmt.Errorf("authentication failed: %s", desc)
		}
	}

	return nil, fmt.Errorf("login timed out. Please try again.\nYou can also set KONVU_ACCESS_TOKEN environment variable manually.")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
```

Note: `openBrowser` replaces Python's `webbrowser.open`. The `strings` import can be removed if unused — check at build time.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/auth/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/auth/
git commit -m "feat: add RFC 8628 device flow auth and credential storage"
```

---

### Task 8: Interactive picker

**Files:**
- Create: `internal/output/picker.go`
- Create: `internal/output/picker_test.go`

Port `konvu_cli/output/picker.py`. Arrow-key interactive picker with fallback for non-TTY.

- [ ] **Step 1: Write failing test**

```go
package output

import (
	"strings"
	"testing"
)

func TestFallbackPick(t *testing.T) {
	// FallbackPick with a simulated input of "2\n"
	input := strings.NewReader("2\n")
	idx := FallbackPick("Choose:", []string{"Option A", "Option B"}, 0, input)
	if idx != 1 { // "2" maps to index 1
		t.Errorf("FallbackPick = %d, want 1", idx)
	}
}

func TestFallbackPick_Default(t *testing.T) {
	// Empty input returns default
	input := strings.NewReader("\n")
	idx := FallbackPick("Choose:", []string{"A", "B"}, 0, input)
	if idx != 0 {
		t.Errorf("FallbackPick default = %d, want 0", idx)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/output/ -v -run TestFallback`
Expected: FAIL

- [ ] **Step 3: Implement picker.go**

```go
package output

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// Pick presents an interactive picker. Falls back to numbered prompt for non-TTY.
func Pick(title string, options []string, defaultIdx int) int {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return FallbackPick(title, options, defaultIdx, os.Stdin)
	}
	idx, err := interactivePick(title, options, defaultIdx)
	if err != nil {
		return FallbackPick(title, options, defaultIdx, os.Stdin)
	}
	return idx
}

func interactivePick(title string, options []string, defaultIdx int) (int, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return 0, err
	}
	defer term.Restore(fd, oldState)

	selected := defaultIdx
	buf := make([]byte, 3)

	// Initial render
	renderPicker(title, options, selected)

	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return selected, err
		}

		switch {
		case n == 1 && (buf[0] == '\r' || buf[0] == '\n'):
			// Enter — confirm
			fmt.Fprint(os.Stderr, "\r\n")
			return selected, nil
		case n == 1 && buf[0] == 3:
			// Ctrl-C
			fmt.Fprint(os.Stderr, "\r\n")
			return -1, fmt.Errorf("cancelled")
		case n == 3 && buf[0] == 27 && buf[1] == '[':
			if buf[2] == 'A' { // Up
				selected = (selected - 1 + len(options)) % len(options)
			} else if buf[2] == 'B' { // Down
				selected = (selected + 1) % len(options)
			}
			// Move cursor up to re-render
			for i := 0; i < len(options)+2; i++ {
				fmt.Fprint(os.Stderr, "\033[A\033[2K")
			}
			renderPicker(title, options, selected)
		}
	}
}

func renderPicker(title string, options []string, selected int) {
	fmt.Fprintf(os.Stderr, "  %s\r\n\r\n", title)
	for i, opt := range options {
		if i == selected {
			fmt.Fprintf(os.Stderr, "  \033[1;36m❯\033[0m \033[1m%s\033[0m\r\n", opt)
		} else {
			fmt.Fprintf(os.Stderr, "    \033[2m%s\033[0m\r\n", opt)
		}
	}
}

// FallbackPick is a numbered prompt fallback for non-TTY or when interactive fails.
func FallbackPick(title string, options []string, defaultIdx int, reader io.Reader) int {
	fmt.Fprintf(os.Stderr, "\n%s\n\n", title)
	for i, opt := range options {
		fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, opt)
	}
	fmt.Fprintf(os.Stderr, "\nEnter choice [%d]: ", defaultIdx+1)

	scanner := bufio.NewScanner(reader)
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			return defaultIdx
		}
		if idx, err := strconv.Atoi(text); err == nil && idx >= 1 && idx <= len(options) {
			return idx - 1
		}
	}
	return defaultIdx
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/output/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/output/picker.go internal/output/picker_test.go
git commit -m "feat: add interactive arrow-key picker with non-TTY fallback"
```

---

## Chunk 2: CLI Commands

### Task 9: Version command

**Files:**
- Create: `cmd/version.go`

- [ ] **Step 1: Implement version command**

```go
package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/KonvuTeam/konvu-cli/internal/config"
	"github.com/KonvuTeam/konvu-cli/internal/output"
	"github.com/spf13/cobra"
)

var Version = "dev" // overridden by goreleaser via ldflags

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		outputFlag, _ := cmd.Flags().GetString("output")
		format := output.DetectOutputFormat(outputFlag)

		if format == output.JSON {
			data := map[string]string{
				"version": Version,
				"api_url": config.GetAPIBaseURL(),
			}
			b, _ := json.MarshalIndent(data, "", "  ")
			fmt.Println(string(b))
		} else {
			fmt.Printf("konvu-cli %s (api: %s)\n", Version, config.GetAPIBaseURL())
		}
	},
}

func init() {
	versionCmd.Flags().StringP("output", "o", "", "Output format: json, text")
	rootCmd.AddCommand(versionCmd)
}
```

- [ ] **Step 2: Build and verify**

Run: `go build -o konvu . && ./konvu version`
Expected: `konvu-cli 0.2.0 (api: https://api.konvu.com)`

- [ ] **Step 3: Commit**

```bash
git add cmd/version.go
git commit -m "feat: add version command"
```

---

### Task 10: Skills path command

**Files:**
- Modify: `cmd/root.go`

The spec requires a `konvu skills path` command so Claude Code can discover bundled skills.

- [ ] **Step 1: Add skills command to root.go**

```go
var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage bundled Claude Code skills",
}

var skillsPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the path to bundled skills directory",
	Run: func(cmd *cobra.Command, args []string) {
		// Skills are bundled alongside the binary
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		// Resolve symlinks (npm installs create symlinks)
		exe, _ = filepath.EvalSymlinks(exe)
		skillsDir := filepath.Join(filepath.Dir(exe), "..", "skills")
		// Clean the path
		fmt.Println(filepath.Clean(skillsDir))
	},
}

func init() {
	skillsCmd.AddCommand(skillsPathCmd)
	rootCmd.AddCommand(skillsCmd)
}
```

Add `"path/filepath"` to the imports in root.go.

- [ ] **Step 2: Build and verify**

Run: `go build -o konvu . && ./konvu skills path`
Expected: Prints a path ending in `/skills`

- [ ] **Step 3: Commit**

```bash
git add cmd/root.go
git commit -m "feat: add skills path command for Claude Code skill discovery"
```

---

### Task 11: Auth commands (login, logout, whoami)

**Files:**
- Create: `cmd/auth.go`

Port `konvu_cli/commands/auth.py`. This is the most complex command due to the interactive picker and dual auth flows.

- [ ] **Step 1: Implement auth commands**

```go
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/KonvuTeam/konvu-cli/internal/api"
	"github.com/KonvuTeam/konvu-cli/internal/auth"
	"github.com/KonvuTeam/konvu-cli/internal/config"
	"github.com/KonvuTeam/konvu-cli/internal/output"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication commands",
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show current user and company",
	RunE: func(cmd *cobra.Command, args []string) error {
		outputFlag, _ := cmd.Flags().GetString("output")
		format := output.DetectOutputFormat(outputFlag)

		client := api.NewClient("", "")
		defer client.Close()

		data, err := client.Get("/companies/current", nil)
		if err != nil {
			if _, ok := err.(*api.AuthenticationError); ok {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		if format == output.JSON {
			fmt.Println(output.FormatJSON(data))
		} else {
			fmt.Printf("Company:        %v\n", data["name"])
			fmt.Printf("Repositories:   %v\n", data["repositories_count"])
			fmt.Printf("Integrations:   %v\n", data["integrations_count"])
		}
		return nil
	},
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Konvu",
	RunE: func(cmd *cobra.Command, args []string) error {
		timeout, _ := cmd.Flags().GetInt("timeout")
		apiKeyFlag, _ := cmd.Flags().GetString("api-key")
		apiKeySet := cmd.Flags().Changed("api-key")

		if apiKeySet {
			key := apiKeyFlag
			if key == "" {
				key = promptAPIKey()
			}
			return loginWithAPIKey(key)
		}

		// Interactive picker
		domain := config.GetZitadelDomain()
		clientID := config.GetZitadelClientID()
		oauthAvailable := clientID != "" && strings.HasPrefix(domain, "https://")

		if !oauthAvailable {
			key := promptAPIKey()
			return loginWithAPIKey(key)
		}

		choice := output.Pick(
			"How would you like to authenticate?",
			[]string{"Browser login (OAuth)", "API key"},
			0,
		)

		if choice == 1 {
			key := promptAPIKey()
			return loginWithAPIKey(key)
		}
		return loginWithOAuth(timeout)
	},
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear stored credentials",
	Run: func(cmd *cobra.Command, args []string) {
		credsPath := config.GetCredentialsPath()
		if err := os.Remove(credsPath); err != nil {
			if os.IsNotExist(err) {
				fmt.Println("Not currently logged in.")
				return
			}
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		fmt.Println("Logged out successfully.")
	},
}

func promptAPIKey() string {
	fmt.Fprintln(os.Stderr, "\nCreate an API key at: https://app.konvu.com/configuration/api_keys\n")
	fmt.Fprint(os.Stderr, "Paste your API key: ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

func loginWithAPIKey(apiKey string) error {
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "Error: API key cannot be empty.")
		os.Exit(1)
	}

	fmt.Println("Validating API key...")
	client := api.NewClient("", apiKey)
	defer client.Close()

	company, err := client.Get("/companies/current", nil)
	if err != nil {
		if _, ok := err.(*api.AuthenticationError); ok {
			fmt.Fprintln(os.Stderr, "Error: Invalid API key.")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	if err := auth.SaveCredentials(config.GetCredentialsPath(), map[string]any{
		"access_token": apiKey,
	}); err != nil {
		return err
	}

	fmt.Printf("Logged in to: %v\n", company["name"])
	return nil
}

func loginWithOAuth(timeout int) error {
	fmt.Println("Starting browser login...")

	echo := func(msg string) { fmt.Println(msg) }

	tokenData, err := auth.PerformDeviceFlowLogin(
		config.GetZitadelDomain(),
		config.GetZitadelClientID(),
		float64(timeout),
		echo,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		fmt.Fprintln(os.Stderr, "If browser login fails, try: konvu login --api-key")
		os.Exit(1)
	}

	if err := auth.SaveCredentials(config.GetCredentialsPath(), tokenData); err != nil {
		return err
	}

	fmt.Println("\nLogin successful!")

	client := api.NewClient("", "")
	defer client.Close()
	if company, err := client.Get("/companies/current", nil); err == nil {
		fmt.Printf("Logged in to: %v\n", company["name"])
	}

	return nil
}

func init() {
	whoamiCmd.Flags().StringP("output", "o", "", "Output format: json, table")
	loginCmd.Flags().IntP("timeout", "t", auth.DefaultLoginTimeout, "Login timeout in seconds")
	loginCmd.Flags().String("api-key", "", "Authenticate with an API key")

	authCmd.AddCommand(whoamiCmd)
	authCmd.AddCommand(loginCmd)
	authCmd.AddCommand(logoutCmd)

	rootCmd.AddCommand(authCmd)

	// Top-level convenience aliases — must clone flag definitions
	whoamiAlias := &cobra.Command{
		Use:   "whoami",
		Short: "Show current user and company",
		RunE:  whoamiCmd.RunE,
	}
	whoamiAlias.Flags().StringP("output", "o", "", "Output format: json, table")
	rootCmd.AddCommand(whoamiAlias)

	loginAlias := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with Konvu",
		RunE:  loginCmd.RunE,
	}
	loginAlias.Flags().IntP("timeout", "t", auth.DefaultLoginTimeout, "Login timeout in seconds")
	loginAlias.Flags().String("api-key", "", "Authenticate with an API key")
	rootCmd.AddCommand(loginAlias)

	logoutAlias := &cobra.Command{
		Use:   "logout",
		Short: "Clear stored credentials",
		Run:   logoutCmd.Run,
	}
	rootCmd.AddCommand(logoutAlias)
}
```

Note: The convenience aliases share RunE/Run funcs with their subcommand counterparts. The `--api-key` flag uses `cmd.Flags().Changed()` to differentiate between `--api-key ""` (prompt) and not passed.

- [ ] **Step 2: Build and verify**

Run: `go build -o konvu . && ./konvu auth --help && ./konvu whoami --help`
Expected: Help text for auth commands

- [ ] **Step 3: Commit**

```bash
git add cmd/auth.go
git commit -m "feat: add auth commands (login, logout, whoami)"
```

---

### Task 11: Finding commands

**Files:**
- Create: `cmd/finding.go`

This is the largest command file (~500 lines in Go). Port `konvu_cli/commands/finding.py`.

Read the Python source at `konvu_cli/commands/finding.py` for exact behavior. Key functions to port:
- `_parse_relative_date` — regex match `(\d+)d` → ISO date
- `_transform_finding` — API finding → CLI output dict
- `_compute_assessment_counts` — per-category counts via filtered API calls
- `_handle_error` — structured error output
- `list_findings` — 21 flags, grouping, multiple output formats
- `get_finding` — detail view with evidence/logs sections
- `rate_finding` — positional rating + comment
- `finding_counts` — grouped counts (severity/week/month)
- `_generate_time_periods` — week/month period windows

- [ ] **Step 1: Implement finding.go helpers and flag registration**

This is the largest command file. Reference: `konvu_cli/commands/finding.py`.

```go
package cmd

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/KonvuTeam/konvu-cli/internal/api"
	clierrors "github.com/KonvuTeam/konvu-cli/internal/errors"
	"github.com/KonvuTeam/konvu-cli/internal/mapping"
	"github.com/KonvuTeam/konvu-cli/internal/output"
	"github.com/spf13/cobra"
)

var findingCmd = &cobra.Command{
	Use:   "finding",
	Short: "Security findings",
}

var defaultTableColumns = []string{"cve", "dependency", "repository", "assessment", "assessment_summary"}
var validCountsGroupBy = map[string]bool{"severity": true, "week": true, "month": true}
var validListGroupBy = map[string]bool{"repository": true, "dependency": true, "severity": true, "assessment": true}

var relDateRe = regexp.MustCompile(`^(\d+)d$`)

func parseRelativeDate(value string) string {
	m := relDateRe.FindStringSubmatch(value)
	if m != nil {
		days := 0
		fmt.Sscanf(m[1], "%d", &days)
		t := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
		return t.Format(time.RFC3339)
	}
	return value
}

func getStr(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func getMap(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}

func getSlice(m map[string]any, key string) []any {
	v, _ := m[key].([]any)
	return v
}

func transformFinding(finding map[string]any) map[string]any {
	vuln := getMap(finding, "vulnerability")
	ml := getMap(finding, "manifest_location")
	dep := getMap(finding, "dependency")
	source := getMap(finding, "source")
	rec := getStr(finding, "calculated_recommendation")
	assessment := mapping.RecommendationToAssessment(rec)

	analyses := getMap(finding, "analyses")

	aliases := getSlice(vuln, "aliases")
	cve := ""
	if len(aliases) > 0 {
		cve, _ = aliases[0].(string)
	}
	if cve == "" {
		cve = getStr(vuln, "id")
	}

	qualSummary := getStr(analyses, "qualification_summary")
	if qualSummary == "" {
		qualSummary, _ = mapping.GetAssessmentSummary(assessment)
	}

	severity := strings.ToLower(getStr(vuln, "severity"))
	if severity == "" {
		severity = "unknown"
	}
	hasFix := strings.ToLower(getStr(vuln, "has_fix"))
	if hasFix == "" {
		hasFix = "unknown"
	}

	return map[string]any{
		"id":                  getStr(finding, "id"),
		"cve":                 cve,
		"severity":            severity,
		"dependency":          getStr(dep, "name"),
		"repository":          getStr(ml, "vcs_repository_url"),
		"manifest":            getStr(ml, "location"),
		"assessment":          string(assessment),
		"assessment_summary":  qualSummary,
		"has_fix":             hasFix,
		"first_seen":          getStr(source, "remote_created_at"),
		"state":               getStr(source, "state"),
		"source_id":           getStr(source, "identifier"),
		"scanner":             getStr(source, "source_name"),
	}
}

func computeAssessmentCounts(client *api.Client, baseParams map[string]any) map[string]int {
	counts := make(map[string]int)
	for _, status := range mapping.AllStatuses {
		recs := mapping.AssessmentToRecommendation(status)
		params := map[string]any{"per_page": 1, "recommendation": recs}
		for k, v := range baseParams {
			params[k] = v
		}
		params["recommendation"] = recs // always override
		data, err := client.Get("/sca_findings", params)
		if err != nil {
			continue
		}
		if total, ok := data["total"].(float64); ok {
			counts[string(status)] = int(total)
		}
	}
	return counts
}

func handleFindingError(err error, format output.OutputFormat) {
	var cliErr *clierrors.CLIError
	switch e := err.(type) {
	case *clierrors.CLIError:
		cliErr = e
	case *api.AuthenticationError:
		cliErr = clierrors.NewAuthError(e.Error())
	default:
		cliErr = clierrors.NewAPIError(e.Error())
	}

	if format == output.JSON {
		fmt.Println(clierrors.FormatErrorJSON(cliErr))
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", cliErr.Message)
		if cliErr.Suggestion != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", cliErr.Suggestion)
		}
	}
	os.Exit(cliErr.ExitCode)
}
```

- [ ] **Step 2: Implement finding list command**

```go
var findingListCmd = &cobra.Command{
	Use:   "list",
	Short: "List security findings",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Read all flags
		outputFlag, _ := cmd.Flags().GetString("output")
		format := output.DetectOutputFormat(outputFlag)

		since, _ := cmd.Flags().GetString("since")
		until, _ := cmd.Flags().GetString("until")
		severity, _ := cmd.Flags().GetStringSlice("severity")
		assessment, _ := cmd.Flags().GetStringSlice("assessment")
		state, _ := cmd.Flags().GetStringSlice("state")
		hasFix, _ := cmd.Flags().GetString("has-fix")
		repo, _ := cmd.Flags().GetString("repo")
		cve, _ := cmd.Flags().GetString("cve")
		dependency, _ := cmd.Flags().GetString("dependency")
		source, _ := cmd.Flags().GetString("source")
		sourceID, _ := cmd.Flags().GetString("source-id")
		sortFlag, _ := cmd.Flags().GetString("sort")
		order, _ := cmd.Flags().GetString("order")
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		quiet, _ := cmd.Flags().GetBool("quiet")
		count, _ := cmd.Flags().GetBool("count")
		groupBy, _ := cmd.Flags().GetString("group-by")
		fields, _ := cmd.Flags().GetString("fields")

		// Validate group-by
		if groupBy != "" && !validListGroupBy[groupBy] {
			fmt.Fprintf(os.Stderr, "Invalid group-by: %s. Valid: assessment, dependency, repository, severity\n", groupBy)
			os.Exit(clierrors.ExitUsageError)
		}

		client := api.NewClient("", "")
		defer client.Close()

		// Build params — port from Python finding.py:316-351
		params := map[string]any{
			"per_page": limit,
			"page":     (offset / max(limit, 1)) + 1,
			"sort":     sortFlag,
			"order":    order,
		}
		if since != "" {
			params["first_seen_after"] = parseRelativeDate(since)
		}
		if until != "" && until != "now" {
			params["first_seen_before"] = parseRelativeDate(until)
		}
		if len(severity) > 0 {
			upper := make([]string, len(severity))
			for i, s := range severity {
				upper[i] = strings.ToUpper(s)
			}
			params["severity"] = upper
		}
		if len(assessment) > 0 {
			var recs []string
			for _, a := range assessment {
				normalized := strings.ToLower(strings.ReplaceAll(a, "_", "-"))
				r := mapping.AssessmentToRecommendation(mapping.AssessmentStatus(normalized))
				recs = append(recs, r...)
			}
			params["recommendation"] = recs
		}
		if len(state) > 0 {
			params["any_source_state"] = state
		}
		if hasFix != "" {
			params["has_fix"] = hasFix
		}
		if repo != "" {
			params["vcs_repository_url"] = []string{repo}
		}
		if cve != "" {
			params["cve"] = []string{cve}
		}
		if dependency != "" {
			params["dependency_name"] = []string{dependency}
		}
		if source != "" {
			params["source"] = []string{source}
		}

		data, err := client.Get("/sca_findings", params)
		if err != nil {
			handleFindingError(err, format)
			return nil
		}

		total := int(data["total"].(float64))

		if count {
			fmt.Println(total)
			return nil
		}

		items := getSlice(data, "items")

		// Client-side source_id filter
		if sourceID != "" {
			var filtered []any
			for _, item := range items {
				m, _ := item.(map[string]any)
				src := getMap(m, "source")
				if getStr(src, "identifier") == sourceID {
					filtered = append(filtered, item)
				}
			}
			items = filtered
			total = len(items)
		}

		// Transform findings
		var transformed []map[string]any
		for _, item := range items {
			m, _ := item.(map[string]any)
			transformed = append(transformed, transformFinding(m))
		}

		if quiet {
			fmt.Println(output.FormatQuiet(transformed, "id"))
			return nil
		}

		// Assessment breakdown
		breakdown := make(map[string]int)
		for _, f := range transformed {
			a, _ := f["assessment"].(string)
			breakdown[a]++
		}

		showing := len(transformed)

		// Field filtering
		var fieldList []string
		if fields != "" {
			for _, f := range strings.Split(fields, ",") {
				fieldList = append(fieldList, strings.TrimSpace(f))
			}
		}

		// Apply field filter + format output
		// Port grouping logic from finding.py:377-500
		// (group_by branch vs flat branch, then format per output type)

		if groupBy != "" {
			// Group findings by field — port finding.py:378-449
			groups := make(map[string][]map[string]any)
			for _, f := range transformed {
				key, _ := f[groupBy].(string)
				if key == "" {
					key = "unknown"
				}
				groups[key] = append(groups[key], f)
			}

			// Sort groups by count desc, then key
			type groupEntry struct {
				Key      string
				Findings []map[string]any
			}
			var sorted []groupEntry
			for k, v := range groups {
				sorted = append(sorted, groupEntry{k, v})
			}
			sort.Slice(sorted, func(i, j int) bool {
				if len(sorted[i].Findings) != len(sorted[j].Findings) {
					return len(sorted[i].Findings) > len(sorted[j].Findings)
				}
				return sorted[i].Key < sorted[j].Key
			})

			if format == output.JSON {
				result := map[string]any{
					"summary": map[string]any{
						"total": total, "showing": showing, "offset": offset,
						"group_by": groupBy, "groups": len(sorted),
						"assessment_breakdown": breakdown,
					},
					"groups": sorted,
				}
				fmt.Println(output.FormatJSON(result))
			} else {
				fmt.Fprintf(os.Stderr, "\nShowing %d of %d findings\n", showing, total)
				fmt.Fprintf(os.Stderr, "  Grouped by %s: %d groups\n\n", groupBy, len(sorted))
				for _, g := range sorted {
					fmt.Printf("  %s (%d)\n", g.Key, len(g.Findings))
				}
				fmt.Println()
			}
		} else {
			if fieldList != nil {
				for i, f := range transformed {
					transformed[i] = output.FilterFields(f, fieldList)
				}
			}
			result := map[string]any{
				"summary": map[string]any{
					"total": total, "showing": showing, "offset": offset,
					"assessment_breakdown": breakdown,
				},
				"findings": transformed,
			}
			if format == output.JSON {
				fmt.Println(output.FormatJSON(result))
			} else if format == output.CSV {
				// Convert transformed to []any for FormatCSV
				items := make([]any, len(transformed))
				for i, f := range transformed {
					items[i] = f
				}
				csvData := map[string]any{"findings": items}
				fmt.Print(output.FormatCSV(csvData, []string{"id", "cve", "severity", "dependency", "repository", "assessment", "assessment_summary"}, "findings"))
			} else {
				// Table output with summary line
				fmt.Fprintf(os.Stderr, "\nShowing %d of %d findings\n\n", showing, total)
				items := make([]any, len(transformed))
				for i, f := range transformed {
					items[i] = f
				}
				tableData := map[string]any{"findings": items}
				fmt.Print(output.FormatTable(tableData, defaultTableColumns, "findings", output.DefaultStyleCell))
			}
		}
		return nil
	},
}
```

- [ ] **Step 3: Implement finding get, rate, counts commands**

Port these three subcommands from `konvu_cli/commands/finding.py`:

**`finding get`** (port lines 506-752): Fetches finding detail, builds assessment/finding/vulnerability sections, handles `--include evidence`, `--include logs`, `--verbose` (auto-includes evidence), `--fields`. Table output prints assessment status with ANSI color, checklist items, finding details, vulnerability info, and optionally recommendation history.

**`finding rate`** (port lines 755-842): Takes positional `finding_id` and `rating` (agree/disagree). Validates rating is "agree" or "disagree". Optionally takes `--recommendation-id` to skip the lookup. POSTs to `/recommendation_decision_history/{rec_id}/integration_issue/{finding_id}/scoring`.

**`finding counts`** (port lines 845-987): Uses `computeAssessmentCounts()`. Supports `--group-by severity` (queries per severity level), `--group-by week/month` (generates time periods, queries per period). Table output shows aligned columns with category headers.

- [ ] **Step 4: Implement init() with complete flag registration**

```go
func init() {
	// finding list — all 21 flags
	findingListCmd.Flags().String("since", "", "Start date: '7d', '30d', or ISO date")
	findingListCmd.Flags().String("until", "", "End date: 'now' or ISO date")
	findingListCmd.Flags().StringSliceP("severity", "s", nil, "Filter: critical,high,moderate,low")
	findingListCmd.Flags().StringSliceP("assessment", "a", nil, "Filter: exploitable,false-positive,inconclusive,not-assessed")
	findingListCmd.Flags().StringSlice("state", nil, "Filter: open,dismissed,fixed,muted")
	findingListCmd.Flags().String("has-fix", "", "Filter: fixed, no_fix")
	findingListCmd.Flags().StringP("repo", "r", "", "Filter by repository URL or name")
	findingListCmd.Flags().String("cve", "", "Filter by CVE ID")
	findingListCmd.Flags().StringP("dependency", "d", "", "Filter by dependency name")
	findingListCmd.Flags().String("source", "", "Filter by scanner source: snyk, dependabot, etc.")
	findingListCmd.Flags().String("source-id", "", "Filter by external source identifier")
	findingListCmd.Flags().String("sort", "recommendation", "Sort: severity,recommendation,first_seen_at,updated_at,dependency_name,cve")
	findingListCmd.Flags().String("order", "desc", "Order: asc,desc")
	findingListCmd.Flags().IntP("limit", "n", 50, "Maximum findings to return")
	findingListCmd.Flags().Int("offset", 0, "Skip N results")
	findingListCmd.Flags().StringP("output", "o", "", "Output format: json, table, csv")
	findingListCmd.Flags().BoolP("quiet", "q", false, "Output bare finding IDs only")
	findingListCmd.Flags().Bool("count", false, "Output only the total count")
	findingListCmd.Flags().StringP("group-by", "g", "", "Group by: repository, dependency, severity, assessment")
	findingListCmd.Flags().String("fields", "", "Comma-separated fields to include in JSON output")

	// finding get
	findingGetCmd.Flags().StringSliceP("include", "i", nil, "Include: evidence, logs")
	findingGetCmd.Flags().BoolP("verbose", "v", false, "Show all details for each check")
	findingGetCmd.Flags().StringP("output", "o", "", "Output format: json, table")
	findingGetCmd.Flags().String("fields", "", "Comma-separated fields to include")

	// finding rate
	findingRateCmd.Flags().StringP("comment", "c", "", "Optional feedback comment")
	findingRateCmd.Flags().String("recommendation-id", "", "Recommendation ID (skips extra API call)")
	findingRateCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	// finding counts
	findingCountsCmd.Flags().String("since", "", "Start date: '7d', '30d', or ISO date")
	findingCountsCmd.Flags().String("until", "", "End date: 'now' or ISO date")
	findingCountsCmd.Flags().StringSliceP("severity", "s", nil, "Filter: critical,high,moderate,low")
	findingCountsCmd.Flags().StringP("repo", "r", "", "Filter by repository URL or name")
	findingCountsCmd.Flags().String("source", "", "Filter by scanner source")
	findingCountsCmd.Flags().StringP("group-by", "g", "", "Break down by: severity, week, month")
	findingCountsCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	findingCmd.AddCommand(findingListCmd)
	findingCmd.AddCommand(findingGetCmd)
	findingCmd.AddCommand(findingRateCmd)
	findingCmd.AddCommand(findingCountsCmd)
	rootCmd.AddCommand(findingCmd)
}
```

- [ ] **Step 2: Build and verify**

Run: `go build -o konvu . && ./konvu finding --help && ./konvu finding list --help`
Expected: Help text matching Python CLI's flags and descriptions

- [ ] **Step 3: Commit**

```bash
git add cmd/finding.go
git commit -m "feat: add finding commands (list, get, rate, counts)"
```

---

### Task 12: Vuln command

**Files:**
- Create: `cmd/vuln.go`

Port `konvu_cli/commands/vuln.py`. Read the Python source for exact behavior.

- [ ] **Step 1: Implement vuln.go**

```go
package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/KonvuTeam/konvu-cli/internal/api"
	"github.com/KonvuTeam/konvu-cli/internal/mapping"
	"github.com/KonvuTeam/konvu-cli/internal/output"
	"github.com/spf13/cobra"
)

var vulnCmd = &cobra.Command{
	Use:   "vuln",
	Short: "Vulnerability lookup",
}

var vulnGetCmd = &cobra.Command{
	Use:   "get [vuln-id]",
	Short: "Look up vulnerability details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		vulnID := args[0]
		includeFlag, _ := cmd.Flags().GetStringSlice("include")
		outputFlag, _ := cmd.Flags().GetString("output")

		if len(includeFlag) == 0 {
			includeFlag = []string{"summary", "affected"}
		}

		format := output.DetectOutputFormat(outputFlag)

		client := api.NewClient("", "")
		defer client.Close()

		fmt.Fprintf(os.Stderr, "Looking up %s...\n", vulnID)

		// Port the rest from konvu_cli/commands/vuln.py:get_vulnerability
		// - Query /sca_issues with cve filter
		// - Query /sca_findings for detailed findings
		// - Build output dict matching Python structure exactly
		// - Table output uses tabwriter with ANSI colors

		// ... (implement by reading vuln.py line by line)

		return nil
	},
}

func init() {
	vulnGetCmd.Flags().StringSliceP("include", "i", nil, "Data to include: summary,technical,exploitability,remediation,references,affected")
	vulnGetCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	vulnCmd.AddCommand(vulnGetCmd)

	// Make `konvu vuln <id>` work as alias for `konvu vuln get <id>`
	// by handling args on the parent command directly
	vulnCmd.Args = cobra.ArbitraryArgs
	vulnCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			// Forward to vulnGetCmd
			return vulnGetCmd.RunE(cmd, args)
		}
		return cmd.Help()
	}
	// Copy flags from vulnGetCmd to vulnCmd so they work on the alias
	vulnCmd.Flags().StringSliceP("include", "i", nil, "Data to include: summary,technical,exploitability,remediation,references,affected")
	vulnCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	rootCmd.AddCommand(vulnCmd)
}
```

Implement the full `RunE` body by porting `konvu_cli/commands/vuln.py:get_vulnerability` (lines 20-239).

- [ ] **Step 2: Build and verify**

Run: `go build -o konvu . && ./konvu vuln get --help`
Expected: Help text matching Python CLI

- [ ] **Step 3: Commit**

```bash
git add cmd/vuln.go
git commit -m "feat: add vuln command"
```

---

### Task 13: Metrics command

**Files:**
- Create: `cmd/metrics.go`

Port `konvu_cli/commands/metrics.py`. Read the Python source for exact behavior.

- [ ] **Step 1: Implement metrics.go**

Structure follows the same pattern. Port `konvu_cli/commands/metrics.py:show_metrics` (lines 17-257).

Key pieces:
- `metrics show` subcommand with `--since`, `--until`, `--interval`, `--include`, `--compare`, `--output`
- Calls multiple API endpoints: `/overview/backlog`, `/overview/backlog_to_fix`, `/overview/backlog_to_dismiss`, `/overview/top_cves_to_prioritize`, `/overview/new_vs_closed`
- Table output with ANSI-colored counts
- Top-level `konvu metrics` convenience alias

- [ ] **Step 2: Build and verify**

Run: `go build -o konvu . && ./konvu metrics show --help`

- [ ] **Step 3: Commit**

```bash
git add cmd/metrics.go
git commit -m "feat: add metrics command"
```

---

### Task 14: Dismiss command

**Files:**
- Create: `cmd/dismiss.go`

Port `konvu_cli/commands/dismiss.py`. Read the Python source for exact behavior.

- [ ] **Step 1: Implement dismiss.go**

Port `konvu_cli/commands/dismiss.py:dismiss_issues` (lines 13-186).

Key pieces:
- `dismiss run` subcommand with `--issues`, `--assessment`, `--severity`, `--repo`, `--reason`, `--dry-run`, `--output`
- Must specify either `--issues` or `--assessment`
- Dry run previews what would be dismissed
- Executes dismissals one by one, tracks succeeded/failed
- Top-level `konvu dismiss` convenience alias

- [ ] **Step 2: Build and verify**

Run: `go build -o konvu . && ./konvu dismiss run --help`

- [ ] **Step 3: Commit**

```bash
git add cmd/dismiss.go
git commit -m "feat: add dismiss command"
```

---

### Task 15: Root command finalization (help-all, callback)

**Files:**
- Modify: `cmd/root.go`

Port `_HELP_ALL_TEXT` and `--help-all` flag from `konvu_cli/main.py`.

- [ ] **Step 1: Update root.go**

Add to `cmd/root.go`:
- `help-all` hidden command
- `--help-all` persistent flag on root
- Callback that shows help when no subcommand is given

```go
// Add to root.go

var helpAllText = `konvu-cli — Security vulnerability management
...` // Copy exact text from konvu_cli/main.py:44-132

var helpAllCmd = &cobra.Command{
	Use:    "help-all",
	Short:  "Print full CLI reference",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(helpAllText)
	},
}

func init() {
	rootCmd.PersistentFlags().Bool("help-all", false, "Show full CLI reference")
	rootCmd.AddCommand(helpAllCmd)

	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if helpAll, _ := cmd.Flags().GetBool("help-all"); helpAll {
			fmt.Println(helpAllText)
			os.Exit(0)
		}
	}
}
```

- [ ] **Step 2: Build and verify**

Run: `go build -o konvu . && ./konvu help-all && ./konvu --help-all`
Expected: Full CLI reference text

- [ ] **Step 3: Commit**

```bash
git add cmd/root.go
git commit -m "feat: add help-all command and --help-all flag"
```

---

### Task 16: Command integration tests

**Files:**
- Create: `cmd/cmd_test.go`

Test that commands are properly registered and flags are accessible.

- [ ] **Step 1: Write command registration tests**

```go
package cmd

import (
	"testing"
)

func TestRootCommandHasSubcommands(t *testing.T) {
	expected := []string{"auth", "finding", "vuln", "metrics", "dismiss", "version", "skills",
		"whoami", "login", "logout", "help-all"}
	for _, name := range expected {
		found := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("root command missing subcommand: %s", name)
		}
	}
}

func TestFindingListFlags(t *testing.T) {
	flags := []string{"since", "until", "severity", "assessment", "state", "has-fix",
		"repo", "cve", "dependency", "source", "source-id", "sort", "order",
		"limit", "offset", "output", "quiet", "count", "group-by", "fields"}
	for _, flag := range flags {
		if findingListCmd.Flags().Lookup(flag) == nil {
			t.Errorf("finding list missing flag: --%s", flag)
		}
	}
}

func TestFindingGetFlags(t *testing.T) {
	flags := []string{"include", "verbose", "output", "fields"}
	for _, flag := range flags {
		if findingGetCmd.Flags().Lookup(flag) == nil {
			t.Errorf("finding get missing flag: --%s", flag)
		}
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./cmd/ -v`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/cmd_test.go
git commit -m "test: add command registration and flag tests"
```

---

### Task 17: Full build verification

- [ ] **Step 1: Build clean**

Run: `go build -o konvu . && go vet ./... && go test ./...`
Expected: Clean build, no vet issues, all tests pass

- [ ] **Step 2: Verify command parity**

Run: `./konvu --help` and compare output against Python `konvu --help`. Verify all commands and aliases are present.

- [ ] **Step 3: Commit any fixes**

---

## Chunk 3: Distribution & Skills Scaffolding

### Task 17: goreleaser configuration

**Files:**
- Create: `goreleaser.yml`

- [ ] **Step 1: Create goreleaser.yml**

```yaml
project_name: konvu
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - binary: konvu
    env:
      - CGO_ENABLED=0
    goos:
      - darwin
      - linux
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    ldflags:
      - -s -w -X github.com/KonvuTeam/konvu-cli/cmd.Version={{.Version}}

archives:
  - format: tar.gz
    name_template: "{{ .ProjectName }}-{{ .Os }}-{{ .Arch }}"
    format_overrides:
      - goos: windows
        format: zip
    files:
      - skills/**/*
      - registry/**/*

checksum:
  name_template: "checksums.txt"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
```

- [ ] **Step 2: Commit**

```bash
git add goreleaser.yml
git commit -m "feat: add goreleaser config for cross-platform builds"
```

---

### Task 18: npm package wrapper

**Files:**
- Create: `package.json`
- Create: `scripts/postinstall.js`

- [ ] **Step 1: Create package.json**

```json
{
  "name": "@konvu/cli",
  "version": "0.2.0",
  "description": "Konvu CLI - Security vulnerability management from your terminal",
  "bin": {
    "konvu": "bin/konvu"
  },
  "scripts": {
    "postinstall": "node scripts/postinstall.js"
  },
  "os": ["darwin", "linux", "win32"],
  "cpu": ["x64", "arm64"],
  "keywords": ["security", "vulnerabilities", "cli", "konvu"],
  "license": "Proprietary",
  "repository": {
    "type": "git",
    "url": "https://github.com/KonvuTeam/konvu-cli"
  }
}
```

- [ ] **Step 2: Create postinstall script**

```js
#!/usr/bin/env node
// Downloads the correct pre-built binary for the current platform.
const { execSync } = require("child_process");
const fs = require("fs");
const path = require("path");
const https = require("https");

const PLATFORM_MAP = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};

const ARCH_MAP = {
  x64: "amd64",
  arm64: "arm64",
};

const platform = PLATFORM_MAP[process.platform];
const arch = ARCH_MAP[process.arch];

if (!platform || !arch) {
  console.error(`Unsupported platform: ${process.platform}-${process.arch}`);
  process.exit(1);
}

const version = require("../package.json").version;
const ext = platform === "windows" ? "zip" : "tar.gz";
const filename = `konvu-${platform}-${arch}.${ext}`;
const url = `https://github.com/KonvuTeam/konvu-cli/releases/download/v${version}/${filename}`;

const binDir = path.join(__dirname, "..", "bin");
fs.mkdirSync(binDir, { recursive: true });

console.log(`Downloading konvu ${version} for ${platform}-${arch}...`);

// Download and extract binary
const binPath = path.join(binDir, platform === "windows" ? "konvu.exe" : "konvu");

const file = fs.createWriteStream(path.join(binDir, filename));
https.get(url, (response) => {
  if (response.statusCode === 302) {
    // Follow redirect (GitHub releases)
    https.get(response.headers.location, (r) => {
      r.pipe(file);
      file.on("finish", () => {
        file.close();
        extract(path.join(binDir, filename), binDir, platform);
      });
    });
  } else {
    response.pipe(file);
    file.on("finish", () => {
      file.close();
      extract(path.join(binDir, filename), binDir, platform);
    });
  }
});

function extract(archive, dest, platform) {
  if (platform === "windows") {
    // Use PowerShell to extract zip
    execSync(`powershell -command "Expand-Archive -Path '${archive}' -DestinationPath '${dest}' -Force"`);
  } else {
    execSync(`tar -xzf "${archive}" -C "${dest}"`);
  }
  // Clean up archive
  fs.unlinkSync(archive);
  // Make binary executable
  if (platform !== "windows") {
    fs.chmodSync(binPath, 0o755);
  }
  console.log("konvu installed successfully.");
}
```

- [ ] **Step 3: Commit**

```bash
git add package.json scripts/postinstall.js
git commit -m "feat: add npm package wrapper for binary distribution"
```

---

### Task 19: GitHub Actions CI/CD

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create CI workflow**

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: go test ./... -v
      - run: go vet ./...

  build:
    runs-on: ubuntu-latest
    needs: test
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: go build -o konvu .
```

- [ ] **Step 2: Create release workflow**

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

  npm-publish:
    runs-on: ubuntu-latest
    needs: goreleaser
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: "20"
          registry-url: "https://registry.npmjs.org"
      - run: npm publish --access public
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/
git commit -m "feat: add CI/CD workflows for testing and releases"
```

---

### Task 20: Skills scaffolding

**Files:**
- Create: `skills/konvu-shared/SKILL.md`
- Create: stub SKILL.md files for all service skills
- Create: `registry/recipes.yaml`

- [ ] **Step 1: Create konvu-shared skill**

```markdown
---
name: konvu-shared
version: 1.0.0
description: "Konvu CLI: Auth setup, global flags, env vars, and common patterns."
metadata:
  requires:
    bins: ["konvu"]
---
# Konvu CLI — Shared Reference

## Authentication

```bash
konvu login                    # Interactive picker (OAuth or API key)
konvu login --api-key API_KEY  # Direct API key (CI/CD)
konvu whoami                   # Verify authentication
konvu logout                   # Clear credentials
```

## Global Flags

All commands support:
- `-o, --output json|table|csv` — Output format (default: table for TTY, json for pipe)

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `KONVU_API_URL` | API base URL (default: https://api.konvu.com) |
| `KONVU_ACCESS_TOKEN` | Token auth (skips OAuth) |
| `KONVU_ZITADEL_DOMAIN` | OAuth domain |
| `KONVU_ZITADEL_CLIENT_ID` | OAuth client ID |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Not found |
| 4 | Authentication failed |

## Tips

- Pipe with `-o json` for structured output: `konvu finding list -o json | jq '.findings[]'`
- Use `-q` on finding list for bare IDs: `konvu finding list -q | xargs -I {} konvu finding get {}`
- Use `--help-all` for the complete reference of all commands and flags
```

- [ ] **Step 2: Create stub service skills**

Create these files with minimal frontmatter (content to be filled in later):

- `skills/konvu-auth/SKILL.md`
- `skills/konvu-finding-list/SKILL.md`
- `skills/konvu-finding-get/SKILL.md`
- `skills/konvu-finding-rate/SKILL.md`
- `skills/konvu-vuln/SKILL.md`
- `skills/konvu-metrics/SKILL.md`
- `skills/konvu-dismiss/SKILL.md`
- `skills/recipe-weekly-triage/SKILL.md`
- `skills/recipe-posture-report/SKILL.md`

Each stub:
```markdown
---
name: <skill-name>
version: 1.0.0
description: "<one-line description>"
metadata:
  requires:
    bins: ["konvu"]
  cliHelp: "konvu <command> --help"
---
# <command>

> **PREREQUISITE:** Read `../konvu-shared/SKILL.md` for auth and global flags.

TODO: Add usage, flags, examples.
```

- [ ] **Step 3: Create recipes registry**

```yaml
# Curated Recipe Registry — Konvu CLI workflows
recipes:
  - name: weekly-triage
    title: Weekly Critical Finding Triage
    description: "Review and act on this week's critical and exploitable findings."
    services: [finding, vuln, dismiss]
    steps:
      - "List this week's exploitable findings: `konvu finding list --since 7d --assessment exploitable`"
      - "Review details: `konvu finding get <id> --include evidence`"
      - "Rate assessment: `konvu finding rate <id> agree` or `disagree --comment 'reason'`"
      - "Dismiss false positives: `konvu dismiss --assessment false-positive --dry-run`"

  - name: posture-report
    title: Security Posture Report
    description: "Generate a security posture summary for stakeholders."
    services: [metrics, finding]
    steps:
      - "Get overall metrics: `konvu metrics --include summary,trends,top_cves -o json`"
      - "Get assessment breakdown: `konvu finding counts --group-by severity -o json`"
      - "List exploitable findings by repo: `konvu finding list --assessment exploitable --group-by repository -o json`"
```

- [ ] **Step 4: Commit**

```bash
git add skills/ registry/
git commit -m "feat: add skills scaffolding and recipe registry"
```

---

### Task 21: Update .claude/settings.json

**Files:**
- Modify: `.claude/settings.local.json`

- [ ] **Step 1: Update settings with Go-relevant permissions**

Add the `konvu` binary and `go` commands to allowed tools. Read the existing file first to preserve current settings.

- [ ] **Step 2: Commit**

```bash
git add .claude/
git commit -m "feat: update Claude Code settings for Go project"
```

---

## Chunk 4: Cutover

### Task 22: Remove Python code

**Files:**
- Remove: `konvu_cli/` directory
- Remove: `pyproject.toml`
- Remove: `tests/` directory (Python tests)

- [ ] **Step 1: Verify Go CLI is feature-complete**

Run `./konvu --help-all` and compare against the Python version's help-all output. Every command, flag, and alias must be present.

- [ ] **Step 2: Remove Python files**

```bash
rm -rf konvu_cli/ tests/ pyproject.toml
```

- [ ] **Step 3: Update .gitignore**

Remove Python-specific entries, add Go-specific entries:
```
# Go
konvu
*.exe
bin/
dist/
node_modules/
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: remove Python codebase after Go port complete"
```

---

### Task 23: Update README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update installation section**

Replace pip install with:
```markdown
## Installation

```bash
npm install -g @konvu/cli
```

Or download from [GitHub Releases](https://github.com/KonvuTeam/konvu-cli/releases).
```

- [ ] **Step 2: Update development section**

Replace Python dev setup with Go:
```markdown
## Development

```bash
go build -o konvu .
go test ./...
go vet ./...
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: update README for Go-based CLI and npm distribution"
```

---

### Task 24: Tag release

- [ ] **Step 1: Final verification**

Run: `go build -o konvu . && go test ./... && ./konvu --help-all`
Expected: Clean build, all tests pass, full help output matches spec

- [ ] **Step 2: Tag v0.2.0**

```bash
git tag -a v0.2.0 -m "v0.2.0: Go rewrite with npm distribution and skills"
```

Do NOT push the tag until you've verified the release workflow is configured correctly.
