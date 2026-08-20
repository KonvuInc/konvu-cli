package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

// guardrailsCloudFrontBase is the CDN in front of the guardrails-downloads S3
// bucket. No auth gate; archives and checksums.txt live under
// <base>/guardrails/<version>/.
const guardrailsCloudFrontBase = "https://dneaqnz3vqe4a.cloudfront.net"

// guardrailsPinnedVersion is the guardrails-cli release this build of konvu
// fetches. It tracks the guardrails-cli release cadence, which is separate
// from konvu-cli's own version -- the two must never share a mechanism.
const guardrailsPinnedVersion = "v0.1.0"

// guardrailsConfigDir returns ~/.config/guardrails, the fixed path the
// guardrails binary itself reads its credentials from.
// pkg/config.GetConfigDir() is the wrong helper here: it's hardcoded to
// AppName "konvu" and branches to Application Support on macOS.
func guardrailsConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", clierrors.NewAPIError(fmt.Sprintf("could not determine home directory: %v", err))
	}
	return filepath.Join(home, ".config", "guardrails"), nil
}

func guardrailsCredentialsPath() (string, error) {
	dir, err := guardrailsConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials"), nil
}

func guardrailsBinaryPath(version string) (string, error) {
	dir, err := guardrailsConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bin", version, "guardrails"), nil
}

// guardrailsTargetTriple maps the running platform to guardrails-cli's
// release target triple. Windows is not supported.
func guardrailsTargetTriple() (string, error) {
	return guardrailsTargetTripleFor(runtime.GOOS, runtime.GOARCH)
}

