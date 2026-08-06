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
		"repo", "cve", "dependency", "source", "source-id", "sort", "order",
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

func TestFindingGetFlags(t *testing.T) {
	flags := []string{"include", "verbose", "output", "fields"}
	for _, flag := range flags {
		if findingGetCmd.Flags().Lookup(flag) == nil {
			t.Errorf("finding get missing flag: --%s", flag)
		}
	}
}
