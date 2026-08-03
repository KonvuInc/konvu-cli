// Package gitbundle packages a local checkout into a git bundle for server-side analysis.
// It shells out to the system git rather than linking a git library: the only operations
// needed are rev-parse, remote lookup and bundle create.
package gitbundle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Ref names the server checks out the bundle by. They are part of the wire contract, not
// an implementation detail, so they are fixed rather than per-invocation — two concurrent
// runs in the same checkout would race on them, which needs a protocol change to fix.
const (
	refBase = "refs/authzprover/base"
	refHead = "refs/authzprover/head"
)

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// Head resolves a ref to a full sha.
func Head(dir, ref string) (string, error) {
	return git(dir, "rev-parse", ref)
}

// There is deliberately no DefaultBranch here. A clone records its remote's default in
// refs/remotes/origin/HEAD when it is created, and nothing routinely refreshes it: after the
// remote's default branch is renamed, a plain `git fetch` leaves both that symbolic ref and the
// old remote-tracking branch in place, so reading it locally cannot tell a current default from
// one that moved months ago. Only `git fetch --prune` repairs it, and asking the remote directly
// costs a network round trip and the user's git credentials.
//
// The branch is an address, and sending one is what makes it authoritative, so a stale read would
// file or look up a baseline under a branch no pull request targets. The server already asks the
// host for the current default when the field is omitted -- see cmd.requestedBranch.

// RepoSlug infers "owner/name" from the origin remote, falling back to the directory name.
func RepoSlug(dir string) string {
	if remote, err := git(dir, "remote", "get-url", "origin"); err == nil && remote != "" {
		s := strings.TrimSuffix(strings.TrimSuffix(remote, "/"), ".git")
		// An scp-style remote (git@host:owner/name) has no scheme, and its path starts
		// after the colon. Keyed on "//" rather than on whether the tail has a slash:
		// git@host:owner/name does have one, so testing for its absence leaves the host
		// glued to the owner and the repo gets recorded under the wrong id.
		if !strings.Contains(s, "//") {
			if i := strings.LastIndex(s, ":"); i >= 0 {
				s = s[i+1:]
			}
		}
		if parts := strings.Split(strings.Trim(s, "/"), "/"); len(parts) >= 2 {
			return parts[len(parts)-2] + "/" + parts[len(parts)-1]
		}
	}
	abs, _ := filepath.Abs(dir)
	return filepath.Base(abs)
}

// Create writes a bundle containing sha and returns its path plus a cleanup func.
// A raw sha cannot be bundled — git requires named refs — so the sha is staged under
// the two ref names above and they are removed again on the way out.
func Create(dir, sha string) (string, func(), error) {
	if _, err := git(dir, "update-ref", refBase, sha); err != nil {
		return "", nil, err
	}
	if _, err := git(dir, "update-ref", refHead, sha); err != nil {
		_, _ = git(dir, "update-ref", "-d", refBase)
		return "", nil, err
	}
	dropRefs := func() {
		_, _ = git(dir, "update-ref", "-d", refBase)
		_, _ = git(dir, "update-ref", "-d", refHead)
	}

	f, err := os.CreateTemp("", "konvu-*.bundle")
	if err != nil {
		dropRefs()
		return "", nil, err
	}
	_ = f.Close()

	if _, err := git(dir, "bundle", "create", f.Name(), refBase, refHead); err != nil {
		_ = os.Remove(f.Name())
		dropRefs()
		return "", nil, err
	}
	return f.Name(), func() {
		dropRefs()
		_ = os.Remove(f.Name())
	}, nil
}
