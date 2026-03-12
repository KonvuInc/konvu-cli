package skills

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Skill directories are copied from skills/ at the repo root.
// Keep in sync: cp -R ../../skills/konvu-shared ../../skills/recipe-weekly-triage .
//
//go:embed konvu-shared recipe-weekly-triage
var embedded embed.FS

// SkillDirs maps embed directory names to their install directory names.
var SkillDirs = map[string]string{
	"konvu-shared":         "konvu-shared",
	"recipe-weekly-triage": "konvu-recipe-weekly-triage",
}

// ComputeEmbeddedHash returns a deterministic SHA-256 hex digest of all
// embedded skill files. Files are sorted by path to ensure determinism.
func ComputeEmbeddedHash() string {
	h := sha256.New()

	var paths []string
	fs.WalkDir(embedded, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		paths = append(paths, path)
		return nil
	})

	sort.Strings(paths)

	for _, p := range paths {
		data, err := embedded.ReadFile(p)
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s\n%d\n", p, len(data))
		h.Write(data)
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// InstallDir returns the path to ~/.agents/skills, creating it if needed.
func InstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create skills directory: %w", err)
	}
	return dir, nil
}

const versionFileName = ".konvu-skills-version"

// InstalledHash reads the installed version hash, or returns "" if not found.
func InstalledHash() string {
	dir, err := InstallDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, versionFileName))
	if err != nil {
		return ""
	}
	return string(data)
}

// NeedsUpdate returns true when the embedded skills differ from what is installed.
func NeedsUpdate() bool {
	return InstalledHash() != ComputeEmbeddedHash()
}

// Install extracts embedded skills to ~/.agents/skills/konvu-*.
// If force is false and the installed hash matches, it returns 0 files.
// Returns the number of files written.
func Install(force bool) (int, error) {
	if !force && !NeedsUpdate() {
		return 0, nil
	}

	dir, err := InstallDir()
	if err != nil {
		return 0, err
	}

	count := 0
	for embedName, installName := range SkillDirs {
		destDir := filepath.Join(dir, installName)

		// Remove old version if present.
		os.RemoveAll(destDir)

		err := fs.WalkDir(embedded, embedName, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Compute relative path within the embed dir, then map to install dir.
			rel, _ := filepath.Rel(embedName, path)
			dest := filepath.Join(destDir, rel)

			if d.IsDir() {
				return os.MkdirAll(dest, 0o755)
			}

			data, err := embedded.ReadFile(path)
			if err != nil {
				return err
			}

			if err := os.WriteFile(dest, data, 0o644); err != nil {
				return err
			}
			count++
			return nil
		})
		if err != nil {
			return count, fmt.Errorf("extracting %s: %w", embedName, err)
		}
	}

	// Write version file.
	versionPath := filepath.Join(dir, versionFileName)
	if err := os.WriteFile(versionPath, []byte(ComputeEmbeddedHash()), 0o644); err != nil {
		return count, fmt.Errorf("writing version file: %w", err)
	}

	return count, nil
}
