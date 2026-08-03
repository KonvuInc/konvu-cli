package cmd

import (
	"os"
	"os/exec"
	"testing"
)

// newRepoOn builds a clone-like repository: `origin` as its remote, `dflt` recorded as the
// default branch the way `git clone` records it in refs/remotes/origin/HEAD, checked out on `on`.
// Both are reproduced rather than mocked because the point of these tests is that a real checkout
// carrying a real default-branch ref does not get read.
func newRepoOn(t *testing.T, origin, dflt, on string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", dflt)
	run("remote", "add", "origin", origin)
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x")
	run("update-ref", "refs/remotes/origin/"+dflt, "HEAD")
	run("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/"+dflt)
	if on != dflt {
		run("checkout", "-q", "-b", on)
	}
	return dir
}

// inDir runs the rest of the test from dir, since the commands read the checkout they are in.
func inDir(t *testing.T, dir string) {
	t.Helper()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func clearBranchFlag(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { _ = guardrailsRatifyCmd.Flags().Set("branch", "") })
}

func TestAnExplicitBranchIsHonoured(t *testing.T) {
	inDir(t, newRepoOn(t, "git@github.com:acme/web.git", "master", "master"))
	if err := guardrailsRatifyCmd.Flags().Set("branch", "release-2.3"); err != nil {
		t.Fatal(err)
	}
	clearBranchFlag(t)

	if got := requestedBranch(guardrailsRatifyCmd); got != "release-2.3" {
		t.Errorf("branch = %q, want the explicit value", got)
	}
}

func TestTheCheckoutIsNeverReadForABranch(t *testing.T) {
	// The checkout records a default branch, in exactly the ref a clone writes, and the repository
	// being named IS this one -- every reason a client-side read would have fired. It must still
	// say nothing, because that ref is a cache with no invalidation: a plain `git fetch` after the
	// remote's default is renamed leaves the old name in it, and sending a name is what makes it
	// authoritative. A stale read addresses a branch no pull request targets, which is the silent
	// miss this whole change exists to prevent.
	inDir(t, newRepoOn(t, "git@github.com:acme/web.git", "master", "feature/add-export"))
	clearBranchFlag(t)

	if got := requestedBranch(guardrailsRatifyCmd); got != "" {
		t.Errorf("branch = %q, want empty so the server resolves the current default", got)
	}
}

func TestAnUnknownBranchIsOmittedFromTheRequest(t *testing.T) {
	// The half that matters on the wire: "" must travel as an absent field. Sending branch=main
	// looks identical to a deliberate choice, and the server stops resolving.
	if got := branchParam(""); len(got) != 0 {
		t.Errorf("branchParam(\"\") = %v, want an empty map", got)
	}
	if got := branchParam("master"); got["branch"] != "master" {
		t.Errorf("branchParam(master) = %v, want the branch carried", got)
	}
}
