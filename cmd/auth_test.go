package cmd

import (
	"os"
	"strings"
	"testing"
)

// Both login paths pitch the same skills. They used to hold a byte-identical copy each, so a
// skill added to one was invisible to whoever used the other; one function is what keeps them iso.
func TestOneSkillPitchForBothLoginPaths(t *testing.T) {
	src, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatalf("read auth.go: %v", err)
	}
	if n := strings.Count(string(src), "offerSkills()"); n != 3 { // 1 definition + 2 call sites
		t.Errorf("offerSkills appears %d times, want 1 definition and 2 call sites", n)
	}
	// Every bundled skill has to be named, or a user is asked to install something unannounced.
	for _, want := range []string{"/konvu-recipe-weekly-triage", "/konvu-guardrails-onboarding"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("the skills pitch never mentions %s", want)
		}
	}
}
