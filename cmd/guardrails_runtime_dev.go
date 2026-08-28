//go:build guardrails_dev

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

// resolveGuardrailsBinary uses a local, explicitly selected sibling binary
// pair only in guardrails_dev builds. Production builds retain the signed,
// checksum-pinned release resolver.
func resolveGuardrailsBinary() (string, error) {
	dir := strings.TrimSpace(os.Getenv("KONVU_GUARDRAILS_DEV_DIR"))
	if dir == "" {
		return "", &clierrors.CLIError{
			Code:       "MISSING_GUARDRAILS_DEV_DIR",
			Message:    "KONVU_GUARDRAILS_DEV_DIR is required by this development build",
			Suggestion: "Set it to the directory containing guardrails and guardrails-resource-scan.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", guardrailsDevRuntimeError(dir, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", guardrailsDevRuntimeError(abs, err)
	}
	mainPath := filepath.Join(resolved, "guardrails")
	scannerPath := filepath.Join(resolved, "guardrails-resource-scan")
	for _, path := range []string{mainPath, scannerPath} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return "", guardrailsDevRuntimeError(path, statErr)
		}
		if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
			return "", guardrailsDevRuntimeError(path, fmt.Errorf("must be an executable regular file"))
		}
	}
	return mainPath, nil
}

// The local Rust runtime confines every model-controlled command to its own read-only repository
// and writable scratch sandbox. Wrapping the whole runtime would prevent that nested sandbox from
// starting on macOS.
func guardrailsRuntimeOwnsSandbox() bool {
	return true
}

func guardrailsDevRuntimeError(path string, err error) error {
	return &clierrors.CLIError{
		Code:       "INVALID_GUARDRAILS_DEV_RUNTIME",
		Message:    fmt.Sprintf("invalid local Guardrails runtime %s: %v", path, err),
		Suggestion: "Build both Guardrails binaries into one directory, then try again.",
		ExitCode:   clierrors.ExitGeneralError,
	}
}
