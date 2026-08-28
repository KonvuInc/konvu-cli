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
	if !guardrailsRuntimeOwnsSandbox() {
		t.Fatal("development runtime must own its agent sandbox")
	}
}
