//go:build !darwin && !linux

package cmd

import (
	"errors"
	"os/exec"
)

func platformGuardrailsSandboxCommand(
	_ string,
	_ []string,
	_ guardrailsSandboxPaths,
) (*exec.Cmd, error) {
	return nil, sandboxUnavailableError(errors.New("unsupported platform"))
}
