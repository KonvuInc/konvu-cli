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

func TestConfirmation(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		defaultYes bool
		want       bool
	}{
		{name: "typed yes", input: "y", want: true},
		{name: "pasted yes", input: "\x1b[200~y\x1b[201~", want: true},
		{name: "typed no", input: "n", defaultYes: true, want: false},
		{name: "default no", input: "", want: false},
		{name: "default yes", input: "", defaultYes: true, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := confirmation(test.input, test.defaultYes); got != test.want {
				t.Errorf("confirmation() = %t, want %t", got, test.want)
			}
		})
	}
}
