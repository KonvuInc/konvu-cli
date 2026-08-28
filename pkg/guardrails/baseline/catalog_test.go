package baseline

import (
	"reflect"
	"sort"
	"testing"
)

func TestCatalogIndexesCollectionsAndRelationships(t *testing.T) {
	catalog, err := NewCatalog(canonicalDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	if !containsCollection(catalog.Collections(), CollectionAssetObservations) {
		t.Fatal("Asset observations must remain listable")
	}
	assetObservations, err := catalog.Entities(CollectionAssetObservations)
	if err != nil || len(assetObservations) != 1 || assetObservations[0].ID != "asset:user" {
		t.Fatalf("asset observations = %#v, error = %v", assetObservations, err)
	}
	assets, err := catalog.Entities(CollectionAssets)
	if err != nil {
		t.Fatal(err)
	}
	if got := entityIDs(assets); !reflect.DeepEqual(got, []string{"asset:user", "asset:user-email"}) {
		t.Fatalf("assets = %v", got)
	}
	unresolved, err := catalog.Entities(CollectionUnresolved)
	if err != nil || len(unresolved) != 1 ||
		unresolved[0].ID != "control-observation:rate-limit-user-read" {
		t.Fatalf("unresolved = %#v, error = %v", unresolved, err)
	}
	asset, ok := catalog.Lookup("asset:user")
	if !ok || asset.Collection != CollectionAssets || asset.Value["controls"] == nil {
		t.Fatalf("normalized asset lookup = %#v, found = %v", asset, ok)
	}
	if _, ok := catalog.Lookup("missing"); ok {
		t.Fatal("Lookup found missing id")
	}

	links := catalog.LinksForAsset("asset:user")
	if len(links) != 1 || links[0].ControlID != "control:authorize-user-read" ||
		links[0].Status != "present" {
		t.Fatalf("asset links = %#v", links)
	}
	if len(catalog.LinksForControl("control:authorize-user-read")) != 1 ||
		len(catalog.LinksForImplementation("implementation:authorize-user-read")) != 1 ||
		len(catalog.LinksForObservation("control-observation:authorize-user-read")) != 1 {
		t.Fatal("reverse relationship indexes are incomplete")
	}

	wantRelated := []string{
		"assets:asset:user-email",
		"control-observations:control-observation:authorize-user-read",
		"controls:control:authorize-user-read",
		"implementations:implementation:authorize-user-read",
		"resources:resource:user",
	}
	if got := entityKeys(catalog.Related("asset:user")); !reflect.DeepEqual(got, wantRelated) {
		t.Fatalf("asset related = %v, want %v", got, wantRelated)
	}
	wantControlRelated := []string{
		"assets:asset:user",
		"control-observations:control-observation:authorize-user-read",
		"implementations:implementation:authorize-user-read",
	}
	if got := entityKeys(catalog.Related("control:authorize-user-read")); !reflect.DeepEqual(got, wantControlRelated) {
		t.Fatalf("control related = %v, want %v", got, wantControlRelated)
	}
	wantResourceRelated := []string{
		"assets:asset:user",
		"assets:asset:user-email",
		"control-observations:control-observation:rate-limit-user-read",
	}
	if got := entityKeys(catalog.Related("resource:user")); !reflect.DeepEqual(got, wantResourceRelated) {
		t.Fatalf("resource related = %v, want %v", got, wantResourceRelated)
	}
}

func TestCatalogResultsAreIndependentCopies(t *testing.T) {
	catalog, err := NewCatalog(canonicalDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	entity, _ := catalog.Lookup("control:authorize-user-read")
	entity.Value["name"] = "changed"
	again, _ := catalog.Lookup("control:authorize-user-read")
	if again.Value["name"] == "changed" {
		t.Fatal("Lookup exposed mutable catalog state")
	}
	links := catalog.LinksForAsset("asset:user")
	links[0].ImplementationIDs[0] = "implementation:changed"
	links[0].Value["status"] = "absent"
	againLinks := catalog.LinksForAsset("asset:user")
	if againLinks[0].ImplementationIDs[0] == "implementation:changed" ||
		againLinks[0].Value["status"] == "absent" {
		t.Fatal("link accessor exposed mutable catalog state")
	}
	if _, err := catalog.Entities("unknown"); err == nil {
		t.Fatal("Entities accepted an unknown collection")
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

func containsCollection(values []Collection, target Collection) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
