package gitbundle

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo makes a throwaway checkout with one commit.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "config", "user.email", "t@example.com")
	run(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", "a.txt")
	run(t, dir, "commit", "-q", "-m", "one")
	return dir
}

func TestHeadResolvesASha(t *testing.T) {
	dir := repo(t)
	sha, err := Head(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) < 40 {
		t.Errorf("Head = %q, want a full sha", sha)
	}
}

func TestCreateBundlesTheRefsTheServerReadsBack(t *testing.T) {
	dir := repo(t)
	sha, _ := Head(dir, "HEAD")

	path, cleanup, err := Create(dir, sha)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("bundle not written: %v", err)
	}
	if st.Size() == 0 {
		t.Error("bundle is empty")
	}

	// The server clones the bundle by these exact ref names, so a rename here is a
	// wire-contract break rather than a local detail.
	heads := run(t, dir, "bundle", "list-heads", path)
	for _, ref := range []string{refBase, refHead} {
		if !strings.Contains(heads, ref) {
			t.Errorf("bundle is missing %s\n%s", ref, heads)
		}
	}
	if !strings.Contains(heads, sha) {
		t.Errorf("bundle does not carry HEAD %s\n%s", sha, heads)
	}
}

func TestCleanupRemovesTheStagedRefsAndTheFile(t *testing.T) {
	dir := repo(t)
	sha, _ := Head(dir, "HEAD")

	path, cleanup, err := Create(dir, sha)
	if err != nil {
		t.Fatal(err)
	}
	if out := run(t, dir, "for-each-ref", "--format=%(refname)", "refs/authzprover/"); !strings.Contains(out, refHead) {
		t.Fatalf("refs were not staged: %q", out)
	}

	cleanup()

	if out := run(t, dir, "for-each-ref", "--format=%(refname)", "refs/authzprover/"); out != "" {
		t.Errorf("refs left behind after cleanup: %q", out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("bundle file left behind: %v", err)
	}
}

func TestRepoSlugFromRemote(t *testing.T) {
	for _, tc := range []struct{ remote, want string }{
		{"https://github.com/acme/web.git", "acme/web"},
		{"https://github.com/acme/web", "acme/web"},
		{"git@github.com:acme/web.git", "acme/web"},
		{"git@github.com:acme/web", "acme/web"},
		{"ssh://git@github.com/acme/web.git", "acme/web"},
		{"https://github.com/acme/web/", "acme/web"},
	} {
		dir := repo(t)
		run(t, dir, "remote", "add", "origin", tc.remote)
		if got := RepoSlug(dir); got != tc.want {
			t.Errorf("RepoSlug(%q) = %q, want %q", tc.remote, got, tc.want)
		}
	}
}

func TestRepoSlugFallsBackToTheDirectoryName(t *testing.T) {
	dir := repo(t)
	// No origin configured, so the slug is the best guess available rather than an error.
	if got := RepoSlug(dir); got != filepath.Base(dir) {
		t.Errorf("RepoSlug = %q, want %q", got, filepath.Base(dir))
	}
}
