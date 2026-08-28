//go:build !guardrails_dev

package cmd

import "testing"

func TestProductionGuardrailsPinRemainsOnSignedReleasePendingCutover(t *testing.T) {
	// The baseline v1 producer is not published yet. Keep production on the
	// signed release until that cutover is made deliberately in its own slice.
	if guardrailsPinnedVersion != "v0.5.3" {
		t.Fatalf(
			"production Guardrails pin = %q; update this pending-cutover assertion with the signed release",
			guardrailsPinnedVersion,
		)
	}
}
