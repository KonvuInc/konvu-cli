package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Words the CLI no longer says to a user. The endpoints and the server's field names still use
// some of them, which is why this checks help text only.
var retiredWords = []string{
	"baseline", "ratify", "ratified", "guarded", "unguarded",
	"capability", "capabilities", "breach", "drift", "invariant", "modelled", "fingerprint",
}

func retiredWordIn(s string) string {
	// "guardrails" is the product name and carries "guard".
	lower := strings.ReplaceAll(strings.ToLower(s), "guardrails", "")
	for _, w := range retiredWords {
		if strings.Contains(lower, w) {
			return w
		}
	}
	return ""
}

func TestNoRetiredWordInGuardrailsHelp(t *testing.T) {
	for _, c := range append([]*cobra.Command{guardrailsCmd}, guardrailsCmd.Commands()...) {
		for label, text := range map[string]string{
			"Use": c.Use, "Short": c.Short, "Long": c.Long, "Example": c.Example,
		} {
			if w := retiredWordIn(text); w != "" {
				t.Errorf("%s %s says %q:\n%s", c.Name(), label, w, text)
			}
		}
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if w := retiredWordIn(f.Usage); w != "" {
				t.Errorf("%s --%s usage says %q: %q", c.Name(), f.Name, w, f.Usage)
			}
		})
	}
}

func TestGuardrailsSubcommandNames(t *testing.T) {
	have := map[string]bool{}
	for _, c := range guardrailsCmd.Commands() {
		have[c.Name()] = true
	}
	for _, want := range []string{"scan", "approve", "connect", "list", "show", "review", "explain"} {
		if !have[want] {
			t.Errorf("missing subcommand: %s", want)
		}
	}
	for _, gone := range []string{"baseline", "ratify", "install"} {
		if have[gone] {
			t.Errorf("retired subcommand still registered: %s", gone)
		}
	}
}

func TestConditionDisplayOnlyTranslatesTrue(t *testing.T) {
	if got := conditionDisplay("true"); got != "always" {
		t.Errorf("conditionDisplay(true) = %q, want always", got)
	}
	for _, in := range []string{"owns(USER, Document)", "true_owner(USER)", ""} {
		if got := conditionDisplay(in); got != in {
			t.Errorf("conditionDisplay(%q) = %q, want it unchanged", in, got)
		}
	}
}
