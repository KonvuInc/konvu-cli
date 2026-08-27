package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	releaseBaseURL     = "https://dneaqnz3vqe4a.cloudfront.net/guardrails"
	expectedIssuer     = "https://token.actions.githubusercontent.com"
	expectedRepository = "KonvuTeam/konvu-guardrails"
	expectedWorkflow   = ".github/workflows/release-guardrails.yml"
	legacyVersion      = "v0.5.1"
)

var (
	targets = []string{
		"aarch64-apple-darwin",
		"x86_64-apple-darwin",
		"aarch64-unknown-linux-gnu",
		"x86_64-unknown-linux-gnu",
	}
	versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$`)
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	pinnedPattern  = regexp.MustCompile(`const guardrailsPinnedVersion = "([^"]+)"`)
)

type releaseManifest struct {
	SchemaVersion int                 `json:"schema_version"`
	Version       string              `json:"version"`
	Source        *releaseSource      `json:"source,omitempty"`
	Artifacts     map[string]artifact `json:"artifacts"`
}

type releaseSource struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Workflow   string `json:"workflow"`
}

type artifact struct {
	Archive  archiveInfo           `json:"archive"`
	Binaries map[string]digestInfo `json:"binaries"`
}

type archiveInfo struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type digestInfo struct {
	SHA256 string `json:"sha256"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	check := len(args) == 1 && args[0] == "--check"
	if !check && len(args) != 1 {
		return fmt.Errorf("usage: updateguardrails <version> | updateguardrails --check")
	}
	for _, command := range []string{"cosign", "curl", "tar"} {
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("%s is required", command)
		}
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	destination := filepath.Join(repoRoot, "cmd", "guardrails_artifacts.go")
	version := args[0]
	if check {
		version, err = pinnedVersion(destination)
		if err != nil {
			return err
		}
	}
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid Guardrails version: %s", version)
	}

	tmpDir, err := os.MkdirTemp("", "update-guardrails-")
	if err != nil {
		return fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	manifest, err := loadManifest(ctx, tmpDir, version)
	if err != nil {
		return err
	}
	if err := validateManifest(manifest, version); err != nil {
		return fmt.Errorf("invalid Guardrails release manifest for %s: %w", version, err)
	}
	generated, err := generateSource(manifest)
	if err != nil {
		return fmt.Errorf("generate %s: %w", destination, err)
	}
	if check {
		current, err := os.ReadFile(destination)
		if err != nil {
			return fmt.Errorf("read %s: %w", destination, err)
		}
		if !bytes.Equal(generated, current) {
			return fmt.Errorf("%s does not match the signed Guardrails %s release", destination, version)
		}
		return nil
	}
	if err := os.WriteFile(destination, generated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", destination, err)
	}
	return nil
}

func findRepoRoot() (string, error) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("locate updateguardrails source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !bytes.Contains(data, []byte("module github.com/KonvuInc/konvu-cli")) {
		return "", fmt.Errorf("could not locate the konvu-cli repository")
	}
	return root, nil
}

func pinnedVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	match := pinnedPattern.FindSubmatch(data)
	if len(match) != 2 {
		return "", fmt.Errorf("could not read pinned Guardrails version from %s", path)
	}
	return string(match[1]), nil
}

func loadManifest(ctx context.Context, tmpDir, version string) (*releaseManifest, error) {
	baseURL := releaseBaseURL + "/" + version
	manifestPath := filepath.Join(tmpDir, "guardrails-release.json")
	if download(ctx, baseURL+"/guardrails-release.json", manifestPath, false) == nil {
		bundlePath := manifestPath + ".bundle"
		if err := download(ctx, baseURL+"/guardrails-release.json.bundle", bundlePath, true); err != nil {
			return nil, err
		}
		if err := verifyBlob(ctx, version, manifestPath, bundlePath); err != nil {
			return nil, err
		}
		return readManifest(manifestPath)
	}
	if version != legacyVersion {
		return nil, fmt.Errorf("signed Guardrails release manifest not found for %s", version)
	}
	return legacyManifest(ctx, tmpDir, version, baseURL)
}

func download(ctx context.Context, url, destination string, required bool) error {
	cmd := exec.CommandContext(ctx, "curl", "--proto", "=https", "--tlsv1.2", "-fsSLo", destination, url)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	os.Remove(destination)
	if !required {
		return err
	}
	return fmt.Errorf("download %s: %s", url, commandError(err, output))
}

func verifyBlob(ctx context.Context, version, artifactPath, bundlePath string) error {
	identity := "https://github.com/" + expectedRepository + "/" + expectedWorkflow + "@refs/tags/" + version
	cmd := exec.CommandContext(ctx, "cosign", "verify-blob",
		"--bundle", bundlePath,
		"--certificate-identity", identity,
		"--certificate-oidc-issuer", expectedIssuer,
		artifactPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify %s: %s", filepath.Base(artifactPath), commandError(err, output))
	}
	return nil
}

func commandError(err error, output []byte) string {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err.Error()
	}
	return message
}

func legacyManifest(ctx context.Context, tmpDir, version, baseURL string) (*releaseManifest, error) {
	manifest := &releaseManifest{
		SchemaVersion: 1,
		Version:       version,
		Artifacts:     make(map[string]artifact, len(targets)),
	}
	for _, target := range targets {
		archiveName := "guardrails-cli-" + target + ".tar.xz"
		archivePath := filepath.Join(tmpDir, archiveName)
		bundlePath := archivePath + ".bundle"
		if err := download(ctx, baseURL+"/"+archiveName, archivePath, true); err != nil {
			return nil, err
		}
		if err := download(ctx, baseURL+"/"+archiveName+".bundle", bundlePath, true); err != nil {
			return nil, err
		}
		if err := verifyBlob(ctx, version, archivePath, bundlePath); err != nil {
			return nil, err
		}

		mainHash, err := hashTarMember(ctx, archivePath, "guardrails")
		if err != nil {
			return nil, err
		}
		scannerHash, err := hashTarMember(ctx, archivePath, "guardrails-resource-scan")
		if err != nil {
			return nil, err
		}
		archiveHash, err := hashFile(archivePath)
		if err != nil {
			return nil, err
		}
		manifest.Artifacts[target] = artifact{
			Archive: archiveInfo{Name: archiveName, SHA256: archiveHash},
			Binaries: map[string]digestInfo{
				"guardrails":               {SHA256: mainHash},
				"guardrails-resource-scan": {SHA256: scannerHash},
			},
		}
	}
	return manifest, nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func hashTarMember(ctx context.Context, archivePath, binaryName string) (string, error) {
	list := exec.CommandContext(ctx, "tar", "-tf", archivePath)
	output, err := list.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("list %s: %s", filepath.Base(archivePath), commandError(err, output))
	}
	var member string
	for _, candidate := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if filepath.Base(candidate) != binaryName {
			continue
		}
		if member != "" {
			return "", fmt.Errorf("%s contains multiple %s binaries", filepath.Base(archivePath), binaryName)
		}
		member = candidate
	}
	if member == "" {
		return "", fmt.Errorf("%s is missing %s", filepath.Base(archivePath), binaryName)
	}

	hash := sha256.New()
	var stderr bytes.Buffer
	extract := exec.CommandContext(ctx, "tar", "-xOf", archivePath, member)
	extract.Stdout = hash
	extract.Stderr = &stderr
	if err := extract.Run(); err != nil {
		return "", fmt.Errorf("extract %s from %s: %s", binaryName, filepath.Base(archivePath), commandError(err, stderr.Bytes()))
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func readManifest(path string) (*releaseManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest releaseManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode %s: trailing JSON data", path)
	}
	return &manifest, nil
}

func validateManifest(manifest *releaseManifest, version string) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema version %d", manifest.SchemaVersion)
	}
	if manifest.Version != version {
		return fmt.Errorf("version is %q, expected %q", manifest.Version, version)
	}
	if manifest.Source == nil {
		if version != legacyVersion {
			return fmt.Errorf("source is required")
		}
	} else {
		if manifest.Source.Repository != expectedRepository {
			return fmt.Errorf("unexpected source repository %q", manifest.Source.Repository)
		}
		if manifest.Source.Workflow != expectedWorkflow {
			return fmt.Errorf("unexpected source workflow %q", manifest.Source.Workflow)
		}
		if !commitPattern.MatchString(manifest.Source.Commit) {
			return fmt.Errorf("invalid source commit")
		}
	}
	for _, target := range targets {
		artifact, ok := manifest.Artifacts[target]
		if !ok {
			return fmt.Errorf("missing artifact for %s", target)
		}
		if artifact.Archive.Name != "guardrails-cli-"+target+".tar.xz" {
			return fmt.Errorf("unexpected archive name for %s", target)
		}
		if !digestPattern.MatchString(artifact.Archive.SHA256) {
			return fmt.Errorf("invalid archive digest for %s", target)
		}
		for _, binary := range []string{"guardrails", "guardrails-resource-scan"} {
			digest, ok := artifact.Binaries[binary]
			if !ok || !digestPattern.MatchString(digest.SHA256) {
				return fmt.Errorf("invalid %s digest for %s", binary, target)
			}
		}
	}
	return nil
}

func generateSource(manifest *releaseManifest) ([]byte, error) {
	var source bytes.Buffer
	fmt.Fprintln(&source, "// Code generated by go run ./internal/tools/updateguardrails; DO NOT EDIT.")
	fmt.Fprintln(&source, "\npackage cmd")
	fmt.Fprintln(&source, "\n// guardrailsPinnedVersion is the Guardrails release installed by this version")
	fmt.Fprintln(&source, "// of Konvu CLI.")
	fmt.Fprintf(&source, "const guardrailsPinnedVersion = %q\n", manifest.Version)
	fmt.Fprintln(&source, "\ntype guardrailsArtifact struct {")
	fmt.Fprintln(&source, "\tarchiveSHA256         string")
	fmt.Fprintln(&source, "\tmainSHA256            string")
	fmt.Fprintln(&source, "\tresourceScannerSHA256 string")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "\n// guardrailsArtifacts is the trust anchor for downloaded runtime bytes.")
	fmt.Fprintln(&source, "var guardrailsArtifacts = map[string]guardrailsArtifact{")
	for _, target := range targets {
		artifact := manifest.Artifacts[target]
		fmt.Fprintf(&source, "\t%q: {\n", target)
		fmt.Fprintf(&source, "\t\tarchiveSHA256:         %q,\n", artifact.Archive.SHA256)
		fmt.Fprintf(&source, "\t\tmainSHA256:            %q,\n", artifact.Binaries["guardrails"].SHA256)
		fmt.Fprintf(&source, "\t\tresourceScannerSHA256: %q,\n", artifact.Binaries["guardrails-resource-scan"].SHA256)
		fmt.Fprintln(&source, "\t},")
	}
	fmt.Fprintln(&source, "}")
	return format.Source(source.Bytes())
}
