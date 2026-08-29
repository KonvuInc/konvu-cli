//go:build !guardrails_dev

package cmd

import "testing"

func TestProductionGuardrailsPinUsesBaselineV1Release(t *testing.T) {
	if guardrailsPinnedVersion != "v0.6.1" {
		t.Fatalf(
			"production Guardrails pin = %q, want v0.6.1",
			guardrailsPinnedVersion,
		)
	}
}

func TestProductionRuntimeKeepsLauncherSandbox(t *testing.T) {
	if guardrailsRuntimeOwnsSandbox("/unused/guardrails") {
		t.Fatal("pinned production runtime has not declared sandbox ownership")
	}
}
