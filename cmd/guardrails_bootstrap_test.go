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
	// Confirms archives are named from the Cargo *package* name
	// ("guardrails-cli"), not the [[bin]] name ("guardrails").
	got := guardrailsArchiveName("aarch64-apple-darwin")
	want := "guardrails-cli-aarch64-apple-darwin.tar.xz"
	if got != want {
		t.Errorf("guardrailsArchiveName() = %q, want %q", got, want)
	}
}

func TestWriteGuardrailsCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := guardrailsCredentialsPath()
	if err != nil {
		t.Fatalf("guardrailsCredentialsPath: %v", err)
	}

	// Pre-seed a stale Azure-shaped file. The provider on the Rust side is
	// picked by presence of an "endpoint" line, so a leftover one here would
	// silently force Azure mode -- writeGuardrailsCredentials must fully
	// overwrite, not merge/upsert, to guard against that.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("endpoint = https://stale.example.com\nkey = old\n"), 0o600); err != nil {
		t.Fatalf("seed WriteFile: %v", err)
	}

	if err := writeGuardrailsCredentials("sk-test", "gpt-4o"); err != nil {
		t.Fatalf("writeGuardrailsCredentials: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "key = sk-test\nmodel = gpt-4o\n"
	if string(got) != want {
		t.Errorf("credentials file = %q, want %q", got, want)
	}
}

// buildFixtureArchive shells out to the system tar to produce a real
// tar.xz containing a single "guardrails" file, since Go's stdlib has no xz
// encoder either. Skips (not fails) if the local tar lacks xz support.
func buildFixtureArchive(t *testing.T, contents []byte) []byte {
	t.Helper()
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not found on PATH")
	}

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "guardrails"), contents, 0o755); err != nil {
		t.Fatalf("write fixture binary: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "fixture.tar.xz")
	cmd := exec.Command("tar", "-cJf", archivePath, "-C", src, "guardrails")
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
	archive := buildFixtureArchive(t, binaryContents)
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
	if requests != 4 {
		t.Errorf("expected a fresh fetch (2 more requests) after cache deletion, got %d total", requests)
	}
}

func TestEnsureGuardrailsBinaryChecksumMismatch(t *testing.T) {
	triple, err := guardrailsTargetTriple()
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}
	archiveName := guardrailsArchiveName(triple)
	archive := buildFixtureArchive(t, []byte("irrelevant"))
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
