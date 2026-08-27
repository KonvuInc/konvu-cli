//go:build darwin

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var darwinSandboxSystemPaths = []string{
	"/usr",
	"/bin",
	"/sbin",
	"/System",
	"/Library",
	"/opt",
	"/private/etc",
}

func platformGuardrailsSandboxCommand(
	binPath string,
	args []string,
	paths guardrailsSandboxPaths,
) (*exec.Cmd, error) {
	const sandboxExec = "/usr/bin/sandbox-exec"
	if _, err := os.Stat(sandboxExec); err != nil {
		return nil, sandboxUnavailableError(err)
	}

	var profile strings.Builder
	profile.WriteString("(version 1)\n(deny default)\n(allow process*)\n")
	profile.WriteString("(allow sysctl-read)\n(allow mach*)\n(allow ipc-posix*)\n(allow network*)\n")
	profile.WriteString("(allow file-read-metadata)\n(allow file-read-data (literal \"/\"))\n")
	profile.WriteString("(allow file-read* (subpath \"/dev\"))\n")
	profile.WriteString("(allow file-write-data (literal \"/dev/null\"))\n")
	for _, path := range darwinSandboxSystemPaths {
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(&profile, "(allow file-read* (subpath %s))\n", strconv.Quote(path))
		}
	}
	for _, path := range paths.readOnly {
		fmt.Fprintf(&profile, "(allow file-read* (subpath %s))\n", strconv.Quote(path))
	}
	for _, path := range paths.writable {
		fmt.Fprintf(&profile, "(allow file-read* (subpath %s))\n", strconv.Quote(path))
		fmt.Fprintf(&profile, "(allow file-write* (subpath %s))\n", strconv.Quote(path))
	}

	commandArgs := []string{"-p", profile.String(), binPath}
	commandArgs = append(commandArgs, args...)
	child := exec.Command(sandboxExec, commandArgs...)
	child.Dir = paths.workDir
	return child, nil
}
