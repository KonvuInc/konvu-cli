package baseline

import (
	"fmt"
	"sort"
)

// Entity is one lossless query result from a named collection.
type Entity struct {
	Collection Collection
	ID         string
	Value      map[string]any
}

// ControlLink is one Asset-to-Control relationship embedded in an Asset.
type ControlLink struct {
	AssetID                     string
	ControlID                   string
	Status                      string
	ImplementationIDs           []string
	SourceControlObservationIDs []string
	Value                       map[string]any
}

// Catalog is a completed baseline indexed for CLI and TUI exploration.
type Catalog struct {
	document              *Document
	byID                  map[string]Entity
	allByID               map[string][]Entity
	byCollection          map[Collection][]Entity
	linksByAsset          map[string][]ControlLink
	linksByControl        map[string][]ControlLink
	linksByImplementation map[string][]ControlLink
	linksByObservation    map[string][]ControlLink
	childrenByParent      map[string][]string
	recordsBySource       map[string][]Entity
	routesByAsset         map[string][]string
	assetsByRoute         map[string][]string
	resourcesByClass      map[string][]string
	classesByResource     map[string][]string
}

// NewCatalog indexes a completed document. Running, failed, and cancelled runs
// remain listable through Store but cannot be explored as complete catalogs.
func NewCatalog(document *Document) (*Catalog, error) {
	if document == nil {
		return nil, &Error{Code: ErrorRunIncomplete, Message: "baseline document is nil"}
	}
	if document.Run.Status != StatusCompleted {
		return nil, &Error{
			Code:    ErrorRunIncomplete,
			Message: fmt.Sprintf("run %q has status %q; only completed runs can be explored", document.Run.ID, document.Run.Status),
		}
	}
	visibility := baselineControlVisibility(document)
	catalog := &Catalog{
		document:              document,
		byID:                  make(map[string]Entity),
		allByID:               make(map[string][]Entity),
		byCollection:          make(map[Collection][]Entity),
		linksByAsset:          make(map[string][]ControlLink),
		linksByControl:        make(map[string][]ControlLink),
		linksByImplementation: make(map[string][]ControlLink),
		linksByObservation:    make(map[string][]ControlLink),
		childrenByParent:      make(map[string][]string),
		recordsBySource:       make(map[string][]Entity),
		routesByAsset:         make(map[string][]string),
		assetsByRoute:         make(map[string][]string),
		resourcesByClass:      make(map[string][]string),
		classesByResource:     make(map[string][]string),
	}
	for _, collection := range queryCollections {
		records := document.sections[collection]
		entities := make([]Entity, 0, len(records))
		for _, record := range records {
			if !visibility.includeRecord(collection, record) {
				continue
			}
			record = visibility.filterRecord(collection, record)
			id, _ := record["id"].(string)
			if collection == CollectionUnresolved {
				id, _ = record["control_observation_id"].(string)
			}
			entity := Entity{Collection: collection, ID: id, Value: cloneMap(record)}
			entities = append(entities, entity)
			if id != "" {
				catalog.allByID[id] = append(catalog.allByID[id], entity)
			}
			// Asset observations intentionally share asset: IDs with normalized
			// Assets. They remain listable but never participate in global lookup.
			if collection != CollectionUnresolved &&
				collection != CollectionAssetObservations && id != "" {
				catalog.byID[id] = entity
				for _, field := range relationshipIDFields {
					sourceIDs, _, _ := optionalStringArray(record, field, "entity")
					for _, sourceID := range sourceIDs {
						catalog.recordsBySource[sourceID] = append(catalog.recordsBySource[sourceID], entity)
					}
				}
				if collection == CollectionAssets {
					if parent, _ := record["parent"].(string); parent != "" {
						catalog.childrenByParent[parent] = append(catalog.childrenByParent[parent], id)
					}
				}
				if collection == CollectionControlObservations {
					if assetID, _ := record["asset_id"].(string); assetID != "" {
						catalog.recordsBySource[assetID] = append(catalog.recordsBySource[assetID], entity)
					}
				}
			}
			if collection == CollectionUnresolved && id != "" {
				catalog.recordsBySource[id] = append(catalog.recordsBySource[id], entity)
			}
		}
		sort.SliceStable(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })
		catalog.byCollection[collection] = entities
	}
	for assetID, records := range document.assetControls {
		for _, record := range records {
			status, _ := record["status"].(string)
			if status == "absent" {
				continue
			}
			record = visibility.filterLink(record)
			controlID, _ := record["control_id"].(string)
			implementationIDs, _ := requiredStringArray(record, "implementation_ids", "control link")
			sourceIDs, _ := requiredStringArray(record, "source_control_observation_ids", "control link")
			link := ControlLink{
				AssetID:                     assetID,
				ControlID:                   controlID,
				Status:                      status,
				ImplementationIDs:           append([]string(nil), implementationIDs...),
				SourceControlObservationIDs: append([]string(nil), sourceIDs...),
				Value:                       cloneMap(record),
			}
			catalog.linksByAsset[assetID] = append(catalog.linksByAsset[assetID], link)
			catalog.linksByControl[controlID] = append(catalog.linksByControl[controlID], link)
			for _, implementationID := range implementationIDs {
				catalog.linksByImplementation[implementationID] = append(
					catalog.linksByImplementation[implementationID],
					link,
				)
			}
			for _, observationID := range sourceIDs {
				catalog.linksByObservation[observationID] = append(
					catalog.linksByObservation[observationID],
					link,
				)
			}
		}
	}
	catalog.buildRouteIndexes()
	catalog.buildClassIndexes()
	return catalog, nil
}

