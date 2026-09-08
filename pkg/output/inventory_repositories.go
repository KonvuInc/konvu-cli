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

// InventoryRepositoryOption is one repository shown in the Inventory TUI.
// String fields allow unavailable local or hosted facets to render as an em dash.
type InventoryRepositoryOption struct {
	Repository    string
	Source        string
	ThreatProfile string
	SecurityGraph string
	Updated       string
	Assets        string
	Controls      string
}

// PickInventoryRepository opens the rich Inventory repository table.
func PickInventoryRepository(
	options []InventoryRepositoryOption,
	selected int,
) (index int, opened bool, err error) {
	if len(options) == 0 {
		return 0, false, errors.New("inventory repository picker requires at least one repository")
	}
	selected = clampBaseline(selected, 0, len(options)-1)
	if !BaselineTerminalInteractive() {
		return selected, false, errors.New("inventory repository picker requires terminal stdin and stdout")
	}
	stdinFD := int(os.Stdin.Fd())
	restore, err := enterBaselineRawTerminal(stdinFD, nil)
	if err != nil {
		return selected, false, fmt.Errorf("entering raw terminal mode: %w", err)
	}
	defer restore()
	return pickInventoryRepositoryIO(
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

// RenderInventoryRepositoryTableWidth renders the Inventory catalog without
// terminal control sequences. It is exported for deterministic tests.
func RenderInventoryRepositoryTableWidth(options []InventoryRepositoryOption, width int) string {
	return renderInventoryRepositoryTable(options, -1, baselineStyle{}, "\n", width)
}

func pickInventoryRepositoryIO(
	reader *bufio.Reader,
	writer io.Writer,
	options []InventoryRepositoryOption,
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
		frame := renderInventoryRepositoryTable(
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

type inventoryRepositoryColumn struct {
	key     string
	header  string
	minimum int
	desired int
	width   int
}

func renderInventoryRepositoryTable(
	options []InventoryRepositoryOption,
	selected int,
	style baselineStyle,
	newline string,
	width int,
) string {
	width = max(20, width)
	columns := inventoryRepositoryColumns(width)
	renderColumns := func(option *InventoryRepositoryOption) string {
		parts := make([]string, len(columns))
		for index, column := range columns {
			value := column.header
			if option != nil {
				value = inventoryRepositoryColumnValue(*option, column.key)
			}
			parts[index] = baselinePadRight(baselineFit(value, column.width), column.width)
		}
		return strings.Join(parts, "  ")
	}
	var out strings.Builder
	out.WriteString(style.bold(baselineFit("Inventory", width-1)))
	out.WriteString(newline)
	out.WriteString(style.dim(baselineFit(
		"Select a repository to explore its available security context.",
		width-1,
	)))
	out.WriteString(newline)
	out.WriteString(newline)
	out.WriteString(style.bold(renderColumns(nil)))
	out.WriteString(newline)
	start, end := 0, len(options)
	if selected >= 0 && len(options) > baselineRepositoryPickerMaxVisible {
		start = max(0, min(selected-baselineRepositoryPickerMaxVisible/2, len(options)-baselineRepositoryPickerMaxVisible))
		end = start + baselineRepositoryPickerMaxVisible
	}
	for index := start; index < end; index++ {
		row := renderColumns(&options[index])
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

func inventoryRepositoryColumns(terminalWidth int) []inventoryRepositoryColumn {
	full := []inventoryRepositoryColumn{
		{key: "repository", header: "Repository", minimum: 12, desired: 24},
		{key: "source", header: "Source", minimum: 6, desired: 6},
		{key: "profile", header: "Threat profile", minimum: 14, desired: 20},
		{key: "graph", header: "Security graph", minimum: 14, desired: 18},
		{key: "updated", header: "Updated", minimum: 7, desired: 16},
		{key: "assets", header: "Assets", minimum: 6, desired: 6},
		{key: "controls", header: "Controls", minimum: 8, desired: 8},
	}
	narrow := []inventoryRepositoryColumn{
		{key: "repository", header: "Repository", minimum: 12, desired: 22},
		{key: "source", header: "Source", minimum: 6, desired: 6},
		{key: "profile", header: "Threat profile", minimum: 14, desired: 18},
		{key: "graph", header: "Security graph", minimum: 14, desired: 18},
	}
	available := max(1, terminalWidth-3)
	columns := full
	if inventoryRepositoryColumnsMinimum(full) > available {
		columns = narrow
	}
	for index := range columns {
		columns[index].width = columns[index].minimum
	}
	extra := max(0, available-inventoryRepositoryColumnsMinimum(columns))
	for _, key := range []string{"repository", "profile", "graph", "updated"} {
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

func inventoryRepositoryColumnsMinimum(columns []inventoryRepositoryColumn) int {
	width := max(0, len(columns)-1) * 2
	for _, column := range columns {
		width += column.minimum
	}
	return width
}

func inventoryRepositoryColumnValue(option InventoryRepositoryOption, key string) string {
	values := map[string]string{
		"repository": option.Repository,
		"source":     option.Source,
		"profile":    option.ThreatProfile,
		"graph":      option.SecurityGraph,
		"updated":    option.Updated,
		"assets":     option.Assets,
		"controls":   option.Controls,
	}
	value := sanitizeBaselineText(values[key], false)
	if value == "" {
		return "—"
	}
	return value
}
