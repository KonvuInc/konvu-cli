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

// BrowseInventoryRepositoryDetail presents a read-only repository detail view
// with the same navigation conventions as the Security Context Graph workspace.
func BrowseInventoryRepositoryDetail(content string) (BaselineWorkspaceOutcome, error) {
	return browseTextDetail(content, "repositories")
}

// BrowseFindingDetail presents a read-only finding detail view using the same
// scrolling and back-navigation conventions as Inventory.
func BrowseFindingDetail(content string) (BaselineWorkspaceOutcome, error) {
	return browseTextDetail(content, "findings")
}

func browseTextDetail(content, backLabel string) (BaselineWorkspaceOutcome, error) {
	if !BaselineTerminalInteractive() {
		return BaselineWorkspaceQuit, errors.New("detail browser requires terminal stdin and stdout")
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

	rawLines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	reader := bufio.NewReader(os.Stdin)
	scroll := 0
	for {
		width, height, sizeErr := term.GetSize(int(os.Stdout.Fd()))
		if sizeErr != nil || width <= 0 || height <= 0 {
			width, height = 100, 24
		}
		lines := inventoryDetailWrapLines(rawLines, width)
		pageSize := max(1, height-3)
		maxScroll := max(0, len(lines)-pageSize)
		scroll = clampBaseline(scroll, 0, maxScroll)
		if err := renderInventoryRepositoryDetail(
			os.Stdout,
			lines,
			scroll,
			pageSize,
			width,
			baselineStyle{enabled: baselineColorEnabled(os.Stdout)},
			backLabel,
		); err != nil {
			return BaselineWorkspaceQuit, err
		}

		key, err := readBaselineKey(reader, func() (bool, error) {
			return baselineWaitForInput(stdinFD, baselineEscapeSequenceWait)
		})
		if err != nil {
			return BaselineWorkspaceQuit, err
		}
		switch key.kind {
		case baselineKeyUp:
			scroll = max(0, scroll-1)
		case baselineKeyDown:
			scroll = min(maxScroll, scroll+1)
		case baselineKeyPageUp:
			scroll = max(0, scroll-pageSize)
		case baselineKeyPageDown:
			scroll = min(maxScroll, scroll+pageSize)
		case baselineKeyLeft, baselineKeyEscape:
			return BaselineWorkspaceBack, nil
		case baselineKeyQuit:
			return BaselineWorkspaceQuit, nil
		case baselineKeyCancel:
			return BaselineWorkspaceCancelled, ErrBaselineCancelled
		}
	}
}

func renderInventoryRepositoryDetail(
	writer io.Writer,
	lines []string,
	scroll, pageSize, width int,
	style baselineStyle,
	backLabels ...string,
) error {
	backLabel := "repositories"
	if len(backLabels) > 0 && backLabels[0] != "" {
		backLabel = backLabels[0]
	}
	var frame strings.Builder
	frame.WriteString("\033[H\033[2J")
	end := min(len(lines), scroll+pageSize)
	for _, line := range lines[scroll:end] {
		line = baselineFit(sanitizeBaselineText(line, true), width)
		frame.WriteString(inventoryDetailStyledLine(line, style))
		frame.WriteString("\r\n")
	}
	for range max(0, pageSize-(end-scroll)) {
		frame.WriteString("\r\n")
	}
	frame.WriteString("\r\n")
	frame.WriteString(style.dim("↑↓ scroll · PgUp/PgDn page · ←/Esc " + backLabel + " · Q quit"))
	frame.WriteString("\r\n")
	_, err := io.WriteString(writer, frame.String())
	return err
}

func inventoryDetailWrapLines(lines []string, terminalWidth int) []string {
	contentWidth := min(110, max(40, terminalWidth-2))
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		line = sanitizeBaselineText(line, true)
		if visibleLen(line) <= contentWidth {
			wrapped = append(wrapped, line)
			continue
		}
		trimmed := strings.TrimLeft(line, " ")
		indent := line[:len(line)-len(trimmed)]
		continuationIndent := indent
		if strings.HasPrefix(trimmed, "• ") {
			continuationIndent += "  "
		}
		available := max(10, contentWidth-visibleLen(indent))
		parts := wordWrap(trimmed, available)
		for index, part := range parts {
			prefix := indent
			if index > 0 {
				prefix = continuationIndent
			}
			wrapped = append(wrapped, prefix+part)
		}
	}
	return wrapped
}

func inventoryDetailStyledLine(line string, style baselineStyle) string {
	trimmed := strings.TrimSpace(line)
	switch {
	case trimmed == "THREAT PROFILE · HOSTED":
		return style.cyan(line)
	case trimmed == "SCA FINDING · HOSTED",
		trimmed == "SAST FINDING · HOSTED",
		trimmed == "SECRET FINDING · HOSTED",
		trimmed == "CONTAINER FINDING · HOSTED":
		return style.cyan(line)
	case trimmed == "SUMMARY",
		trimmed == "DOMAINS",
		trimmed == "SECURITY SIGNALS",
		trimmed == "SCORE FACTORS",
		trimmed == "ASSESSMENT",
		trimmed == "EVIDENCE",
		trimmed == "WHY THIS ASSESSMENT",
		trimmed == "RUNTIME REACHABILITY",
		trimmed == "VULNERABILITY",
		trimmed == "SECRET",
		trimmed == "ASSET",
		trimmed == "FINDING":
		return style.cyan(line)
	case style.enabled && strings.HasPrefix(trimmed, "Status"):
		status := strings.TrimSpace(strings.TrimPrefix(trimmed, "Status"))
		normalized := findingAssessmentStatus(status)
		if mapping.IsValidStatus(normalized) {
			prefixLength := strings.LastIndex(line, status)
			if prefixLength >= 0 {
				return line[:prefixLength] + mapping.Colorize(status, normalized)
			}
		}
		return line
	case strings.Trim(trimmed, "─") == "":
		return style.dim(line)
	case strings.HasPrefix(trimmed, "github:"),
		strings.HasPrefix(trimmed, "gitlab:"),
		strings.HasPrefix(trimmed, "http://"),
		strings.HasPrefix(trimmed, "https://"),
		strings.HasPrefix(trimmed, "Assessment"),
		strings.HasPrefix(trimmed, "Evidence"),
		strings.HasPrefix(trimmed, "•"):
		return style.dim(line)
	default:
		return line
	}
}
