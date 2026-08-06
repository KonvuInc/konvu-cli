package cmd

import (
	"testing"
)

func TestDevBuildVersion(t *testing.T) {
	tests := []struct {
		name     string
		revision string
		dirty    bool
		want     string
	}{
		{"shortens the revision", "9c1a51edd2d25f1b574eb86923f9d8f3d2dc1f94", false, "dev+9c1a51e"},
		{"marks a dirty tree", "9c1a51edd2d25f1b574eb86923f9d8f3d2dc1f94", true, "dev+9c1a51e-dirty"},
		{"tolerates a revision shorter than the cut", "abc", false, "dev+abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := devBuildVersion(tt.revision, tt.dirty); got != tt.want {
				t.Errorf("devBuildVersion(%q, %v) = %q, want %q", tt.revision, tt.dirty, got, tt.want)
			}
		})
	}
}

// A test binary records no module version, so anything that is not a usable
// ldflags value falls through to the placeholder.
func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"ldflags value wins", "0.7.0", "0.7.0"},
		{"only the module fallback is normalized, ldflags pass through", "v1.2.3", "v1.2.3"},
		{"the placeholder stands when unset", devVersion, devVersion},
		{"empty is not a version", "", devVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := Version
			t.Cleanup(func() { Version = original })

			Version = tt.version
			if got := resolveVersion(); got != tt.want {
				t.Errorf("resolveVersion() = %q, want %q", got, tt.want)
			}
		})
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
