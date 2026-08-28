package output

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/guardrails/baseline"
)

// NewBaselineWorkspaceV1 adapts one canonical completed baseline to the
// existing read-only workspace. The canonical document remains the lossless
// source of truth; the typed catalog below is only a presentation index.
func NewBaselineWorkspaceV1(document *baseline.Document) (*BaselineWorkspace, error) {
	if document == nil {
		return nil, baselineCatalogErrorf("baseline document is nil")
	}
	if _, err := baseline.NewCatalog(document); err != nil {
		return nil, err
	}

	raw := document.Raw()
	assets, err := document.Section(baseline.CollectionAssets)
	if err != nil {
		return nil, err
	}
	controls, err := document.Section(baseline.CollectionControls)
	if err != nil {
		return nil, err
	}
	implementations, err := document.Section(baseline.CollectionImplementations)
	if err != nil {
		return nil, err
	}

	catalog := &BaselineCatalog{
		Repo:          document.Codebase.Path,
		Source:        map[string]any{"kind": "baseline", "observation_count": document.Counts.ControlObservations},
		FormatVersion: baselineCatalogFormatVersion,
		raw:           cloneBaselineMap(raw),
		assets:        make(map[string]BaselineAsset, len(assets)),
		controls:      make(map[string]BaselineControl, len(controls)),
		implementations: make(
			map[string]BaselineImplementation,
			len(implementations),
		),
		protections:                 make([]BaselineProtection, 0),
		fieldsByParent:              make(map[string][]BaselineAsset),
		protectionsByAsset:          make(map[string][]BaselineProtection),
		protectionsByControl:        make(map[string][]BaselineProtection),
		protectionsByImplementation: make(map[string][]BaselineProtection),
	}

	for index, record := range assets {
		asset, parseErr := parseBaselineV1Asset(record, index)
		if parseErr != nil {
			return nil, parseErr
		}
		if _, exists := catalog.assets[asset.ID]; exists {
			return nil, baselineCatalogErrorf("asset id %s is declared more than once", asset.ID)
		}
		catalog.assets[asset.ID] = asset
		edges, parseErr := parseBaselineV1Edges(record, asset.ID, index)
		if parseErr != nil {
			return nil, parseErr
		}
		catalog.protections = append(catalog.protections, edges...)
	}
	for index, record := range controls {
		control, parseErr := parseBaselineV1Control(record, index)
		if parseErr != nil {
			return nil, parseErr
		}
		if _, exists := catalog.controls[control.ID]; exists {
			return nil, baselineCatalogErrorf("control id %s is declared more than once", control.ID)
		}
		catalog.controls[control.ID] = control
	}
	for index, record := range implementations {
		implementation, parseErr := parseBaselineV1Implementation(record, index)
		if parseErr != nil {
			return nil, parseErr
		}
		if _, exists := catalog.implementations[implementation.ID]; exists {
			return nil, baselineCatalogErrorf(
				"implementation id %s is declared more than once",
				implementation.ID,
			)
		}
		catalog.implementations[implementation.ID] = implementation
	}

	if err := catalog.validateReferences(); err != nil {
		return nil, err
	}
	catalog.buildIndexes()
	blueprint := baselineV1Blueprint(raw)
	catalog.ApplyRepositoryBlueprint(blueprint, document.Codebase.Git.Commit)

	return &BaselineWorkspace{
		catalog:                   catalog,
		blueprint:                 blueprint,
		repositoryID:              sanitizeBaselineText(document.Codebase.Path, false),
		commit:                    sanitizeBaselineText(document.Codebase.Git.Commit, false),
		color:                     false,
		includeUncontrolledAssets: true,
	}, nil
}

func parseBaselineV1Asset(record map[string]any, index int) (BaselineAsset, error) {
	context := fmt.Sprintf("asset record %d", index)
	id, err := baselineRequiredString(record, "id", context)
	if err != nil {
		return BaselineAsset{}, err
	}
	kind := baselineV1String(record, "kind")
	if !isBaselineAssetKind(kind) && kind != "code" {
		return BaselineAsset{}, baselineCatalogErrorf(
			"%s field kind has unsupported value %q",
			context,
			kind,
		)
	}
	name := baselineV1String(record, "name")
	if name == "" {
		name = id
	}
	return BaselineAsset{
		ID:          id,
		Kind:        kind,
		Name:        name,
		Description: baselineV1String(record, "description"),
		Decl:        baselineV1Location(record),
		Origin:      baselineV1String(record, "origin"),
		SourceIDs:   baselineV1Strings(record, "source_ids"),
		ParentID:    baselineV1String(record, "parent"),
		Routes:      baselineV1Routes(record),
	}, nil
}

func parseBaselineV1Control(record map[string]any, index int) (BaselineControl, error) {
	context := fmt.Sprintf("control record %d", index)
	id, err := baselineRequiredString(record, "id", context)
	if err != nil {
		return BaselineControl{}, err
	}
	name := baselineV1String(record, "name")
	if name == "" {
		name = id
	}
	return BaselineControl{
		ID:                   id,
		Name:                 name,
		Description:          baselineV1String(record, "description"),
		Property:             baselineV1String(record, "property"),
		ASVS:                 baselineV1Strings(record, "asvs"),
		SourceObservationIDs: baselineV1Strings(record, "source_control_observation_ids"),
	}, nil
}

