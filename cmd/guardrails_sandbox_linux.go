//go:build linux

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var linuxSandboxSystemPaths = []string{
	"/usr",
	"/bin",
	"/sbin",
	"/lib",
	"/lib64",
	"/opt",
	"/etc",
	"/nix/store",
}

func platformGuardrailsSandboxCommand(
	binPath string,
	args []string,
	paths guardrailsSandboxPaths,
) (*exec.Cmd, error) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, sandboxUnavailableError(fmt.Errorf("bubblewrap (bwrap) was not found: %w", err))
	}

	commandArgs := []string{"--die-with-parent", "--new-session"}
	if paths.workDir != "/" {
		commandArgs = append(commandArgs, "--dir", paths.workDir)
	}
	for _, path := range linuxSandboxSystemPaths {
		if _, err := os.Stat(path); err == nil {
			commandArgs = append(commandArgs, "--ro-bind", path, path)
		}
	}
	for _, path := range []string{"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf"} {
		if canonical, err := filepath.EvalSymlinks(path); err == nil && canonical != path {
			commandArgs = append(commandArgs, "--ro-bind", canonical, canonical)
		}
	}
	for _, path := range paths.readOnly {
		commandArgs = append(commandArgs, "--ro-bind", path, path)
	}
	for _, path := range paths.writable {
		commandArgs = append(commandArgs, "--bind", path, path)
	}
	commandArgs = append(commandArgs,
		"--dev", "/dev",
		"--proc", "/proc",
		"--unshare-pid",
		"--unshare-ipc",
		"--unshare-uts",
		"--chdir", paths.workDir,
		"--", binPath,
	)
	commandArgs = append(commandArgs, args...)
	return exec.Command(bwrap, commandArgs...), nil
}
