package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeHash(t *testing.T) {
	hash := ComputeEmbeddedHash()

	if hash == "" {
		t.Fatal("hash should not be empty")
	}
	if len(hash) != 64 {
		t.Fatalf("hash should be 64 hex chars, got %d: %s", len(hash), hash)
	}

	// Deterministic: calling again gives the same result.
	if h2 := ComputeEmbeddedHash(); h2 != hash {
		t.Fatalf("hash not deterministic: %s vs %s", hash, h2)
	}
}

func TestInstall(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// First install should extract files.
	n, err := Install(false)
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if n == 0 {
		t.Fatal("expected at least one file installed")
	}

	// Verify skill directories exist with expected install names.
	for _, name := range []string{"konvu-shared", "konvu-recipe-weekly-triage"} {
		dir := filepath.Join(tmpDir, ".agents", "skills", name)
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected directory %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
		// Each skill directory should contain a SKILL.md.
		skillFile := filepath.Join(dir, "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			t.Fatalf("expected SKILL.md in %s: %v", dir, err)
		}
	}

	// Version file should exist.
	versionFile := filepath.Join(tmpDir, ".agents", "skills", ".konvu-skills-version")
	data, err := os.ReadFile(versionFile)
	if err != nil {
		t.Fatalf("expected version file: %v", err)
	}
	if string(data) != ComputeEmbeddedHash() {
		t.Fatalf("version file mismatch: got %s, want %s", string(data), ComputeEmbeddedHash())
	}

	// NeedsUpdate should be false now.
	if NeedsUpdate() {
		t.Fatal("NeedsUpdate should be false after install")
	}

	// Second install (no force) should be a no-op.
	n2, err := Install(false)
	if err != nil {
		t.Fatalf("second Install failed: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("expected 0 files on no-op install, got %d", n2)
	}

	// Force install should re-extract.
	n3, err := Install(true)
	if err != nil {
		t.Fatalf("force Install failed: %v", err)
	}
	if n3 == 0 {
		t.Fatal("expected files on force install")
	}
}
