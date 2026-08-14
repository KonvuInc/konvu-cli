package cmd

import (
	"strings"
	"testing"
)

func TestBriefTierNote(t *testing.T) {
	if n := briefTierNote(map[string]any{"enrichment_status": "succeeded"}); !strings.Contains(n, "full plan") {
		t.Errorf("succeeded: got %q", n)
	}
	if n := briefTierNote(map[string]any{"enrichment_status": "partial"}); !strings.Contains(n, "partial plan") {
		t.Errorf("partial: got %q", n)
	}
	for _, s := range []string{"failed", "ongoing", ""} {
		if n := briefTierNote(map[string]any{"enrichment_status": s}); n != "" {
			t.Errorf("status %q: want empty note, got %q", s, n)
		}
	}
}

func TestRemediateHasBriefSubcommand(t *testing.T) {
	found := false
	for _, c := range remediateCmd.Commands() {
		if c.Name() == "brief" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("remediate command missing subcommand: brief")
	}
}

func TestRemediateBriefFlags(t *testing.T) {
	if remediateBriefCmd.Flags().Lookup("output") == nil {
		t.Error("remediate brief missing flag: --output")
	}
}

func TestAgentPrompts(t *testing.T) {
	briefs := []map[string]any{
		{"agent_prompt": "prompt one", "id": "a"},
		{"id": "b"}, // no prompt — skipped
		{"agent_prompt": "prompt two", "id": "c"},
	}
	prompts := agentPrompts(briefs)
	if len(prompts) != 2 || prompts[0] != "prompt one" || prompts[1] != "prompt two" {
		t.Errorf("unexpected prompts: %v", prompts)
	}
}
