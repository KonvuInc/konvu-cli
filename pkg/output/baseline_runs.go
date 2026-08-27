package output

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// BaselineRunOption is one historical run shown by the TUI catalog.
type BaselineRunOption struct {
	ID              string
	Repository      string
	Commit          string
	Scanned         string
	Duration        string
	Assets          int
	Controls        int
	Implementations int
	Status          string
	Problem         string
}

// RenderBaselineRunTable renders the runs-first TUI screen without ANSI
// escapes. It is also the deterministic fallback when stdout is not a TTY.
func RenderBaselineRunTable(options []BaselineRunOption) string {
	return renderBaselineRunTable(options, -1, baselineStyle{}, "\n")
}

// BaselineRunDiagnostics renders the complete diagnostic view for a run that
// cannot open the completed-baseline workspace.
func BaselineRunDiagnostics(option BaselineRunOption) string {
	status := sanitizeBaselineText(option.Status, false)
	if status == "" {
		status = "invalid"
	}
	problem := sanitizeBaselineText(option.Problem, true)
	if problem == "" {
		problem = "This run is not complete. Inspect run.log for execution details."
	}
	return fmt.Sprintf(
		"Guardrails baseline\n\nRepository: %s\nRun: %s\nCommit: %s\nStatus: %s\n\n%s\n",
		baselineRunRepository(option),
		sanitizeBaselineText(option.ID, false),
		baselineRunCommit(option),
		status,
		problem,
	)
}

// PickBaselineRun opens the historical run catalog. Up/Down move, Enter/Right
// opens a run, and Escape/Q exits.
func PickBaselineRun(
	options []BaselineRunOption,
	selected int,
) (index int, opened bool, err error) {
	if len(options) == 0 {
		return 0, false, errors.New("baseline run picker requires at least one run")
	}
	selected = clampBaseline(selected, 0, len(options)-1)
	if !BaselineTerminalInteractive() {
		return selected, false, errors.New("baseline run picker requires terminal stdin and stdout")
	}

	stdinFD := int(os.Stdin.Fd())
	restore, err := enterBaselineRawTerminal(stdinFD, nil)
	if err != nil {
		return selected, false, fmt.Errorf("entering raw terminal mode: %w", err)
	}
	defer restore()

	return pickBaselineRunIO(
		bufio.NewReader(os.Stdin),
		os.Stdout,
		options,
		selected,
		baselineColorEnabled(os.Stdout),
		func() (bool, error) {
			return baselineWaitForInput(stdinFD, baselineEscapeSequenceWait)
		},
	)
}

// BrowseBaselineRunDiagnostics opens a non-completed run and returns Back on
// Escape/Left so the caller can restore the runs catalog.
func BrowseBaselineRunDiagnostics(
	option BaselineRunOption,
) (BaselineWorkspaceOutcome, error) {
	if !BaselineTerminalInteractive() {
		return BaselineWorkspaceQuit, errors.New("baseline diagnostics require terminal stdin and stdout")
	}
	stdinFD := int(os.Stdin.Fd())
	restore, err := enterBaselineRawTerminal(stdinFD, func() {
		_, _ = io.WriteString(os.Stdout, "\033[?25h\033[?1049l")
	})
	if err != nil {
		return BaselineWorkspaceQuit, fmt.Errorf("entering raw terminal mode: %w", err)
	}
	defer restore()
	if _, err := io.WriteString(os.Stdout, "\033[?1049h\033[?25l"); err != nil {
		return BaselineWorkspaceQuit, err
	}
	defer func() { _, _ = io.WriteString(os.Stdout, "\033[?25h\033[?1049l") }()

	width, height, sizeErr := term.GetSize(int(os.Stdout.Fd()))
	if sizeErr != nil || width <= 0 || height <= 0 {
		width, height = 100, 24
	}
	frame := renderBaselineRunDiagnostics(option, width, height, baselineStyle{
		enabled: baselineColorEnabled(os.Stdout),
	})
	if _, err := io.WriteString(os.Stdout, frame); err != nil {
		return BaselineWorkspaceQuit, err
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		key, readErr := readBaselineKey(reader, func() (bool, error) {
			return baselineWaitForInput(stdinFD, baselineEscapeSequenceWait)
		})
		if readErr != nil {
			return BaselineWorkspaceQuit, readErr
		}
		switch key.kind {
		case baselineKeyEscape, baselineKeyLeft:
			return BaselineWorkspaceBack, nil
		case baselineKeyQuit:
			return BaselineWorkspaceQuit, nil
		case baselineKeyCancel:
			return BaselineWorkspaceCancelled, ErrBaselineCancelled
		}
	}
}

