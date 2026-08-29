//go:build guardrails_dev

package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveGuardrailsBinaryUsesCompleteExecutablePair(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"guardrails", "guardrails-resource-scan"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("runtime"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("KONVU_GUARDRAILS_DEV_DIR", dir)
	path, err := resolveGuardrailsBinary()
	if err != nil {
		t.Fatal(err)
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(resolvedDir, "guardrails") {
		t.Fatalf("resolved path = %q", path)
	}
}

func TestResolveGuardrailsBinaryRejectsIncompletePair(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "guardrails"), []byte("runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KONVU_GUARDRAILS_DEV_DIR", dir)
	if _, err := resolveGuardrailsBinary(); err == nil {
		t.Fatal("incomplete local runtime was accepted")
	}
}

func TestDevelopmentRuntimeOwnsItsSandbox(t *testing.T) {
	root := t.TempDir()
	buildDir := filepath.Join(root, "build")
	currentDir := filepath.Join(root, "current")
	for _, dir := range []string{buildDir, currentDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	buildBinary := filepath.Join(buildDir, "guardrails")
	binaryPath := filepath.Join(currentDir, "guardrails")
	if err := os.WriteFile(buildBinary, []byte("runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(buildDir, "guardrails-resource-scan"),
		[]byte("scanner"),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(buildBinary, binaryPath); err != nil {
		t.Fatal(err)
	}
	prepared, ownsSandbox, cleanup, err := prepareGuardrailsRuntime(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if prepared != binaryPath || ownsSandbox {
		t.Fatal("runtime without a capability marker claimed sandbox ownership")
	}
	if err := os.WriteFile(
		filepath.Join(currentDir, guardrailsAgentSandboxCapability),
		[]byte(guardrailsAgentSandboxCapability+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	prepared, ownsSandbox, cleanup, err = prepareGuardrailsRuntime(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if prepared != binaryPath || ownsSandbox {
		t.Fatal("marker beside a runtime symlink claimed sandbox ownership")
	}
	marker, err := guardrailsSandboxCapabilityMarker(buildBinary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(buildDir, guardrailsAgentSandboxCapability),
		[]byte(marker+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	prepared, ownsSandbox, cleanup, err = prepareGuardrailsRuntime(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !ownsSandbox || prepared == binaryPath {
		t.Fatal("runtime with a capability marker did not claim sandbox ownership")
	}
	preparedDir := filepath.Dir(prepared)
	preparedData, err := os.ReadFile(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildBinary, []byte("replaced runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	if string(preparedData) != "runtime" {
		t.Fatalf("prepared runtime = %q", preparedData)
	}
	preparedData, err = os.ReadFile(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if string(preparedData) != "runtime" {
		t.Fatal("prepared runtime changed after the source executable was replaced")
	}
	cleanup()
	if _, err := os.Stat(preparedDir); !os.IsNotExist(err) {
		t.Fatalf("prepared runtime directory survived cleanup: %v", err)
	}

	prepared, ownsSandbox, cleanup, err = prepareGuardrailsRuntime(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if prepared != binaryPath || ownsSandbox {
		t.Fatal("replacement runtime claimed sandbox ownership using a stale marker")
	}
}
