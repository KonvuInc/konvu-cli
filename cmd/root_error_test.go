package cmd

import (
	"bytes"
	"fmt"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"strings"
	"testing"
)

func TestExecutionErrorGuidanceAndExitCode(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
		want string
	}{
		{fmt.Errorf("ordinary failure"), clierrors.ExitGeneralError, "ordinary failure"},
		{instructionUsage("missing repository", "Pass --repo."), clierrors.ExitUsageError, "Pass --repo."},
		{fmt.Errorf("wrapped: %w", &clierrors.CLIError{Message: "stale", Suggestion: "Read the current sha256.", ExitCode: clierrors.ExitGeneralError}), clierrors.ExitGeneralError, "Read the current sha256."},
	} {
		out := new(bytes.Buffer)
		if code := writeExecutionError(out, tc.err); code != tc.code {
			t.Errorf("code=%d want=%d", code, tc.code)
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("output=%q want=%q", out.String(), tc.want)
		}
	}
}
