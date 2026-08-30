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
	TotalCost       string
	Assets          int
	Controls        int
	Implementations int
	Status          string
	Problem         string
}

// RenderBaselineRunTable renders the runs-first TUI screen without ANSI
// escapes. It is also the deterministic fallback when stdout is not a TTY.
func RenderBaselineRunTable(options []BaselineRunOption) string {
	return RenderBaselineRunTableWidth(options, 120)
}

// RenderBaselineRunTableWidth renders the run catalog within a fixed terminal
// width. It is exported for deterministic non-interactive and PTY tests.
func RenderBaselineRunTableWidth(options []BaselineRunOption, width int) string {
	return renderBaselineRunTable(options, -1, baselineStyle{}, "\n", width)
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
		func() int {
			width, _, sizeErr := term.GetSize(int(os.Stdout.Fd()))
			if sizeErr != nil || width <= 0 {
				return 120
			}
			return width
		},
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
	width func() int,
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
		terminalWidth := max(20, width())
		frame := renderBaselineRunTable(
			options,
			selected,
			baselineStyle{enabled: color},
			"\r\n",
			terminalWidth,
		)
		if _, err := io.WriteString(writer, frame); err != nil {
			return selected, false, err
		}
		renderedLines = baselinePhysicalLineCount(frame, terminalWidth)

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
	width int,
) string {
	width = max(20, width)
	columns := baselineRunColumns(width)
	renderColumns := func(option *BaselineRunOption) string {
		parts := make([]string, len(columns))
		for index, column := range columns {
			value := column.header
			if option != nil {
				value = baselineRunColumnValue(*option, column.key)
			}
			parts[index] = baselinePadRight(baselineFit(value, column.width), column.width)
		}
		return strings.Join(parts, "  ")
	}

	var out strings.Builder
	out.WriteString(style.bold(baselineFit("Guardrails baselines", width-1)))
	out.WriteString(newline)
	out.WriteString(style.dim(baselineFit(
		"Select a run to explore its Assets, Controls, and Implementations.",
		width-1,
	)))
	out.WriteString(newline)
	out.WriteString(newline)
	out.WriteString(style.bold(renderColumns(nil)))
	out.WriteString(newline)
	start, end := 0, len(options)
	if selected >= 0 && len(options) > baselineRepositoryPickerMaxVisible {
		visible := baselineRepositoryPickerMaxVisible
		start = max(0, min(selected-visible/2, len(options)-visible))
		end = start + visible
	}
	for index := start; index < end; index++ {
		option := options[index]
		renderedRow := renderColumns(&option)
		marker := "  "
		if index == selected {
			marker = "› "
			renderedRow = style.highlight(renderedRow)
		}
		out.WriteString(marker)
		out.WriteString(renderedRow)
		out.WriteString(newline)
	}
	if end-start < len(options) {
		out.WriteString(style.dim(baselineFit(fmt.Sprintf(
			"  %d–%d of %d · use ↑↓ to scroll",
			start+1,
			end,
			len(options),
		), width-1)))
		out.WriteString(newline)
	}
	if selected >= 0 {
		out.WriteString(newline)
		out.WriteString(style.dim(baselineFit("↑↓ select  Enter/→ open  Esc/Q exit", width-1)))
		out.WriteString(newline)
	}
	return out.String()
}

type baselineRunColumn struct {
	key     string
	header  string
	minimum int
	desired int
	width   int
}

func baselineRunColumns(terminalWidth int) []baselineRunColumn {
	full := []baselineRunColumn{
		{key: "repository", header: "Repository", minimum: 10, desired: 20},
		{key: "commit", header: "Commit", minimum: 9, desired: 9},
		{key: "run", header: "Run", minimum: 3, desired: 28},
		{key: "scanned", header: "Scanned", minimum: 7, desired: 16},
		{key: "duration", header: "Duration", minimum: 8, desired: 8},
		{key: "total_cost", header: "Total cost", minimum: 10, desired: 10},
		{key: "assets", header: "Assets", minimum: 6, desired: 6},
		{key: "controls", header: "Controls", minimum: 8, desired: 8},
		{key: "implementations", header: "Implementations", minimum: 15, desired: 15},
		{key: "status", header: "Status", minimum: 9, desired: 10},
	}
	compact := []baselineRunColumn{
		{key: "repository", header: "Repository", minimum: 10, desired: 16},
		{key: "commit", header: "Commit", minimum: 9, desired: 9},
		{key: "run", header: "Run", minimum: 6, desired: 24},
		{key: "duration", header: "Duration", minimum: 8, desired: 8},
		{key: "total_cost", header: "Total cost", minimum: 10, desired: 10},
		{key: "assets", header: "Assets", minimum: 6, desired: 6},
		{key: "controls", header: "Controls", minimum: 8, desired: 8},
		{key: "implementations", header: "Implementations", minimum: 15, desired: 15},
		{key: "status", header: "Status", minimum: 9, desired: 10},
	}
	narrow := []baselineRunColumn{
		{key: "repository", header: "Repository", minimum: 10, desired: 18},
		{key: "commit", header: "Commit", minimum: 9, desired: 9},
		{key: "run", header: "Run", minimum: 8, desired: 24},
		{key: "status", header: "Status", minimum: 9, desired: 10},
	}
	available := max(1, terminalWidth-3)
	columns := full
	if baselineRunColumnsMinimum(full) > available {
		columns = compact
	}
	if baselineRunColumnsMinimum(columns) > available {
		columns = narrow
	}
	for index := range columns {
		columns[index].width = columns[index].minimum
	}
	extra := max(0, available-baselineRunColumnsMinimum(columns))
	for _, key := range []string{"run", "repository", "scanned", "status"} {
		for index := range columns {
			if columns[index].key != key || extra == 0 {
				continue
			}
			growth := min(extra, columns[index].desired-columns[index].width)
			columns[index].width += growth
			extra -= growth
		}
	}
	return columns
}

func baselineRunColumnsMinimum(columns []baselineRunColumn) int {
	width := max(0, len(columns)-1) * 2
	for _, column := range columns {
		width += column.minimum
	}
	return width
}

func baselineRunColumnValue(option BaselineRunOption, key string) string {
	switch key {
	case "repository":
		return baselineRunRepository(option)
	case "commit":
		return baselineRunCommit(option)
	case "run":
		return sanitizeBaselineText(option.ID, false)
	case "scanned":
		return sanitizeBaselineText(option.Scanned, false)
	case "duration":
		return sanitizeBaselineText(option.Duration, false)
	case "total_cost":
		return sanitizeBaselineText(option.TotalCost, false)
	case "assets":
		return fmt.Sprintf("%d", option.Assets)
	case "controls":
		return fmt.Sprintf("%d", option.Controls)
	case "implementations":
		return fmt.Sprintf("%d", option.Implementations)
	case "status":
		return baselineRunStatus(option)
	default:
		return ""
	}
}

func baselinePhysicalLineCount(frame string, width int) int {
	width = max(1, width)
	normalized := strings.ReplaceAll(frame, "\r\n", "\n")
	normalized = strings.TrimSuffix(normalized, "\n")
	if normalized == "" {
		return 0
	}
	lines := strings.Split(normalized, "\n")
	count := 0
	for _, line := range lines {
		count += max(1, (visibleLen(line)+width-1)/width)
	}
	return count
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
	value := sanitizeBaselineText(option.Commit, false)
	if value == "" {
		return "—"
	}
	dirty := strings.HasSuffix(value, "*")
	value = strings.TrimSuffix(value, "*")
	if value != "no-commit" && len(value) > 8 {
		value = value[:8]
	}
	if dirty {
		value += "*"
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
