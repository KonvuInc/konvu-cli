package cmd

import (
	"crypto/sha256"
	"encoding/hex"
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

func TestGuardrailsEnvironmentReplacesOpenAIValues(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=old-key",
		"OPENAI_MODEL=old-model",
	}

	got := guardrailsEnvironment(base, " new-key ", " gpt-5.6-luna ")
	want := []string{
		"PATH=/usr/bin",
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

func TestGuardrailsEnvironmentWithoutExplicitKeyIsUnchanged(t *testing.T) {
	base := []string{"PATH=/usr/bin", "OPENAI_API_KEY=ambient"}
	got := guardrailsEnvironment(base, "", "gpt-5.6-luna")
	if len(got) != len(base) || got[1] != base[1] {
		t.Errorf("environment = %v, want unchanged %v", got, base)
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

func checksumsFile(archiveName string, data []byte) []byte {
	sum := sha256.Sum256(data)
	return []byte(hex.EncodeToString(sum[:]) + "  " + archiveName + "\n")
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
	checksums := checksumsFile(archiveName, archive)

	const version = "v0.1.0-test"
	var requests int
	mux := http.NewServeMux()
	mux.HandleFunc("/guardrails/"+version+"/"+archiveName, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write(archive)
	})
	mux.HandleFunc("/guardrails/"+version+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write(checksums)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)

	// Cache-miss: fetches and installs.
	binPath, err := ensureGuardrailsBinary(server.URL, version)
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
	if requests != 2 {
		t.Fatalf("expected 2 requests (archive + checksums) on cache-miss, got %d", requests)
	}

	// Cache-hit: same version, must not touch the network again.
	if _, err := ensureGuardrailsBinary(server.URL, version); err != nil {
		t.Fatalf("ensureGuardrailsBinary (cache-hit): %v", err)
	}
	if requests != 2 {
		t.Errorf("expected cache-hit to skip the fetch, but requests = %d", requests)
	}

	// Incomplete bundle: a missing sidecar invalidates the cached release.
	if err := os.Remove(scannerPath); err != nil {
		t.Fatalf("remove resource scanner: %v", err)
	}
	if _, err := ensureGuardrailsBinary(server.URL, version); err != nil {
		t.Fatalf("ensureGuardrailsBinary (missing sidecar): %v", err)
	}
	if requests != 4 {
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
	if _, err := ensureGuardrailsBinary(server.URL, version); err != nil {
		t.Fatalf("ensureGuardrailsBinary (post-deletion): %v", err)
	}
	if requests != 6 {
		t.Errorf("expected a fresh fetch (2 more requests) after cache deletion, got %d total", requests)
	}
}

func TestEnsureGuardrailsBinaryChecksumMismatch(t *testing.T) {
	triple, err := guardrailsTargetTriple()
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}
	archiveName := guardrailsArchiveName(triple)
	archive := buildFixtureArchive(t, []byte("irrelevant"), []byte("scanner"))
	badChecksums := []byte("0000000000000000000000000000000000000000000000000000000000000000  " + archiveName + "\n")

	const version = "v0.1.0-badsum"
	mux := http.NewServeMux()
	mux.HandleFunc("/guardrails/"+version+"/"+archiveName, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/guardrails/"+version+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Write(badChecksums)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err = ensureGuardrailsBinary(server.URL, version)
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
	checksums := checksumsFile(archiveName, archive)

	const version = "v0.1.0-missing-scanner"
	mux := http.NewServeMux()
	mux.HandleFunc("/guardrails/"+version+"/"+archiveName, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/guardrails/"+version+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Write(checksums)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("HOME", t.TempDir())
	_, err = ensureGuardrailsBinary(server.URL, version)
	if err == nil {
		t.Fatal("expected invalid archive error, got nil")
	}
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok || cliErr.Code != "INVALID_ARCHIVE" {
		t.Errorf("expected INVALID_ARCHIVE, got %v", err)
	}
}
