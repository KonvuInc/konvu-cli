package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsPath(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"skills", "path"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("skills path failed: %v", err)
	}

	got := strings.TrimSpace(buf.String())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot get home dir: %v", err)
	}
	want := filepath.Join(home, ".agents", "skills")
	if got != want {
		t.Errorf("skills path = %q, want %q", got, want)
	}
}
