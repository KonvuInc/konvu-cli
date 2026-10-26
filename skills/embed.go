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

//go:embed konvu-shared recipe-weekly-triage guardrails-onboarding konvu-onboarding konvu-invariants konvu-upload-invariants konvu-decide-suggestions konvu-audit-rules konvu-review-pr-checks
var embedded embed.FS

// SkillDir pairs an embed directory name with its install directory name, and carries what the
// login offer says about it.
type SkillDir struct {
	EmbedName   string
	InstallName string
	// Support marks reference material a user never invokes, so the offer does not name it.
	// Everything else must carry a Pitch: a skill installed without being mentioned is one the
	// user was asked to accept sight unseen.
	Support bool
	// Pitch is the offer's lines for this skill, printed as given.
	Pitch []string
}

// skillDirs lists the skills to embed and their install names.
var skillDirs = []SkillDir{
	{EmbedName: "konvu-shared", InstallName: "konvu-shared", Support: true},
	{
		EmbedName:   "recipe-weekly-triage",
		InstallName: "konvu-recipe-weekly-triage",
		Pitch: []string{
			"  Weekly Triage — guided review of your security findings with inline",
			"  rating, bulk dismiss, and ticket creation. Run it in Claude Code",
			"  with: /konvu-recipe-weekly-triage",
		},
	},
	{
		EmbedName:   "guardrails-onboarding",
		InstallName: "konvu-guardrails-onboarding",
		Pitch: []string{
			"  Security Context Graphs — map a codebase and explore its Assets, Controls,",
			"  and Implementations across historical runs. Run it in Claude Code with:",
			"  /konvu-guardrails-onboarding, or ask it to explore a Security Context Graph.",
		},
	},
	{
		EmbedName:   "konvu-onboarding",
		InstallName: "konvu-onboarding",
		Pitch: []string{
			"  Konvu onboarding — write your first security rule with your agent and see",
			"  how Konvu enforces it. Run it in Claude Code with: /konvu-onboarding",
		},
	},
	{
		EmbedName:   "konvu-invariants",
		InstallName: "konvu-invariants",
		Pitch: []string{
			"  Security rules — pick a repo and a theme, draft the rule that matters most,",
			"  shape it together. Run it with: /konvu-invariants",
		},
	},
	{
		EmbedName:   "konvu-upload-invariants",
		InstallName: "konvu-upload-invariants",
		Pitch: []string{
			"  Save security rules you wrote to Konvu. Run it with: /konvu-upload-invariants",
		},
	},
	{
		EmbedName:   "konvu-decide-suggestions",
		InstallName: "konvu-decide-suggestions",
		Pitch: []string{
			"  Decide on the security rules Konvu suggested: checked against your code,",
			"  then accepted or declined as you pick. Run it with: /konvu-decide-suggestions",
		},
	},
	{
		EmbedName:   "konvu-audit-rules",
		InstallName: "konvu-audit-rules",
		Pitch: []string{
			"  Audit how your code enforces your active security rules, and where it",
			"  doesn't (read-only). Run it with: /konvu-audit-rules",
		},
	},
	{
		EmbedName:   "konvu-review-pr-checks",
		InstallName: "konvu-review-pr-checks",
		Pitch: []string{
			"  Review what Konvu's PR check found on your pull requests, or on one PR",
			"  (read-only). Run it with: /konvu-review-pr-checks",
		},
	},
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

// installDirs returns the target directories for skill installation.
// Claude Code reads from ~/.claude/skills/ (primary).
// ~/.agents/skills/ is the cross-agent convention (Cursor, OpenCode, etc.).
func installDirs() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return []string{
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}, nil
}

// InstallDir returns the primary skills directory (~/.claude/skills).
func InstallDir() (string, error) {
	dirs, err := installDirs()
	if err != nil {
		return "", err
	}
	return dirs[0], nil
}

const versionFileName = ".konvu-skills-version"

// InstalledHash reads the installed version hash from the primary directory,
// or returns "" if not found.
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

// Install extracts embedded skills to both ~/.claude/skills/ and ~/.agents/skills/.
// If force is false and the installed hash matches, it returns 0 files.
// Returns the number of skill directories written (per target).
func Install(force bool) (int, error) {
	if !force && !NeedsUpdate() {
		return 0, nil
	}

	dirs, err := installDirs()
	if err != nil {
		return 0, err
	}

	hash := ComputeEmbeddedHash()
	count := 0

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return count, fmt.Errorf("cannot create %s: %w", dir, err)
		}

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

				return os.WriteFile(dest, data, 0o644)
			})
			if err != nil {
				return count, fmt.Errorf("extracting %s to %s: %w", sd.EmbedName, dir, err)
			}
		}

		// Write version file per target directory.
		versionPath := filepath.Join(dir, versionFileName)
		if err := os.WriteFile(versionPath, []byte(hash), 0o644); err != nil {
			return count, fmt.Errorf("writing version file in %s: %w", dir, err)
		}

		count += len(skillDirs)
	}

	return count, nil
}
