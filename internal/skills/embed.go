package skills

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Skill directories are copied from skills/ at the repo root.
// Keep in sync: cp -R ../../skills/konvu-shared ../../skills/recipe-weekly-triage .
//
//go:embed konvu-shared recipe-weekly-triage
var embedded embed.FS

// SkillDir pairs an embed directory name with its install directory name.
type SkillDir struct {
	EmbedName   string
	InstallName string
}

// skillDirs lists the skills to embed and their install names.
var skillDirs = []SkillDir{
	{EmbedName: "konvu-shared", InstallName: "konvu-shared"},
	{EmbedName: "recipe-weekly-triage", InstallName: "konvu-recipe-weekly-triage"},
}

// SkillDirs returns the list of skill directories that are embedded.
func SkillDirs() []SkillDir {
	return skillDirs
}

var (
	embeddedHashOnce sync.Once
	embeddedHash     string
)

// ComputeEmbeddedHash returns a deterministic SHA-256 hex digest of all
// embedded skill files. Files are sorted by path to ensure determinism.
// The result is cached after the first call.
func ComputeEmbeddedHash() string {
	embeddedHashOnce.Do(func() {
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

		embeddedHash = fmt.Sprintf("%x", h.Sum(nil))
	})
	return embeddedHash
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
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".agents", "skills", versionFileName))
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

	hash := ComputeEmbeddedHash()

	count := 0
	for _, sd := range skillDirs {
		destDir := filepath.Join(dir, sd.InstallName)

		// Remove old version if present.
		if err := os.RemoveAll(destDir); err != nil {
			return count, fmt.Errorf("removing old %s: %w", sd.InstallName, err)
		}

		err := fs.WalkDir(embedded, sd.EmbedName, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Compute relative path within the embed dir, then map to install dir.
			rel, err := filepath.Rel(sd.EmbedName, path)
			if err != nil {
				return fmt.Errorf("computing relative path for %s: %w", path, err)
			}
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
			return count, fmt.Errorf("extracting %s: %w", sd.EmbedName, err)
		}
	}

	// Write version file.
	versionPath := filepath.Join(dir, versionFileName)
	if err := os.WriteFile(versionPath, []byte(hash), 0o644); err != nil {
		return count, fmt.Errorf("writing version file: %w", err)
	}

	return count, nil
}
