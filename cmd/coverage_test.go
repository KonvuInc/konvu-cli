package cmd

import (
	"reflect"
	"testing"
)

func coverageRepoFixture() []any {
	return []any{
		map[string]any{"id": "id-a", "url": "github:org/alpha"},
		map[string]any{"id": "id-b", "url": "github:org/beta"},
		map[string]any{"id": "id-c", "url": "gitlab:org/alpha-tools"},
	}
}

func TestResolveRepoIDs(t *testing.T) {
	repos := coverageRepoFixture()
	cases := []struct {
		name string
		args []string
		want []string
		err  bool
	}{
		{"by id", []string{"id-b"}, []string{"id-b"}, false},
		{"by exact url", []string{"github:org/beta"}, []string{"id-b"}, false},
		{"by unique substring", []string{"beta"}, []string{"id-b"}, false},
		{"multiple", []string{"id-a", "github:org/beta"}, []string{"id-a", "id-b"}, false},
		{"dedup", []string{"id-a", "github:org/alpha"}, []string{"id-a"}, false},
		{"ambiguous substring", []string{"alpha"}, nil, true},
		{"missing", []string{"nope"}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveRepoIDs(repos, c.args)
			if c.err {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestNormalizeSeverities(t *testing.T) {
	got, err := normalizeSeverities([]string{"critical", "Medium", "HIGH", "medium"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"CRITICAL", "MODERATE", "HIGH"} // upper, MEDIUM->MODERATE, dedup, order kept
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	if _, err := normalizeSeverities([]string{"bogus"}); err == nil {
		t.Error("expected error for invalid severity")
	}
	if _, err := normalizeSeverities(nil); err == nil {
		t.Error("expected error for empty input")
	}
}

func TestResolveSeverityValue(t *testing.T) {
	// --all -> nil value (JSON null = all), no error
	if v, err := resolveSeverityValue(nil, true); err != nil || v != nil {
		t.Errorf("all: got (%v, %v), want (nil, nil)", v, err)
	}
	// --set -> normalized list
	v, err := resolveSeverityValue([]string{"high"}, false)
	if err != nil {
		t.Fatalf("set: unexpected error: %v", err)
	}
	if !reflect.DeepEqual(v, []string{"HIGH"}) {
		t.Errorf("set: got %v, want [HIGH]", v)
	}
	// neither -> error (never send empty [])
	if _, err := resolveSeverityValue(nil, false); err == nil {
		t.Error("expected error when neither --set nor --all given")
	}
	// both -> error
	if _, err := resolveSeverityValue([]string{"high"}, true); err == nil {
		t.Error("expected error when both --set and --all given")
	}
}

func TestSeveritiesDisplay(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "all"},
		{[]any{}, "all"},
		{[]any{"CRITICAL", "MODERATE"}, "Critical, Medium"},
		{[]string{"LOW"}, "Low"},
	}
	for _, c := range cases {
		if got := severitiesDisplay(c.in); got != c.want {
			t.Errorf("severitiesDisplay(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
