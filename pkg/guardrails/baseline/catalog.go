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
	byCollection          map[Collection][]Entity
	linksByAsset          map[string][]ControlLink
	linksByControl        map[string][]ControlLink
	linksByImplementation map[string][]ControlLink
	linksByObservation    map[string][]ControlLink
	childrenByParent      map[string][]string
	recordsBySource       map[string][]string
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
	catalog := &Catalog{
		document:              document,
		byID:                  make(map[string]Entity),
		byCollection:          make(map[Collection][]Entity),
		linksByAsset:          make(map[string][]ControlLink),
		linksByControl:        make(map[string][]ControlLink),
		linksByImplementation: make(map[string][]ControlLink),
		linksByObservation:    make(map[string][]ControlLink),
		childrenByParent:      make(map[string][]string),
		recordsBySource:       make(map[string][]string),
	}
	for _, collection := range queryCollections {
		records := document.sections[collection]
		entities := make([]Entity, 0, len(records))
		for _, record := range records {
			id, _ := record["id"].(string)
			if collection == CollectionUnresolved {
				id, _ = record["control_observation_id"].(string)
			}
			entity := Entity{Collection: collection, ID: id, Value: cloneMap(record)}
			entities = append(entities, entity)
			// Asset observations intentionally share asset: IDs with normalized
			// Assets. They remain listable but never participate in global lookup.
			if collection != CollectionUnresolved &&
				collection != CollectionAssetObservations && id != "" {
				catalog.byID[id] = entity
				for _, field := range []string{"source_ids", "source_control_observation_ids"} {
					sourceIDs, _, _ := optionalStringArray(record, field, "entity")
					for _, sourceID := range sourceIDs {
						catalog.recordsBySource[sourceID] = append(catalog.recordsBySource[sourceID], id)
					}
				}
				if collection == CollectionAssets {
					if parent, _ := record["parent"].(string); parent != "" {
						catalog.childrenByParent[parent] = append(catalog.childrenByParent[parent], id)
					}
				}
				if collection == CollectionControlObservations {
					if assetID, _ := record["asset_id"].(string); assetID != "" {
						catalog.recordsBySource[assetID] = append(catalog.recordsBySource[assetID], id)
					}
				}
			}
		}
		sort.SliceStable(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })
		catalog.byCollection[collection] = entities
	}
	for assetID, records := range document.assetControls {
		for _, record := range records {
			controlID, _ := record["control_id"].(string)
			status, _ := record["status"].(string)
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
	return catalog, nil
}

// Document returns the immutable source document for run metadata and raw output.
func (c *Catalog) Document() *Document { return c.document }

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
	related := make(map[string]Entity)
	add := func(candidateID string) {
		if candidateID == "" || candidateID == id {
			return
		}
		if entity, ok := c.byID[candidateID]; ok {
			related[string(entity.Collection)+"\x00"+entity.ID] = entity
		}
	}
	addSources := func(record map[string]any) {
		values, _, _ := optionalStringArray(record, "source_control_observation_ids", "entity")
		for _, value := range values {
			add(value)
		}
		values, _, _ = optionalStringArray(record, "source_ids", "entity")
		for _, value := range values {
			add(value)
		}
	}

	entity, found := c.byID[id]
	if found {
		addSources(entity.Value)
		for _, derivedID := range c.recordsBySource[id] {
			add(derivedID)
		}
		if entity.Collection == CollectionAssets {
			if parent, _ := entity.Value["parent"].(string); parent != "" {
				add(parent)
			}
			for _, childID := range c.childrenByParent[id] {
				add(childID)
			}
		}
		if entity.Collection == CollectionControlObservations {
			if assetID, _ := entity.Value["asset_id"].(string); assetID != "" {
				add(assetID)
			}
		}
	}
	links := make([]ControlLink, 0)
	switch entity.Collection {
	case CollectionAssets:
		links = c.linksByAsset[id]
	case CollectionControls:
		links = c.linksByControl[id]
	case CollectionImplementations:
		links = c.linksByImplementation[id]
	case CollectionControlObservations:
		links = c.linksByObservation[id]
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
