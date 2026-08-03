package output

import (
	"strings"
	"testing"
)

// truncate appended an ANSI reset unconditionally, so a plain string cut to width carried a bare
// escape that renders as "[0m" wherever stdout is not a terminal.
func TestTruncateDoesNotInventAnEscapeForPlainText(t *testing.T) {
	got := truncate("acme/some-long-repo-name", 14)
	if strings.ContainsRune(got, '\033') {
		t.Errorf("truncate = %q, want no escape sequence in plain text", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate = %q, want it to end in the ellipsis", got)
	}
}

// ...but a coloured string still has to be closed, or the colour bleeds into the next cell.
func TestTruncateStillClosesAColourItCut(t *testing.T) {
	got := truncate("\033[31macme/some-long-repo-name\033[0m", 14)
	if !strings.HasSuffix(got, "\033[0m") {
		t.Errorf("truncate = %q, want a trailing reset", got)
	}
}
