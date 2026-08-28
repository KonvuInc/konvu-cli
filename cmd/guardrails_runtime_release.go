//go:build !guardrails_dev

package cmd

func resolveGuardrailsBinary() (string, error) {
	return ensureGuardrailsBinary(guardrailsCloudFrontBase, guardrailsPinnedVersion)
}
