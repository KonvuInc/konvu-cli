package output

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/mapping"
	"golang.org/x/term"
)

// FindingOption is one normalized finding shown in the interactive browser.
type FindingOption struct {
	Kind       string
	Severity   string
	Assessment string
	Finding    string
	Summary    string
	Repository string
	State      string
}

// PickFinding opens the interactive Findings table.
func PickFinding(options []FindingOption, selected int) (index int, opened bool, err error) {
	if len(options) == 0 {
		return 0, false, errors.New("finding picker requires at least one finding")
	}
	selected = clampBaseline(selected, 0, len(options)-1)
	if !BaselineTerminalInteractive() {
		return selected, false, errors.New("finding picker requires terminal stdin and stdout")
	}
	stdinFD := int(os.Stdin.Fd())
	restore, err := enterBaselineRawTerminal(stdinFD, nil)
	if err != nil {
		return selected, false, fmt.Errorf("entering raw terminal mode: %w", err)
	}
	defer restore()
	return pickFindingIO(
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

// RenderFindingTableWidth renders the finding browser without terminal control
// sequences. It is exported for deterministic tests.
func RenderFindingTableWidth(options []FindingOption, width int) string {
	return renderFindingTable(options, -1, baselineStyle{}, "\n", width)
}

func pickFindingIO(
	reader *bufio.Reader,
	writer io.Writer,
	options []FindingOption,
	selected int,
	color bool,
	width func() int,
	waiters ...baselineInputWaiter,
) (int, bool, error) {
	selected = clampBaseline(selected, 0, len(options)-1)
	renderedLines := 0
	for {
		if renderedLines > 0 {
			clearBaselineLines(writer, renderedLines)
		}
		terminalWidth := max(20, width())
		frame := renderFindingTable(options, selected, baselineStyle{enabled: color}, "\r\n", terminalWidth)
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

type findingColumn struct {
	key     string
	header  string
	minimum int
	desired int
	width   int
}

func renderFindingTable(options []FindingOption, selected int, style baselineStyle, newline string, width int) string {
	width = max(20, width)
	columns := findingColumns(width)
	renderColumns := func(option *FindingOption, semanticColor bool) string {
		parts := make([]string, len(columns))
		for index, column := range columns {
			value := column.header
			if option != nil {
				value = findingColumnValue(*option, column.key)
			}
			value = baselinePadRight(baselineFit(value, column.width), column.width)
			if option != nil && semanticColor && column.key == "assessment" {
				value = mapping.Colorize(value, findingAssessmentStatus(option.Assessment))
			}
			parts[index] = value
		}
		return strings.Join(parts, "  ")
	}

	var out strings.Builder
	out.WriteString(style.bold(baselineFit("Findings", width-1)))
	out.WriteString(newline)
	out.WriteString(style.dim(baselineFit(findingBrowserSubtitle(options), width-1)))
	out.WriteString(newline)
	out.WriteString(newline)
	out.WriteString(style.bold(renderColumns(nil, false)))
	out.WriteString(newline)

	start, end := 0, len(options)
	if selected >= 0 && len(options) > baselineRepositoryPickerMaxVisible {
		start = max(0, min(selected-baselineRepositoryPickerMaxVisible/2, len(options)-baselineRepositoryPickerMaxVisible))
		end = start + baselineRepositoryPickerMaxVisible
	}
	for index := start; index < end; index++ {
		row := renderColumns(&options[index], index != selected && style.enabled)
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
		out.WriteString(style.dim(baselineFit(fmt.Sprintf("  %d–%d of %d · use ↑↓ to scroll", start+1, end, len(options)), width-1)))
		out.WriteString(newline)
	}
	if selected >= 0 {
		out.WriteString(newline)
		out.WriteString(style.dim(baselineFit("↑↓ select  Enter/→ open  Esc/Q exit", width-1)))
		out.WriteString(newline)
	}
	return out.String()
}

func findingBrowserSubtitle(options []FindingOption) string {
	kind := "Security"
	if len(options) > 0 && strings.TrimSpace(options[0].Kind) != "" {
		kind = strings.ToUpper(strings.TrimSpace(options[0].Kind))
	}
	return kind + " findings · select one to inspect its details."
}

func findingColumns(terminalWidth int) []findingColumn {
	full := []findingColumn{
		{key: "severity", header: "Severity", minimum: 8, desired: 10},
		{key: "assessment", header: "Assessment", minimum: 12, desired: 16},
		{key: "finding", header: "Finding", minimum: 16, desired: 30},
		{key: "summary", header: "Assessment summary", minimum: 18, desired: 38},
		{key: "repository", header: "Repository / asset", minimum: 18, desired: 32},
		{key: "state", header: "State", minimum: 6, desired: 10},
	}
	columns := full
	available := max(1, terminalWidth-3)
	if findingColumnsMinimum(columns) > available {
		columns = full[:5]
	}
	if findingColumnsMinimum(columns) > available {
		columns = full[:4]
	}
	for index := range columns {
		columns[index].width = columns[index].minimum
	}
	extra := max(0, available-findingColumnsMinimum(columns))
	for _, key := range []string{"assessment", "summary", "finding", "repository", "severity", "state"} {
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

func findingAssessmentStatus(value string) mapping.AssessmentStatus {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), " ", "-"))
	switch normalized {
	case "applicable":
		return mapping.Exploitable
	case "not-applicable":
		return mapping.FalsePositive
	case "unknown", "pending":
		return mapping.NotAssessed
	default:
		return mapping.AssessmentStatus(normalized)
	}
}

func findingColumnsMinimum(columns []findingColumn) int {
	width := max(0, len(columns)-1) * 2
	for _, column := range columns {
		width += column.minimum
	}
	return width
}

func findingColumnValue(option FindingOption, key string) string {
	values := map[string]string{
		"severity":   option.Severity,
		"assessment": option.Assessment,
		"finding":    option.Finding,
		"summary":    option.Summary,
		"repository": option.Repository,
		"state":      option.State,
	}
	value := sanitizeBaselineText(values[key], false)
	if value == "" {
		return "—"
	}
	return value
}
