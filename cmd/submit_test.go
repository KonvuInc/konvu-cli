package cmd

import "testing"

func TestFindingSubmitRegistered(t *testing.T) {
	found := false
	for _, c := range findingCmd.Commands() {
		if c.Name() == "submit" {
			found = true
		}
	}
	if !found {
		t.Error("finding missing subcommand: submit")
	}
}

func TestFindingSubmitFlags(t *testing.T) {
	for _, flag := range []string{"repo", "ref", "file", "dry-run", "output"} {
		if findingSubmitCmd.Flags().Lookup(flag) == nil {
			t.Errorf("finding submit missing flag: --%s", flag)
		}
	}
}

func TestParseFindings(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{"array", `[{"vulnerability_id":"CVE-1"},{"vulnerability_id":"CVE-2"}]`, 2, false},
		{"empty array", `[]`, 0, false},
		{"object with findings", `{"repository":{},"findings":[{"vulnerability_id":"CVE-1"}]}`, 1, false},
		{"object without findings", `{"foo":1}`, 0, true},
		{"scalar", `123`, 0, true},
		{"not json", `nope`, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseFindings([]byte(tc.in))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && len(got) != tc.want {
				t.Errorf("got %d findings, want %d", len(got), tc.want)
			}
		})
	}
}

func TestIntField(t *testing.T) {
	m := map[string]any{"created_count": float64(3), "missing": nil}
	if got := intField(m, "created_count"); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
	if got := intField(m, "missing"); got != 0 {
		t.Errorf("got %d, want 0 for missing/non-numeric", got)
	}
}
