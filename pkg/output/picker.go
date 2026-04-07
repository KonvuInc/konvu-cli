package output

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// Pick presents an interactive picker. Falls back to numbered prompt for non-TTY.
func Pick(title string, options []string, defaultIdx int) int {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return FallbackPick(title, options, defaultIdx, os.Stdin)
	}
	idx, err := interactivePick(title, options, defaultIdx)
	if err != nil {
		return FallbackPick(title, options, defaultIdx, os.Stdin)
	}
	return idx
}

func interactivePick(title string, options []string, defaultIdx int) (int, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return 0, err
	}
	defer term.Restore(fd, oldState)

	selected := defaultIdx
	buf := make([]byte, 3)

	renderPicker(title, options, selected)

	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return selected, err
		}

		switch {
		case n == 1 && (buf[0] == '\r' || buf[0] == '\n'):
			fmt.Fprint(os.Stderr, "\r\n")
			return selected, nil
		case n == 1 && buf[0] == 3:
			fmt.Fprint(os.Stderr, "\r\n")
			return -1, fmt.Errorf("cancelled")
		case n == 3 && buf[0] == 27 && buf[1] == '[':
			if buf[2] == 'A' {
				selected = (selected - 1 + len(options)) % len(options)
			} else if buf[2] == 'B' {
				selected = (selected + 1) % len(options)
			}
			for i := 0; i < len(options)+2; i++ {
				fmt.Fprint(os.Stderr, "\033[A\033[2K")
			}
			renderPicker(title, options, selected)
		}
	}
}

func renderPicker(title string, options []string, selected int) {
	fmt.Fprintf(os.Stderr, "  %s\r\n\r\n", title)
	for i, opt := range options {
		if i == selected {
			fmt.Fprintf(os.Stderr, "  \033[1;36m❯\033[0m \033[1m%s\033[0m\r\n", opt)
		} else {
			fmt.Fprintf(os.Stderr, "    \033[2m%s\033[0m\r\n", opt)
		}
	}
}

// Confirm asks a yes/no question. Returns true for yes. defaultYes controls the default on Enter.
func Confirm(prompt string, defaultYes bool) bool {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	fmt.Fprintf(os.Stderr, "%s %s ", prompt, hint)

	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		text := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if text == "" {
			return defaultYes
		}
		return text == "y" || text == "yes"
	}
	return defaultYes
}

// FallbackPick is a numbered prompt fallback for non-TTY or when interactive fails.
func FallbackPick(title string, options []string, defaultIdx int, reader io.Reader) int {
	fmt.Fprintf(os.Stderr, "\n%s\n\n", title)
	for i, opt := range options {
		fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, opt)
	}
	fmt.Fprintf(os.Stderr, "\nEnter choice [%d]: ", defaultIdx+1)

	scanner := bufio.NewScanner(reader)
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			return defaultIdx
		}
		if idx, err := strconv.Atoi(text); err == nil && idx >= 1 && idx <= len(options) {
			return idx - 1
		}
	}
	return defaultIdx
}
