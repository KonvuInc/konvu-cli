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
	if _, ok := errObj["context"]; ok {
		t.Fatal("context should be omitted when the error has no structured context")
	}
}

func TestFormatErrorJSONIncludesStructuredContext(t *testing.T) {
	err := &CLIError{
		Code:     "REPOSITORY_REQUIRED",
		Message:  "Select a stored repository",
		ExitCode: ExitUsageError,
		Context: map[string]any{
			"available_repositories": []string{"example/api", "example/web"},
		},
	}

	var parsed struct {
		Error struct {
			Context struct {
				Available []string `json:"available_repositories"`
			} `json:"context"`
		} `json:"error"`
	}
	if jsonErr := json.Unmarshal([]byte(FormatErrorJSON(err)), &parsed); jsonErr != nil {
		t.Fatalf("FormatErrorJSON() returned invalid JSON: %v", jsonErr)
	}
	want := []string{"example/api", "example/web"}
	if len(parsed.Error.Context.Available) != len(want) {
		t.Fatalf("available repositories = %v, want %v", parsed.Error.Context.Available, want)
	}
	for index := range want {
		if parsed.Error.Context.Available[index] != want[index] {
			t.Fatalf("available repositories = %v, want %v", parsed.Error.Context.Available, want)
		}
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
