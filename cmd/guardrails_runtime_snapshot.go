package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// snapshotGuardrailsRuntime copies the executable pair into a private directory.
// Callers validate the copied bytes before allowing the runtime to own its sandbox.
func snapshotGuardrailsRuntime(binaryPath string) (string, func(), error) {
	resolved, err := filepath.EvalSymlinks(binaryPath)
	if err != nil {
		return "", func() {}, err
	}

	snapshotDir, err := os.MkdirTemp("", "konvu-guardrails-runtime-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(snapshotDir) }
	snapshotMain := filepath.Join(snapshotDir, "guardrails")
	for _, pair := range [][2]string{
		{resolved, snapshotMain},
		{
			filepath.Join(filepath.Dir(resolved), "guardrails-resource-scan"),
			filepath.Join(snapshotDir, "guardrails-resource-scan"),
		},
	} {
		if err := copyGuardrailsExecutable(pair[0], pair[1]); err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("snapshot %s: %w", pair[0], err)
		}
	}
	return snapshotMain, cleanup, nil
}

func copyGuardrailsExecutable(sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("must be an executable regular file")
	}

	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o500)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		return err
	}
	return destination.Close()
}
