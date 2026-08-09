package findings

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newRenderCmd(buf *bytes.Buffer) *cobra.Command {
	c := &cobra.Command{Use: "test"}
	c.SetOut(buf)
	RegisterCommonFlags(c)
	return c
}

var testRows = []Row{
	{"id": "a", "severity": "high", "title": "one"},
	{"id": "b", "severity": "low", "title": "two"},
}

func TestRender_JSON(t *testing.T) {
	var buf bytes.Buffer
	c := newRenderCmd(&buf)
	c.Flags().Set("output", "json")
	if err := Render(c, testRows, []string{"id", "severity"}); err != nil {
		t.Fatalf("render: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("bad JSON: %v (%q)", err, buf.String())
	}
	if len(got) != 2 || got[0]["id"] != "a" {
		t.Fatalf("json mismatch: %v", got)
	}
}

func TestRender_Table(t *testing.T) {
	var buf bytes.Buffer
	c := newRenderCmd(&buf)
	c.Flags().Set("output", "table")
	if err := Render(c, testRows, []string{"id", "severity"}); err != nil {
		t.Fatalf("render: %v", err)
	}
	// pkg/output.FormatTable title-cases headers ("Id", "Severity").
	out := strings.ToLower(buf.String())
	if !strings.Contains(out, "id") || !strings.Contains(out, "severity") {
		t.Fatalf("table missing headers: %q", buf.String())
	}
	if !strings.Contains(out, "high") || !strings.Contains(out, "low") {
		t.Fatalf("table missing rows: %q", buf.String())
	}
}

func TestRenderBareIDs(t *testing.T) {
	var buf bytes.Buffer
	c := newRenderCmd(&buf)
	if err := RenderBareIDs(c, testRows, "id"); err != nil {
		t.Fatalf("render: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "a\nb" {
		t.Fatalf("bare IDs: %q", got)
	}
}
