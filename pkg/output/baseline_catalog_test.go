package output

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseBaselineCatalogBuildsValidatedIndexes(t *testing.T) {
	raw := syntheticBaselineResult()
	catalog, err := ParseBaselineCatalog(raw)
	if err != nil {
		t.Fatalf("ParseBaselineCatalog() error = %v", err)
	}

	firstRaw := catalog.Raw()
	if firstRaw["preserved_extension"] != "kept" {
		t.Fatal("Raw() dropped an unknown result field")
	}
	firstRaw["preserved_extension"] = "changed"
	if catalog.Raw()["preserved_extension"] != "kept" {
		t.Fatal("Raw() exposed mutable catalog state")
	}
	if catalog.Repo != "example/service" || catalog.FormatVersion != 1 {
		t.Fatalf("identity = (%q, %d)", catalog.Repo, catalog.FormatVersion)
	}
	if catalog.AssetCount() != 7 || catalog.ControlCount() != 4 || catalog.ImplementationCount() != 3 {
		t.Fatalf(
			"counts = assets %d controls %d implementations %d",
			catalog.AssetCount(),
			catalog.ControlCount(),
			catalog.ImplementationCount(),
		)
	}

	field, ok := catalog.Asset("field:user_email")
	if !ok || field.ParentID != "obj:user" {
		t.Fatalf("field = %#v, found = %v", field, ok)
	}
	fields := catalog.FieldsForParent("obj:user")
	if got := baselineAssetIDs(fields); !reflect.DeepEqual(got, []string{"field:user_email", "field:user_name"}) {
		t.Fatalf("fields = %v", got)
	}
	endpoint, ok := catalog.Asset("ep:users")
	if !ok || len(endpoint.Routes) != 3 || endpoint.Routes[0].Line != 10 {
		t.Fatalf("endpoint routes = %#v", endpoint.Routes)
	}
	if endpoint.ParentID != "" || endpoint.Routes[2].Handler != "" {
		t.Fatalf("nullable endpoint fields were not normalized: %#v", endpoint)
	}
	if got := len(catalog.ProtectionsForImplementation("impl:auth")); got != 2 {
		t.Fatalf("auth implementation protections = %d, want 2", got)
	}

	catalog.ApplyRepositoryBlueprint(map[string]any{"repo": map[string]any{"name": "service"}}, "abc123")
	if catalog.BaselineCommit != "abc123" {
		t.Fatalf("commit = %q", catalog.BaselineCommit)
	}
}

func TestParseBaselineCatalogRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]any)
		message string
	}{
		{
			name: "missing required top level field",
			mutate: func(raw map[string]any) {
				delete(raw, "repo")
			},
			message: "missing required fields: repo",
		},
		{
			name: "unsupported version",
			mutate: func(raw map[string]any) {
				raw["format_version"] = float64(2)
			},
			message: "unsupported baseline format version: 2",
		},
		{
			name: "fractional route line",
			mutate: func(raw map[string]any) {
				assets := raw["assets"].([]any)
				routes := assets[0].(map[string]any)["routes"].([]any)
				routes[0].(map[string]any)["line"] = 10.5
			},
			message: "field line must be a non-negative integer",
		},
		{
			name: "invalid protection presence",
			mutate: func(raw map[string]any) {
				protections := raw["protections"].([]any)
				protections[0].(map[string]any)["presence"] = "unknown"
			},
			message: `field presence has unsupported value "unknown"`,
		},
		{
			name: "missing required control field",
			mutate: func(raw map[string]any) {
				controls := raw["controls"].([]any)
				delete(controls[0].(map[string]any), "description")
			},
			message: "control record 0 is missing required field description",
		},
		{
			name: "duplicate implementation id",
			mutate: func(raw map[string]any) {
				implementations := raw["implementations"].([]any)
				implementations[1].(map[string]any)["id"] = "impl:auth"
			},
			message: "implementation id impl:auth is declared more than once",
		},
		{
			name: "unknown field parent",
			mutate: func(raw map[string]any) {
				assets := raw["assets"].([]any)
				assets[4].(map[string]any)["parent"] = "obj:missing"
			},
			message: "references unknown object parent obj:missing",
		},
		{
			name: "unknown protection asset",
			mutate: func(raw map[string]any) {
				protections := raw["protections"].([]any)
				protections[0].(map[string]any)["asset_id"] = "ep:missing"
			},
			message: "references unknown asset ep:missing",
		},
		{
			name: "unknown protection control",
			mutate: func(raw map[string]any) {
				protections := raw["protections"].([]any)
				protections[0].(map[string]any)["control_id"] = "ctrl:missing"
			},
			message: "references unknown control ctrl:missing",
		},
		{
			name: "unknown implementation",
			mutate: func(raw map[string]any) {
				protections := raw["protections"].([]any)
				protections[0].(map[string]any)["implementation_ids"] = []any{"impl:missing"}
			},
			message: "references unknown implementation impl:missing",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := cloneSyntheticBaselineResult(t, syntheticBaselineResult())
			test.mutate(raw)
			_, err := ParseBaselineCatalog(raw)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want message containing %q", err, test.message)
			}
			var catalogErr *BaselineCatalogError
			if !errors.As(err, &catalogErr) {
				t.Fatalf("error type = %T, want *BaselineCatalogError", err)
			}
		})
	}
}

