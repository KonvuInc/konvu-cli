package cmd

import (
	"os"
	"os/exec"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/gitbundle"
)

// repoOn makes a checkout sitting on `branch` and chdirs into it for the test.
func repoOn(t *testing.T, branch string) {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", branch},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestBranchDefaultsToTheCheckoutNotMain(t *testing.T) {
	// The bug this exists for: on a `master` repo the label said `main`, so the baseline was filed
	// under a branch no pull request has and the gate never found it.
	repoOn(t, "master")
	if got := branchOrCheckout(guardrailsRatifyCmd); got != "master" {
		t.Errorf("branch = %q, want %q", got, "master")
	}
}

func TestAnExplicitBranchStillWins(t *testing.T) {
	repoOn(t, "master")
	if err := guardrailsRatifyCmd.Flags().Set("branch", "release-2.3"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = guardrailsRatifyCmd.Flags().Set("branch", "main")
		guardrailsRatifyCmd.Flags().Lookup("branch").Changed = false
	})
	if got := branchOrCheckout(guardrailsRatifyCmd); got != "release-2.3" {
		t.Errorf("branch = %q, want the explicit value", got)
	}
}

func TestOutsideACheckoutItFallsBackToTheFlagDefault(t *testing.T) {
	// Nothing to infer from, so the documented default stands rather than an empty label.
	dir := t.TempDir()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	if got := gitbundle.CurrentBranch("."); got != "" {
		t.Fatalf("CurrentBranch = %q outside a repo, want empty", got)
	}
	if got := branchOrCheckout(guardrailsRatifyCmd); got != "main" {
		t.Errorf("branch = %q, want the flag default", got)
	}
}

func TestADetachedHeadInfersNothing(t *testing.T) {
	// `rev-parse --abbrev-ref HEAD` answers "HEAD" when detached, which is not a branch name and
	// must never be recorded as one.
	repoOn(t, "master")
	c := exec.Command("git", "checkout", "-q", "--detach")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("detach: %v %s", err, out)
	}
	if got := gitbundle.CurrentBranch("."); got != "" {
		t.Errorf("CurrentBranch = %q on a detached HEAD, want empty", got)
	}
}
