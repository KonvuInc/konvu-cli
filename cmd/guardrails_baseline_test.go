package cmd

import "testing"

type recordedGuardrailsRun struct {
	args   []string
	apiKey string
	model  string
}

func TestResolveGuardrailsAPIKey(t *testing.T) {
	if got := resolveGuardrailsAPIKey(" flag-key ", "env-key"); got != "flag-key" {
		t.Errorf("explicit key = %q, want flag-key", got)
	}
	if got := resolveGuardrailsAPIKey("", " env-key "); got != "env-key" {
		t.Errorf("environment key = %q, want env-key", got)
	}
}

func TestRunGuardrailsBaselineScanDeclined(t *testing.T) {
	var runs []recordedGuardrailsRun
	run := func(args []string, apiKey, model string) {
		runs = append(runs, recordedGuardrailsRun{args: args, apiKey: apiKey, model: model})
	}
	confirm := func(prompt string, defaultYes bool) bool {
		if prompt != "Continue with the baseline scan?" {
			t.Errorf("prompt = %q", prompt)
		}
		if defaultYes {
			t.Error("confirmation must default to no")
		}
		return false
	}

	runGuardrailsBaselineScan("../repo", "sk-test", false, run, confirm)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want prepare only", len(runs))
	}
	assertGuardrailsRun(t, runs[0], []string{"baseline", "prepare", "../repo"}, "", "")
}

func TestRunGuardrailsBaselineScanAccepted(t *testing.T) {
	var runs []recordedGuardrailsRun
	run := func(args []string, apiKey, model string) {
		runs = append(runs, recordedGuardrailsRun{args: args, apiKey: apiKey, model: model})
	}

	runGuardrailsBaselineScan("../repo", "sk-test", false, run, func(string, bool) bool {
		return true
	})
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want prepare and continue", len(runs))
	}
	assertGuardrailsRun(t, runs[0], []string{"baseline", "prepare", "../repo"}, "", "")
	assertGuardrailsRun(t, runs[1], []string{"baseline", "continue", "../repo"}, "sk-test", guardrailsBaselineModel)
}

func TestRunGuardrailsBaselineScanYesSkipsConfirmation(t *testing.T) {
	var runs int
	run := func([]string, string, string) { runs++ }
	confirm := func(string, bool) bool {
		t.Fatal("confirmation called with --yes")
		return false
	}

	runGuardrailsBaselineScan(".", "sk-test", true, run, confirm)
	if runs != 2 {
		t.Errorf("runs = %d, want 2", runs)
	}
}

func assertGuardrailsRun(t *testing.T, got recordedGuardrailsRun, args []string, apiKey, model string) {
	t.Helper()
	if len(got.args) != len(args) {
		t.Fatalf("args = %v, want %v", got.args, args)
	}
	for i := range args {
		if got.args[i] != args[i] {
			t.Errorf("args[%d] = %q, want %q", i, got.args[i], args[i])
		}
	}
	if got.apiKey != apiKey {
		t.Errorf("api key = %q, want %q", got.apiKey, apiKey)
	}
	if got.model != model {
		t.Errorf("model = %q, want %q", got.model, model)
	}
}
