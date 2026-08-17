package cmd

import "testing"

func TestParseDependabotRef(t *testing.T) {
	tests := []struct {
		name      string
		arg       string
		wantParam string
		wantValue string
		wantNum   string
		wantOK    bool
	}{
		{
			name:      "full alert url",
			arg:       "https://github.com/octo-org/octo-repo/security/dependabot/42",
			wantParam: "vcs_repository_url",
			wantValue: "https://github.com/octo-org/octo-repo",
			wantNum:   "42",
			wantOK:    true,
		},
		{
			name:      "url with trailing slash",
			arg:       "https://github.com/octo-org/octo-repo/security/dependabot/312/",
			wantParam: "vcs_repository_url",
			wantValue: "https://github.com/octo-org/octo-repo",
			wantNum:   "312",
			wantOK:    true,
		},
		{
			name:      "owner/repo#n shorthand",
			arg:       "octo-org/octo-repo#42",
			wantParam: "repo_glob",
			wantValue: "octo-org/octo-repo",
			wantNum:   "42",
			wantOK:    true,
		},
		{name: "konvu uuid is not a dependabot ref", arg: "3f2a1c9e-1b2d-4c5e-8a9b-0d1e2f3a4b5c", wantOK: false},
		{name: "bare number is ambiguous, not a ref", arg: "312", wantOK: false},
		{name: "ghsa id is not a dependabot ref", arg: "GHSA-abcd-1234-wxyz", wantOK: false},
		{name: "code-scanning url is not dependabot", arg: "https://github.com/octo-org/octo-repo/security/code-scanning/42", wantOK: false},
		{name: "empty string", arg: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			param, value, num, ok := parseDependabotRef(tt.arg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if param != tt.wantParam || value != tt.wantValue || num != tt.wantNum {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)", param, value, num, tt.wantParam, tt.wantValue, tt.wantNum)
			}
		})
	}
}
