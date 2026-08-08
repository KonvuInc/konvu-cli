package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const (
	updateRepo          = "KonvuInc/konvu-cli"
	updateLatestRelease = "https://api.github.com/repos/" + updateRepo + "/releases/latest"
)

var (
	updateCheckOnly bool
	updateForce     bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the Konvu CLI to the latest release",
	Long: "Download and install the latest Konvu CLI release, replacing the current binary in place.\n\n" +
		"Use --check to see whether a newer version is available without installing it.",
	Run: func(cmd *cobra.Command, args []string) {
		format := output.DetectOutputFormat(mustOutputFlag(cmd))
		if err := runUpdate(cmd, format); err != nil {
			reportUpdateError(err, format)
		}
	},
}

func runUpdate(cmd *cobra.Command, format output.OutputFormat) error {
	out := cmd.OutOrStdout()

	latest, err := latestVersion()
	if err != nil {
		return err
	}

	current := strings.TrimPrefix(Version, "v")
	updateAvailable := current == "dev" || current != latest

	if updateCheckOnly {
		if format == output.JSON {
			fmt.Fprintln(out, output.FormatJSON(map[string]any{
				"current_version":  Version,
				"latest_version":   latest,
				"update_available": updateAvailable,
			}))
			return nil
		}
		if updateAvailable {
			fmt.Fprintf(out, "A new version is available: %s (current: %s)\n", latest, Version)
			fmt.Fprintln(out, "Run 'konvu update' to install it.")
		} else {
			fmt.Fprintf(out, "konvu is up to date (%s)\n", Version)
		}
		return nil
	}

	if !updateAvailable && !updateForce {
		if format == output.JSON {
			fmt.Fprintln(out, output.FormatJSON(map[string]any{
				"current_version": Version,
				"latest_version":  latest,
				"updated":         false,
			}))
			return nil
		}
		fmt.Fprintf(out, "konvu is already up to date (%s)\n", Version)
		return nil
	}

	// Refuse to clobber a Homebrew-managed install; brew tracks the binary
	// itself and a self-replace would desync its metadata.
	if managedByHomebrew() && !updateForce {
		return &clierrors.CLIError{
			Code:       "HOMEBREW_MANAGED",
			Message:    "konvu was installed via Homebrew",
			Suggestion: "Run 'brew upgrade konvu' to update, or pass --force to overwrite in place.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}

	if runtime.GOOS == "windows" {
		return &clierrors.CLIError{
			Code:       "UNSUPPORTED_PLATFORM",
			Message:    "self-update is not supported on Windows",
			Suggestion: "Download the latest release from https://github.com/" + updateRepo + "/releases/latest",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}

	if format != output.JSON {
		fmt.Fprintf(out, "Updating konvu %s -> %s...\n", Version, latest)
	}

	binary, err := downloadRelease(latest)
	if err != nil {
		return err
	}

	if err := replaceRunningBinary(binary); err != nil {
		return err
	}

	if format == output.JSON {
		fmt.Fprintln(out, output.FormatJSON(map[string]any{
			"current_version": Version,
			"latest_version":  latest,
			"updated":         true,
		}))
		return nil
	}
	fmt.Fprintf(out, "konvu updated to %s\n", latest)
	return nil
}

var updateHTTPClient = &http.Client{Timeout: 60 * time.Second}

// latestVersion queries the GitHub API for the latest release tag, returning
// the version without a leading "v" (e.g. "1.2.3").
func latestVersion() (string, error) {
	req, err := http.NewRequest(http.MethodGet, updateLatestRelease, nil)
	if err != nil {
		return "", clierrors.NewAPIError(err.Error())
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "konvu-cli")

	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return "", &clierrors.CLIError{
			Code:       "NETWORK_ERROR",
			Message:    fmt.Sprintf("could not reach GitHub: %v", err),
			Suggestion: "Check your network connection and try again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", &clierrors.CLIError{
			Code:       "RELEASE_LOOKUP_FAILED",
			Message:    fmt.Sprintf("GitHub returned status %d when looking up the latest release", resp.StatusCode),
			Suggestion: "Try again later, or download manually from https://github.com/" + updateRepo + "/releases/latest",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", clierrors.NewAPIError(fmt.Sprintf("could not parse release info: %v", err))
	}
	if release.TagName == "" {
		return "", clierrors.NewAPIError("no release tag found")
	}
	return strings.TrimPrefix(release.TagName, "v"), nil
}

// downloadRelease fetches the release archive for the current platform, verifies
// its checksum against checksums.txt, and returns the extracted konvu binary.
func downloadRelease(version string) ([]byte, error) {
	filename := fmt.Sprintf("konvu-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	base := fmt.Sprintf("https://github.com/%s/releases/download/v%s", updateRepo, version)

	archive, err := httpGet(base + "/" + filename)
	if err != nil {
		return nil, err
	}

	checksums, err := httpGet(base + "/checksums.txt")
	if err != nil {
		return nil, err
	}

	if err := verifyChecksum(archive, checksums, filename); err != nil {
		return nil, err
	}

	return extractBinary(archive)
}

func httpGet(url string) ([]byte, error) {
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
			Suggestion: "Try again later, or download manually from https://github.com/" + updateRepo + "/releases/latest",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return io.ReadAll(resp.Body)
}

// verifyChecksum confirms the SHA-256 of data matches the entry for filename in
// a goreleaser-style checksums.txt ("<hash>  <filename>" per line).
func verifyChecksum(data, checksums []byte, filename string) error {
	var expected string
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == filename {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return &clierrors.CLIError{
			Code:       "CHECKSUM_MISSING",
			Message:    fmt.Sprintf("no checksum found for %s", filename),
			Suggestion: "The release may be incomplete; try again later.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}

	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return &clierrors.CLIError{
			Code:     "CHECKSUM_MISMATCH",
			Message:  fmt.Sprintf("checksum verification failed (expected %s, got %s)", expected, actual),
			ExitCode: clierrors.ExitGeneralError,
		}
	}
	return nil
}

// extractBinary pulls the "konvu" file out of a gzip-compressed tar archive.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, clierrors.NewAPIError(fmt.Sprintf("could not read archive: %v", err))
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, clierrors.NewAPIError(fmt.Sprintf("could not read archive: %v", err))
		}
		if filepath.Base(hdr.Name) == "konvu" && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
	return nil, clierrors.NewAPIError("konvu binary not found in release archive")
}

// replaceRunningBinary atomically swaps the currently running executable with
// the freshly downloaded one by writing a sibling temp file and renaming it.
func replaceRunningBinary(binary []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return clierrors.NewAPIError(fmt.Sprintf("could not locate current binary: %v", err))
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".konvu-update-*")
	if err != nil {
		return &clierrors.CLIError{
			Code:       "PERMISSION_DENIED",
			Message:    fmt.Sprintf("cannot write to %s: %v", dir, err),
			Suggestion: "Re-run with elevated permissions (e.g. sudo), or reinstall via the install script.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return clierrors.NewAPIError(fmt.Sprintf("could not write update: %v", err))
	}
	if err := tmp.Close(); err != nil {
		return clierrors.NewAPIError(fmt.Sprintf("could not write update: %v", err))
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return clierrors.NewAPIError(fmt.Sprintf("could not set permissions: %v", err))
	}
	if err := os.Rename(tmpName, exe); err != nil {
		return &clierrors.CLIError{
			Code:       "REPLACE_FAILED",
			Message:    fmt.Sprintf("could not replace %s: %v", exe, err),
			Suggestion: "Re-run with elevated permissions (e.g. sudo), or reinstall via the install script.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return nil
}

// managedByHomebrew reports whether the running binary lives inside a Homebrew
// prefix, in which case brew should own updates.
func managedByHomebrew() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return strings.Contains(exe, "/Cellar/") || strings.Contains(exe, "/homebrew/")
}

func reportUpdateError(err error, format output.OutputFormat) {
	var cliErr *clierrors.CLIError
	if e, ok := err.(*clierrors.CLIError); ok {
		cliErr = e
	} else {
		cliErr = clierrors.NewAPIError(err.Error())
	}

	if format == output.JSON {
		fmt.Println(clierrors.FormatErrorJSON(cliErr))
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", cliErr.Message)
		if cliErr.Suggestion != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", cliErr.Suggestion)
		}
	}
	os.Exit(cliErr.ExitCode)
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "Check for a newer version without installing")
	updateCmd.Flags().BoolVar(&updateForce, "force", false, "Reinstall even if already up to date or Homebrew-managed")
	updateCmd.Flags().StringP("output", "o", "", "Output format: json, text")
	rootCmd.AddCommand(updateCmd)
}
