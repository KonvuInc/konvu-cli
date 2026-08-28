package baseline

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func TestCatalogIndexesEveryProducerCollection(t *testing.T) {
	catalog, err := NewCatalog(canonicalDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := map[Collection]int{
		CollectionClasses:             1,
		CollectionRoutes:              1,
		CollectionResources:           3,
		CollectionRoles:               1,
		CollectionAssetObservations:   1,
		CollectionControlObservations: 2,
		CollectionAssets:              4,
		CollectionControls:            1,
		CollectionImplementations:     1,
		CollectionUnresolved:          1,
	}
	wantCollections := []Collection{
		CollectionClasses,
		CollectionRoutes,
		CollectionResources,
		CollectionRoles,
		CollectionAssetObservations,
		CollectionControlObservations,
		CollectionAssets,
		CollectionControls,
		CollectionImplementations,
		CollectionUnresolved,
	}
	if got := catalog.Collections(); !reflect.DeepEqual(got, wantCollections) {
		t.Fatalf("collections = %v, want %v", got, wantCollections)
	}
	for _, collection := range wantCollections {
		want := wantCounts[collection]
		entities, err := catalog.Entities(collection)
		if err != nil {
			t.Errorf("Entities(%q) error = %v", collection, err)
			continue
		}
		if len(entities) != want {
			t.Errorf("Entities(%q) count = %d, want %d", collection, len(entities), want)
		}
	}

	assets, err := catalog.Entities(CollectionAssets)
	if err != nil {
		t.Fatal(err)
	}
	wantAssets := []string{
		"asset:code:audit-log",
		"asset:endpoint:accounts",
		"asset:field:account.owner_id",
		"asset:object:account",
	}
	if got := entityIDs(assets); !reflect.DeepEqual(got, wantAssets) {
		t.Fatalf("assets = %v, want %v", got, wantAssets)
	}

	asset, ok := catalog.Lookup("asset:code:audit-log")
	if !ok || asset.Collection != CollectionAssets || asset.Value["origin"] != "controls" {
		t.Fatalf("normalized residual Asset lookup = %#v, found = %v", asset, ok)
	}
	observation, ok := catalog.LookupIn(CollectionAssetObservations, "asset:code:audit-log")
	if !ok || observation.Collection != CollectionAssetObservations || observation.Value["origin"] != nil {
		t.Fatalf("qualified Asset observation lookup = %#v, found = %v", observation, ok)
	}
	unresolved, ok := catalog.LookupIn(
		CollectionUnresolved,
		"control-observation:audit-retention",
	)
	if !ok || unresolved.Collection != CollectionUnresolved {
		t.Fatalf("qualified unresolved lookup = %#v, found = %v", unresolved, ok)
	}
	if _, ok := catalog.Lookup("missing"); ok {
		t.Fatal("Lookup found missing id")
	}
	if _, err := catalog.Entities("unknown"); err == nil {
		t.Fatal("Entities accepted an unknown collection")
	}
}

func TestCatalogIndexesControlLinksAndProducerReverseRelationships(t *testing.T) {
	catalog, err := NewCatalog(canonicalDocument(t))
	if err != nil {
		t.Fatal(err)
	}

	links := catalog.LinksForAsset("asset:endpoint:accounts")
	if len(links) != 1 || links[0].ControlID != "control:account-owner" ||
		links[0].Status != "present" ||
		!reflect.DeepEqual(links[0].ImplementationIDs, []string{"implementation:account-owner"}) ||
		!reflect.DeepEqual(
			links[0].SourceControlObservationIDs,
			[]string{"control-observation:account-owner"},
		) {
		t.Fatalf("endpoint Control links = %#v", links)
	}
	if len(catalog.LinksForControl("control:account-owner")) != 1 ||
		len(catalog.LinksForImplementation("implementation:account-owner")) != 1 ||
		len(catalog.LinksForObservation("control-observation:account-owner")) != 1 {
		t.Fatal("reverse Control-link indexes are incomplete")
	}

	assertRelatedBothWays(
		t,
		catalog,
		CollectionRoutes,
		"route:app/routes.py-5-get-/accounts-list_accounts",
		CollectionAssets,
		"asset:endpoint:accounts",
	)
	assertRelatedBothWays(
		t,
		catalog,
		CollectionClasses,
		"class:app/models.py-3-account",
		CollectionResources,
		"resource:object:account",
	)
	for _, pair := range [][2]string{
		{"asset:endpoint:accounts", "resource:endpoint:accounts"},
		{"asset:field:account.owner_id", "resource:field:account.owner_id"},
		{"asset:object:account", "resource:object:account"},
	} {
		assertRelatedBothWays(t, catalog, CollectionAssets, pair[0], CollectionResources, pair[1])
	}
	assertRelatedBothWays(
		t,
		catalog,
		CollectionAssets,
		"asset:field:account.owner_id",
		CollectionAssets,
		"asset:object:account",
	)
	assertRelatedBothWays(
		t,
		catalog,
		CollectionAssetObservations,
		"asset:code:audit-log",
		CollectionAssets,
		"asset:code:audit-log",
	)
	assertRelatedBothWays(
		t,
		catalog,
		CollectionControlObservations,
		"control-observation:audit-retention",
		CollectionUnresolved,
		"control-observation:audit-retention",
	)
	assertRelatedBothWays(
		t,
		catalog,
		CollectionControlObservations,
		"control-observation:audit-retention",
		CollectionAssets,
		"asset:code:audit-log",
	)
	for _, target := range []struct {
		collection Collection
		id         string
	}{
		{CollectionAssets, "asset:endpoint:accounts"},
		{CollectionControls, "control:account-owner"},
		{CollectionImplementations, "implementation:account-owner"},
	} {
		assertRelatedBothWays(
			t,
			catalog,
			CollectionControlObservations,
			"control-observation:account-owner",
			target.collection,
			target.id,
		)
	}
}

func TestCatalogResultsAreIndependentCopies(t *testing.T) {
	catalog, err := NewCatalog(canonicalDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	entity, _ := catalog.Lookup("control:account-owner")
	entity.Value["name"] = "changed"
	again, _ := catalog.Lookup("control:account-owner")
	if again.Value["name"] == "changed" {
		t.Fatal("Lookup exposed mutable catalog state")
	}
	links := catalog.LinksForAsset("asset:endpoint:accounts")
	links[0].ImplementationIDs[0] = "implementation:changed"
	links[0].Value["status"] = "absent"
	againLinks := catalog.LinksForAsset("asset:endpoint:accounts")
	if againLinks[0].ImplementationIDs[0] == "implementation:changed" ||
		againLinks[0].Value["status"] == "absent" {
		t.Fatal("link accessor exposed mutable catalog state")
	}
}

func TestCatalogExcludesAbsentControlsWithoutMutatingDocument(t *testing.T) {
	document := canonicalDocument(t)
	raw := document.Raw()
	assets := raw["assets"].([]any)
	endpoint := assets[1].(map[string]any)
	links := endpoint["controls"].([]any)
	links[0].(map[string]any)["status"] = "absent"
	observations := raw["observations"].(map[string]any)["controls"].([]any)
	observations[0].(map[string]any)["status"] = "absent"
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	document, err = Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}

	catalog, err := NewCatalog(document)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Counts(); got.Controls != 0 || got.Implementations != 0 || got.ControlObservations != 1 {
		t.Fatalf("visible counts = %#v", got)
	}
	if links := catalog.LinksForAsset("asset:endpoint:accounts"); len(links) != 0 {
		t.Fatalf("absent links remained queryable: %#v", links)
	}
	for _, target := range []struct {
		collection Collection
		id         string
	}{
		{CollectionControls, "control:account-owner"},
		{CollectionImplementations, "implementation:account-owner"},
		{CollectionControlObservations, "control-observation:account-owner"},
	} {
		if _, found := catalog.LookupIn(target.collection, target.id); found {
			t.Errorf("absent %s remained queryable in %s", target.id, target.collection)
		}
	}
	visibleRaw := catalog.Raw()
	visibleAssets := visibleRaw["assets"].([]any)
	visibleLinks := visibleAssets[1].(map[string]any)["controls"].([]any)
	if len(visibleLinks) != 0 {
		t.Fatalf("query view retained absent links: %#v", visibleLinks)
	}
	originalAssets := document.Raw()["assets"].([]any)
	originalLinks := originalAssets[1].(map[string]any)["controls"].([]any)
	if len(originalLinks) != 1 || originalLinks[0].(map[string]any)["status"] != "absent" {
		t.Fatalf("stored document projection was mutated: %#v", originalLinks)
	}
}

func entityIDs(entities []Entity) []string {
	result := make([]string, len(entities))
	for index, entity := range entities {
		result[index] = entity.ID
	}
	return result
}

func entityKeys(entities []Entity) []string {
	result := make([]string, len(entities))
	for index, entity := range entities {
		result[index] = string(entity.Collection) + ":" + entity.ID
	}
	sort.Strings(result)
	return result
}

func assertRelatedBothWays(
	t *testing.T,
	catalog *Catalog,
	leftCollection Collection,
	leftID string,
	rightCollection Collection,
	rightID string,
) {
	t.Helper()
	assertRelatedContains(
		t,
		catalog.RelatedIn(leftCollection, leftID),
		string(rightCollection)+":"+rightID,
	)
	assertRelatedContains(
		t,
		catalog.RelatedIn(rightCollection, rightID),
		string(leftCollection)+":"+leftID,
	)
}

func assertRelatedContains(t *testing.T, entities []Entity, expected string) {
	t.Helper()
	for _, key := range entityKeys(entities) {
		if key == expected {
			return
		}
	}
	t.Fatalf("related = %v, missing %q", entityKeys(entities), expected)
}
