package cmd

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const guardrailsBaselineEmptyState = "No existing baseline detected. Run `konvu guardrails baseline scan <codebase>` to scan a codebase.\n"

var guardrailsBaselineTUIRunID string

type guardrailsBaselineTUIDependencies struct {
	list            func() ([]baseline.RunEntry, error)
	interactive     func() bool
	pick            func([]output.BaselineRunOption, int) (int, bool, error)
	openCompleted   func(*baseline.Document, bool, io.Writer) (output.BaselineWorkspaceOutcome, error)
	openDiagnostics func(output.BaselineRunOption, bool, io.Writer) (output.BaselineWorkspaceOutcome, error)
	renderRuns      func([]output.BaselineRunOption) string
}

var guardrailsBaselineTUICmd = &cobra.Command{
	Use:    "tui",
	Short:  "Explore historical baselines interactively",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		runGuardrailsBaselineCommand(cmd, func() error {
			if err := guardrailsBaselineValidateOptionalFlag(
				cmd,
				"run",
				guardrailsBaselineTUIRunID,
			); err != nil {
				return err
			}
			store, err := defaultGuardrailsBaselineStore()
			if err != nil {
				return err
			}
			return executeGuardrailsBaselineTUI(
				cmd,
				guardrailsBaselineTUIRunID,
				defaultGuardrailsBaselineTUIDependencies(store),
			)
		})
	},
}

func defaultGuardrailsBaselineTUIDependencies(
	store baseline.Store,
) guardrailsBaselineTUIDependencies {
	return guardrailsBaselineTUIDependencies{
		list:        store.List,
		interactive: output.BaselineTerminalInteractive,
		pick:        output.PickBaselineRun,
		openCompleted: func(
			document *baseline.Document,
			interactive bool,
			writer io.Writer,
		) (output.BaselineWorkspaceOutcome, error) {
			workspace, err := output.NewBaselineWorkspaceV1(document)
			if err != nil {
				return output.BaselineWorkspaceQuit, err
			}
			if interactive {
				return workspace.Browse()
			}
			return output.BaselineWorkspaceQuit, output.WriteString(writer, workspace.StaticSummary())
		},
		openDiagnostics: func(
			option output.BaselineRunOption,
			interactive bool,
			writer io.Writer,
		) (output.BaselineWorkspaceOutcome, error) {
			if interactive {
				return output.BrowseBaselineRunDiagnostics(option)
			}
			return output.BaselineWorkspaceQuit, output.WriteString(
				writer,
				output.BaselineRunDiagnostics(option),
			)
		},
		renderRuns: output.RenderBaselineRunTable,
	}
}

func executeGuardrailsBaselineTUI(
	cmd *cobra.Command,
	runID string,
	deps guardrailsBaselineTUIDependencies,
) error {
	runs, err := deps.list()
	if err != nil {
		return wrapGuardrailsBaselineError(err)
	}
	if len(runs) == 0 {
		return output.WriteString(cmd.OutOrStdout(), guardrailsBaselineEmptyState)
	}
	options := make([]output.BaselineRunOption, len(runs))
	for index, run := range runs {
		options[index] = guardrailsBaselineTUIOption(run)
	}

	selected := 0
	direct := strings.TrimSpace(runID) != ""
	if direct {
		selected = guardrailsBaselineRunIndex(runs, strings.TrimSpace(runID))
		if selected < 0 {
			return guardrailsBaselineError(
				"GUARDRAILS_BASELINE_NOT_FOUND",
				fmt.Sprintf("run %q was not found", strings.TrimSpace(runID)),
				clierrors.ExitNotFound,
			)
		}
	}

	interactive := deps.interactive()
	if !interactive && !direct {
		return output.WriteString(cmd.OutOrStdout(), deps.renderRuns(options))
	}

	for {
		if !direct {
			var opened bool
			selected, opened, err = deps.pick(options, selected)
			if err != nil {
				if errors.Is(err, output.ErrBaselineCancelled) {
					return nil
				}
				return err
			}
			if !opened {
				return nil
			}
		}

		run := runs[selected]
		option := options[selected]
		var outcome output.BaselineWorkspaceOutcome
		if run.Valid && run.Run.Status == baseline.StatusCompleted {
			outcome, err = deps.openCompleted(run.Document, interactive, cmd.OutOrStdout())
		} else {
			outcome, err = deps.openDiagnostics(option, interactive, cmd.OutOrStdout())
		}
		if err != nil {
			if errors.Is(err, output.ErrBaselineCancelled) {
				return nil
			}
			return wrapGuardrailsBaselineError(err)
		}
		if !interactive || outcome != output.BaselineWorkspaceBack {
			return nil
		}
		direct = false
	}
}

func guardrailsBaselineTUIOption(run baseline.RunEntry) output.BaselineRunOption {
	repository := run.Codebase.Name
	if repository == "" && run.Codebase.Path != "" {
		repository = filepath.Base(run.Codebase.Path)
	}
	commit := run.Codebase.Git.Commit
	if run.Codebase.Git.Dirty && commit != "" {
		commit += "*"
	}
	status := string(run.Run.Status)
	if !run.Valid {
		status = "invalid"
	}
	problem := run.Problem
	if problem == "" && run.Document != nil {
		raw := run.Document.Raw()
		if metadata, ok := raw["run"].(map[string]any); ok {
			problem, _ = metadata["error"].(string)
		}
	}
	scanned := run.Run.CompletedAt
	if scanned == "" {
		scanned = run.Run.StartedAt
	}
	counts := run.Counts
	if run.Valid && run.Run.Status == baseline.StatusCompleted {
		if catalog, err := baseline.NewCatalog(run.Document); err == nil {
			counts = catalog.Counts()
		}
	}
	return output.BaselineRunOption{
		ID:              run.ID,
		Repository:      repository,
		Commit:          commit,
		Scanned:         formatGuardrailsBaselineScanned(scanned),
		Duration:        formatGuardrailsBaselineDuration(run.Run.DurationSeconds),
		Assets:          counts.Assets,
		Controls:        counts.Controls,
		Implementations: counts.Implementations,
		Status:          status,
		Problem:         problem,
	}
}

func formatGuardrailsBaselineScanned(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.Local().Format("2006-01-02 15:04")
}

func guardrailsBaselineRunIndex(runs []baseline.RunEntry, id string) int {
	for index := range runs {
		if runs[index].ID == id {
			return index
		}
	}
	return -1
}

func init() {
	guardrailsBaselineTUICmd.Flags().StringVar(
		&guardrailsBaselineTUIRunID,
		"run",
		"",
		"open an exact run first (Escape returns to the run list)",
	)
	guardrailsBaselineCmd.AddCommand(guardrailsBaselineTUICmd)
}
