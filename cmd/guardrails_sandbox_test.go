package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestGuardrailsRepoArgument(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"baseline", "scan", "/repo"}, "/repo"},
		{[]string{"baseline", "scan"}, ""},
		{[]string{"scan", "/repo"}, ""},
		{[]string{"baseline", "list"}, ""},
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
		[]string{"baseline", "scan", repo},
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
	workDir, err := canonicalPath(".")
	if err != nil {
		t.Fatal(err)
	}
	if workDir != repo && containsPath(paths.readOnly, workDir) {
		t.Errorf("unrelated working directory is readable: %v", paths.readOnly)
	}
	store, err := canonicalPath(filepath.Join(home, ".konvu", "guardrails"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(paths.writable, store) {
		t.Errorf("writable paths do not include baseline store %s: %v", store, paths.writable)
	}

	cleanup()
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("private temp directory still exists after cleanup: %v", err)
	}
}

func TestGuardrailsSandboxCanonicalizesCodebaseArgument(t *testing.T) {
	base := t.TempDir()
	working := filepath.Join(base, "work", "current")
	nested := filepath.Join(working, "nested", "repo")
	parent := filepath.Join(base, "work", "parent")
	for _, directory := range []string{working, nested, parent} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "nested", path: filepath.Join("nested", "repo"), want: nested},
		{name: "parent", path: filepath.Join("..", "parent"), want: parent},
	}

	link := filepath.Join(working, "linked-repo")
	if err := os.Symlink(nested, link); err == nil {
		tests = append(tests, struct {
			name string
			path string
			want string
		}{name: "symlink", path: "linked-repo", want: nested})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, err := canonicalPathFrom(working, test.path)
			if err != nil {
				t.Fatal(err)
			}
			want, err := canonicalPath(test.want)
			if err != nil {
				t.Fatal(err)
			}
			if root != want {
				t.Fatalf("canonical root = %q, want %q", root, want)
			}
			got := guardrailsSandboxArguments(
				[]string{"baseline", "scan", test.path, "--yes"},
				root,
			)
			if got[2] != want {
				t.Fatalf("sandbox args = %v, want canonical codebase %q", got, want)
			}
		})
	}
}

func TestPrepareGuardrailsSandboxOnlyMakesGlobalStoreWritable(t *testing.T) {
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
		[]string{"baseline", "scan", repo},
		[]string{"HOME=" + home, "PATH=/usr/bin"},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	root, _ := canonicalPath(repo)
	store, _ := canonicalPath(filepath.Join(home, ".konvu", "guardrails"))
	credentials, _ = canonicalPath(credentials)
	if !containsPath(paths.readOnly, root) {
		t.Errorf("repo is not readable: %v", paths.readOnly)
	}
	if containsPath(paths.writable, root) {
		t.Errorf("whole repo is writable: %v", paths.writable)
	}
	if !containsPath(paths.writable, store) {
		t.Errorf("global baseline store is not writable: %v", paths.writable)
	}
	if _, err := os.Stat(filepath.Join(repo, ".konvu")); !os.IsNotExist(err) {
		t.Errorf("scan created a repository-local artifact directory: %v", err)
	}
	if !containsPath(paths.readOnly, credentials) {
		t.Errorf("existing Guardrails credentials are not readable: %v", paths.readOnly)
	}
}

func TestPrepareGuardrailsSandboxRejectsSymlinkedBaselineStore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Guardrails is not shipped for Windows")
	}
	for _, symlinkParent := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent=%v", symlinkParent), func(t *testing.T) {
			repo := t.TempDir()
			outside := t.TempDir()
			home := t.TempDir()
			binDir := t.TempDir()
			binPath := filepath.Join(binDir, "guardrails")
			if err := os.WriteFile(binPath, []byte("fixture"), 0o755); err != nil {
				t.Fatal(err)
			}

			link := filepath.Join(home, ".konvu")
			if !symlinkParent {
				if err := os.Mkdir(link, 0o755); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(link, "guardrails")
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}

			_, _, cleanup, err := prepareGuardrailsSandbox(
				binPath,
				[]string{"baseline", "scan", repo},
				[]string{"HOME=" + home, "PATH=/usr/bin"},
			)
			cleanup()
			if err == nil || !strings.Contains(err.Error(), "symlinked baseline store") {
				t.Fatalf("error = %v, want symlink rejection", err)
			}
		})
	}
}

func TestPrepareGuardrailsStoreRootSupportsConcurrentFirstUse(t *testing.T) {
	home := t.TempDir()
	const workers = 32
	start := make(chan struct{})
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := prepareGuardrailsStoreRoot([]string{"HOME=" + home})
			errors <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent store initialization failed: %v", err)
		}
	}
	root := filepath.Join(home, ".konvu", "guardrails")
	if info, err := os.Lstat(root); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("store root = %#v, error = %v", info, err)
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