type controlVisibility struct {
	referencedControls        map[string]bool
	visibleControls           map[string]bool
	referencedImplementations map[string]bool
	visibleImplementations    map[string]bool
	absentObservations        map[string]bool
}

func baselineControlVisibility(document *Document) controlVisibility {
	visibility := controlVisibility{
		referencedControls:        make(map[string]bool),
		visibleControls:           make(map[string]bool),
		referencedImplementations: make(map[string]bool),
		visibleImplementations:    make(map[string]bool),
		absentObservations:        make(map[string]bool),
	}
	for _, observation := range document.controlObservations {
		status, _ := observation["status"].(string)
		id, _ := observation["id"].(string)
		if status == "absent" && id != "" {
			visibility.absentObservations[id] = true
		}
	}
	for _, records := range document.assetControls {
		for _, record := range records {
			controlID, _ := record["control_id"].(string)
			status, _ := record["status"].(string)
			if controlID != "" {
				visibility.referencedControls[controlID] = true
				if status != "absent" {
					visibility.visibleControls[controlID] = true
				}
			}
			implementationIDs, _, _ := optionalStringArray(record, "implementation_ids", "control link")
			for _, implementationID := range implementationIDs {
				visibility.referencedImplementations[implementationID] = true
				if status != "absent" {
					visibility.visibleImplementations[implementationID] = true
				}
			}
		}
	}
	return visibility
}

func (v controlVisibility) includeRecord(collection Collection, record map[string]any) bool {
	id, _ := record["id"].(string)
	switch collection {
	case CollectionControls:
		if v.referencedControls[id] {
			return v.visibleControls[id]
		}
		return !v.onlyAbsentObservations(record)
	case CollectionImplementations:
		if v.referencedImplementations[id] {
			return v.visibleImplementations[id]
		}
		return !v.onlyAbsentObservations(record)
	case CollectionControlObservations:
		status, _ := record["status"].(string)
		return status != "absent"
	case CollectionUnresolved:
		observationID, _ := record["control_observation_id"].(string)
		return !v.absentObservations[observationID]
	default:
		return true
	}
}

func (v controlVisibility) onlyAbsentObservations(record map[string]any) bool {
	values, _, _ := optionalStringArray(record, "source_control_observation_ids", "record")
	if len(values) == 0 {
		return false
	}
	for _, id := range values {
		if !v.absentObservations[id] {
			return false
		}
	}
	return true
}

