package cmd

import (
	"testing"
)

func TestResolveVersionPrefersLdflagsValue(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	Version = "0.7.0"
	if got := resolveVersion(); got != "0.7.0" {
		t.Errorf("resolveVersion() = %q, want %q", got, "0.7.0")
	}
}

func TestResolveVersionFallsBackWhenUnset(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	// A test binary has no module version recorded, so the placeholder stands.
	Version = "dev"
	if got := resolveVersion(); got != "dev" {
		t.Errorf("resolveVersion() = %q, want %q", got, "dev")
	}
}

func TestResolveVersionDoesNotRewriteLdflagsValue(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	// Only the module-version fallback is normalized; ldflags pass through.
	Version = "v1.2.3"
	if got := resolveVersion(); got != "v1.2.3" {
		t.Errorf("resolveVersion() = %q, want %q", got, "v1.2.3")
	}
}
