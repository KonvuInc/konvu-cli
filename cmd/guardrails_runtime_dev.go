//go:build guardrails_dev

package cmd

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

const guardrailsAgentSandboxCapability = "guardrails-agent-sandbox-v1"

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

// prepareGuardrailsRuntime snapshots a self-sandboxing development runtime before declaring
// sandbox ownership. The marker binds both binaries to their content, and the private snapshot
// keeps a concurrent local rebuild from changing the executable after validation.
func prepareGuardrailsRuntime(binaryPath string) (string, bool, func(), error) {
	noop := func() {}
	resolved, err := filepath.EvalSymlinks(binaryPath)
	if err != nil {
		return binaryPath, false, noop, nil
	}
	markerPath := filepath.Join(filepath.Dir(resolved), guardrailsAgentSandboxCapability)
	marker, err := os.ReadFile(markerPath)
	if err != nil || !strings.HasPrefix(
		strings.TrimSpace(string(marker)),
		guardrailsAgentSandboxCapability+"\n",
	) {
		return binaryPath, false, noop, nil
	}

	snapshotMain, cleanup, err := snapshotGuardrailsRuntime(resolved)
	if err != nil {
		return "", false, noop, guardrailsDevRuntimeError(binaryPath, err)
	}

	expected, err := guardrailsSandboxCapabilityMarker(snapshotMain)
	if err != nil {
		cleanup()
		return "", false, noop, guardrailsDevRuntimeError(binaryPath, err)
	}
	if strings.TrimSpace(string(marker)) != expected {
		cleanup()
		return binaryPath, false, noop, nil
	}
	return snapshotMain, true, cleanup, nil
}

func guardrailsSandboxCapabilityMarker(binaryPath string) (string, error) {
	mainDigest, err := guardrailsExecutableDigest(binaryPath)
	if err != nil {
		return "", err
	}
	scannerDigest, err := guardrailsExecutableDigest(
		filepath.Join(filepath.Dir(binaryPath), "guardrails-resource-scan"),
	)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"%s\nguardrails-sha256:%s\nguardrails-resource-scan-sha256:%s",
		guardrailsAgentSandboxCapability,
		mainDigest,
		scannerDigest,
	), nil
}

func guardrailsExecutableDigest(path string) (string, error) {
	binary, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer binary.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, binary); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func guardrailsDevRuntimeError(path string, err error) error {
	return &clierrors.CLIError{
		Code:       "INVALID_GUARDRAILS_DEV_RUNTIME",
		Message:    fmt.Sprintf("invalid local Guardrails runtime %s: %v", path, err),
		Suggestion: "Build both Guardrails binaries into one directory, then try again.",
		ExitCode:   clierrors.ExitGeneralError,
	}
}