func TestBaselineCatalogReviewableAssetsUseRelationshipSemantics(t *testing.T) {
	catalog := mustSyntheticBaselineCatalog(t)

	counts := catalog.ReviewableAssetCounts()
	wantCounts := map[string]int{"endpoint": 2, "object": 1, "field": 1, "code": 0}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("reviewable counts = %v, want %v", counts, wantCounts)
	}

	endpoints, err := catalog.ReviewableAssets("endpoint")
	if err != nil {
		t.Fatal(err)
	}
	if got := baselineDiscoveryIDs(endpoints); !reflect.DeepEqual(got, []string{"ep:admin", "ep:users"}) {
		t.Fatalf("endpoint order = %v", got)
	}
	objects, err := catalog.ReviewableAssets("object")
	if err != nil {
		t.Fatal(err)
	}
	if got := baselineDiscoveryIDs(objects); !reflect.DeepEqual(got, []string{"obj:user"}) {
		t.Fatalf("objects = %v", got)
	}
	if got := catalog.ReviewControlIDs("obj:user"); !reflect.DeepEqual(got, []string{"ctrl:minimize", "ctrl:tenant"}) {
		t.Fatalf("object review controls = %v", got)
	}
	if got := catalog.AssetDisplayName("field:user_email"); got != "User › email" {
		t.Fatalf("field display name = %q", got)
	}

	fields, err := catalog.ReviewableAssets("field")
	if err != nil {
		t.Fatal(err)
	}
	if got := baselineDiscoveryIDs(fields); !reflect.DeepEqual(got, []string{"field:user_email"}) {
		t.Fatalf("fields = %v", got)
	}
	if fields[0].ControlCount != 1 {
		t.Fatalf("direct field control count = %d", fields[0].ControlCount)
	}
	if _, err := catalog.ReviewableAssets("implementation"); err == nil {
		t.Fatal("ReviewableAssets accepted implementation as an asset kind")
	}
}

func TestBaselineCatalogControlAggregatesDistinguishDetectedAndExpected(t *testing.T) {
	catalog := mustSyntheticBaselineCatalog(t)

	auth, ok := catalog.ControlAggregate("ctrl:auth")
	if !ok {
		t.Fatal("auth aggregate not found")
	}
	if !reflect.DeepEqual(auth.DetectedAssetIDs, []string{"ep:admin", "ep:users"}) {
		t.Fatalf("detected auth assets = %v", auth.DetectedAssetIDs)
	}
	if !reflect.DeepEqual(auth.ImplementationIDs, []string{"impl:auth"}) {
		t.Fatalf("detected auth implementations = %v", auth.ImplementationIDs)
	}
	if auth.PresenceCounts["present"] != 2 || auth.PresenceCounts["absent"] != 1 {
		t.Fatalf("presence counts = %v", auth.PresenceCounts)
	}

	forms := catalog.ImplementationForms(auth)
	if len(forms) != 1 || forms[0].Implementation.ID != "impl:auth" || forms[0].AssetCount() != 2 {
		t.Fatalf("detected forms = %#v", forms)
	}
	allForms := catalog.ControlForms("ctrl:auth")
	if got := baselineImplementationIDs(allForms); !reflect.DeepEqual(got, []string{"impl:auth", "impl:expected"}) {
		t.Fatalf("all control forms = %v", got)
	}

	applications := catalog.ControlApplications("ctrl:auth")
	wantApplications := []BaselineControlApplication{
		{AssetID: "ep:admin", Kind: "endpoint", Name: "Admin routes", Presence: "present"},
		{AssetID: "ep:users", Kind: "endpoint", Name: "User routes", Presence: "absent"},
	}
	if !reflect.DeepEqual(applications, wantApplications) {
		t.Fatalf("applications = %#v, want %#v", applications, wantApplications)
	}

	frequent := catalog.FrequentControls(4)
	if got := baselineAggregateIDs(frequent); !reflect.DeepEqual(
		got,
		[]string{"ctrl:auth", "ctrl:tenant", "ctrl:minimize", "ctrl:unused"},
	) {
		t.Fatalf("frequent controls = %v", got)
	}
	minimize, ok := catalog.ControlAggregate("ctrl:minimize")
	if !ok || minimize.AssetCount() != 0 || minimize.PresenceCounts["absent"] != 1 {
		t.Fatalf("absent aggregate = %#v, found = %v", minimize, ok)
	}
}

