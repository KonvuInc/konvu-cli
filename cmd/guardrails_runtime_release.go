//go:build !guardrails_dev

package cmd

func resolveGuardrailsBinary() (string, error) {
	return ensureGuardrailsBinary(guardrailsCloudFrontBase, guardrailsPinnedVersion)
}

// The currently pinned release predates the runtime-owned sandbox contract, so retain the
// launcher's whole-process sandbox for production builds.
func guardrailsRuntimeOwnsSandbox() bool {
	return false
}
