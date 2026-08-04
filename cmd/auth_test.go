package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/skills"
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
}

// Derived from the bundled inventory, never a list repeated here: repeating it would pass while a
// newly bundled skill went unmentioned, which is the regression this is meant to catch.
func TestEveryBundledSkillIsPitchedOrMarkedSupport(t *testing.T) {
	var pitched int
	for _, sd := range skills.SkillDirs() {
		switch {
		case sd.Support && len(sd.Pitch) > 0:
			t.Errorf("%s is marked support yet carries a pitch", sd.InstallName)
		case sd.Support:
			continue
		case len(sd.Pitch) == 0:
			t.Errorf("%s is bundled but says nothing in the offer, so it installs unannounced",
				sd.InstallName)
		default:
			pitched++
			if !strings.Contains(strings.Join(sd.Pitch, "\n"), sd.InstallName) {
				t.Errorf("%s's pitch never names how to run it", sd.InstallName)
			}
		}
	}
	if pitched == 0 {
		t.Fatal("no skill is pitched, so this asserts nothing")
	}
}
