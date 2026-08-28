//go:build !guardrails_dev

package cmd

import "testing"

func TestProductionGuardrailsPinUsesBaselineV1Release(t *testing.T) {
	if guardrailsPinnedVersion != "v0.6.0" {
		t.Fatalf(
			"production Guardrails pin = %q, want v0.6.0",
			guardrailsPinnedVersion,
		)
	}
}
