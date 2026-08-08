package findings

import (
	"testing"

	"github.com/spf13/cobra"
)

func newTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "test"}
	RegisterCommonFlags(c)
	return c
}

func TestReadCommonFilters_Defaults(t *testing.T) {
	c := newTestCmd()
	if err := c.ParseFlags(nil); err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ReadCommonFilters(c)
	if got.Since != "" || len(got.Severity) != 0 || len(got.Repository) != 0 ||
		len(got.Assessment) != 0 || got.Limit != 0 || got.Format != "" || got.QuietIDs {
		t.Fatalf("defaults not zero-valued: %+v", got)
	}
	if n := got.LimitOr(30); n != 30 {
		t.Errorf("LimitOr(30) with unset limit: got %d want 30", n)
	}
}

func TestReadCommonFilters_Populated(t *testing.T) {
	c := newTestCmd()
	err := c.ParseFlags([]string{
		"--since", "7d",
		"--severity", "critical,high",
		"--repo", "foo/bar",
		"--repo", "baz/qux",
		"--assessment", "exploitable",
		"--limit", "50",
		"-o", "json",
		"-q",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ReadCommonFilters(c)
	if got.Since != "7d" {
		t.Errorf("since: %q", got.Since)
	}
	if len(got.Severity) != 2 || got.Severity[0] != "critical" || got.Severity[1] != "high" {
		t.Errorf("severity: %v", got.Severity)
	}
	if len(got.Repository) != 2 {
		t.Errorf("repo: %v", got.Repository)
	}
	if got.Limit != 50 || got.Format != "json" || !got.QuietIDs {
		t.Errorf("scalar mismatch: %+v", got)
	}
	if n := got.LimitOr(30); n != 50 {
		t.Errorf("LimitOr(30) with limit=50: got %d want 50", n)
	}
}

// A subcommand that registers only a subset of common flags should still be
// able to call ReadCommonFilters without error, missing flags returning zero.
func TestReadCommonFilters_SubsetRegistration(t *testing.T) {
	c := &cobra.Command{Use: "test"}
	c.Flags().StringSlice("assessment", nil, "")
	c.Flags().Int("limit", 0, "")
	if err := c.ParseFlags([]string{"--assessment", "foo", "--limit", "7"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ReadCommonFilters(c)
	if len(got.Assessment) != 1 || got.Assessment[0] != "foo" {
		t.Errorf("assessment: %v", got.Assessment)
	}
	if got.Limit != 7 {
		t.Errorf("limit: %d", got.Limit)
	}
	if got.Since != "" || len(got.Severity) != 0 {
		t.Errorf("unregistered flags should be zero: %+v", got)
	}
}