func parseBaselineV1Implementation(
	record map[string]any,
	index int,
) (BaselineImplementation, error) {
	context := fmt.Sprintf("implementation record %d", index)
	id, err := baselineRequiredString(record, "id", context)
	if err != nil {
		return BaselineImplementation{}, err
	}
	name := baselineV1String(record, "name")
	if name == "" {
		name = id
	}
	return BaselineImplementation{
		ID:                   id,
		Name:                 name,
		Description:          baselineV1String(record, "description"),
		Kind:                 baselineV1String(record, "kind"),
		Anchors:              baselineV1Anchors(record, "anchors"),
		SourceObservationIDs: baselineV1Strings(record, "source_control_observation_ids"),
	}, nil
}

func parseBaselineV1Edges(
	record map[string]any,
	assetID string,
	assetIndex int,
) ([]BaselineProtection, error) {
	value, exists := record["controls"]
	if !exists {
		return nil, baselineCatalogErrorf("asset record %d is missing required field controls", assetIndex)
	}
	records, err := baselineRecords(value, fmt.Sprintf("asset record %d field controls", assetIndex))
	if err != nil {
		return nil, err
	}
	edges := make([]BaselineProtection, 0, len(records))
	for index, edge := range records {
		context := fmt.Sprintf("asset record %d control %d", assetIndex, index)
		controlID, requiredErr := baselineRequiredString(edge, "control_id", context)
		if requiredErr != nil {
			return nil, requiredErr
		}
		status, requiredErr := baselineRequiredString(edge, "status", context)
		if requiredErr != nil {
			return nil, requiredErr
		}
		edges = append(edges, BaselineProtection{
			ID:                   fmt.Sprintf("association:%s:%s:%d", assetID, controlID, index+1),
			AssetID:              assetID,
			ControlID:            controlID,
			ImplementationIDs:    baselineV1Strings(edge, "implementation_ids"),
			Presence:             status,
			Description:          baselineV1String(edge, "description"),
			Evidence:             baselineV1Anchors(edge, "evidence"),
			Checked:              baselineV1Strings(edge, "checked"),
			SourceObservationIDs: baselineV1Strings(edge, "source_control_observation_ids"),
		})
	}
	return edges, nil
}

func baselineV1Blueprint(raw map[string]any) map[string]any {
	codebase, _ := raw["codebase"].(map[string]any)
	result := map[string]any{
		"repo": map[string]any{
			"name":    baselineV1String(codebase, "name"),
			"layout":  baselineV1String(codebase, "layout"),
			"summary": baselineV1String(codebase, "summary"),
		},
	}
	for _, field := range []string{
		"metrics", "languages", "components", "frameworks", "databases", "orms", "unknowns",
	} {
		if value, exists := codebase[field]; exists {
			result[field] = cloneBaselineValue(value)
		}
	}
	return result
}

func baselineV1Routes(record map[string]any) []BaselineEndpointRoute {
	value, exists := record["routes"]
	if !exists {
		return nil
	}
	records, err := baselineRecords(value, "asset routes")
	if err != nil {
		return nil
	}
	routes := make([]BaselineEndpointRoute, 0, len(records))
	for _, route := range records {
		routes = append(routes, BaselineEndpointRoute{
			Method:  baselineV1String(route, "method"),
			Path:    baselineV1String(route, "path"),
			Handler: baselineV1String(route, "handler"),
			Decl:    baselineV1Location(route),
			Line:    baselineV1Int(route["line"]),
		})
	}
	return routes
}

func baselineV1Anchors(record map[string]any, field string) []BaselineAnchor {
	value, exists := record[field]
	if !exists {
		if field == "anchors" {
			return baselineV1LocationAnchors(record["locations"])
		}
		return nil
	}
	records, err := baselineRecords(value, field)
	if err != nil {
		return baselineV1LocationAnchors(value)
	}
	anchors := make([]BaselineAnchor, 0, len(records))
	for _, anchor := range records {
		anchors = append(anchors, BaselineAnchor{
			Decl:  baselineV1Location(anchor),
			Quote: baselineV1String(anchor, "quote"),
		})
	}
	return anchors
}

func baselineV1LocationAnchors(value any) []BaselineAnchor {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	anchors := make([]BaselineAnchor, 0, len(values))
	for _, item := range values {
		if location, ok := item.(string); ok {
			anchors = append(anchors, BaselineAnchor{Decl: location})
		}
	}
	return anchors
}

func baselineV1Location(record map[string]any) string {
	if value := baselineV1String(record, "decl"); value != "" {
		return value
	}
	value, exists := record["location"]
	if !exists || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	location, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	path := baselineV1String(location, "path")
	line := baselineV1Int(location["line"])
	if path == "" || line <= 0 {
		return path
	}
	return fmt.Sprintf("%s:%d", filepath.ToSlash(path), line)
}

func baselineV1String(record map[string]any, field string) string {
	value, _ := record[field].(string)
	return strings.TrimSpace(value)
}

func baselineV1Strings(record map[string]any, field string) []string {
	value, exists := record[field]
	if !exists {
		return nil
	}
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, strings.TrimSpace(text))
		}
	}
	return result
}

func baselineV1Int(value any) int {
	switch typed := value.(type) {
	case json.Number:
		integer, _ := typed.Int64()
		return int(integer)
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}