// guardrailsTargetTripleFor takes goos/goarch as parameters (rather than
// reading runtime.GOOS/GOARCH directly) purely so the mapping can be unit
// tested against every known combination, not just the one the test binary
// happens to run on.
func guardrailsTargetTripleFor(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "darwin/arm64":
		return "aarch64-apple-darwin", nil
	case "darwin/amd64":
		return "x86_64-apple-darwin", nil
	case "linux/arm64":
		// runtime.GOARCH can't distinguish glibc from musl; assumes glibc.
		return "aarch64-unknown-linux-gnu", nil
	case "linux/amd64":
		return "x86_64-unknown-linux-gnu", nil
	default:
		return "", &clierrors.CLIError{
			Code:       "UNSUPPORTED_PLATFORM",
			Message:    fmt.Sprintf("guardrails is not available for %s/%s", goos, goarch),
			Suggestion: "Supported platforms: macOS (arm64, amd64) and Linux (arm64, amd64, glibc).",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
}

// guardrailsArchiveName follows the release naming convention: the archive
// is named after the package ("guardrails-cli"), not the binary it contains
// ("guardrails").
func guardrailsArchiveName(triple string) string {
	return fmt.Sprintf("guardrails-cli-%s.tar.xz", triple)
}

// guardrailsFetch is a minimal GET-into-memory helper, kept separate from
// update.go's httpGet so a download failure here doesn't surface a
// suggestion about konvu-cli's own GitHub releases.
func guardrailsFetch(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, clierrors.NewAPIError(err.Error())
	}
	req.Header.Set("User-Agent", "konvu-cli")

	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, &clierrors.CLIError{
			Code:       "NETWORK_ERROR",
			Message:    fmt.Sprintf("download failed: %v", err),
			Suggestion: "Check your network connection and try again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &clierrors.CLIError{
			Code:       "DOWNLOAD_FAILED",
			Message:    fmt.Sprintf("download failed with status %d for %s", resp.StatusCode, url),
			Suggestion: "Try again later.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return io.ReadAll(resp.Body)
}

// ensureGuardrailsBinary returns the path to the cached guardrails binary for
// version, fetching and caching it from baseURL first if it isn't already
// there, or if what's cached isn't a valid executable (e.g. left truncated by
// an interrupted install) -- existence alone isn't enough to trust the cache,
// since callers rely on a successful return meaning the binary can actually
// run.
func ensureGuardrailsBinary(baseURL, version string) (string, error) {
	binPath, err := guardrailsBinaryPath(version)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(binPath); err == nil && info.Size() > 0 && info.Mode()&0o111 != 0 {
		return binPath, nil
	}

	triple, err := guardrailsTargetTriple()
	if err != nil {
		return "", err
	}
	archiveName := guardrailsArchiveName(triple)
	base := strings.TrimRight(baseURL, "/") + "/guardrails/" + version

	fmt.Fprintf(os.Stderr, "Fetching guardrails %s...\n", version)

	archive, err := guardrailsFetch(base + "/" + archiveName)
	if err != nil {
		return "", err
	}
	checksums, err := guardrailsFetch(base + "/checksums.txt")
	if err != nil {
		return "", err
	}
	if err := verifyChecksum(archive, checksums, archiveName); err != nil {
		return "", err
	}

	binary, err := extractGuardrailsBinary(archive)
	if err != nil {
		return "", err
	}

	if err := installGuardrailsBinary(binPath, binary); err != nil {
		return "", err
	}
	return binPath, nil
}

// extractGuardrailsBinary pulls the "guardrails" file out of a tar.xz
// archive. Go's stdlib has no xz decoder, so this shells out to the system
// tar, which auto-detects xz on both Linux and macOS.
func extractGuardrailsBinary(archive []byte) ([]byte, error) {
	if _, err := exec.LookPath("tar"); err != nil {
		return nil, &clierrors.CLIError{
			Code:       "MISSING_DEPENDENCY",
			Message:    "tar is required to extract the guardrails archive but was not found",
			Suggestion: "Install tar (present by default on macOS and Linux) and try again.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}

	tmpDir, err := os.MkdirTemp("", "guardrails-extract-*")
	if err != nil {
		return nil, clierrors.NewAPIError(fmt.Sprintf("could not create temp dir: %v", err))
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, "archive.tar.xz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		return nil, clierrors.NewAPIError(fmt.Sprintf("could not write archive: %v", err))
	}

	destDir := filepath.Join(tmpDir, "extracted")
	if err := os.Mkdir(destDir, 0o755); err != nil {
		return nil, clierrors.NewAPIError(fmt.Sprintf("could not create extract dir: %v", err))
	}

	if out, err := exec.Command("tar", "-xf", archivePath, "-C", destDir).CombinedOutput(); err != nil {
		return nil, clierrors.NewAPIError(fmt.Sprintf("could not extract archive: %v: %s", err, strings.TrimSpace(string(out))))
	}

	// Basename match, not path match, so this is agnostic to whether the
	// archive nests the binary in a subdirectory.
	var found string
	walkErr := filepath.WalkDir(destDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "guardrails" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return nil, clierrors.NewAPIError(fmt.Sprintf("could not read extracted archive: %v", walkErr))
	}
	if found == "" {
		return nil, clierrors.NewAPIError("guardrails binary not found in release archive")
	}
	return os.ReadFile(found)
}

// installGuardrailsBinary atomically writes data to destPath via a sibling
// temp-file-write + rename.
func installGuardrailsBinary(destPath string, data []byte) error {
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &clierrors.CLIError{
			Code:       "PERMISSION_DENIED",
			Message:    fmt.Sprintf("cannot create %s: %v", dir, err),
			Suggestion: "Check permissions on ~/.config/guardrails.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}

	tmp, err := os.CreateTemp(dir, ".guardrails-*")
	if err != nil {
		return &clierrors.CLIError{
			Code:       "PERMISSION_DENIED",
			Message:    fmt.Sprintf("cannot write to %s: %v", dir, err),
			Suggestion: "Check permissions on ~/.config/guardrails.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return clierrors.NewAPIError(fmt.Sprintf("could not write guardrails binary: %v", err))
	}
	if err := tmp.Close(); err != nil {
		return clierrors.NewAPIError(fmt.Sprintf("could not write guardrails binary: %v", err))
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return clierrors.NewAPIError(fmt.Sprintf("could not set permissions: %v", err))
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return &clierrors.CLIError{
			Code:     "REPLACE_FAILED",
			Message:  fmt.Sprintf("could not install %s: %v", destPath, err),
			ExitCode: clierrors.ExitGeneralError,
		}
	}
	return nil
}

// atomicWriteFile writes data to path via a sibling temp-file-write + rename
// in the same directory, so a failure partway through (e.g. disk full) never
// leaves path itself truncated -- only ever the old content or the new
// content, in full.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".guardrails-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// writeGuardrailsCredentials fully overwrites ~/.config/guardrails/credentials
// with the plain-OpenAI shape guardrails expects ("key = value" lines). This
// must overwrite rather than merge: guardrails picks Azure vs. plain OpenAI by
// the mere presence of an "endpoint" line, so a stale leftover from an
// earlier config would silently force Azure mode and ignore these flags.
func writeGuardrailsCredentials(apiKey, model string) error {
	path, err := guardrailsCredentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return &clierrors.CLIError{
			Code:       "PERMISSION_DENIED",
			Message:    fmt.Sprintf("cannot create %s: %v", filepath.Dir(path), err),
			Suggestion: "Check permissions on ~/.config/guardrails.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	content := fmt.Sprintf("key = %s\nmodel = %s\n", apiKey, model)
	if err := atomicWriteFile(path, []byte(content), 0o600); err != nil {
		return clierrors.NewAPIError(fmt.Sprintf("could not write %s: %v", path, err))
	}
	return nil
}

// backupGuardrailsCredentials snapshots the credentials file before it's
// overwritten and returns a restore func that puts the snapshot back (or
// removes the file, if none existed). No cache-validity check on the
// guardrails binary can ever be airtight -- a file can be nonempty and
// executable-marked and still fail to actually start (a corrupt binary from
// an interrupted write, say). Restoring on a genuine start failure is the
// only way to guarantee a run that never happened never destroys real
// credentials, regardless of why the binary didn't start.
func backupGuardrailsCredentials() (restore func(), err error) {
	path, err := guardrailsCredentialsPath()
	if err != nil {
		return nil, err
	}
	previous, readErr := os.ReadFile(path)
	return func() {
		var restoreErr error
		if readErr == nil {
			restoreErr = atomicWriteFile(path, previous, 0o600)
		} else {
			restoreErr = os.Remove(path)
		}
		if restoreErr != nil && !os.IsNotExist(restoreErr) {
			fmt.Fprintf(os.Stderr, "warning: could not restore prior credentials at %s: %v\n", path, restoreErr)
		}
	}, nil
}

// runGuardrailsExec is the shared os/exec shim behind all four guardrails
// verbs: ensure the binary is cached, write credentials if given, run the
// child with stdio wired straight through, and propagate its exit code.
// The binary must be ensured before credentials are written: if the
// download/checksum/extract step fails, we must not have already destroyed
// the user's existing credentials for a run that's about to fail anyway. If
// the binary is ensured but still fails to actually start (a corrupt cache
// entry that passed ensureGuardrailsBinary's checks, say), whatever
// credentials existed before this run are restored -- see
// backupGuardrailsCredentials.
func runGuardrailsExec(args []string, apiKey, model string) {
	binPath, err := ensureGuardrailsBinary(guardrailsCloudFrontBase, guardrailsPinnedVersion)
	if err != nil {
		reportGuardrailsError(err)
	}

	var restoreCredentials func()
	if apiKey != "" {
		restoreCredentials, err = backupGuardrailsCredentials()
		if err != nil {
			reportGuardrailsError(err)
		}
		if err := writeGuardrailsCredentials(apiKey, model); err != nil {
			reportGuardrailsError(err)
		}
	}

	child := exec.Command(binPath, args...)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	if err := child.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		if restoreCredentials != nil {
			restoreCredentials()
		}
		reportGuardrailsError(clierrors.NewAPIError(fmt.Sprintf("could not run guardrails: %v", err)))
	}
}

func reportGuardrailsError(err error) {
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok {
		cliErr = clierrors.NewAPIError(err.Error())
	}
	fmt.Fprintf(os.Stderr, "Error: %s\n", cliErr.Message)
	if cliErr.Suggestion != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", cliErr.Suggestion)
	}
	os.Exit(cliErr.ExitCode)
}
