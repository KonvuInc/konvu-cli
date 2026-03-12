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
