package output

import (
	"strings"
	"testing"
)

func TestFallbackPick(t *testing.T) {
	input := strings.NewReader("2\n")
	idx := FallbackPick("Choose:", []string{"Option A", "Option B"}, 0, input)
	if idx != 1 {
		t.Errorf("FallbackPick = %d, want 1", idx)
	}
}

func TestFallbackPick_Default(t *testing.T) {
	input := strings.NewReader("\n")
	idx := FallbackPick("Choose:", []string{"A", "B"}, 0, input)
	if idx != 0 {
		t.Errorf("FallbackPick default = %d, want 0", idx)
	}
}
