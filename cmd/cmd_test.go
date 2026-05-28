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
		"limit", "offset", "output", "quiet", "count", "group-by", "fields"}
	for _, flag := range flags {
		if findingListCmd.Flags().Lookup(flag) == nil {
			t.Errorf("finding list missing flag: --%s", flag)
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