func (v controlVisibility) filterRecord(collection Collection, record map[string]any) map[string]any {
	filtered := cloneMap(record)
	if collection == CollectionAssets {
		controls, _ := filtered["controls"].([]any)
		visible := make([]any, 0, len(controls))
		for _, value := range controls {
			link, _ := value.(map[string]any)
			status, _ := link["status"].(string)
			if status != "absent" {
				visible = append(visible, v.filterLink(link))
			}
		}
		filtered["controls"] = visible
	}
	if collection == CollectionControls || collection == CollectionImplementations {
		filtered["source_control_observation_ids"] = v.visibleObservationIDs(filtered["source_control_observation_ids"])
	}
	return filtered
}

func (v controlVisibility) filterLink(record map[string]any) map[string]any {
	filtered := cloneMap(record)
	filtered["source_control_observation_ids"] = v.visibleObservationIDs(filtered["source_control_observation_ids"])
	return filtered
}

func (v controlVisibility) visibleObservationIDs(value any) []any {
	values, _ := value.([]any)
	visible := make([]any, 0, len(values))
	for _, value := range values {
		id, _ := value.(string)
		if id != "" && !v.absentObservations[id] {
			visible = append(visible, id)
		}
	}
	return visible
}

var relationshipIDFields = []string{
	"source_ids",
	"source_control_observation_ids",
	"route_ids",
	"resource_ids",
	"role_ids",
	"class_ids",
}

// Document returns the immutable source document for run metadata and raw output.
func (c *Catalog) Document() *Document { return c.document }

// Raw returns the query-visible baseline document. Absent control observations,
// absent Asset-to-Control links, and records referenced only by those links are
// omitted without mutating the stored artifact.
func (c *Catalog) Raw() map[string]any {
	if c == nil || c.document == nil {
		return nil
	}
	raw := c.document.Raw()
	for _, collection := range topLevelCollections {
		raw[string(collection)] = c.collectionValues(collection)
	}
	observations, _ := raw["observations"].(map[string]any)
	if observations == nil {
		observations = make(map[string]any)
	}
	observations["assets"] = c.collectionValues(CollectionAssetObservations)
	observations["controls"] = c.collectionValues(CollectionControlObservations)
	raw["observations"] = observations
	return raw
}

// Counts returns counts for the query-visible catalog rather than the lossless
// artifact. This keeps summaries consistent with list, search, and explain.
func (c *Catalog) Counts() Counts {
	if c == nil {
		return Counts{}
	}
	return Counts{
		Classes:             len(c.byCollection[CollectionClasses]),
		Routes:              len(c.byCollection[CollectionRoutes]),
		Resources:           len(c.byCollection[CollectionResources]),
		Roles:               len(c.byCollection[CollectionRoles]),
		AssetObservations:   len(c.byCollection[CollectionAssetObservations]),
		ControlObservations: len(c.byCollection[CollectionControlObservations]),
		Assets:              len(c.byCollection[CollectionAssets]),
		Controls:            len(c.byCollection[CollectionControls]),
		Implementations:     len(c.byCollection[CollectionImplementations]),
		Unresolved:          len(c.byCollection[CollectionUnresolved]),
	}
}

func (c *Catalog) collectionValues(collection Collection) []any {
	entities := c.byCollection[collection]
	values := make([]any, 0, len(entities))
	for _, entity := range entities {
		values = append(values, cloneMap(entity.Value))
	}
	return values
}

// Collections returns every collection accepted by Entities.
func (c *Catalog) Collections() []Collection {
	return append([]Collection(nil), queryCollections...)
}

// Entities returns a stable ID-sorted, lossless collection.
func (c *Catalog) Entities(collection Collection) ([]Entity, error) {
	values, ok := c.byCollection[collection]
	if !ok {
		return nil, fmt.Errorf("unknown baseline collection %q", collection)
	}
	return cloneEntities(values), nil
}