func TestBaselineCatalogEndpointDisplayRoutesPreserveDefinitions(t *testing.T) {
	catalog := mustSyntheticBaselineCatalog(t)
	routes := catalog.EndpointDisplayRoutes("ep:users")
	if len(routes) != 2 {
		t.Fatalf("display routes = %#v", routes)
	}
	if routes[0].Method != "GET" || routes[0].Path != "/users" || len(routes[0].Definitions) != 1 {
		t.Fatalf("first display route = %#v", routes[0])
	}
	if routes[1].Method != "POST" || len(routes[1].Definitions) != 2 {
		t.Fatalf("duplicate display route = %#v", routes[1])
	}
	asset, _ := catalog.Asset("ep:users")
	if len(asset.Routes) != 3 {
		t.Fatalf("structured routes were collapsed: %#v", asset.Routes)
	}
}

func syntheticBaselineResult() map[string]any {
	result := map[string]any{
		"format_version": float64(1),
		"repo":           "example/service",
		"source": map[string]any{
			"kind":              "verified-mechanism-catalog",
			"observation_count": float64(8),
		},
		"assets": []any{
			map[string]any{
				"id": "ep:users", "kind": "endpoint", "name": "user routes",
				"decl": "service.go#users", "origin": "resources", "source_ids": []any{"ep:users"}, "parent": nil,
				"routes": []any{
					map[string]any{"method": "GET", "path": "/users", "handler": "listUsers", "decl": "service.go#listUsers", "line": float64(10)},
					map[string]any{"method": "POST", "path": "/users", "handler": "createUser", "decl": "service.go#createUser", "line": float64(20)},
					map[string]any{"method": "POST", "path": "/users", "handler": nil, "decl": "service.go#createUser", "line": float64(21)},
				},
			},
			map[string]any{"id": "ep:admin", "kind": "endpoint", "name": "admin routes", "decl": "admin.go#routes"},
			map[string]any{"id": "ep:empty", "kind": "endpoint", "name": "empty routes"},
			map[string]any{"id": "obj:user", "kind": "object", "name": "User", "decl": "model.go#User"},
			map[string]any{"id": "field:user_email", "kind": "field", "name": "User.email", "decl": "model.go#User.Email", "parent": "obj:user"},
			map[string]any{"id": "field:user_name", "kind": "field", "name": "User.name", "decl": "model.go#User.Name", "parent": "obj:user"},
			map[string]any{"id": "obj:empty", "kind": "object", "name": "Empty"},
		},
		"controls": []any{
			map[string]any{"id": "ctrl:auth", "name": "active authentication", "description": "Requests require a valid credential.", "property": "authentication"},
			map[string]any{"id": "ctrl:minimize", "name": "response minimization", "description": "Responses expose necessary fields only.", "property": "confidentiality"},
			map[string]any{"id": "ctrl:tenant", "name": "tenant scoped access", "description": "Queries remain in the active tenant.", "property": "authorization"},
			map[string]any{"id": "ctrl:unused", "name": "unused control", "property": "integrity"},
		},
		"implementations": []any{
			map[string]any{"id": "impl:auth", "name": "active token dependency", "anchors": []any{map[string]any{"decl": "auth.go#token", "quote": "token.Active"}}},
			map[string]any{"id": "impl:tenant", "name": "tenant query filter", "kind": "code", "anchors": []any{map[string]any{"decl": "repo.go#find", "quote": "TenantID: tenant.ID"}}},
			map[string]any{"id": "impl:expected", "name": "expected response filter", "anchors": []any{map[string]any{"decl": "schema.go#response", "quote": "Email string"}}},
		},
		"protections": []any{
			map[string]any{"id": "prot:users_auth", "asset_id": "ep:users", "control_id": "ctrl:auth", "implementation_ids": []any{"impl:auth"}, "presence": "present"},
			map[string]any{"id": "prot:users_auth_expected", "asset_id": "ep:users", "control_id": "ctrl:auth", "implementation_ids": []any{"impl:expected"}, "presence": "absent"},
			map[string]any{"id": "prot:admin_auth", "asset_id": "ep:admin", "control_id": "ctrl:auth", "implementation_ids": []any{"impl:auth"}, "presence": "present"},
			map[string]any{"id": "prot:admin_tenant", "asset_id": "ep:admin", "control_id": "ctrl:tenant", "implementation_ids": []any{"impl:tenant"}, "presence": "present"},
			map[string]any{"id": "prot:user_tenant", "asset_id": "obj:user", "control_id": "ctrl:tenant", "implementation_ids": []any{"impl:tenant"}, "presence": "partial"},
			map[string]any{"id": "prot:email_minimize", "asset_id": "field:user_email", "control_id": "ctrl:minimize", "implementation_ids": []any{}, "presence": "absent"},
		},
		"unresolved":          []any{},
		"preserved_extension": "kept",
	}
	for _, value := range result["assets"].([]any) {
		record := value.(map[string]any)
		id := record["id"].(string)
		if _, ok := record["decl"]; !ok {
			record["decl"] = "synthetic.go#" + id
		}
		if _, ok := record["origin"]; !ok {
			record["origin"] = "resources"
		}
		if _, ok := record["source_ids"]; !ok {
			record["source_ids"] = []any{id}
		}
		if record["kind"] == "endpoint" {
			if _, ok := record["routes"]; !ok {
				record["routes"] = []any{}
			}
		}
	}
	for _, value := range result["controls"].([]any) {
		record := value.(map[string]any)
		if _, ok := record["description"]; !ok {
			record["description"] = "Synthetic control description."
		}
		record["asvs"] = []any{}
		record["source_observation_ids"] = []any{"mech:synthetic"}
	}
	for _, value := range result["implementations"].([]any) {
		record := value.(map[string]any)
		if _, ok := record["description"]; !ok {
			record["description"] = "Synthetic implementation description."
		}
		if _, ok := record["kind"]; !ok {
			record["kind"] = "code"
		}
		record["source_observation_ids"] = []any{"mech:synthetic"}
	}
	for _, value := range result["protections"].([]any) {
		record := value.(map[string]any)
		record["description"] = "Synthetic protection description."
		record["evidence"] = []any{}
		record["checked"] = []any{}
		record["source_observation_ids"] = []any{"mech:synthetic"}
	}
	return result
}

func cloneSyntheticBaselineResult(t *testing.T, raw map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func mustSyntheticBaselineCatalog(t *testing.T) *BaselineCatalog {
	t.Helper()
	catalog, err := ParseBaselineCatalog(syntheticBaselineResult())
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func baselineAssetIDs(assets []BaselineAsset) []string {
	ids := make([]string, len(assets))
	for index, asset := range assets {
		ids[index] = asset.ID
	}
	return ids
}

func baselineDiscoveryIDs(assets []BaselineDiscovery) []string {
	ids := make([]string, len(assets))
	for index, asset := range assets {
		ids[index] = asset.ID
	}
	return ids
}

func baselineImplementationIDs(forms []BaselineImplementation) []string {
	ids := make([]string, len(forms))
	for index, form := range forms {
		ids[index] = form.ID
	}
	return ids
}

func baselineAggregateIDs(aggregates []BaselineControlAggregate) []string {
	ids := make([]string, len(aggregates))
	for index, aggregate := range aggregates {
		ids[index] = aggregate.Control.ID
	}
	return ids
}
