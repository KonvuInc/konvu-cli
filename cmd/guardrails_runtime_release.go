//go:build !guardrails_dev

package cmd

import (
	"fmt"
	"path/filepath"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

func resolveGuardrailsBinary() (string, error) {
	return ensureGuardrailsBinary(guardrailsCloudFrontBase, guardrailsPinnedVersion)
}

// The pinned runtime owns its agent sandbox. Snapshot and re-verify both executables so the
// bytes that run cannot change between cache verification and process launch.
func prepareGuardrailsRuntime(binaryPath string) (string, bool, func(), error) {
	triple, err := guardrailsTargetTriple()
	if err != nil {
		return "", false, func() {}, err
	}
	artifact, ok := guardrailsArtifacts[triple]
	if !ok {
		return "", false, func() {}, unverifiedGuardrailsRuntimeError(triple)
	}
	return prepareVerifiedGuardrailsRuntime(binaryPath, artifact)
}

func prepareVerifiedGuardrailsRuntime(
	binaryPath string,
	artifact guardrailsArtifact,
) (string, bool, func(), error) {
	snapshotMain, cleanup, err := snapshotGuardrailsRuntime(binaryPath)
	if err != nil {
		return "", false, func() {}, invalidGuardrailsRuntimeError(binaryPath, err)
	}
	snapshotScanner := filepath.Join(filepath.Dir(snapshotMain), "guardrails-resource-scan")
	if !verifiedExecutable(snapshotMain, artifact.mainSHA256) ||
		!verifiedExecutable(snapshotScanner, artifact.resourceScannerSHA256) {
		cleanup()
		return "", false, func() {}, invalidGuardrailsRuntimeError(
			binaryPath,
			fmt.Errorf("snapshot checksum verification failed"),
		)
	}
	return snapshotMain, true, cleanup, nil
}

func unverifiedGuardrailsRuntimeError(triple string) error {
	return &clierrors.CLIError{
		Code:       "UNVERIFIED_RELEASE",
		Message:    fmt.Sprintf("guardrails %s has no trusted artifact for %s", guardrailsPinnedVersion, triple),
		Suggestion: "Upgrade Konvu CLI to a release that supports this platform.",
		ExitCode:   clierrors.ExitGeneralError,
	}
}

func invalidGuardrailsRuntimeError(path string, err error) error {
	return &clierrors.CLIError{
		Code:       "INVALID_GUARDRAILS_RUNTIME",
		Message:    fmt.Sprintf("could not prepare verified Guardrails runtime %s: %v", path, err),
		Suggestion: "Remove the cached Guardrails runtime and try again.",
		ExitCode:   clierrors.ExitGeneralError,
	}
}
