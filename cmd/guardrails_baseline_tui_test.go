package cmd

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

func TestGuardrailsBaselineTUIEmptyState(t *testing.T) {
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	err := executeGuardrailsBaselineTUI(cmd, "", guardrailsBaselineTUIDependencies{
		list: func() ([]baseline.RunEntry, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != guardrailsBaselineEmptyState {
		t.Fatalf("empty state = %q", stdout.String())
	}
}

func TestGuardrailsBaselineTUINonInteractiveStartsWithRuns(t *testing.T) {
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	runs := []baseline.RunEntry{{
		ID:    "payments--a17c2e99--000042",
		Valid: true,
		Run: baseline.RunMetadata{
			Status:          baseline.StatusCompleted,
			CompletedAt:     "2026-08-27T10:00:12Z",
			DurationSeconds: 12.5,
		},
		Codebase: baseline.CodebaseMetadata{
			Name: "payments",
			Git:  baseline.GitMetadata{Commit: "a17c2e9987654321"},
		},
		Counts: baseline.Counts{Assets: 7, Controls: 4, Implementations: 3},
	}}
	err := executeGuardrailsBaselineTUI(cmd, "", guardrailsBaselineTUIDependencies{
		list:        func() ([]baseline.RunEntry, error) { return runs, nil },
		interactive: func() bool { return false },
		renderRuns:  output.RenderBaselineRunTable,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Guardrails baselines", "payments", "Assets", "Controls", "completed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("runs-first output is missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestGuardrailsBaselineTUIDirectRunEscapesBackToCatalog(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	runs := []baseline.RunEntry{{
		ID:    "failed--no-commit--000001",
		Valid: true,
		Run:   baseline.RunMetadata{Status: baseline.StatusFailed},
	}}
	openedDiagnostics := 0
	picked := 0
	err := executeGuardrailsBaselineTUI(cmd, runs[0].ID, guardrailsBaselineTUIDependencies{
		list:        func() ([]baseline.RunEntry, error) { return runs, nil },
		interactive: func() bool { return true },
		pick: func(_ []output.BaselineRunOption, selected int) (int, bool, error) {
			picked++
			return selected, false, nil
		},
		openDiagnostics: func(
			_ output.BaselineRunOption,
			_ bool,
			_ io.Writer,
		) (output.BaselineWorkspaceOutcome, error) {
			openedDiagnostics++
			return output.BaselineWorkspaceBack, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if openedDiagnostics != 1 || picked != 1 {
		t.Fatalf("diagnostics opened %d times, picker opened %d times", openedDiagnostics, picked)
	}
}

func TestGuardrailsBaselineTUIReportsUnknownDirectRun(t *testing.T) {
	cmd := &cobra.Command{}
	err := executeGuardrailsBaselineTUI(cmd, "missing", guardrailsBaselineTUIDependencies{
		list: func() ([]baseline.RunEntry, error) {
			return []baseline.RunEntry{{ID: "known"}}, nil
		},
	})
	var cliErr interface{ Error() string }
	if err == nil || !errors.As(err, &cliErr) || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("unknown-run error = %#v", err)
	}
}
