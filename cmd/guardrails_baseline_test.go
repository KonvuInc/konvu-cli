package cmd

import (
	"os"
	"os/exec"
	"strings"
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

func TestInventoryMapCommandContract(t *testing.T) {
	if inventoryMapCmd.Use != "map <local-path>" {
		t.Fatalf("Use = %q", inventoryMapCmd.Use)
	}
	if err := inventoryMapCmd.Args(inventoryMapCmd, nil); err == nil {
		t.Error("map accepted no local path")
	}
	if err := inventoryMapCmd.Args(inventoryMapCmd, []string{"/repo"}); err != nil {
		t.Errorf("map rejected one local path: %v", err)
	}
	if err := inventoryMapCmd.Args(inventoryMapCmd, []string{"/one", "/two"}); err == nil {
		t.Error("map accepted more than one local path")
	}
	for _, name := range []string{"yes", "openai-api-key"} {
		if inventoryMapCmd.Flags().Lookup(name) == nil {
			t.Errorf("map missing --%s", name)
		}
	}
	if inventoryMapCmd.Flags().Lookup("repo") != nil {
		t.Error("map unexpectedly exposes --repo")
	}
}

func TestSecurityContextGraphCommandsLiveUnderInventoryMap(t *testing.T) {
	direct := func(command *cobra.Command) map[string]bool {
		children := make(map[string]bool)
		for _, child := range command.Commands() {
			children[child.Name()] = true
		}
		return children
	}

	for _, command := range rootCmd.Commands() {
		if command.Name() == "guardrails" {
			t.Fatal("root command still exposes guardrails")
		}
	}
	mapChildren := direct(inventoryMapCmd)
	if len(mapChildren) != 4 {
		t.Fatalf("map children = %v, want history, show, diff, and records", mapChildren)
	}
	for _, name := range []string{"history", "show", "diff", "records"} {
		if !mapChildren[name] {
			t.Errorf("inventory map command missing %q: %v", name, mapChildren)
		}
	}
	for _, name := range []string{"scan", "list", "get", "counts", "tui"} {
		if mapChildren[name] {
			t.Errorf("inventory map still exposes redundant command %q", name)
		}
	}
	recordChildren := direct(guardrailsBaselineRecordsCmd)
	if len(recordChildren) != 4 {
		t.Fatalf("record children = %v, want exactly four commands", recordChildren)
	}
	for _, name := range []string{"list", "search", "get", "explain"} {
		if !recordChildren[name] {
			t.Errorf("records command missing %q: %v", name, recordChildren)
		}
	}
}

func TestRemovedGuardrailsCommandsAreRejected(t *testing.T) {
	const helperEnv = "KONVU_TEST_INVALID_GUARDRAILS_ARGS"
	if rawArgs := os.Getenv(helperEnv); rawArgs != "" {
		rootCmd.SetArgs(strings.Fields(rawArgs))
		if err := rootCmd.Execute(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "guardrails", args: []string{"guardrails"}},
		{name: "legacy list", args: []string{"inventory", "map", "list"}},
		{name: "legacy get", args: []string{"inventory", "map", "get"}},
		{name: "redundant counts", args: []string{"inventory", "map", "counts"}},
		{name: "legacy tui", args: []string{"inventory", "map", "tui"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestRemovedGuardrailsCommandsAreRejected$")
			command.Env = append(os.Environ(), helperEnv+"="+strings.Join(test.args, " "))
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("konvu %s exited successfully; output:\n%s", strings.Join(test.args, " "), output)
			}
			if test.name == "guardrails" && !strings.Contains(string(output), "unknown command") {
				t.Fatalf("konvu %s did not report the removed command:\n%s", strings.Join(test.args, " "), output)
			}
		})
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
