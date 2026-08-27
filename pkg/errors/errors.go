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
	Context    map[string]any
}

func (e *CLIError) Error() string {
	return e.Message
}

func FormatErrorJSON(err *CLIError) string {
	errorObject := map[string]any{
		"code":       err.Code,
		"message":    err.Message,
		"suggestion": err.Suggestion,
		"retryable":  err.Retryable,
	}
	if err.Context != nil {
		errorObject["context"] = err.Context
	}
	obj := map[string]any{"error": errorObject}
	b, _ := json.MarshalIndent(obj, "", "  ")
	return string(b)
}

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
