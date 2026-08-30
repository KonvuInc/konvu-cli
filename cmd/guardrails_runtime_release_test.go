//go:build !guardrails_dev

package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProductionGuardrailsPinUsesBaselineV1Release(t *testing.T) {
	if guardrailsPinnedVersion != "v0.6.3" {
		t.Fatalf(
			"production Guardrails pin = %q, want v0.6.3",
			guardrailsPinnedVersion,
		)
	}
}

func TestProductionRuntimeOwnsSandboxUsingVerifiedSnapshot(t *testing.T) {
	sourceDir := t.TempDir()
	mainContents := []byte("main runtime")
	scannerContents := []byte("resource scanner")
	mainPath := filepath.Join(sourceDir, "guardrails")
	if err := os.WriteFile(mainPath, mainContents, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(sourceDir, "guardrails-resource-scan"),
		scannerContents,
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	artifact := guardrailsArtifact{
		mainSHA256:            sha256Hex(mainContents),
		resourceScannerSHA256: sha256Hex(scannerContents),
	}

	path, ownsSandbox, cleanup, err := prepareVerifiedGuardrailsRuntime(mainPath, artifact)
	if err != nil {
		t.Fatal(err)
	}
	if !ownsSandbox {
		t.Fatal("verified production runtime did not claim sandbox ownership")
	}
	if path == mainPath {
		t.Fatal("production runtime was not snapshotted")
	}
	snapshotDir := filepath.Dir(path)
	if err := os.WriteFile(mainPath, []byte("replacement"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(mainContents) {
		t.Fatalf("prepared runtime changed with source: got %q", got)
	}
	cleanup()
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Fatalf("prepared runtime directory survived cleanup: %v", err)
	}
}

func TestProductionRuntimeRejectsUnverifiedSnapshot(t *testing.T) {
	sourceDir := t.TempDir()
	mainPath := filepath.Join(sourceDir, "guardrails")
	for name, contents := range map[string][]byte{
		"guardrails":               []byte("main runtime"),
		"guardrails-resource-scan": []byte("resource scanner"),
	} {
		if err := os.WriteFile(filepath.Join(sourceDir, name), contents, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	path, ownsSandbox, cleanup, err := prepareVerifiedGuardrailsRuntime(
		mainPath,
		guardrailsArtifact{
			mainSHA256:            sha256Hex([]byte("different main")),
			resourceScannerSHA256: sha256Hex([]byte("resource scanner")),
		},
	)
	cleanup()
	if err == nil {
		t.Fatal("unverified production snapshot was accepted")
	}
	if path != "" || ownsSandbox {
		t.Fatal("unverified production snapshot claimed sandbox ownership")
	}
}
