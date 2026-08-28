package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

func TestGuardrailsTargetTripleFor(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         string
		wantErr      bool
	}{
		{"darwin", "arm64", "aarch64-apple-darwin", false},
		{"darwin", "amd64", "x86_64-apple-darwin", false},
		{"linux", "arm64", "aarch64-unknown-linux-gnu", false},
		{"linux", "amd64", "x86_64-unknown-linux-gnu", false},
		{"windows", "amd64", "", true},
		{"linux", "386", "", true},
	}
	for _, c := range cases {
		got, err := guardrailsTargetTripleFor(c.goos, c.goarch)
		if c.wantErr {
			if err == nil {
				t.Errorf("guardrailsTargetTripleFor(%s, %s) = %q, want error", c.goos, c.goarch, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("guardrailsTargetTripleFor(%s, %s) unexpected error: %v", c.goos, c.goarch, err)
			continue
		}
		if got != c.want {
			t.Errorf("guardrailsTargetTripleFor(%s, %s) = %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

func TestGuardrailsArchiveName(t *testing.T) {
	got := guardrailsArchiveName("aarch64-apple-darwin")
	want := "guardrails-cli-aarch64-apple-darwin.tar.xz"
	if got != want {
		t.Errorf("guardrailsArchiveName() = %q, want %q", got, want)
	}
}

func TestGuardrailsEnvironmentKeepsOnlyRequiredValues(t *testing.T) {
	base := []string{
		"HOME=/home/test",
		"PATH=/usr/bin",
		"HTTPS_PROXY=http://proxy.test",
		"SSL_CERT_FILE=/certs/ca.pem",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"GUARDRAILS_VERBOSE=1",
		"OPENAI_API_KEY=old-key",
		"OPENAI_MODEL=old-model",
		"AWS_SECRET_ACCESS_KEY=aws-secret",
		"GITHUB_TOKEN=github-secret",
		"SSH_AUTH_SOCK=/tmp/ssh-agent",
	}

	got := guardrailsEnvironment(base, " new-key ", " gpt-5.6-luna ")
	want := []string{
		"HOME=/home/test",
		"PATH=/usr/bin",
		"HTTPS_PROXY=http://proxy.test",
		"SSL_CERT_FILE=/certs/ca.pem",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"GUARDRAILS_VERBOSE=1",
		"OPENAI_API_KEY=new-key",
		"OPENAI_MODEL=gpt-5.6-luna",
	}
	if len(got) != len(want) {
		t.Fatalf("environment length = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("environment[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGuardrailsEnvironmentModelOverridePreservesAmbientKey(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=ambient-key",
		"OPENAI_MODEL=ambient-model",
		"AWS_ACCESS_KEY_ID=not-needed",
	}

	got := guardrailsEnvironment(base, "", " gpt-5.6-luna ")
	want := []string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=ambient-key",
		"OPENAI_MODEL=gpt-5.6-luna",
	}
	if len(got) != len(want) {
		t.Fatalf("environment length = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("environment[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGuardrailsEnvironmentWithoutModelGetsNoOpenAICredentials(t *testing.T) {
	base := []string{
		"HOME=/home/test",
		"OPENAI_API_KEY=ambient-key",
		"OPENAI_MODEL=ambient-model",
		"KUBECONFIG=/home/test/.kube/config",
	}

	got := guardrailsEnvironment(base, "", "")
	want := []string{"HOME=/home/test"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("environment = %v, want %v", got, want)
	}
}

func TestGuardrailsOuterSandboxSelection(t *testing.T) {
	tests := []struct {
		name               string
		noSandbox          bool
		runtimeOwnsSandbox bool
		want               bool
	}{
		{name: "launcher owns sandbox", want: true},
		{name: "runtime owns sandbox", runtimeOwnsSandbox: true, want: false},
		{name: "explicitly disabled", noSandbox: true, want: false},
		{
			name:               "runtime owns sandbox and explicitly disabled",
			noSandbox:          true,
			runtimeOwnsSandbox: true,
			want:               false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := shouldUseGuardrailsOuterSandbox(test.noSandbox, test.runtimeOwnsSandbox)
			if got != test.want {
				t.Fatalf("outer sandbox = %v, want %v", got, test.want)
			}
		})
	}
}

func TestGuardrailsReadOnlyCommandsHaveNoOpenAIFlags(t *testing.T) {
	for _, commandName := range []string{"list", "show", "explain"} {
		command, _, err := guardrailsCmd.Find([]string{"baseline", commandName})
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"openai-api-key", "openai-model"} {
			if command.Flags().Lookup(name) != nil {
				t.Errorf("%s unexpectedly accepts --%s", command.Name(), name)
			}
		}
	}
}

// buildFixtureArchive shells out to the system tar to produce a real
// tar.xz containing the runtime binaries, since Go's stdlib has no xz encoder
// either. A nil scanner omits the sidecar for invalid-archive tests. Skips (not
// fails) if the local tar lacks xz support.
func buildFixtureArchive(t *testing.T, main, scanner []byte) []byte {
	t.Helper()
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not found on PATH")
	}

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "guardrails"), main, 0o755); err != nil {
		t.Fatalf("write fixture binary: %v", err)
	}
	files := []string{"guardrails"}
	if scanner != nil {
		if err := os.WriteFile(filepath.Join(src, "guardrails-resource-scan"), scanner, 0o755); err != nil {
			t.Fatalf("write fixture resource scanner: %v", err)
		}
		files = append(files, "guardrails-resource-scan")
	}

	archivePath := filepath.Join(t.TempDir(), "fixture.tar.xz")
	tarArgs := append([]string{"-cJf", archivePath, "-C", src}, files...)
	cmd := exec.Command("tar", tarArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("local tar cannot create .tar.xz (%v): %s", err, out)
	}

	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read fixture archive: %v", err)
	}
	return data
}

func fixtureArtifact(archive, main, scanner []byte) guardrailsArtifact {
	return guardrailsArtifact{
		archiveSHA256:         sha256Hex(archive),
		mainSHA256:            sha256Hex(main),
		resourceScannerSHA256: sha256Hex(scanner),
	}
}

func TestEnsureGuardrailsBinary(t *testing.T) {
	triple, err := guardrailsTargetTriple()
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}
	archiveName := guardrailsArchiveName(triple)
	binaryContents := []byte("#!/bin/sh\necho fake-guardrails\n")
	scannerContents := []byte("#!/bin/sh\necho fake-resource-scanner\n")
	archive := buildFixtureArchive(t, binaryContents, scannerContents)
	artifact := fixtureArtifact(archive, binaryContents, scannerContents)

	const version = "v0.1.0-test"
	var requests int
	mux := http.NewServeMux()
	mux.HandleFunc("/guardrails/"+version+"/"+archiveName, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write(archive)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)

	// Cache-miss: fetches and installs.
	binPath, err := ensureGuardrailsBinaryForArtifact(server.URL, version, triple, artifact)
	if err != nil {
		t.Fatalf("ensureGuardrailsBinary (cache-miss): %v", err)
	}
	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(got) != string(binaryContents) {
		t.Errorf("installed binary contents = %q, want %q", got, binaryContents)
	}
	if info, err := os.Stat(binPath); err != nil || info.Mode()&0o111 == 0 {
		t.Errorf("installed binary is not executable: %v", err)
	}
	scannerPath, err := guardrailsResourceScannerPath(version)
	if err != nil {
		t.Fatalf("guardrailsResourceScannerPath: %v", err)
	}
	gotScanner, err := os.ReadFile(scannerPath)
	if err != nil {
		t.Fatalf("read installed resource scanner: %v", err)
	}
	if string(gotScanner) != string(scannerContents) {
		t.Errorf("installed resource scanner contents = %q, want %q", gotScanner, scannerContents)
	}
	if info, err := os.Stat(scannerPath); err != nil || info.Mode()&0o111 == 0 {
		t.Errorf("installed resource scanner is not executable: %v", err)
	}
	if requests != 1 {
		t.Fatalf("expected 1 archive request on cache-miss, got %d", requests)
	}

	// Cache-hit: matching executable hashes must not touch the network again.
	if _, err := ensureGuardrailsBinaryForArtifact(server.URL, version, triple, artifact); err != nil {
		t.Fatalf("ensureGuardrailsBinary (cache-hit): %v", err)
	}
	if requests != 1 {
		t.Errorf("expected cache-hit to skip the fetch, but requests = %d", requests)
	}

	// A modified executable is never run; a valid release archive heals it.
	if err := os.WriteFile(binPath, []byte("tampered"), 0o755); err != nil {
		t.Fatalf("tamper cached binary: %v", err)
	}
	if _, err := ensureGuardrailsBinaryForArtifact(server.URL, version, triple, artifact); err != nil {
		t.Fatalf("ensureGuardrailsBinary (tampered cache): %v", err)
	}
	if requests != 2 {
		t.Errorf("expected tampered cache to fetch once, got %d requests", requests)
	}
	if got, err := os.ReadFile(binPath); err != nil || string(got) != string(binaryContents) {
		t.Errorf("tampered binary was not healed: content %q, err %v", got, err)
	}

	// Incomplete bundle: a missing sidecar invalidates the cached release.
	if err := os.Remove(scannerPath); err != nil {
		t.Fatalf("remove resource scanner: %v", err)
	}
	if _, err := ensureGuardrailsBinaryForArtifact(server.URL, version, triple, artifact); err != nil {
		t.Fatalf("ensureGuardrailsBinary (missing sidecar): %v", err)
	}
	if requests != 3 {
		t.Errorf("expected a fresh fetch for an incomplete bundle, got %d requests", requests)
	}

	// Cache-miss-after-deletion: remove the cached dir, must re-fetch cleanly.
	dir, err := guardrailsConfigDir()
	if err != nil {
		t.Fatalf("guardrailsConfigDir: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "bin", version)); err != nil {
		t.Fatalf("RemoveAll cache dir: %v", err)
	}
	if _, err := ensureGuardrailsBinaryForArtifact(server.URL, version, triple, artifact); err != nil {
		t.Fatalf("ensureGuardrailsBinary (post-deletion): %v", err)
	}
	if requests != 4 {
		t.Errorf("expected a fresh fetch after cache deletion, got %d total", requests)
	}
}

func TestEnsureGuardrailsBinaryChecksumMismatch(t *testing.T) {
	triple, err := guardrailsTargetTriple()
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}
	archiveName := guardrailsArchiveName(triple)
	archive := buildFixtureArchive(t, []byte("irrelevant"), []byte("scanner"))
	artifact := fixtureArtifact(archive, []byte("irrelevant"), []byte("scanner"))
	artifact.archiveSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

	const version = "v0.1.0-badsum"
	mux := http.NewServeMux()
	mux.HandleFunc("/guardrails/"+version+"/"+archiveName, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err = ensureGuardrailsBinaryForArtifact(server.URL, version, triple, artifact)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok || cliErr.Code != "CHECKSUM_MISMATCH" {
		t.Errorf("expected CHECKSUM_MISMATCH, got %v", err)
	}

	binPath, err := guardrailsBinaryPath(version)
	if err != nil {
		t.Fatalf("guardrailsBinaryPath: %v", err)
	}
	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Errorf("expected no binary installed after checksum failure, stat err = %v", err)
	}
}

func TestEnsureGuardrailsBinaryRejectsMissingResourceScanner(t *testing.T) {
	triple, err := guardrailsTargetTriple()
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}
	archiveName := guardrailsArchiveName(triple)
	archive := buildFixtureArchive(t, []byte("guardrails"), nil)
	artifact := fixtureArtifact(archive, []byte("guardrails"), []byte("scanner"))

	const version = "v0.1.0-missing-scanner"
	mux := http.NewServeMux()
	mux.HandleFunc("/guardrails/"+version+"/"+archiveName, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("HOME", t.TempDir())
	_, err = ensureGuardrailsBinaryForArtifact(server.URL, version, triple, artifact)
	if err == nil {
		t.Fatal("expected invalid archive error, got nil")
	}
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok || cliErr.Code != "INVALID_ARCHIVE" {
		t.Errorf("expected INVALID_ARCHIVE, got %v", err)
	}
}

func TestGuardrailsPinnedArtifactsCoverSupportedPlatforms(t *testing.T) {
	for _, triple := range []string{
		"aarch64-apple-darwin",
		"x86_64-apple-darwin",
		"aarch64-unknown-linux-gnu",
		"x86_64-unknown-linux-gnu",
	} {
		artifact, ok := guardrailsArtifacts[triple]
		if !ok {
			t.Errorf("missing trusted artifact for %s", triple)
			continue
		}
		for name, digest := range map[string]string{
			"archive":                  artifact.archiveSHA256,
			"guardrails":               artifact.mainSHA256,
			"guardrails-resource-scan": artifact.resourceScannerSHA256,
		} {
			if len(digest) != 64 {
				t.Errorf("%s %s digest length = %d, want 64", triple, name, len(digest))
			}
		}
	}
}
