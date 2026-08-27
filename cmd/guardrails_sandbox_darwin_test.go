//go:build darwin

package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinGuardrailsSandboxAllowsGitHeadMetadata(t *testing.T) {
	const sandboxExec = "/usr/bin/sandbox-exec"
	if _, err := os.Stat(sandboxExec); err != nil {
		t.Skipf("macOS sandbox is unavailable: %v", err)
	}

	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git is required for the Guardrails scan: %v", err)
	}

	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	scratch := filepath.Join(base, "scratch")
	for _, directory := range []string{repo, scratch} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.Command(git, "-C", repo, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, output)
	}
	tracked := filepath.Join(repo, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(git, "-C", repo, "add", "tracked.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, output)
	}
	commit := exec.Command(
		git,
		"-C", repo,
		"-c", "user.name=Guardrails Test",
		"-c", "user.email=guardrails@example.invalid",
		"commit", "--quiet", "-m", "fixture",
	)
	if output, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, output)
	}
	expected, err := exec.Command(git, "-C", repo, "rev-parse", "--verify", "HEAD").Output()
	if err != nil {
		t.Fatalf("read fixture HEAD: %v", err)
	}

	repo, err = canonicalPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	scratch, err = canonicalPath(scratch)
	if err != nil {
		t.Fatal(err)
	}
	child, err := platformGuardrailsSandboxCommand(
		git,
		[]string{"rev-parse", "--verify", "HEAD"},
		guardrailsSandboxPaths{
			readOnly: []string{repo},
			writable: []string{scratch},
			workDir:  repo,
		},
	)
	if err != nil {
		t.Fatalf("configure macOS sandbox: %v", err)
	}
	child.Env = []string{
		"HOME=" + scratch,
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"TMPDIR=" + scratch,
	}
	var stderr strings.Builder
	child.Stderr = &stderr
	output, err := child.Output()
	if err != nil {
		t.Fatalf("git rev-parse failed inside macOS sandbox: %v\n%s", err, stderr.String())
	}
	if got, want := strings.TrimSpace(string(output)), strings.TrimSpace(string(expected)); got != want {
		t.Fatalf("sandboxed HEAD = %q, want %q", got, want)
	}
}
