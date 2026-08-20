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
// fetches. This tracks the guardrails-cli release cadence (S3/CloudFront),
// which is entirely separate from konvu-cli's own GitHub-releases version
// (see cmd/version.go) -- the two must never share a mechanism.
//
// ponytail: no release has been tagged yet, so whether the tag (and thus this
// S3 path segment) carries a "v" prefix is unconfirmed. Update this constant
// once the first real guardrails-cli tag is cut.
const guardrailsPinnedVersion = "v0.1.0"

// guardrailsConfigDir returns ~/.config/guardrails -- the literal,
// un-XDG'd, un-Windows-branched path the Rust binary itself reads credentials
// from (client/crates/guardrails-cli/src/agent/creds.rs joins raw $HOME).
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
// cargo-dist target triple, confirmed against dist-workspace.toml's six
// targets. Windows is excluded (not in the target matrix).
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
		// ponytail: runtime.GOARCH can't distinguish glibc from musl, but the
		// target matrix ships both. Defaulting to gnu (the common case); add
		// real musl detection (e.g. probing ldd/os-release) if that's ever needed.
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

// guardrailsArchiveName follows cargo-dist's naming, confirmed from
// client/crates/guardrails-cli/Cargo.toml: archives are named from the Cargo
// *package* name ("guardrails-cli"), not the [[bin]] name ("guardrails").
func guardrailsArchiveName(triple string) string {
	return fmt.Sprintf("guardrails-cli-%s.tar.xz", triple)
}

// guardrailsFetch is a minimal GET-into-memory helper, deliberately separate
// from update.go's httpGet: that helper's error Suggestion points at
// konvu-cli's own GitHub releases, which would be a misleading suggestion for
// a guardrails-cli/CloudFront failure. verifyChecksum (update.go) has no such
// coupling and is reused as-is below.
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
// there. Presence of the versioned path is the entire cache-hit signal: the
// version is pinned per-directory, so unlike skills/embed.go there is nothing
// to hash -- an existing file at that path can only be that exact build.
func ensureGuardrailsBinary(baseURL, version string) (string, error) {
	binPath, err := guardrailsBinaryPath(version)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(binPath); err == nil {
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
// archive. Go's stdlib has no xz/lzma decoder (compress/* covers
// gzip/zlib/flate/bzip2-read-only only), so this shells out to the system
// tar -- both GNU tar and macOS's bsdtar auto-detect xz from the archive's
// magic bytes, which avoids adding a new dependency for one decoder.
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

	// Basename match, not path match: matches update.go's extractBinary
	// precedent, so this is agnostic to whether the archive nests the
	// binary in a subdirectory.
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
// temp-file-write + rename, mirroring update.go's replaceRunningBinary.
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

// writeGuardrailsCredentials fully overwrites ~/.config/guardrails/credentials
// with the plain-OpenAI shape the Rust binary expects (creds.rs: "key = value",
// "#" comments, blank line = absent field). This must overwrite rather than
// merge/upsert: the Rust side picks Azure vs plain OpenAI by the mere presence
// of an "endpoint" line, so a stale leftover from an earlier config would
// silently force Azure mode and ignore these flags entirely.
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
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return clierrors.NewAPIError(fmt.Sprintf("could not write %s: %v", path, err))
	}
	return nil
}

// runGuardrailsExec is the shared os/exec shim behind all four guardrails
// verbs: ensure the binary is cached, write credentials if the flags were
// given, then run the child with stdio wired straight through so Rust's
// rendering reaches the terminal live and unmodified, and propagate its real
// exit code.
func runGuardrailsExec(args []string, apiKey, model string) {
	if apiKey != "" {
		if err := writeGuardrailsCredentials(apiKey, model); err != nil {
			reportGuardrailsError(err)
		}
	}

	binPath, err := ensureGuardrailsBinary(guardrailsCloudFrontBase, guardrailsPinnedVersion)
	if err != nil {
		reportGuardrailsError(err)
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
