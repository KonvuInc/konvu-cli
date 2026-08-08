package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

func makeTarGz(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}
	return buf.Bytes()
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("hello konvu")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	filename := "konvu-darwin-arm64.tar.gz"
	checksums := []byte(hash + "  " + filename + "\n" + "deadbeef  konvu-linux-amd64.tar.gz\n")

	if err := verifyChecksum(data, checksums, filename); err != nil {
		t.Errorf("expected checksum to match, got %v", err)
	}
}

func TestVerifyChecksumMismatch(t *testing.T) {
	data := []byte("hello konvu")
	filename := "konvu-darwin-arm64.tar.gz"
	checksums := []byte("0000000000000000000000000000000000000000000000000000000000000000  " + filename + "\n")

	err := verifyChecksum(data, checksums, filename)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok || cliErr.Code != "CHECKSUM_MISMATCH" {
		t.Errorf("expected CHECKSUM_MISMATCH, got %v", err)
	}
}

func TestVerifyChecksumMissing(t *testing.T) {
	data := []byte("hello konvu")
	checksums := []byte("deadbeef  some-other-file.tar.gz\n")

	err := verifyChecksum(data, checksums, "konvu-darwin-arm64.tar.gz")
	if err == nil {
		t.Fatal("expected missing checksum error, got nil")
	}
	cliErr, ok := err.(*clierrors.CLIError)
	if !ok || cliErr.Code != "CHECKSUM_MISSING" {
		t.Errorf("expected CHECKSUM_MISSING, got %v", err)
	}
}

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.2.3", "1.2.2", true},           // patch bump
		{"1.10.0", "1.9.0", true},          // numeric, not lexical
		{"2.0.0", "1.9.9", true},           // major bump
		{"1.2.3", "1.2.3", false},          // equal
		{"1.2.2", "1.2.3", false},          // older release — never downgrade
		{"1.2.0", "1.2.3", false},          // installed ahead
		{"1.2.3", "1.2.3-rc.1", true},      // stable supersedes its own pre-release
		{"1.2.3-rc.1", "1.2.3", false},     // pre-release never supersedes stable
		{"1.2.3-rc.2", "1.2.3-rc.1", true}, // later pre-release, numeric compare
		{"1.2.4", "1.2.3-rc1", true},       // newer core wins over a pre-release
		{"1.2.3+build5", "1.2.3", false},   // build metadata ignored for precedence
		{"1.2.3", "bogus", false},          // unparseable current
		{"", "1.2.3", false},               // empty release
	}
	for _, c := range cases {
		if got := isNewerVersion(c.latest, c.current); got != c.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestExtractBinary(t *testing.T) {
	want := []byte("\x7fELF fake binary contents")
	archive := makeTarGz(t, map[string][]byte{
		"README.md": []byte("docs"),
		"konvu":     want,
	})

	got, err := extractBinary(archive)
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("extracted binary = %q, want %q", got, want)
	}
}

func TestExtractBinaryMissing(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{
		"README.md": []byte("docs"),
	})

	if _, err := extractBinary(archive); err == nil {
		t.Fatal("expected error when konvu binary is absent, got nil")
	}
}
