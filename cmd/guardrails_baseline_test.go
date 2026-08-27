package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

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

func TestGuardrailsBaselineScanCommandContract(t *testing.T) {
	if guardrailsBaselineScanCmd.Use != "scan <codebase>" {
		t.Fatalf("Use = %q", guardrailsBaselineScanCmd.Use)
	}
	if err := guardrailsBaselineScanCmd.Args(guardrailsBaselineScanCmd, nil); err == nil {
		t.Error("scan accepted no codebase")
	}
	if err := guardrailsBaselineScanCmd.Args(guardrailsBaselineScanCmd, []string{"/repo"}); err != nil {
		t.Errorf("scan rejected one codebase: %v", err)
	}
	if err := guardrailsBaselineScanCmd.Args(guardrailsBaselineScanCmd, []string{"/one", "/two"}); err == nil {
		t.Error("scan accepted more than one codebase")
	}
	for _, name := range []string{"yes", "openai-api-key"} {
		if guardrailsBaselineScanCmd.Flags().Lookup(name) == nil {
			t.Errorf("scan missing --%s", name)
		}
	}
	if guardrailsBaselineScanCmd.Flags().Lookup("repo") != nil {
		t.Error("scan still exposes legacy --repo")
	}
}

func TestGuardrailsCommandTreeOnlyExposesBaselineExperience(t *testing.T) {
	direct := func(command *cobra.Command) map[string]bool {
		children := make(map[string]bool)
		for _, child := range command.Commands() {
			children[child.Name()] = true
		}
		return children
	}

	guardrailsChildren := direct(guardrailsCmd)
	if len(guardrailsChildren) != 1 || !guardrailsChildren["baseline"] {
		t.Fatalf("guardrails children = %v, want baseline only", guardrailsChildren)
	}
	baselineChildren := direct(guardrailsBaselineCmd)
	if len(baselineChildren) != 5 {
		t.Fatalf("baseline children = %v, want exactly five commands", baselineChildren)
	}
	for _, name := range []string{"scan", "list", "show", "explain", "tui"} {
		if !baselineChildren[name] {
			t.Errorf("baseline command missing %q: %v", name, baselineChildren)
		}
	}
}

func TestRunGuardrailsBaselineScanDelegatesInteractiveWorkflow(t *testing.T) {
	var runs []recordedGuardrailsRun
	run := func(args []string, apiKey, model string) {
		runs = append(runs, recordedGuardrailsRun{args: args, apiKey: apiKey, model: model})
	}

	runGuardrailsBaselineScan("../repo", "sk-test", false, run)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want one runtime workflow", len(runs))
	}
	assertGuardrailsRun(
		t,
		runs[0],
		[]string{"baseline", "scan", "../repo"},
		"sk-test",
		guardrailsBaselineModel,
	)
}

func TestRunGuardrailsBaselineScanAllowsEstimateWithoutAPIKey(t *testing.T) {
	var runs []recordedGuardrailsRun
	run := func(args []string, apiKey, model string) {
		runs = append(runs, recordedGuardrailsRun{args: args, apiKey: apiKey, model: model})
	}

	runGuardrailsBaselineScan("../repo", "", false, run)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want one runtime workflow", len(runs))
	}
	assertGuardrailsRun(
		t,
		runs[0],
		[]string{"baseline", "scan", "../repo"},
		"",
		guardrailsBaselineModel,
	)
}

func TestRunGuardrailsBaselineScanYesForwardsFlag(t *testing.T) {
	var runs []recordedGuardrailsRun
	run := func(args []string, apiKey, model string) {
		runs = append(runs, recordedGuardrailsRun{args: args, apiKey: apiKey, model: model})
	}

	runGuardrailsBaselineScan(".", "sk-test", true, run)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want one runtime workflow", len(runs))
	}
	assertGuardrailsRun(
		t,
		runs[0],
		[]string{"baseline", "scan", ".", "--yes"},
		"sk-test",
		guardrailsBaselineModel,
	)
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
