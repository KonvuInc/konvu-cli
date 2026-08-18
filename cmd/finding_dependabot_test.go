package cmd

import "testing"

// isFindingID decides whether `finding get` treats its argument as a Konvu
// finding ID (fetched directly) or an external reference (resolved server-side).
func TestIsFindingID(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want bool
	}{
		{"canonical uuid", "3f2a1c9e-1b2d-4c5e-8a9b-0d1e2f3a4b5c", true},
		{"uppercase uuid", "3F2A1C9E-1B2D-4C5E-8A9B-0D1E2F3A4B5C", true},
		{"bare hex uuid (API id format)", "01a011cc09717b42b2431f9eb99d98fc", true},
		{"uppercase bare hex uuid", "01A011CC09717B42B2431F9EB99D98FC", true},
		{"31 hex chars", "01a011cc09717b42b2431f9eb99d98f", false},
		{"33 hex chars", "01a011cc09717b42b2431f9eb99d98fc0", false},
		{"dependabot alert url", "https://github.com/octo-org/octo-repo/security/dependabot/42", false},
		{"graphql node id", "RVA_kwDOAGqid88AAAABngIBDw", false},
		{"bare number", "312", false},
		{"ghsa id", "GHSA-abcd-1234-wxyz", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFindingID(tt.arg); got != tt.want {
				t.Errorf("isFindingID(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}
