package cmd

import (
	"os"
	"os/exec"
	"testing"
)

// checkoutOf makes a clone-like repository for `origin`, whose default branch is `dflt`, sitting
// on `on`, and chdirs into it. `git clone` records the default branch in refs/remotes/origin/HEAD;
// this reproduces that rather than mocking it, because that ref is the whole mechanism.
func checkoutOf(t *testing.T, origin, dflt, on string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
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
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	return dir
}

func clearBranchFlag(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { _ = guardrailsRatifyCmd.Flags().Set("branch", "") })
}

func TestOnAFeatureBranchItStillMeansTheDefaultBranch(t *testing.T) {
	// The scenario that was broken: a developer on a feature branch asked about their own repo and
	// was told "no such baseline", because the tool searched the branch they happened to be on.
	// A baseline describes what pull requests are measured against, which is the default branch.
	checkoutOf(t, "git@github.com:acme/web.git", "master", "feature/add-export")
	clearBranchFlag(t)

	if got := resolveBranch(guardrailsRatifyCmd, "acme/web", "."); got != "master" {
		t.Errorf("branch = %q, want master", got)
	}
}

func TestAnExplicitBranchStillWins(t *testing.T) {
	checkoutOf(t, "git@github.com:acme/web.git", "master", "master")
	if err := guardrailsRatifyCmd.Flags().Set("branch", "release-2.3"); err != nil {
		t.Fatal(err)
	}
	clearBranchFlag(t)

	if got := resolveBranch(guardrailsRatifyCmd, "acme/web", "."); got != "release-2.3" {
		t.Errorf("branch = %q, want the explicit value", got)
	}
}

func TestAnUnrelatedCheckoutDoesNotLendItsDefaultBranch(t *testing.T) {
	// Standing in one repository and naming another: an unguarded read would hand over a branch
	// belonging to neither, and it addresses a real baseline, so it lands on the wrong one.
	checkoutOf(t, "git@github.com:acme/web.git", "trunk", "trunk")
	clearBranchFlag(t)

	// Empty, not "main": the CLI says nothing and the server resolves the repository's default.
	// Guessing here would override an answer only the server can look up.
	if got := resolveBranch(guardrailsRatifyCmd, "AcmeKonvu/pygoat", "."); got != "" {
		t.Errorf("branch = %q, want empty so the server resolves it", got)
	}
}

func TestTheSameRepoInAnotherCaseStillResolves(t *testing.T) {
	// GitHub is case-insensitive about owner/name, so a differently-cased remote must not silently
	// stop resolving and quietly fall back to main.
	checkoutOf(t, "git@github.com:acme/web.git", "master", "master")
	clearBranchFlag(t)

	if got := resolveBranch(guardrailsRatifyCmd, "Acme/Web", "."); got != "master" {
		t.Errorf("branch = %q, want master despite the casing", got)
	}
}

func TestWithoutAKnownDefaultItSaysNothingRatherThanGuessingTheCheckout(t *testing.T) {
	// A repository with no origin/HEAD (git init, never cloned) knows no default. Answering with
	// the checked-out branch here is what would file a baseline under `feature/x`.
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "some-branch")
	run("remote", "add", "origin", "git@github.com:acme/web.git")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x")
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	clearBranchFlag(t)

	if got := resolveBranch(guardrailsRatifyCmd, "acme/web", "."); got != "" {
		t.Errorf("branch = %q, want empty rather than the checked-out branch", got)
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
