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

// guardrailsCloudFrontBase hosts the public Guardrails release archives and
// checksums under <base>/guardrails/<version>/.
const guardrailsCloudFrontBase = "https://dneaqnz3vqe4a.cloudfront.net"

// guardrailsPinnedVersion is the Guardrails release installed by this version
// of Konvu CLI.
const guardrailsPinnedVersion = "v0.5.0"

// guardrailsConfigDir returns the fixed location shared with the Guardrails
// binary on every supported platform.
func guardrailsConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", clierrors.NewAPIError(fmt.Sprintf("could not determine home directory: %v", err))
	}
	return filepath.Join(home, ".config", "guardrails"), nil
}

func guardrailsBinaryPath(version string) (string, error) {
	dir, err := guardrailsConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bin", version, "guardrails"), nil
}

func guardrailsResourceScannerPath(version string) (string, error) {
	dir, err := guardrailsConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bin", version, "guardrails-resource-scan"), nil
}

// guardrailsTargetTriple maps the running platform to guardrails-cli's
// release target triple. Windows is not supported.
func guardrailsTargetTriple() (string, error) {
	return guardrailsTargetTripleFor(runtime.GOOS, runtime.GOARCH)
}

// guardrailsTargetTripleFor maps Go platform names to release target triples.
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

// guardrailsArchiveName follows the Guardrails release naming convention.
func guardrailsArchiveName(triple string) string {
	return fmt.Sprintf("guardrails-cli-%s.tar.xz", triple)
}

// guardrailsFetch downloads a Guardrails release asset.
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
	scannerPath, err := guardrailsResourceScannerPath(version)
	if err != nil {
		return "", err
	}
	if validExecutable(binPath) && validExecutable(scannerPath) {
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

	binaries, err := extractGuardrailsBinaries(archive)
	if err != nil {
		return "", err
	}

	if err := installGuardrailsBinary(scannerPath, binaries.resourceScanner); err != nil {
		return "", err
	}
	if err := installGuardrailsBinary(binPath, binaries.main); err != nil {
		return "", err
	}
	return binPath, nil
}

func validExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0 && info.Mode()&0o111 != 0
}

type guardrailsBinaries struct {
	main            []byte
	resourceScanner []byte
}

// extractGuardrailsBinaries pulls both runtime binaries out of a tar.xz archive.
// Go's stdlib has no xz decoder, so this shells out to the system tar, which
// auto-detects xz on Linux and macOS.
func extractGuardrailsBinaries(archive []byte) (guardrailsBinaries, error) {
	if _, err := exec.LookPath("tar"); err != nil {
		return guardrailsBinaries{}, &clierrors.CLIError{
			Code:       "MISSING_DEPENDENCY",
			Message:    "tar is required to extract the guardrails archive but was not found",
			Suggestion: "Install tar (present by default on macOS and Linux) and try again.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}

	tmpDir, err := os.MkdirTemp("", "guardrails-extract-*")
	if err != nil {
		return guardrailsBinaries{}, clierrors.NewAPIError(fmt.Sprintf("could not create temp dir: %v", err))
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, "archive.tar.xz")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		return guardrailsBinaries{}, clierrors.NewAPIError(fmt.Sprintf("could not write archive: %v", err))
	}

	destDir := filepath.Join(tmpDir, "extracted")
	if err := os.Mkdir(destDir, 0o755); err != nil {
		return guardrailsBinaries{}, clierrors.NewAPIError(fmt.Sprintf("could not create extract dir: %v", err))
	}

	if out, err := exec.Command("tar", "-xf", archivePath, "-C", destDir).CombinedOutput(); err != nil {
		return guardrailsBinaries{}, clierrors.NewAPIError(fmt.Sprintf("could not extract archive: %v: %s", err, strings.TrimSpace(string(out))))
	}

	// Basename match, not path match, so this is agnostic to whether the
	// archive nests the binary in a subdirectory.
	found := map[string]string{}
	walkErr := filepath.WalkDir(destDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && (d.Name() == "guardrails" || d.Name() == "guardrails-resource-scan") {
			found[d.Name()] = path
		}
		return nil
	})
	if walkErr != nil {
		return guardrailsBinaries{}, clierrors.NewAPIError(fmt.Sprintf("could not read extracted archive: %v", walkErr))
	}
	if found["guardrails"] == "" || found["guardrails-resource-scan"] == "" {
		return guardrailsBinaries{}, &clierrors.CLIError{
			Code:       "INVALID_ARCHIVE",
			Message:    "guardrails release archive is missing a runtime binary",
			Suggestion: "Try again later.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	main, err := os.ReadFile(found["guardrails"])
	if err != nil {
		return guardrailsBinaries{}, clierrors.NewAPIError(fmt.Sprintf("could not read guardrails binary: %v", err))
	}
	scanner, err := os.ReadFile(found["guardrails-resource-scan"])
	if err != nil {
		return guardrailsBinaries{}, clierrors.NewAPIError(fmt.Sprintf("could not read resource scanner: %v", err))
	}
	return guardrailsBinaries{main: main, resourceScanner: scanner}, nil
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

// guardrailsEnvironment adds explicitly supplied OpenAI credentials to the
// child process only. Existing values are replaced to avoid duplicate env
// entries with platform-dependent precedence.
func guardrailsEnvironment(base []string, apiKey, model string) []string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return base
	}

	env := make([]string, 0, len(base)+2)
	for _, entry := range base {
		if strings.HasPrefix(entry, "OPENAI_API_KEY=") || strings.HasPrefix(entry, "OPENAI_MODEL=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "OPENAI_API_KEY="+apiKey)
	if model = strings.TrimSpace(model); model != "" {
		env = append(env, "OPENAI_MODEL="+model)
	}
	return env
}

// runGuardrailsExec is the shared os/exec shim behind the guardrails commands:
// ensure the release bundle is cached, run it with stdio wired straight
// through, and propagate its exit code. Explicit OpenAI credentials are passed
// to the child only; the user's credentials file is never changed.
func runGuardrailsExec(args []string, apiKey, model string) {
	binPath, err := ensureGuardrailsBinary(guardrailsCloudFrontBase, guardrailsPinnedVersion)
	if err != nil {
		reportGuardrailsError(err)
	}

	child := exec.Command(binPath, args...)
	child.Env = guardrailsEnvironment(os.Environ(), apiKey, model)
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
