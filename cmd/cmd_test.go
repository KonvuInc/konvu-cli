package cmd

import (
	"testing"
)

func TestRootCommandHasSubcommands(t *testing.T) {
	expected := []string{"auth", "finding", "vuln", "metrics", "dismiss", "remediate", "version", "skills",
		"whoami", "login", "logout", "help-all"}
	for _, name := range expected {
		found := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("root command missing subcommand: %s", name)
		}
	}
}

func TestFindingListFlags(t *testing.T) {
	flags := []string{"since", "until", "severity", "assessment", "state", "has-fix",
		"repo", "cve", "ghsa", "dependency", "source", "dependabot-alert", "sort", "order",
		"limit", "offset", "output", "quiet", "count", "group-by", "fields",
		"dismissed-since", "dismissed-before"}
	for _, flag := range flags {
		if findingListCmd.Flags().Lookup(flag) == nil {
			t.Errorf("finding list missing flag: --%s", flag)
		}
	}
}

func TestFindingCountsFlags(t *testing.T) {
	flags := []string{"since", "until", "severity", "assessment", "state",
		"repo", "source", "group-by", "output"}
	for _, flag := range flags {
		if findingCountsCmd.Flags().Lookup(flag) == nil {
			t.Errorf("finding counts missing flag: --%s", flag)
		}
	}
}

func TestDeriveFixSource(t *testing.T) {
	cases := []struct {
		state, autofix, want string
	}{
		{"fixed", "merged", "patcheus"},
		{"fixed", "succeeded", "unknown"},
		{"fixed", "", "unknown"},
		{"open", "merged", ""},
		{"dismissed", "merged", ""},
	}
	for _, c := range cases {
		if got := deriveFixSource(c.state, c.autofix); got != c.want {
			t.Errorf("deriveFixSource(%q,%q) = %q, want %q", c.state, c.autofix, got, c.want)
		}
	}
}

func TestApplyWindow(t *testing.T) {
	mk := func(n int) []map[string]any {
		out := make([]map[string]any, n)
		for i := range out {
			out[i] = map[string]any{"i": i}
		}
		return out
	}
	cases := []struct {
		name           string
		n, offset, lim int
		wantLen        int
		wantFirst      int // -1 when empty
	}{
		{"page1", 10, 0, 3, 3, 0},
		{"page2", 10, 3, 3, 3, 3},
		{"offset-past-end", 10, 20, 5, 0, -1},
		{"limit-exceeds-remaining", 10, 8, 5, 2, 8},
		{"no-limit", 10, 0, 0, 10, 0},
		{"negative-offset", 10, -5, 2, 2, 0},
	}
	for _, c := range cases {
		got := applyWindow(mk(c.n), c.offset, c.lim)
		if len(got) != c.wantLen {
			t.Errorf("%s: len=%d want %d", c.name, len(got), c.wantLen)
			continue
		}
		if c.wantFirst >= 0 && got[0]["i"] != c.wantFirst {
			t.Errorf("%s: first=%v want %d", c.name, got[0]["i"], c.wantFirst)
		}
	}
}

func TestParseAssessments(t *testing.T) {
	got, err := parseAssessments([]string{"exploitable", "false_positive", "NOT-ASSESSED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"exploitable", "false-positive", "not-assessed"}
	if len(got) != len(want) {
		t.Fatalf("got %d statuses, want %d", len(got), len(want))
	}
	for i, w := range want {
		if string(got[i]) != w {
			t.Errorf("status[%d] = %q, want %q", i, got[i], w)
		}
	}
	if _, err := parseAssessments([]string{"exploitable", "bogus"}); err == nil {
		t.Error("expected error for invalid assessment, got nil")
	}
}

func TestFindingGetFlags(t *testing.T) {
	flags := []string{"include", "verbose", "output", "fields"}
	for _, flag := range flags {
		if findingGetCmd.Flags().Lookup(flag) == nil {
			t.Errorf("finding get missing flag: --%s", flag)
		}
	}
}