// Lookup resolves a globally unique public record ID.
func (c *Catalog) Lookup(id string) (Entity, bool) {
	entity, ok := c.byID[id]
	if !ok {
		return Entity{}, false
	}
	entity.Value = cloneMap(entity.Value)
	return entity, true
}

// LookupIn resolves an ID inside one explicit collection, including collections
// whose IDs intentionally overlap the global public namespace.
func (c *Catalog) LookupIn(collection Collection, id string) (Entity, bool) {
	for _, entity := range c.byCollection[collection] {
		if entity.ID == id {
			entity.Value = cloneMap(entity.Value)
			return entity, true
		}
	}
	return Entity{}, false
}

// LinksForAsset returns the Controls attached directly to one Asset.
func (c *Catalog) LinksForAsset(id string) []ControlLink {
	return cloneLinks(c.linksByAsset[id])
}

// LinksForControl returns every Asset relationship using one Control.
func (c *Catalog) LinksForControl(id string) []ControlLink {
	return cloneLinks(c.linksByControl[id])
}

// LinksForImplementation returns every Asset relationship citing one Implementation.
func (c *Catalog) LinksForImplementation(id string) []ControlLink {
	return cloneLinks(c.linksByImplementation[id])
}

// LinksForObservation returns normalized Asset relationships sourced from one observation.
func (c *Catalog) LinksForObservation(id string) []ControlLink {
	return cloneLinks(c.linksByObservation[id])
}

// Related returns the deduplicated records directly connected to a public ID.
// This is the common primitive used by explain views.
func (c *Catalog) Related(id string) []Entity {
	entity, found := c.byID[id]
	if !found {
		return nil
	}
	return c.related(entity)
}

// RelatedIn returns direct relationships for a collection-qualified record.
func (c *Catalog) RelatedIn(collection Collection, id string) []Entity {
	entity, found := c.LookupIn(collection, id)
	if !found {
		return nil
	}
	return c.related(entity)
}

