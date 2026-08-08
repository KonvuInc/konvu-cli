package output

import "testing"

func TestVisibleLen(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"ascii", "score", 5},
		{"em dash is one cell", "—", 1},
		{"mixed ascii + em dash", "a—b", 3},
		{"ansi stripped", "\033[31mred\033[0m", 3},
		{"cjk is two cells", "日本語", 6},            // 日本語
		{"emoji is two cells", "\U0001F680", 2},   // 🚀
		{"combining mark is zero width", "é", 1}, // e + combining acute
		{"zero-width joiner", "a‍b", 2},           // a ZWJ b
	}
	for _, c := range cases {
		if got := visibleLen(c.in); got != c.want {
			t.Errorf("%s: visibleLen(%q) = %d, want %d", c.name, c.in, got, c.want)
		}
	}
}
