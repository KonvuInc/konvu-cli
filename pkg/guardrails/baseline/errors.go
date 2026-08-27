package baseline

import "fmt"

// ErrorCode identifies failures callers commonly need to render differently.
type ErrorCode string

const (
	ErrorInvalidArtifact   ErrorCode = "invalid_artifact"
	ErrorUnsupportedSchema ErrorCode = "unsupported_schema"
	ErrorRunNotFound       ErrorCode = "run_not_found"
	ErrorRunAmbiguous      ErrorCode = "run_ambiguous"
	ErrorRunIncomplete     ErrorCode = "run_incomplete"
)

// Error is a stable, presentation-independent baseline failure.
type Error struct {
	Code    ErrorCode
	Path    string
	Message string
}

func (e *Error) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

func artifactError(path, format string, args ...any) *Error {
	return &Error{Code: ErrorInvalidArtifact, Path: path, Message: fmt.Sprintf(format, args...)}
}
