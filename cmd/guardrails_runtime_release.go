//go:build !guardrails_dev

package cmd

func resolveGuardrailsBinary() (string, error) {
	return ensureGuardrailsBinary(guardrailsCloudFrontBase, guardrailsPinnedVersion)
}

// The currently pinned release predates the runtime-owned sandbox contract, so it needs no
// snapshot and retains the launcher's whole-process sandbox.
func prepareGuardrailsRuntime(binaryPath string) (string, bool, func(), error) {
	return binaryPath, false, func() {}, nil
}
