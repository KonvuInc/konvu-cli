package baseline

import (
	"fmt"
	"strings"
)

// Collection names one queryable array in baseline.json.
type Collection string

const (
	CollectionClasses             Collection = "classes"
	CollectionRoutes              Collection = "routes"
	CollectionResources           Collection = "resources"
	CollectionRoles               Collection = "roles"
	CollectionAssetObservations   Collection = "asset-observations"
	CollectionControlObservations Collection = "control-observations"
	CollectionAssets              Collection = "assets"
	CollectionControls            Collection = "controls"
	CollectionImplementations     Collection = "implementations"
	CollectionUnresolved          Collection = "unresolved"
)

var topLevelCollections = []Collection{
	CollectionClasses,
	CollectionRoutes,
	CollectionResources,
	CollectionRoles,
	CollectionAssets,
	CollectionControls,
	CollectionImplementations,
	CollectionUnresolved,
}

var queryCollections = []Collection{
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

func validateDocument(document *Document) error {
	seen := make(map[string]string)
	for _, records := range []struct {
		collection Collection
		prefix     string
	}{
		{collection: CollectionClasses, prefix: "class:"},
		{collection: CollectionRoutes, prefix: "route:"},
		{collection: CollectionResources, prefix: "resource:"},
		{collection: CollectionRoles, prefix: "role:"},
	} {
		if err := validateRecordIDs(
			document.sections[records.collection],
			records.collection,
			records.prefix,
			seen,
			true,
		); err != nil {
			return err
		}
	}
	if err := validateRecordIDs(
		document.assetObservations,
		CollectionAssetObservations,
		"asset:",
		make(map[string]string),
		true,
	); err != nil {
		return err
	}
	if err := validateRecordIDs(
		document.controlObservations,
		CollectionControlObservations,
		"control-observation:",
		seen,
		true,
	); err != nil {
		return err
	}
	if err := validateRecordIDs(
		document.sections[CollectionAssets],
		CollectionAssets,
		"asset:",
		seen,
		true,
	); err != nil {
		return err
	}
	if err := validateRecordIDs(
		document.sections[CollectionControls],
		CollectionControls,
		"control:",
		seen,
		true,
	); err != nil {
		return err
	}
	if err := validateRecordIDs(
		document.sections[CollectionImplementations],
		CollectionImplementations,
		"implementation:",
		seen,
		true,
	); err != nil {
		return err
	}
	if err := validateRecordShapes(document); err != nil {
		return err
	}

	for index, asset := range document.sections[CollectionAssets] {
		assetID, _ := asset["id"].(string)
		links, err := requiredRecords(asset, "controls", fmt.Sprintf("baseline.assets[%d]", index))
		if err != nil {
			return err
		}
		seenControls := make(map[string]bool, len(links))
		for linkIndex, link := range links {
			context := fmt.Sprintf("baseline.assets[%d].controls[%d]", index, linkIndex)
			controlID, err := prefixedID(link, "control_id", context, "control:")
			if err != nil {
				return err
			}
			if seenControls[controlID] {
				return artifactError(context+".control_id", "duplicates %q for asset %q", controlID, assetID)
			}
			seenControls[controlID] = true
			status, err := requiredString(link, "status", context)
			if err != nil {
				return err
			}
			if !validRelationshipStatus(status) {
				return artifactError(context+".status", "unsupported value %q", status)
			}
			if _, err := requiredText(link, "description", context); err != nil {
				return err
			}
			if _, err := requiredPrefixedIDs(link, "implementation_ids", context, "implementation:"); err != nil {
				return err
			}
			if err := validateEvidenceArray(link, "evidence", context); err != nil {
				return err
			}
			if _, err := requiredStringArray(link, "checked", context); err != nil {
				return err
			}
			if _, err := requiredPrefixedIDs(
				link,
				"source_control_observation_ids",
				context,
				"control-observation:",
			); err != nil {
				return err
			}
		}
		document.assetControls[assetID] = links
	}
	for index, observation := range document.controlObservations {
		context := fmt.Sprintf("baseline.observations.controls[%d]", index)
		assetID, err := requiredString(observation, "asset_id", context)
		if err != nil {
			return err
		}
		if err := validateIDPrefixes(
			assetID,
			context+".asset_id",
			"asset:",
			"resource:",
		); err != nil {
			return err
		}
		status, err := requiredString(observation, "status", context)
		if err != nil {
			return err
		}
		if !validRelationshipStatus(status) {
			return artifactError(context+".status", "unsupported value %q", status)
		}
		if _, err := requiredString(observation, "description", context); err != nil {
			return err
		}
	}
	for index, observation := range document.assetObservations {
		if _, err := requiredString(
			observation,
			"description",
			fmt.Sprintf("baseline.observations.assets[%d]", index),
		); err != nil {
			return err
		}
	}

	for index, control := range document.sections[CollectionControls] {
		if _, err := requiredPrefixedIDs(
			control,
			"source_control_observation_ids",
			fmt.Sprintf("baseline.controls[%d]", index),
			"control-observation:",
		); err != nil {
			return err
		}
	}
	for index, implementation := range document.sections[CollectionImplementations] {
		if _, err := requiredPrefixedIDs(
			implementation,
			"source_control_observation_ids",
			fmt.Sprintf("baseline.implementations[%d]", index),
			"control-observation:",
		); err != nil {
			return err
		}
	}
	seenUnresolved := make(map[string]bool)
	for index, unresolved := range document.sections[CollectionUnresolved] {
		context := fmt.Sprintf("baseline.unresolved[%d]", index)
		observationID, err := prefixedID(
			unresolved,
			"control_observation_id",
			context,
			"control-observation:",
		)
		if err != nil {
			return err
		}
		if seenUnresolved[observationID] {
			return artifactError(
				context+".control_observation_id",
				"duplicates unresolved observation %q",
				observationID,
			)
		}
		seenUnresolved[observationID] = true
		if _, err := requiredString(unresolved, "reason", context); err != nil {
			return err
		}
		if _, err := requiredStringArray(unresolved, "checked", context); err != nil {
			return err
		}
	}

	if document.Run.Status == StatusCompleted {
		return validateCompletedReferences(document)
	}
	return nil
}

func validateRecordShapes(document *Document) error {
	for index, record := range document.sections[CollectionClasses] {
		context := fmt.Sprintf("baseline.classes[%d]", index)
		for _, field := range []string{"name", "module"} {
			if _, err := requiredString(record, field, context); err != nil {
				return err
			}
		}
		if _, err := requiredStringArray(record, "bases", context); err != nil {
			return err
		}
		if _, err := requiredNonNegativeInteger(record, "line", context); err != nil {
			return err
		}
	}
	for index, record := range document.sections[CollectionRoutes] {
		context := fmt.Sprintf("baseline.routes[%d]", index)
		for _, field := range []string{"module", "method", "path", "handler"} {
			if _, err := requiredString(record, field, context); err != nil {
				return err
			}
		}
		if _, err := requiredNonNegativeInteger(record, "line", context); err != nil {
			return err
		}
	}
	for index, record := range document.sections[CollectionResources] {
		context := fmt.Sprintf("baseline.resources[%d]", index)
		for _, field := range []string{"name", "kind"} {
			if _, err := requiredString(record, field, context); err != nil {
				return err
			}
		}
		if err := validateOptionalPrefixedID(record, "parent", context, "resource:"); err != nil {
			return err
		}
	}
	for index, record := range document.sections[CollectionRoles] {
		context := fmt.Sprintf("baseline.roles[%d]", index)
		for _, field := range []string{"name", "raw"} {
			if _, err := requiredString(record, field, context); err != nil {
				return err
			}
		}
		if _, exists := record["location"]; !exists {
			return artifactError(context+".location", "is required")
		}
	}
	for index, record := range document.assetObservations {
		context := fmt.Sprintf("baseline.observations.assets[%d]", index)
		for _, field := range []string{"name", "description"} {
			if _, err := requiredString(record, field, context); err != nil {
				return err
			}
		}
	}
	for index, record := range document.controlObservations {
		context := fmt.Sprintf("baseline.observations.controls[%d]", index)
		for _, field := range []string{"property", "description", "decl", "quote"} {
			if _, err := requiredText(record, field, context); err != nil {
				return err
			}
		}
		if _, err := requiredStringArray(record, "checked", context); err != nil {
			return err
		}
		if _, err := requiredNullableString(record, "asvs", context); err != nil {
			return err
		}
	}
	for index, record := range document.sections[CollectionAssets] {
		context := fmt.Sprintf("baseline.assets[%d]", index)
		for _, field := range []string{"kind", "name", "description", "origin"} {
			if _, err := requiredString(record, field, context); err != nil {
				return err
			}
		}
		if _, err := requiredIDsWithPrefixes(
			record,
			"source_ids",
			context,
			"asset:",
			"resource:",
		); err != nil {
			return err
		}
		if err := validateOptionalPrefixedID(record, "parent", context, "asset:"); err != nil {
			return err
		}
		if routes, exists := record["routes"]; exists {
			values, ok := routes.([]any)
			if !ok {
				return artifactError(context+".routes", "must be an array")
			}
			if err := validateEmbeddedRouteIDs(values, context+".routes"); err != nil {
				return err
			}
		}
	}
	for index, record := range document.sections[CollectionControls] {
		context := fmt.Sprintf("baseline.controls[%d]", index)
		for _, field := range []string{"name", "description", "property"} {
			if _, err := requiredText(record, field, context); err != nil {
				return err
			}
		}
		if _, err := requiredStringArray(record, "asvs", context); err != nil {
			return err
		}
	}
	for index, record := range document.sections[CollectionImplementations] {
		context := fmt.Sprintf("baseline.implementations[%d]", index)
		for _, field := range []string{"name", "description", "kind"} {
			if _, err := requiredText(record, field, context); err != nil {
				return err
			}
		}
		if err := validateEvidenceArray(record, "anchors", context); err != nil {
			return err
		}
	}
	return validateOptionalRelationshipIDs(document)
}

func validateEmbeddedRouteIDs(values []any, context string) error {
	for index, value := range values {
		itemContext := fmt.Sprintf("%s[%d]", context, index)
		switch route := value.(type) {
		case string:
			if err := validateIDPrefix(strings.TrimSpace(route), itemContext, "route:"); err != nil {
				return err
			}
		case map[string]any:
			if _, exists := route["id"]; !exists {
				continue
			}
			if _, err := prefixedID(route, "id", itemContext, "route:"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOptionalRelationshipIDs(document *Document) error {
	fields := []struct {
		name   string
		prefix string
	}{
		{name: "route_ids", prefix: "route:"},
		{name: "resource_ids", prefix: "resource:"},
		{name: "role_ids", prefix: "role:"},
		{name: "class_ids", prefix: "class:"},
	}
	for _, collection := range queryCollections {
		for index, record := range document.sections[collection] {
			context := fmt.Sprintf("baseline.%s[%d]", collection, index)
			for _, field := range fields {
				if _, exists := record[field.name]; !exists {
					continue
				}
				if _, err := requiredPrefixedIDs(
					record,
					field.name,
					context,
					field.prefix,
				); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateEvidenceArray(record map[string]any, field, context string) error {
	anchors, err := requiredRecords(record, field, context)
	if err != nil {
		return err
	}
	for index, anchor := range anchors {
		anchorContext := fmt.Sprintf("%s.%s[%d]", context, field, index)
		if _, err := requiredText(anchor, "quote", anchorContext); err != nil {
			return err
		}
		if declaration, _ := anchor["decl"].(string); strings.TrimSpace(declaration) != "" {
			continue
		}
		location, exists := anchor["location"]
		if !exists {
			return artifactError(anchorContext, "must contain decl or location")
		}
		switch typed := location.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				return artifactError(anchorContext+".location", "must not be empty")
			}
		case map[string]any:
			if len(typed) == 0 {
				return artifactError(anchorContext+".location", "must not be empty")
			}
		default:
			return artifactError(anchorContext+".location", "must be a string or object")
		}
	}
	return nil
}

func validRelationshipStatus(status string) bool {
	return status == "present" || status == "partial" || status == "absent"
}

func validateRecordIDs(
	records []map[string]any,
	collection Collection,
	prefix string,
	seen map[string]string,
	required bool,
) error {
	for index, record := range records {
		context := fmt.Sprintf("baseline.%s[%d]", collection, index)
		value, exists := record["id"]
		if !exists && !required {
			continue
		}
		id, ok := value.(string)
		id = strings.TrimSpace(id)
		if !ok || id == "" {
			return artifactError(context+".id", "must be a non-empty string")
		}
		if prefix != "" {
			if err := validateIDPrefix(id, context+".id", prefix); err != nil {
				return err
			}
		}
		if previous, duplicate := seen[id]; duplicate {
			return artifactError(context+".id", "duplicates %q from %s", id, previous)
		}
		seen[id] = context
	}
	return nil
}

func validateCompletedReferences(document *Document) error {
	assets := idSet(document.sections[CollectionAssets])
	assetObservations := idSet(document.assetObservations)
	resources := idSet(document.sections[CollectionResources])
	sources := make(map[string]bool, len(assetObservations)+len(resources))
	for sourceID := range assetObservations {
		sources[sourceID] = true
	}
	for sourceID := range resources {
		sources[sourceID] = true
	}
	controls := idSet(document.sections[CollectionControls])
	implementations := idSet(document.sections[CollectionImplementations])
	observations := idSet(document.controlObservations)

	for index, resource := range document.sections[CollectionResources] {
		parent, err := optionalString(
			resource,
			"parent",
			fmt.Sprintf("baseline.resources[%d]", index),
		)
		if err != nil {
			return err
		}
		if parent != "" && !resources[parent] {
			return artifactError(
				fmt.Sprintf("baseline.resources[%d].parent", index),
				"references unknown resource %q",
				parent,
			)
		}
	}

	for index, asset := range document.sections[CollectionAssets] {
		assetID, _ := asset["id"].(string)
		if parent, err := optionalString(asset, "parent", fmt.Sprintf("baseline.assets[%d]", index)); err != nil {
			return err
		} else if parent != "" && !assets[parent] {
			return artifactError(
				fmt.Sprintf("baseline.assets[%d].parent", index),
				"references unknown asset %q",
				parent,
			)
		}
		sourceIDs, _ := requiredStringArray(
			asset,
			"source_ids",
			fmt.Sprintf("baseline.assets[%d]", index),
		)
		if err := validateReferences(
			sourceIDs,
			sources,
			fmt.Sprintf("baseline.assets[%d].source_ids", index),
			"Asset observation or Resource",
		); err != nil {
			return err
		}
		for linkIndex, link := range document.assetControls[assetID] {
			context := fmt.Sprintf("baseline.assets[%d].controls[%d]", index, linkIndex)
			controlID, _ := link["control_id"].(string)
			if !controls[controlID] {
				return artifactError(context+".control_id", "references unknown control %q", controlID)
			}
			implementationIDs, _ := requiredStringArray(link, "implementation_ids", context)
			for _, implementationID := range implementationIDs {
				if !implementations[implementationID] {
					return artifactError(
						context+".implementation_ids",
						"references unknown implementation %q",
						implementationID,
					)
				}
			}
			sourceIDs, _ := requiredStringArray(link, "source_control_observation_ids", context)
			if err := validateReferences(sourceIDs, observations, context+".source_control_observation_ids", "control observation"); err != nil {
				return err
			}
		}
	}

	for index, control := range document.sections[CollectionControls] {
		context := fmt.Sprintf("baseline.controls[%d]", index)
		sourceIDs, _ := requiredStringArray(control, "source_control_observation_ids", context)
		if err := validateReferences(
			sourceIDs,
			observations,
			context+".source_control_observation_ids",
			"control observation",
		); err != nil {
			return err
		}
	}
	for index, implementation := range document.sections[CollectionImplementations] {
		context := fmt.Sprintf("baseline.implementations[%d]", index)
		sourceIDs, _ := requiredStringArray(
			implementation,
			"source_control_observation_ids",
			context,
		)
		if err := validateReferences(
			sourceIDs,
			observations,
			context+".source_control_observation_ids",
			"control observation",
		); err != nil {
			return err
		}
	}
	for index, unresolved := range document.sections[CollectionUnresolved] {
		observationID, _ := unresolved["control_observation_id"].(string)
		if !observations[observationID] {
			return artifactError(
				fmt.Sprintf("baseline.unresolved[%d].control_observation_id", index),
				"references unknown control observation %q",
				observationID,
			)
		}
	}
	for index, observation := range document.controlObservations {
		assetID, _ := observation["asset_id"].(string)
		if !assets[assetID] {
			return artifactError(
				fmt.Sprintf("baseline.observations.controls[%d].asset_id", index),
				"references unknown normalized Asset %q",
				assetID,
			)
		}
	}
	return nil
}

func validateReferences(values []string, known map[string]bool, path, kind string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return artifactError(path, "contains duplicate %s reference %q", kind, value)
		}
		seen[value] = true
		if !known[value] {
			return artifactError(path, "references unknown %s %q", kind, value)
		}
	}
	return nil
}

func idSet(records []map[string]any) map[string]bool {
	result := make(map[string]bool, len(records))
	for _, record := range records {
		if id, _ := record["id"].(string); id != "" {
			result[id] = true
		}
	}
	return result
}

func prefixedID(record map[string]any, field, context, prefix string) (string, error) {
	id, err := requiredString(record, field, context)
	if err != nil {
		return "", err
	}
	if err := validateIDPrefix(id, context+"."+field, prefix); err != nil {
		return "", err
	}
	return id, nil
}

func validateOptionalPrefixedID(
	record map[string]any,
	field, context, prefix string,
) error {
	value, exists := record[field]
	if !exists || value == nil {
		return nil
	}
	_, err := prefixedID(record, field, context, prefix)
	return err
}

func requiredPrefixedIDs(
	record map[string]any,
	field, context, prefix string,
) ([]string, error) {
	values, err := requiredStringArray(record, field, context)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if err := validateIDPrefix(value, context+"."+field, prefix); err != nil {
			return nil, err
		}
		if seen[value] {
			return nil, artifactError(context+"."+field, "contains duplicate value %q", value)
		}
		seen[value] = true
	}
	return values, nil
}

func requiredIDsWithPrefixes(
	record map[string]any,
	field, context string,
	prefixes ...string,
) ([]string, error) {
	values, err := requiredStringArray(record, field, context)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(values))
	for index, value := range values {
		if err := validateIDPrefixes(
			value,
			fmt.Sprintf("%s.%s[%d]", context, field, index),
			prefixes...,
		); err != nil {
			return nil, err
		}
		if seen[value] {
			return nil, artifactError(context+"."+field, "contains duplicate value %q", value)
		}
		seen[value] = true
	}
	return values, nil
}

func validateIDPrefixes(id, context string, prefixes ...string) error {
	for _, prefix := range prefixes {
		if strings.HasPrefix(id, prefix) {
			return validateIDPrefix(id, context, prefix)
		}
	}
	quoted := make([]string, len(prefixes))
	for index, prefix := range prefixes {
		quoted[index] = fmt.Sprintf("%q", prefix)
	}
	return artifactError(context, "must start with one of %s", strings.Join(quoted, ", "))
}

func validateIDPrefix(id, context, prefix string) error {
	if !strings.HasPrefix(id, prefix) {
		return artifactError(context, "must start with %q", prefix)
	}
	if strings.TrimSpace(strings.TrimPrefix(id, prefix)) == "" {
		return artifactError(context, "must have a non-empty suffix after %q", prefix)
	}
	return nil
}

func requiredStringArray(record map[string]any, field, context string) ([]string, error) {
	value, exists := record[field]
	if !exists {
		return nil, artifactError(context+"."+field, "is required")
	}
	items, ok := value.([]any)
	if !ok {
		return nil, artifactError(context+"."+field, "must be an array of strings")
	}
	result := make([]string, len(items))
	for index, item := range items {
		text, ok := item.(string)
		text = strings.TrimSpace(text)
		if !ok || text == "" {
			return nil, artifactError(
				fmt.Sprintf("%s.%s[%d]", context, field, index),
				"must be a non-empty string",
			)
		}
		result[index] = text
	}
	return result, nil
}

func optionalStringArray(
	record map[string]any,
	field, context string,
) ([]string, bool, error) {
	if _, exists := record[field]; !exists {
		return nil, false, nil
	}
	values, err := requiredStringArray(record, field, context)
	return values, true, err
}
