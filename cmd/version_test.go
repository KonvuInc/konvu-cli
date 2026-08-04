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

// The `go install` path depends on this normalization, and debug.ReadBuildInfo
// reports no version inside a test binary, so exercise it directly.
func TestNormalizeModuleVersion(t *testing.T) {
	tests := []struct {
		name          string
		moduleVersion string
		want          string
	}{
		{"tagged install strips the v", "v0.7.0", "0.7.0"},
		{"prerelease keeps its suffix", "v1.0.0-rc.1", "1.0.0-rc.1"},
		{"dirty tree keeps its marker", "v0.7.0+dirty", "0.7.0+dirty"},
		{"pseudo-version passes through", "v0.0.0-20260804150000-abcdef123456", "0.0.0-20260804150000-abcdef123456"},
		{"already unprefixed is untouched", "0.7.0", "0.7.0"},
		{"untagged build is unusable", "(devel)", ""},
		{"missing metadata is unusable", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeModuleVersion(tt.moduleVersion); got != tt.want {
				t.Errorf("normalizeModuleVersion(%q) = %q, want %q", tt.moduleVersion, got, tt.want)
			}
		})
	}
}
