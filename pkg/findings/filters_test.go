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
	got, err := ReadCommonFilters(c)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Since != "" || len(got.Severity) != 0 || len(got.Repository) != 0 ||
		len(got.Assessment) != 0 || got.Limit != 0 || got.Format != "" || got.QuietIDs {
		t.Fatalf("defaults not zero-valued: %+v", got)
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
	got, err := ReadCommonFilters(c)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
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
}
