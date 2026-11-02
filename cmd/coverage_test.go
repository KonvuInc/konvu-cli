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

func TestParseSeverities(t *testing.T) {
	// all -> nil value (JSON null = every severity), no error
	if v, err := parseSeverities([]string{"All"}); err != nil || v != nil {
		t.Errorf("all: got (%v, %v), want (nil, nil)", v, err)
	}

	v, err := parseSeverities([]string{"high", "medium"})
	if err != nil {
		t.Fatalf("list: unexpected error: %v", err)
	}
	if !reflect.DeepEqual(v, []string{"HIGH", "MODERATE"}) {
		t.Errorf("list: got %v, want [HIGH MODERATE]", v)
	}

	// all mixed with a severity is not a severity
	if _, err := parseSeverities([]string{"all", "high"}); err == nil {
		t.Error("expected error for all mixed with severities")
	}
}

func TestScopeRows(t *testing.T) {
	scope := func(id, step, product, name string, sevs any, repos ...any) any {
		return map[string]any{"id": id, "step": step, "product": product, "name": name, "severities": sevs, "repository_ids": repos}
	}
	rows := groupScopes([]any{
		scope("1", "assessment", "sca", "Prod", []any{"CRITICAL"}, "r1"),
		scope("2", "remediation", "sca", "Prod", nil, "r1", "r2"),
		scope("3", "assessment", "sast", "Prod", nil, "r2"),
		scope("4", "prevent", "prevent", "Prod", nil, "r3"),
	})

	// One row per product and name; Prevent is not a Coverage row.
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	// Members the tenant no longer lists are not counted, as on the dashboard.
	known := map[string]bool{"r1": true}
	cells := rows[0].cells(known)
	want := map[string]any{"scope": "Prod", "product": "SCA", "analysis": "Critical", "reproduction": "off", "remediation": "on", "repositories": 1}
	if !reflect.DeepEqual(cells, want) {
		t.Errorf("cells = %v, want %v", cells, want)
	}
	// Each task lists its own repositories; the row lists them all.
	known["r2"] = true
	got := rows[0].json(known)
	if !reflect.DeepEqual(got["repository_ids"], []string{"r1", "r2"}) {
		t.Errorf("json repository_ids = %v, want [r1 r2]", got["repository_ids"])
	}
	analysis := got["tasks"].(map[string]any)["analysis"].(map[string]any)
	if !reflect.DeepEqual(analysis["repository_ids"], []string{"r1"}) {
		t.Errorf("analysis repository_ids = %v, want [r1]", analysis["repository_ids"])
	}

	// A name both products use needs --product.
	if _, err := findScopeRow(rows, "prod", ""); err == nil || err.Code != "AMBIGUOUS_SCOPE" {
		t.Errorf("expected AMBIGUOUS_SCOPE, got %v", err)
	}
	row, err := findScopeRow(rows, "prod", "sast")
	if err != nil || row.product != "sast" {
		t.Errorf("got (%v, %v), want the sast row", row, err)
	}
	if _, err := findScopeRow(rows, "nope", ""); err == nil || err.Code != "SCOPE_NOT_FOUND" {
		t.Errorf("expected SCOPE_NOT_FOUND, got %v", err)
	}

	if got := rowsWithRepo(rows, "r2"); len(got) != 2 {
		t.Errorf("rowsWithRepo(r2) = %d rows, want 2", len(got))
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
