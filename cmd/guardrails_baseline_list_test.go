package cmd

import (
	"strings"
	"testing"
)

func TestMapHistoryEmptyStateDistinguishesFilteredResults(t *testing.T) {
	unfiltered := guardrailsBaselineRunListOptions{Limit: 50, Sort: "mapped", Order: "desc"}
	if got := mapHistoryEmptyState("", "", unfiltered); got != guardrailsBaselineEmptyState {
		t.Fatalf("unfiltered empty state = %q, want default", got)
	}
	cases := map[string]struct {
		runID      string
		repository string
		options    guardrailsBaselineRunListOptions
	}{
		"status": {options: guardrailsBaselineRunListOptions{Statuses: []string{"completed"}, Limit: 50}},
		"offset": {options: guardrailsBaselineRunListOptions{Offset: 10, Limit: 50}},
		"repo":   {repository: "payments", options: unfiltered},
		"run":    {runID: "payments--a17c2e99--000042", options: unfiltered},
	}
	for name, tc := range cases {
		got := mapHistoryEmptyState(tc.runID, tc.repository, tc.options)
		if got == guardrailsBaselineEmptyState {
			t.Errorf("%s: filtered empty state fell back to first-run hint", name)
		}
		if !strings.Contains(got, "matched") {
			t.Errorf("%s: empty state = %q, want a filter explanation", name, got)
		}
	}
}
