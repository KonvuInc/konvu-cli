package cmd

import (
	"reflect"
	"testing"
)

func TestTierLabel(t *testing.T) {
	cases := map[string]string{
		"crown_jewel": "Crown jewel",
		"key_asset":   "Key asset",
		"standard":    "Standard",
		"peripheral":  "Peripheral",
		"":            "",
	}
	for slug, want := range cases {
		if got := tierLabel(slug); got != want {
			t.Errorf("tierLabel(%q) = %q, want %q", slug, got, want)
		}
	}
}

func TestOrderedTierSlugs(t *testing.T) {
	// Canonical tiers come high-to-low regardless of map order; unknown slugs
	// follow, sorted alphabetically.
	tiers := map[string]any{
		"peripheral":  1.0,
		"crown_jewel": 2.0,
		"standard":    3.0,
		"zzz_unknown": 4.0,
		"aaa_unknown": 5.0,
	}
	got := orderedTierSlugs(tiers)
	want := []string{"crown_jewel", "standard", "peripheral", "aaa_unknown", "zzz_unknown"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("orderedTierSlugs = %v, want %v", got, want)
	}
}

func TestRepoTierText(t *testing.T) {
	cases := []struct {
		name string
		m    map[string]any
		want string
	}{
		{"label preferred", map[string]any{"threat_profile_tier_label": "Crown Jewel", "threat_profile_tier": "crown_jewel"}, "Crown Jewel"},
		{"slug fallback", map[string]any{"threat_profile_tier": "key_asset"}, "Key asset"},
		{"unscored", map[string]any{}, "unscored"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := repoTierText(c.m); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestValueDisplay(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "—"},
		{true, "yes"},
		{false, "no"},
		{"api-server", "api-server"},
		{[]any{"a.com", "b.com"}, "a.com, b.com"},
		{float64(42), "42"},
		{float64(3.5), "3.5"},
	}
	for _, c := range cases {
		if got := valueDisplay(c.in); got != c.want {
			t.Errorf("valueDisplay(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestScoreDisplay(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "—"},
		{float64(87), "87"},
		{float64(0), "0"},
	}
	for _, c := range cases {
		if got := scoreDisplay(c.in); got != c.want {
			t.Errorf("scoreDisplay(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBoolDisplay(t *testing.T) {
	// A missing/unknown headline boolean renders as an em dash, not "no".
	cases := []struct {
		in   any
		want string
	}{
		{true, "yes"},
		{false, "no"},
		{nil, "—"},
		{"true", "—"},
	}
	for _, c := range cases {
		if got := boolDisplay(c.in); got != c.want {
			t.Errorf("boolDisplay(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestConfidenceDisplay(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.9, "0.9"},
		{0.75, "0.75"},
		{1.0, "1"},
		{0.5, "0.5"},
	}
	for _, c := range cases {
		if got := confidenceDisplay(c.in); got != c.want {
			t.Errorf("confidenceDisplay(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInventoryQuietLines(t *testing.T) {
	top := []any{
		map[string]any{"vcs_repository_id": "id-a", "threat_profile_tier": "crown_jewel"},
		map[string]any{"vcs_repository_id": "id-b", "threat_profile_tier": "key_asset"},
		map[string]any{"vcs_repository_id": "id-c", "threat_profile_tier": ""}, // no tier -> "unscored"
		"not-a-map", // skipped
	}
	got := inventoryQuietLines(top)
	want := "id-a\tcrown_jewel\nid-b\tkey_asset\nid-c\tunscored"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSplitFields(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ,, c ", []string{"a", "b", "c"}}, // trims blanks and empties
		{"", nil},
		{" , ", nil},
	}
	for _, c := range cases {
		got := splitFields(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitFields(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIntOf(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{float64(5), 5},
		{int(3), 3},
		{int64(7), 7},
		{nil, 0},
		{"x", 0},
	}
	for _, c := range cases {
		if got := intOf(c.in); got != c.want {
			t.Errorf("intOf(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
