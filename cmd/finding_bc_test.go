package cmd

import (
	"sort"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagNames returns the sorted set of non-hidden flags registered on cmd.
func flagNames(c *cobra.Command) []string {
	var names []string
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if !f.Hidden {
			names = append(names, f.Name)
		}
	})
	sort.Strings(names)
	return names
}

// TestBCAlias_FlagParity verifies that every bare-form `konvu finding <op>`
// exposes the same flag surface as its `konvu finding sca <op>` canonical.
// This is the load-bearing test for backward compatibility — copyFlagsFrom's
// invariant is exactly this.
func TestBCAlias_FlagParity(t *testing.T) {
	pairs := []struct {
		name  string
		alias *cobra.Command
		sca   *cobra.Command
	}{
		{"list", findingListCmd, scaListCmd},
		{"get", findingGetCmd, scaGetCmd},
		{"rate", findingRateCmd, scaRateCmd},
		{"counts", findingCountsCmd, scaCountsCmd},
		{"submit", findingSubmitCmd, scaSubmitCmd},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			bare := flagNames(p.alias)
			canonical := flagNames(p.sca)
			if len(bare) != len(canonical) {
				t.Fatalf("flag count: bare=%d canonical=%d\n bare: %v\n canonical: %v",
					len(bare), len(canonical), bare, canonical)
			}
			for i := range bare {
				if bare[i] != canonical[i] {
					t.Errorf("flag %d: bare=%q canonical=%q", i, bare[i], canonical[i])
				}
			}
		})
	}
}

// TestBCAlias_ArgsMatch confirms the alias enforces the same Args validation
// as the canonical form. Divergence here would let bare invocations accept
// arg counts the canonical rejects (or vice versa).
func TestBCAlias_ArgsMatch(t *testing.T) {
	// get: exactly 1
	if err := findingGetCmd.Args(findingGetCmd, []string{"x"}); err != nil {
		t.Errorf("finding get with 1 arg should pass: %v", err)
	}
	if err := findingGetCmd.Args(findingGetCmd, nil); err == nil {
		t.Error("finding get with 0 args should fail")
	}
	if err := findingGetCmd.Args(findingGetCmd, []string{"x", "y"}); err == nil {
		t.Error("finding get with 2 args should fail")
	}
	// rate: exactly 2
	if err := findingRateCmd.Args(findingRateCmd, []string{"x", "y"}); err != nil {
		t.Errorf("finding rate with 2 args should pass: %v", err)
	}
	if err := findingRateCmd.Args(findingRateCmd, []string{"x"}); err == nil {
		t.Error("finding rate with 1 arg should fail")
	}
}

// TestNewSubcommandsRegistered verifies the four canonical parents exist
// under `konvu finding` after init().
func TestNewSubcommandsRegistered(t *testing.T) {
	expected := []string{"sca", "sast", "container", "secrets"}
	for _, name := range expected {
		found := false
		for _, c := range findingCmd.Commands() {
			if c.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("finding command missing subcommand: %s", name)
		}
	}
}
