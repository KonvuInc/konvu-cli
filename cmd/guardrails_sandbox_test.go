package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestGuardrailsRepoArgument(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"scan"}, "."},
		{[]string{"scan", "/repo"}, "/repo"},
		{[]string{"baseline", "prepare", "/repo"}, "/repo"},
		{[]string{"baseline", "continue", "/repo"}, "/repo"},
		{[]string{"show"}, ""},
	}
	for _, test := range tests {
		if got := guardrailsRepoArgument(test.args); got != test.want {
			t.Errorf("guardrailsRepoArgument(%v) = %q, want %q", test.args, got, test.want)
		}
	}
}

func TestPrepareGuardrailsSandboxUsesPrivateTempAndNarrowPaths(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	repo := filepath.Join(base, "repo")
	binDir := filepath.Join(home, ".config", "guardrails", "bin", "v-test")
	for _, dir := range []string{home, repo, binDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	binPath := filepath.Join(binDir, "guardrails")
	if err := os.WriteFile(binPath, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}

	paths, env, cleanup, err := prepareGuardrailsSandbox(
		binPath,
		[]string{"baseline", "prepare", repo},
		[]string{"HOME=" + home, "PATH=/usr/bin", "TMPDIR=/shared/tmp"},
	)
	if err != nil {
		t.Fatal(err)
	}
	tempDir := environmentValue(env, "TMPDIR")
	defer cleanup()

	if tempDir == "" || tempDir == "/shared/tmp" {
		t.Fatalf("TMPDIR = %q, want a private directory", tempDir)
	}
	if environmentValue(env, "TMP") != tempDir || environmentValue(env, "TEMP") != tempDir {
		t.Fatalf("temporary environment is inconsistent: %v", env)
	}
	for _, path := range []string{repo, binDir} {
		canonical, err := canonicalPath(path)
		if err != nil {
			t.Fatal(err)
		}
		if !containsPath(paths.readOnly, canonical) {
			t.Errorf("read-only paths do not include %s: %v", canonical, paths.readOnly)
		}
	}
	cache, err := canonicalPath(filepath.Join(home, ".cache", "guardrails"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(paths.writable, cache) {
		t.Errorf("writable paths do not include cache %s: %v", cache, paths.writable)
	}

	cleanup()
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("private temp directory still exists after cleanup: %v", err)
	}
}

func TestPrepareGuardrailsSandboxOnlyMakesScanOutputWritable(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	binDir := t.TempDir()
	credentials := filepath.Join(home, ".config", "guardrails", "credentials")
	if err := os.MkdirAll(filepath.Dir(credentials), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentials, []byte("key = fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(binDir, "guardrails")
	if err := os.WriteFile(binPath, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}

	paths, _, cleanup, err := prepareGuardrailsSandbox(
		binPath,
		[]string{"scan", repo},
		[]string{"HOME=" + home, "PATH=/usr/bin"},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	root, _ := canonicalPath(repo)
	output, _ := canonicalPath(filepath.Join(repo, ".konvu", "guardrails"))
	credentials, _ = canonicalPath(credentials)
	if !containsPath(paths.readOnly, root) {
		t.Errorf("repo is not readable: %v", paths.readOnly)
	}
	if containsPath(paths.writable, root) {
		t.Errorf("whole repo is writable: %v", paths.writable)
	}
	if !containsPath(paths.writable, output) {
		t.Errorf("scan output is not writable: %v", paths.writable)
	}
	if !containsPath(paths.readOnly, credentials) {
		t.Errorf("existing Guardrails credentials are not readable: %v", paths.readOnly)
	}
}

func TestPlatformGuardrailsSandboxConfinesFiles(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Guardrails is not shipped for this platform")
	}
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("bwrap"); err != nil {
			t.Skip("bubblewrap is not installed")
		}
	}

	base := t.TempDir()
	root := filepath.Join(base, "repo")
	scratch := filepath.Join(root, ".konvu", "guardrails")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{root, scratch, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret-value"), 0o600); err != nil {
		t.Fatal(err)
	}

	root, _ = canonicalPath(root)
	scratch, _ = canonicalPath(scratch)
	outside, _ = canonicalPath(outside)
	paths := guardrailsSandboxPaths{
		readOnly: []string{root},
		writable: []string{scratch},
		workDir:  root,
	}
	script := fmt.Sprintf(
		"cat %s > %s; if cat %s; then exit 91; fi; if printf bad > %s; then exit 92; fi",
		strconv.Quote(filepath.Join(root, "inside.txt")),
		strconv.Quote(filepath.Join(scratch, "copied.txt")),
		strconv.Quote(filepath.Join(outside, "secret.txt")),
		strconv.Quote(filepath.Join(outside, "escaped.txt")),
	)
	child, err := platformGuardrailsSandboxCommand("/bin/sh", []string{"-c", script}, paths)
	if err != nil {
		t.Fatal(err)
	}
	child.Env = []string{"HOME=" + scratch, "PATH=/usr/bin:/bin", "TMPDIR=" + scratch}
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox command failed: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "secret-value") {
		t.Fatalf("sandbox read the outside secret: %s", output)
	}
	if copied, err := os.ReadFile(filepath.Join(scratch, "copied.txt")); err != nil || string(copied) != "inside" {
		t.Fatalf("allowed read/write failed: %q, %v\n%s", copied, err, output)
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("sandbox wrote outside its writable paths: %v", err)
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