func (c *Catalog) related(entity Entity) []Entity {
	related := make(map[string]Entity)
	primaryKey := entityKey(entity)
	addEntity := func(candidate Entity) {
		if candidate.ID == "" || entityKey(candidate) == primaryKey {
			return
		}
		related[entityKey(candidate)] = candidate
	}
	add := func(candidateID string) {
		if candidate, found := c.byID[candidateID]; found {
			addEntity(candidate)
			return
		}
		for _, candidate := range c.allByID[candidateID] {
			addEntity(candidate)
		}
	}
	addSources := func(record map[string]any) {
		for _, field := range relationshipIDFields {
			values, _, _ := optionalStringArray(record, field, "entity")
			for _, value := range values {
				if field == "source_ids" {
					for _, candidate := range c.allByID[value] {
						if candidate.Collection == CollectionAssetObservations ||
							candidate.Collection == CollectionResources {
							addEntity(candidate)
						}
					}
					continue
				}
				add(value)
			}
		}
	}

	add(entity.ID)
	addSources(entity.Value)
	for _, derived := range c.recordsBySource[entity.ID] {
		addEntity(derived)
	}
	if entity.Collection == CollectionAssets {
		if parent, _ := entity.Value["parent"].(string); parent != "" {
			add(parent)
		}
		for _, childID := range c.childrenByParent[entity.ID] {
			add(childID)
		}
		for _, routeID := range c.routesByAsset[entity.ID] {
			add(routeID)
		}
	}
	if entity.Collection == CollectionRoutes {
		for _, assetID := range c.assetsByRoute[entity.ID] {
			add(assetID)
		}
	}
	if entity.Collection == CollectionClasses {
		for _, resourceID := range c.resourcesByClass[entity.ID] {
			add(resourceID)
		}
	}
	if entity.Collection == CollectionResources {
		for _, classID := range c.classesByResource[entity.ID] {
			add(classID)
		}
	}
	if entity.Collection == CollectionControlObservations {
		if assetID, _ := entity.Value["asset_id"].(string); assetID != "" {
			add(assetID)
		}
	}
	links := make([]ControlLink, 0)
	switch entity.Collection {
	case CollectionAssets:
		links = c.linksByAsset[entity.ID]
	case CollectionControls:
		links = c.linksByControl[entity.ID]
	case CollectionImplementations:
		links = c.linksByImplementation[entity.ID]
	case CollectionControlObservations:
		links = c.linksByObservation[entity.ID]
	}
	for _, link := range links {
		add(link.AssetID)
		add(link.ControlID)
		for _, implementationID := range link.ImplementationIDs {
			add(implementationID)
		}
		for _, observationID := range link.SourceControlObservationIDs {
			add(observationID)
		}
	}

	result := make([]Entity, 0, len(related))
	for _, value := range related {
		value.Value = cloneMap(value.Value)
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Collection != result[j].Collection {
			return result[i].Collection < result[j].Collection
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func entityKey(entity Entity) string {
	return string(entity.Collection) + "\x00" + entity.ID
}

func (c *Catalog) buildRouteIndexes() {
	routes := c.byCollection[CollectionRoutes]
	for _, asset := range c.byCollection[CollectionAssets] {
		seen := make(map[string]bool)
		add := func(routeID string) {
			if routeID == "" || seen[routeID] {
				return
			}
			seen[routeID] = true
			c.routesByAsset[asset.ID] = append(c.routesByAsset[asset.ID], routeID)
			c.assetsByRoute[routeID] = append(c.assetsByRoute[routeID], asset.ID)
		}
		if values, _, _ := optionalStringArray(asset.Value, "route_ids", "asset"); len(values) > 0 {
			for _, value := range values {
				add(value)
			}
		}
		embedded, _ := asset.Value["routes"].([]any)
		for _, value := range embedded {
			switch route := value.(type) {
			case string:
				add(route)
			case map[string]any:
				if id, _ := route["id"].(string); id != "" {
					add(id)
					continue
				}
				for _, candidate := range routes {
					if sameRouteRecord(candidate.Value, route) {
						add(candidate.ID)
						break
					}
				}
			}
		}
		sort.Strings(c.routesByAsset[asset.ID])
	}
	for id := range c.assetsByRoute {
		sort.Strings(c.assetsByRoute[id])
	}
}

func sameRouteRecord(left, right map[string]any) bool {
	for _, field := range []string{"method", "path", "line"} {
		leftValue, leftOK := left[field]
		rightValue, rightOK := right[field]
		if !leftOK || !rightOK || fmt.Sprint(leftValue) != fmt.Sprint(rightValue) {
			return false
		}
	}
	return true
}

func (c *Catalog) buildClassIndexes() {
	classesByDeclaration := make(map[string]string)
	for _, class := range c.byCollection[CollectionClasses] {
		module, _ := class.Value["module"].(string)
		name, _ := class.Value["name"].(string)
		if module != "" && name != "" {
			classesByDeclaration[module+"#"+name] = class.ID
		}
	}
	for _, resource := range c.byCollection[CollectionResources] {
		declaration, _ := resource.Value["decl"].(string)
		classID := classesByDeclaration[declaration]
		if classID == "" {
			continue
		}
		c.resourcesByClass[classID] = append(c.resourcesByClass[classID], resource.ID)
		c.classesByResource[resource.ID] = append(c.classesByResource[resource.ID], classID)
	}
}

func cloneEntities(values []Entity) []Entity {
	result := make([]Entity, len(values))
	for index, value := range values {
		result[index] = value
		result[index].Value = cloneMap(value.Value)
	}
	return result
}

func cloneLinks(values []ControlLink) []ControlLink {
	result := make([]ControlLink, len(values))
	for index, value := range values {
		result[index] = value
		result[index].ImplementationIDs = append([]string(nil), value.ImplementationIDs...)
		result[index].SourceControlObservationIDs = append(
			[]string(nil),
			value.SourceControlObservationIDs...,
		)
		result[index].Value = cloneMap(value.Value)
	}
	return result
}