func pickBaselineRunIO(
	reader *bufio.Reader,
	writer io.Writer,
	options []BaselineRunOption,
	selected int,
	color bool,
	waiters ...baselineInputWaiter,
) (int, bool, error) {
	if len(options) == 0 {
		return 0, false, errors.New("baseline run picker requires at least one run")
	}
	selected = clampBaseline(selected, 0, len(options)-1)
	renderedLines := 0
	for {
		if renderedLines > 0 {
			clearBaselineLines(writer, renderedLines)
		}
		frame := renderBaselineRunTable(options, selected, baselineStyle{enabled: color}, "\r\n")
		if _, err := io.WriteString(writer, frame); err != nil {
			return selected, false, err
		}
		renderedLines = strings.Count(frame, "\n")

		key, err := readBaselineKey(reader, waiters...)
		if err != nil {
			return selected, false, err
		}
		switch key.kind {
		case baselineKeyUp:
			selected = max(0, selected-1)
		case baselineKeyDown:
			selected = min(len(options)-1, selected+1)
		case baselineKeyEnter, baselineKeyRight:
			clearBaselineLines(writer, renderedLines)
			return selected, true, nil
		case baselineKeyEscape, baselineKeyQuit:
			clearBaselineLines(writer, renderedLines)
			return selected, false, nil
		case baselineKeyCancel:
			clearBaselineLines(writer, renderedLines)
			return selected, false, ErrBaselineCancelled
		}
	}
}

func renderBaselineRunTable(
	options []BaselineRunOption,
	selected int,
	style baselineStyle,
	newline string,
) string {
	const (
		repositoryWidth      = 20
		commitWidth          = 8
		runWidth             = 28
		scannedWidth         = 16
		durationWidth        = 8
		assetsWidth          = 6
		controlsWidth        = 8
		implementationsWidth = 15
		statusWidth          = 10
	)
	columns := func(values ...string) string {
		widths := []int{
			repositoryWidth, commitWidth, runWidth, scannedWidth, durationWidth,
			assetsWidth, controlsWidth, implementationsWidth, statusWidth,
		}
		parts := make([]string, len(values))
		for index, value := range values {
			parts[index] = baselinePadRight(baselineFit(value, widths[index]), widths[index])
		}
		return strings.Join(parts, "  ")
	}

	var out strings.Builder
	out.WriteString(style.bold("Guardrails baselines"))
	out.WriteString(newline)
	out.WriteString(style.dim("Select a run to explore its Assets, Controls, and Implementations."))
	out.WriteString(newline)
	out.WriteString(newline)
	out.WriteString(style.bold(columns(
		"Repository", "Commit", "Run", "Scanned", "Time", "Assets", "Controls", "Implementations", "Status",
	)))
	out.WriteString(newline)
	start, end := 0, len(options)
	if selected >= 0 && len(options) > baselineRepositoryPickerMaxVisible {
		visible := baselineRepositoryPickerMaxVisible
		start = max(0, min(selected-visible/2, len(options)-visible))
		end = start + visible
	}
	for index := start; index < end; index++ {
		option := options[index]
		row := columns(
			baselineRunRepository(option),
			baselineRunCommit(option),
			sanitizeBaselineText(option.ID, false),
			sanitizeBaselineText(option.Scanned, false),
			sanitizeBaselineText(option.Duration, false),
			fmt.Sprintf("%d", option.Assets),
			fmt.Sprintf("%d", option.Controls),
			fmt.Sprintf("%d", option.Implementations),
			baselineRunStatus(option),
		)
		marker := "  "
		if index == selected {
			marker = "› "
			row = style.highlight(row)
		}
		out.WriteString(marker)
		out.WriteString(row)
		out.WriteString(newline)
	}
	if end-start < len(options) {
		out.WriteString(style.dim(fmt.Sprintf(
			"  %d–%d of %d · use ↑↓ to scroll",
			start+1,
			end,
			len(options),
		)))
		out.WriteString(newline)
	}
	if selected >= 0 {
		out.WriteString(newline)
		out.WriteString(style.dim("↑↓ select  Enter/→ open  Esc/Q exit"))
		out.WriteString(newline)
	}
	return out.String()
}

func renderBaselineRunDiagnostics(
	option BaselineRunOption,
	width, height int,
	style baselineStyle,
) string {
	lines := strings.Split(strings.TrimSuffix(BaselineRunDiagnostics(option), "\n"), "\n")
	for index, line := range lines {
		lines[index] = baselineFit(line, width)
	}
	for len(lines) < max(1, height-1) {
		lines = append(lines, "")
	}
	lines = append(lines, style.dim(baselineFit("Esc/← runs  Q exit", width)))
	return "\033[H" + strings.Join(lines, "\r\n")
}

func baselineRunRepository(option BaselineRunOption) string {
	value := sanitizeBaselineText(option.Repository, false)
	if value == "" {
		return "unknown"
	}
	return value
}

func baselineRunCommit(option BaselineRunOption) string {
	value := baselineShortCommit(sanitizeBaselineText(option.Commit, false))
	if value == "" {
		return "—"
	}
	return value
}

func baselineRunStatus(option BaselineRunOption) string {
	value := sanitizeBaselineText(option.Status, false)
	if value == "" {
		return "invalid"
	}
	return value
}
