package output

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const baselineCatalogFormatVersion = 1

var baselineAssetKinds = [...]string{"endpoint", "object", "field"}

// BaselineCatalogError reports an invalid or unsupported protections result.
type BaselineCatalogError struct {
	Message string
}

func (e *BaselineCatalogError) Error() string { return e.Message }

// BaselineAnchor points to source evidence for an implementation or protection.
type BaselineAnchor struct {
	Decl  string
	Quote string
}

// BaselineEndpointRoute is one endpoint definition retained under an endpoint group.
type BaselineEndpointRoute struct {
	Method  string
	Path    string
	Handler string
	Decl    string
	Line    int
}

// BaselineAsset is an endpoint group, object, or field in a protections result.
type BaselineAsset struct {
	ID          string
	Kind        string
	Name        string
	Description string
	Decl        string
	Origin      string
	SourceIDs   []string
	ParentID    string
	Routes      []BaselineEndpointRoute
}

// BaselineControl describes reusable security guidance.
type BaselineControl struct {
	ID                   string
	Name                 string
	Description          string
	Property             string
	ASVS                 []string
	SourceObservationIDs []string
}

// BaselineImplementation is one code form that realizes a control.
type BaselineImplementation struct {
	ID                   string
	Name                 string
	Description          string
	Kind                 string
	Anchors              []BaselineAnchor
	SourceObservationIDs []string
}

// BaselineProtection associates a control and its implementations with an asset.
type BaselineProtection struct {
	ID                   string
	AssetID              string
	ControlID            string
	ImplementationIDs    []string
	Presence             string
	Description          string
	Evidence             []BaselineAnchor
	Checked              []string
	SourceObservationIDs []string
}

// BaselineDiscovery is the renderer-facing view of an asset and its direct protections.
type BaselineDiscovery struct {
	ID           string
	Kind         string
	Name         string
	Description  string
	Location     string
	Protections  []BaselineProtection
	ParentID     string
	Routes       []BaselineEndpointRoute
	ControlCount int
}

// BaselineControlAggregate summarizes one control across the catalog.
type BaselineControlAggregate struct {
	Control           BaselineControl
	Protections       []BaselineProtection
	DetectedAssetIDs  []string
	ImplementationIDs []string
	PresenceCounts    map[string]int
	AssetKindCounts   map[string]int
}

// AssetCount returns the number of distinct assets with present or partial coverage.
func (a BaselineControlAggregate) AssetCount() int { return len(a.DetectedAssetIDs) }

// ImplementationCount returns the distinct detected implementation count.
func (a BaselineControlAggregate) ImplementationCount() int {
	return len(a.ImplementationIDs)
}

// BaselineImplementationForm associates a detected implementation with its assets.
type BaselineImplementationForm struct {
	Implementation   BaselineImplementation
	DetectedAssetIDs []string
}

// AssetCount returns the number of detected assets using the implementation form.
func (f BaselineImplementationForm) AssetCount() int { return len(f.DetectedAssetIDs) }

// BaselineControlApplication is one non-navigable row in a Control panel's Applies To table.
type BaselineControlApplication struct {
	AssetID  string
	Kind     string
	Name     string
	Presence string
}

// BaselineDisplayRoute is the human view of exact method/path duplicates.
// Definitions retains every structured route record.
type BaselineDisplayRoute struct {
	Method      string
	Path        string
	Definitions []BaselineEndpointRoute
}

// BaselineCatalog is a validated, indexed view over one protections result.
// Raw returns the original map for lossless structured output; the typed records
// are a presentation model and intentionally do not replace it.
type BaselineCatalog struct {
	Repo                string
	Source              map[string]any
	FormatVersion       int
	RepositoryBlueprint map[string]any
	BaselineCommit      string

	raw                         map[string]any
	assets                      map[string]BaselineAsset
	controls                    map[string]BaselineControl
	implementations             map[string]BaselineImplementation
	protections                 []BaselineProtection
	fieldsByParent              map[string][]BaselineAsset
	protectionsByAsset          map[string][]BaselineProtection
	protectionsByControl        map[string][]BaselineProtection
	protectionsByImplementation map[string][]BaselineProtection
}

// ParseBaselineCatalog validates and indexes one format-version 1 protections result.
func ParseBaselineCatalog(raw map[string]any) (*BaselineCatalog, error) {
	if raw == nil {
		return nil, baselineCatalogErrorf("baseline result must be a JSON object")
	}

	required := []string{
		"assets",
		"controls",
		"format_version",
		"implementations",
		"protections",
		"repo",
		"source",
		"unresolved",
	}
	missing := make([]string, 0)
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return nil, baselineCatalogErrorf(
			"baseline result is missing required fields: %s",
			strings.Join(missing, ", "),
		)
	}

	formatVersion, err := baselineInt(raw["format_version"])
	if err != nil {
		return nil, baselineCatalogErrorf("baseline field format_version must be an integer")
	}
	if formatVersion != baselineCatalogFormatVersion {
		return nil, baselineCatalogErrorf(
			"unsupported baseline format version: %d",
			formatVersion,
		)
	}
	repo, err := baselineRequiredString(raw, "repo", "baseline result")
	if err != nil {
		return nil, err
	}
	source, ok := raw["source"].(map[string]any)
	if !ok {
		return nil, baselineCatalogErrorf("baseline field source must be an object")
	}
	if _, err := baselineRequiredString(source, "kind", "baseline source"); err != nil {
		return nil, err
	}
	if err := baselineNonNegativeIntegerField(source, "observation_count", "baseline source", true); err != nil {
		return nil, err
	}
	if err := baselineNonNegativeIntegerField(source, "residual_asset_count", "baseline source", false); err != nil {
		return nil, err
	}

	assetRecords, err := baselineRecords(raw["assets"], "assets")
	if err != nil {
		return nil, err
	}
	controlRecords, err := baselineRecords(raw["controls"], "controls")
	if err != nil {
		return nil, err
	}
	implementationRecords, err := baselineRecords(raw["implementations"], "implementations")
	if err != nil {
		return nil, err
	}
	protectionRecords, err := baselineRecords(raw["protections"], "protections")
	if err != nil {
		return nil, err
	}
	unresolvedRecords, err := baselineRecords(raw["unresolved"], "unresolved")
	if err != nil {
		return nil, err
	}
	if err := validateBaselineUnresolved(unresolvedRecords); err != nil {
		return nil, err
	}

	catalog := &BaselineCatalog{
		Repo:                        repo,
		Source:                      cloneBaselineMap(source),
		FormatVersion:               formatVersion,
		RepositoryBlueprint:         map[string]any{},
		raw:                         cloneBaselineMap(raw),
		assets:                      make(map[string]BaselineAsset, len(assetRecords)),
		controls:                    make(map[string]BaselineControl, len(controlRecords)),
		implementations:             make(map[string]BaselineImplementation, len(implementationRecords)),
		protections:                 make([]BaselineProtection, 0, len(protectionRecords)),
		fieldsByParent:              make(map[string][]BaselineAsset),
		protectionsByAsset:          make(map[string][]BaselineProtection),
		protectionsByControl:        make(map[string][]BaselineProtection),
		protectionsByImplementation: make(map[string][]BaselineProtection),
	}

	for index, record := range assetRecords {
		asset, parseErr := parseBaselineAsset(record, index)
		if parseErr != nil {
			return nil, parseErr
		}
		if _, exists := catalog.assets[asset.ID]; exists {
			return nil, baselineCatalogErrorf("asset id %s is declared more than once", asset.ID)
		}
		catalog.assets[asset.ID] = asset
	}
	for index, record := range controlRecords {
		control, parseErr := parseBaselineControl(record, index)
		if parseErr != nil {
			return nil, parseErr
		}
		if _, exists := catalog.controls[control.ID]; exists {
			return nil, baselineCatalogErrorf("control id %s is declared more than once", control.ID)
		}
		catalog.controls[control.ID] = control
	}
	for index, record := range implementationRecords {
		implementation, parseErr := parseBaselineImplementation(record, index)
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
	seenProtectionIDs := make(map[string]struct{}, len(protectionRecords))
	for index, record := range protectionRecords {
		protection, parseErr := parseBaselineProtection(record, index)
		if parseErr != nil {
			return nil, parseErr
		}
		if _, exists := seenProtectionIDs[protection.ID]; exists {
			return nil, baselineCatalogErrorf(
				"protection id %s is declared more than once",
				protection.ID,
			)
		}
		seenProtectionIDs[protection.ID] = struct{}{}
		catalog.protections = append(catalog.protections, protection)
	}

	if err := catalog.validateReferences(); err != nil {
		return nil, err
	}
	catalog.buildIndexes()
	return catalog, nil
}

// Raw returns the original selected-repository result without normalization or field loss.
// The returned map is an independent copy and may be passed to structured output safely.
func (c *BaselineCatalog) Raw() map[string]any { return cloneBaselineMap(c.raw) }

// ApplyRepositoryBlueprint attaches the immutable scan context used by the workspace header.
func (c *BaselineCatalog) ApplyRepositoryBlueprint(blueprint map[string]any, commit string) {
	if blueprint == nil {
		blueprint = map[string]any{}
	}
	c.RepositoryBlueprint = cloneBaselineMap(blueprint)
	c.BaselineCommit = commit
}

// AssetCount returns the number of parsed asset records by stable ID.
func (c *BaselineCatalog) AssetCount() int { return len(c.assets) }

// ControlCount returns the total number of control records, including unassociated controls.
func (c *BaselineCatalog) ControlCount() int { return len(c.controls) }

// ImplementationCount returns the total number of implementation-form records.
func (c *BaselineCatalog) ImplementationCount() int { return len(c.implementations) }

// Asset returns an asset by stable ID.
func (c *BaselineCatalog) Asset(id string) (BaselineAsset, bool) {
	asset, ok := c.assets[id]
	return cloneBaselineAsset(asset), ok
}

// Control returns a control by stable ID.
func (c *BaselineCatalog) Control(id string) (BaselineControl, bool) {
	control, ok := c.controls[id]
	return cloneBaselineControl(control), ok
}

// Implementation returns an implementation form by stable ID.
func (c *BaselineCatalog) Implementation(id string) (BaselineImplementation, bool) {
	implementation, ok := c.implementations[id]
	return cloneBaselineImplementation(implementation), ok
}

// Assets returns all assets ordered by stable ID.
func (c *BaselineCatalog) Assets() []BaselineAsset {
	assets := make([]BaselineAsset, 0, len(c.assets))
	for _, asset := range c.assets {
		assets = append(assets, cloneBaselineAsset(asset))
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].ID < assets[j].ID })
	return assets
}

// Controls returns all controls ordered by stable ID.
func (c *BaselineCatalog) Controls() []BaselineControl {
	controls := make([]BaselineControl, 0, len(c.controls))
	for _, control := range c.controls {
		controls = append(controls, cloneBaselineControl(control))
	}
	sort.Slice(controls, func(i, j int) bool { return controls[i].ID < controls[j].ID })
	return controls
}

// Implementations returns all implementation forms ordered by stable ID.
func (c *BaselineCatalog) Implementations() []BaselineImplementation {
	implementations := make([]BaselineImplementation, 0, len(c.implementations))
	for _, implementation := range c.implementations {
		implementations = append(implementations, cloneBaselineImplementation(implementation))
	}
	sort.Slice(implementations, func(i, j int) bool {
		return implementations[i].ID < implementations[j].ID
	})
	return implementations
}

// Protections returns every protection association in source order.
func (c *BaselineCatalog) Protections() []BaselineProtection {
	return cloneBaselineProtections(c.protections)
}

// FieldsForParent returns field assets sorted by case-insensitive name and stable ID.
func (c *BaselineCatalog) FieldsForParent(parentID string) []BaselineAsset {
	fields := c.fieldsByParent[parentID]
	result := make([]BaselineAsset, len(fields))
	for index, field := range fields {
		result[index] = cloneBaselineAsset(field)
	}
	return result
}

// ProtectionsForAsset returns direct protection associations in source order.
func (c *BaselineCatalog) ProtectionsForAsset(assetID string) []BaselineProtection {
	return cloneBaselineProtections(c.protectionsByAsset[assetID])
}

// ProtectionsForControl returns all associations for a control in source order.
func (c *BaselineCatalog) ProtectionsForControl(controlID string) []BaselineProtection {
	return cloneBaselineProtections(c.protectionsByControl[controlID])
}

// ProtectionsForImplementation returns all associations naming an implementation.
func (c *BaselineCatalog) ProtectionsForImplementation(implementationID string) []BaselineProtection {
	return cloneBaselineProtections(c.protectionsByImplementation[implementationID])
}

// Discoveries returns all assets of a supported kind ordered by name and stable ID.
func (c *BaselineCatalog) Discoveries(kind string) ([]BaselineDiscovery, error) {
	if !isBaselineAssetKind(kind) {
		return nil, baselineCatalogErrorf("unknown discovery kind: %s", kind)
	}
	discoveries := make([]BaselineDiscovery, 0)
	for _, asset := range c.assets {
		if asset.Kind != kind {
			continue
		}
		discoveries = append(discoveries, c.discovery(asset))
	}
	sort.Slice(discoveries, func(i, j int) bool {
		left, right := baselineFold(discoveries[i].Name), baselineFold(discoveries[j].Name)
		if left != right {
			return left < right
		}
		return discoveries[i].ID < discoveries[j].ID
	})
	return discoveries, nil
}

// ReviewControlIDs returns the unique controls that make an asset reviewable.
// Objects include the union of direct controls and controls on child fields.
func (c *BaselineCatalog) ReviewControlIDs(assetID string) []string {
	asset, ok := c.assets[assetID]
	if !ok {
		return nil
	}
	ids := make(map[string]struct{})
	for _, protection := range c.protectionsByAsset[asset.ID] {
		ids[protection.ControlID] = struct{}{}
	}
	if asset.Kind == "object" {
		for _, field := range c.fieldsByParent[asset.ID] {
			for _, protection := range c.protectionsByAsset[field.ID] {
				ids[protection.ControlID] = struct{}{}
			}
		}
	}
	result := sortedBaselineKeys(ids)
	return result
}

// ReviewControlCount returns the unique direct-and-child control count used in asset lists.
func (c *BaselineCatalog) ReviewControlCount(assetID string) int {
	return len(c.ReviewControlIDs(assetID))
}

// ReviewableAssets omits assets with no relationships to review and applies workspace ordering.
func (c *BaselineCatalog) ReviewableAssets(kind string) ([]BaselineDiscovery, error) {
	discoveries, err := c.Discoveries(kind)
	if err != nil {
		return nil, err
	}
	reviewable := make([]BaselineDiscovery, 0, len(discoveries))
	for _, discovery := range discoveries {
		if c.ReviewControlCount(discovery.ID) > 0 {
			reviewable = append(reviewable, discovery)
		}
	}
	sort.Slice(reviewable, func(i, j int) bool {
		leftCount := c.ReviewControlCount(reviewable[i].ID)
		rightCount := c.ReviewControlCount(reviewable[j].ID)
		if leftCount != rightCount {
			return leftCount > rightCount
		}
		leftName := baselineFold(c.AssetDisplayName(reviewable[i].ID))
		rightName := baselineFold(c.AssetDisplayName(reviewable[j].ID))
		if leftName != rightName {
			return leftName < rightName
		}
		return reviewable[i].ID < reviewable[j].ID
	})
	return reviewable, nil
}

// ReviewableAssetCounts returns only the endpoint, object, and field counts shown in review.
func (c *BaselineCatalog) ReviewableAssetCounts() map[string]int {
	counts := make(map[string]int, len(baselineAssetKinds))
	for _, kind := range baselineAssetKinds {
		assets, _ := c.ReviewableAssets(kind)
		counts[kind] = len(assets)
	}
	return counts
}

// AssetDisplayName humanizes names and renders fields as Parent › field.
func (c *BaselineCatalog) AssetDisplayName(assetID string) string {
	asset, ok := c.assets[assetID]
	if !ok {
		return ""
	}
	name := baselineHumanName(asset.Name)
	if asset.Kind != "field" || asset.ParentID == "" {
		return name
	}
	parent, ok := c.assets[asset.ParentID]
	if !ok {
		return name
	}
	fieldName := asset.Name
	prefix := parent.Name + "."
	if strings.HasPrefix(fieldName, prefix) {
		fieldName = strings.TrimPrefix(fieldName, prefix)
	}
	return baselineHumanName(parent.Name) + " › " + fieldName
}

// FrequentControls orders controls by detected asset count, implementation count, then name.
// Only present and partial associations contribute detected counts.
func (c *BaselineCatalog) FrequentControls(limit int) []BaselineControlAggregate {
	if limit <= 0 {
		return nil
	}
	aggregates := make([]BaselineControlAggregate, 0, len(c.controls))
	for _, control := range c.controls {
		aggregates = append(aggregates, c.controlAggregate(control))
	}
	sort.Slice(aggregates, func(i, j int) bool {
		if aggregates[i].AssetCount() != aggregates[j].AssetCount() {
			return aggregates[i].AssetCount() > aggregates[j].AssetCount()
		}
		if aggregates[i].ImplementationCount() != aggregates[j].ImplementationCount() {
			return aggregates[i].ImplementationCount() > aggregates[j].ImplementationCount()
		}
		leftName := baselineFold(aggregates[i].Control.Name)
		rightName := baselineFold(aggregates[j].Control.Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return aggregates[i].Control.ID < aggregates[j].Control.ID
	})
	if limit < len(aggregates) {
		aggregates = aggregates[:limit]
	}
	return aggregates
}

// ControlAggregate returns the aggregate for one control ID.
func (c *BaselineCatalog) ControlAggregate(controlID string) (BaselineControlAggregate, bool) {
	control, ok := c.controls[controlID]
	if !ok {
		return BaselineControlAggregate{}, false
	}
	return c.controlAggregate(control), true
}

// ImplementationForms returns detected implementation forms for a control aggregate.
func (c *BaselineCatalog) ImplementationForms(
	aggregate BaselineControlAggregate,
) []BaselineImplementationForm {
	detected := make([]BaselineProtection, 0, len(aggregate.Protections))
	for _, protection := range aggregate.Protections {
		if isBaselineDetected(protection.Presence) {
			detected = append(detected, protection)
		}
	}
	forms := make([]BaselineImplementationForm, 0, len(aggregate.ImplementationIDs))
	for _, implementationID := range aggregate.ImplementationIDs {
		implementation, ok := c.implementations[implementationID]
		if !ok {
			continue
		}
		assets := make(map[string]struct{})
		for _, protection := range detected {
			if baselineContains(protection.ImplementationIDs, implementationID) {
				assets[protection.AssetID] = struct{}{}
			}
		}
		forms = append(forms, BaselineImplementationForm{
			Implementation:   cloneBaselineImplementation(implementation),
			DetectedAssetIDs: sortedBaselineKeys(assets),
		})
	}
	sort.Slice(forms, func(i, j int) bool {
		if forms[i].AssetCount() != forms[j].AssetCount() {
			return forms[i].AssetCount() > forms[j].AssetCount()
		}
		leftName := baselineFold(forms[i].Implementation.Name)
		rightName := baselineFold(forms[j].Implementation.Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return forms[i].Implementation.ID < forms[j].Implementation.ID
	})
	return forms
}

// ControlApplications returns the neutral Applies To rows for all associations,
// including absent expectations. Repeated asset associations collapse to their
// least-covered presence (absent after partial after present).
func (c *BaselineCatalog) ControlApplications(controlID string) []BaselineControlApplication {
	presenceByAsset := make(map[string]string)
	for _, protection := range c.protectionsByControl[controlID] {
		existing, ok := presenceByAsset[protection.AssetID]
		if !ok || baselinePresenceRank(protection.Presence) > baselinePresenceRank(existing) {
			presenceByAsset[protection.AssetID] = protection.Presence
		}
	}
	applications := make([]BaselineControlApplication, 0, len(presenceByAsset))
	for assetID, presence := range presenceByAsset {
		asset, ok := c.assets[assetID]
		if !ok {
			continue
		}
		applications = append(applications, BaselineControlApplication{
			AssetID:  assetID,
			Kind:     asset.Kind,
			Name:     c.AssetDisplayName(assetID),
			Presence: presence,
		})
	}
	kindRank := map[string]int{"endpoint": 0, "object": 1, "field": 2}
	sort.Slice(applications, func(i, j int) bool {
		leftRank, leftKnown := kindRank[applications[i].Kind]
		rightRank, rightKnown := kindRank[applications[j].Kind]
		if !leftKnown {
			leftRank = len(kindRank)
		}
		if !rightKnown {
			rightRank = len(kindRank)
		}
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftName := baselineFold(applications[i].Name)
		rightName := baselineFold(applications[j].Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return applications[i].AssetID < applications[j].AssetID
	})
	return applications
}

// ControlForms returns the distinct implementation forms named by every control
// association, including associations whose presence is absent.
func (c *BaselineCatalog) ControlForms(controlID string) []BaselineImplementation {
	ids := make(map[string]struct{})
	for _, protection := range c.protectionsByControl[controlID] {
		for _, implementationID := range protection.ImplementationIDs {
			ids[implementationID] = struct{}{}
		}
	}
	forms := make([]BaselineImplementation, 0, len(ids))
	for implementationID := range ids {
		if implementation, ok := c.implementations[implementationID]; ok {
			forms = append(forms, cloneBaselineImplementation(implementation))
		}
	}
	sort.Slice(forms, func(i, j int) bool {
		leftName := baselineFold(baselineHumanName(forms[i].Name))
		rightName := baselineFold(baselineHumanName(forms[j].Name))
		if leftName != rightName {
			return leftName < rightName
		}
		return forms[i].ID < forms[j].ID
	})
	return forms
}

// EndpointDisplayRoutes collapses exact method/path duplicates for human output only.
func (c *BaselineCatalog) EndpointDisplayRoutes(assetID string) []BaselineDisplayRoute {
	asset, ok := c.assets[assetID]
	if !ok {
		return nil
	}
	type routeKey struct {
		method string
		path   string
	}
	grouped := make(map[routeKey][]BaselineEndpointRoute)
	for _, route := range asset.Routes {
		key := routeKey{method: route.Method, path: route.Path}
		grouped[key] = append(grouped[key], route)
	}
	routes := make([]BaselineDisplayRoute, 0, len(grouped))
	for key, definitions := range grouped {
		routes = append(routes, BaselineDisplayRoute{
			Method:      key.method,
			Path:        key.path,
			Definitions: append([]BaselineEndpointRoute(nil), definitions...),
		})
	}
	sort.Slice(routes, func(i, j int) bool {
		leftPath, rightPath := baselineFold(routes[i].Path), baselineFold(routes[j].Path)
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		leftMethod, rightMethod := baselineFold(routes[i].Method), baselineFold(routes[j].Method)
		if leftMethod != rightMethod {
			return leftMethod < rightMethod
		}
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})
	return routes
}

func (c *BaselineCatalog) discovery(asset BaselineAsset) BaselineDiscovery {
	protections := cloneBaselineProtections(c.protectionsByAsset[asset.ID])
	controlIDs := make(map[string]struct{})
	for _, protection := range protections {
		controlIDs[protection.ControlID] = struct{}{}
	}
	return BaselineDiscovery{
		ID:           asset.ID,
		Kind:         asset.Kind,
		Name:         asset.Name,
		Description:  asset.Description,
		Location:     asset.Decl,
		Protections:  protections,
		ParentID:     asset.ParentID,
		Routes:       append([]BaselineEndpointRoute(nil), asset.Routes...),
		ControlCount: len(controlIDs),
	}
}

func (c *BaselineCatalog) controlAggregate(control BaselineControl) BaselineControlAggregate {
	protections := cloneBaselineProtections(c.protectionsByControl[control.ID])
	detectedAssets := make(map[string]struct{})
	implementationIDs := make(map[string]struct{})
	presenceCounts := make(map[string]int)
	for _, protection := range protections {
		presenceCounts[protection.Presence]++
		if !isBaselineDetected(protection.Presence) {
			continue
		}
		detectedAssets[protection.AssetID] = struct{}{}
		for _, implementationID := range protection.ImplementationIDs {
			implementationIDs[implementationID] = struct{}{}
		}
	}
	assetIDs := sortedBaselineKeys(detectedAssets)
	assetKindCounts := make(map[string]int)
	for _, assetID := range assetIDs {
		if asset, ok := c.assets[assetID]; ok {
			assetKindCounts[asset.Kind]++
		}
	}
	return BaselineControlAggregate{
		Control:           cloneBaselineControl(control),
		Protections:       protections,
		DetectedAssetIDs:  assetIDs,
		ImplementationIDs: sortedBaselineKeys(implementationIDs),
		PresenceCounts:    presenceCounts,
		AssetKindCounts:   assetKindCounts,
	}
}

func (c *BaselineCatalog) validateReferences() error {
	for _, asset := range c.assets {
		if asset.Kind != "field" || asset.ParentID == "" {
			continue
		}
		parent, ok := c.assets[asset.ParentID]
		if !ok || parent.Kind != "object" {
			return baselineCatalogErrorf(
				"field asset %s references unknown object parent %s",
				asset.ID,
				asset.ParentID,
			)
		}
	}
	for _, protection := range c.protections {
		if _, ok := c.assets[protection.AssetID]; !ok {
			return baselineCatalogErrorf(
				"protection %s references unknown asset %s",
				protection.ID,
				protection.AssetID,
			)
		}
		if _, ok := c.controls[protection.ControlID]; !ok {
			return baselineCatalogErrorf(
				"protection %s references unknown control %s",
				protection.ID,
				protection.ControlID,
			)
		}
		for _, implementationID := range protection.ImplementationIDs {
			if _, ok := c.implementations[implementationID]; !ok {
				return baselineCatalogErrorf(
					"protection %s references unknown implementation %s",
					protection.ID,
					implementationID,
				)
			}
		}
	}
	return nil
}

func (c *BaselineCatalog) buildIndexes() {
	for _, asset := range c.assets {
		if asset.Kind == "field" && asset.ParentID != "" {
			c.fieldsByParent[asset.ParentID] = append(c.fieldsByParent[asset.ParentID], asset)
		}
	}
	for parentID := range c.fieldsByParent {
		fields := c.fieldsByParent[parentID]
		sort.Slice(fields, func(i, j int) bool {
			leftName, rightName := baselineFold(fields[i].Name), baselineFold(fields[j].Name)
			if leftName != rightName {
				return leftName < rightName
			}
			return fields[i].ID < fields[j].ID
		})
		c.fieldsByParent[parentID] = fields
	}
	for _, protection := range c.protections {
		c.protectionsByAsset[protection.AssetID] = append(
			c.protectionsByAsset[protection.AssetID], protection,
		)
		c.protectionsByControl[protection.ControlID] = append(
			c.protectionsByControl[protection.ControlID], protection,
		)
		for _, implementationID := range protection.ImplementationIDs {
			c.protectionsByImplementation[implementationID] = append(
				c.protectionsByImplementation[implementationID], protection,
			)
		}
	}
}

func parseBaselineAsset(record map[string]any, index int) (BaselineAsset, error) {
	context := fmt.Sprintf("asset record %d", index)
	id, err := baselineRequiredString(record, "id", context)
	if err != nil {
		return BaselineAsset{}, err
	}
	kind, err := baselineRequiredString(record, "kind", context)
	if err != nil {
		return BaselineAsset{}, err
	}
	if kind != "endpoint" && kind != "object" && kind != "field" && kind != "code" {
		return BaselineAsset{}, baselineCatalogErrorf(
			"%s field kind has unsupported value %q",
			context,
			kind,
		)
	}
	name, err := baselineRequiredString(record, "name", context)
	if err != nil {
		return BaselineAsset{}, err
	}
	description, err := baselineOptionalString(record, "description", context, "")
	if err != nil {
		return BaselineAsset{}, err
	}
	decl, err := baselineRequiredString(record, "decl", context)
	if err != nil {
		return BaselineAsset{}, err
	}
	origin, err := baselineRequiredString(record, "origin", context)
	if err != nil {
		return BaselineAsset{}, err
	}
	if kind == "field" {
		if _, exists := record["parent"]; !exists {
			return BaselineAsset{}, baselineCatalogErrorf("%s is missing required field parent", context)
		}
	}
	parentID, err := baselineOptionalNullableString(record, "parent", context, "")
	if err != nil {
		return BaselineAsset{}, err
	}
	sourceIDs, err := baselineRequiredStrings(record, "source_ids", context)
	if err != nil {
		return BaselineAsset{}, err
	}
	if kind == "endpoint" {
		if _, exists := record["routes"]; !exists {
			return BaselineAsset{}, baselineCatalogErrorf("%s is missing required field routes", context)
		}
	}
	routes, err := parseBaselineRoutes(record, context)
	if err != nil {
		return BaselineAsset{}, err
	}
	return BaselineAsset{
		ID:          id,
		Kind:        kind,
		Name:        name,
		Description: description,
		Decl:        decl,
		Origin:      origin,
		SourceIDs:   sourceIDs,
		ParentID:    parentID,
		Routes:      routes,
	}, nil
}

func parseBaselineRoutes(record map[string]any, context string) ([]BaselineEndpointRoute, error) {
	value, ok := record["routes"]
	if !ok {
		return []BaselineEndpointRoute{}, nil
	}
	records, err := baselineRecords(value, context+" field routes")
	if err != nil {
		return nil, err
	}
	routes := make([]BaselineEndpointRoute, 0, len(records))
	for index, routeRecord := range records {
		routeContext := fmt.Sprintf("%s route %d", context, index)
		method, requiredErr := baselineRequiredString(routeRecord, "method", routeContext)
		if requiredErr != nil {
			return nil, requiredErr
		}
		path, requiredErr := baselineRequiredString(routeRecord, "path", routeContext)
		if requiredErr != nil {
			return nil, requiredErr
		}
		handler, requiredErr := baselineOptionalNullableString(
			routeRecord,
			"handler",
			routeContext,
			"",
		)
		if requiredErr != nil {
			return nil, requiredErr
		}
		decl, requiredErr := baselineRequiredString(routeRecord, "decl", routeContext)
		if requiredErr != nil {
			return nil, requiredErr
		}
		lineValue, exists := routeRecord["line"]
		if !exists {
			return nil, baselineCatalogErrorf("%s is missing required field line", routeContext)
		}
		line, intErr := baselineInt(lineValue)
		if intErr != nil || line < 0 {
			return nil, baselineCatalogErrorf(
				"%s field line must be a non-negative integer",
				routeContext,
			)
		}
		routes = append(routes, BaselineEndpointRoute{
			Method: method, Path: path, Handler: handler, Decl: decl, Line: line,
		})
	}
	return routes, nil
}

func parseBaselineControl(record map[string]any, index int) (BaselineControl, error) {
	context := fmt.Sprintf("control record %d", index)
	id, err := baselineRequiredString(record, "id", context)
	if err != nil {
		return BaselineControl{}, err
	}
	if !strings.HasPrefix(id, "ctrl:") {
		return BaselineControl{}, baselineCatalogErrorf("%s field id must start with ctrl:", context)
	}
	name, err := baselineRequiredString(record, "name", context)
	if err != nil {
		return BaselineControl{}, err
	}
	description, err := baselineRequiredString(record, "description", context)
	if err != nil {
		return BaselineControl{}, err
	}
	property, err := baselineRequiredString(record, "property", context)
	if err != nil {
		return BaselineControl{}, err
	}
	if !isBaselineSecurityProperty(property) {
		return BaselineControl{}, baselineCatalogErrorf(
			"%s field property has unsupported value %q",
			context,
			property,
		)
	}
	asvs, err := baselineRequiredStrings(record, "asvs", context)
	if err != nil {
		return BaselineControl{}, err
	}
	sourceObservationIDs, err := baselineRequiredStrings(record, "source_observation_ids", context)
	if err != nil {
		return BaselineControl{}, err
	}
	return BaselineControl{
		ID:                   id,
		Name:                 name,
		Description:          description,
		Property:             property,
		ASVS:                 asvs,
		SourceObservationIDs: sourceObservationIDs,
	}, nil
}

func parseBaselineImplementation(record map[string]any, index int) (BaselineImplementation, error) {
	context := fmt.Sprintf("implementation record %d", index)
	id, err := baselineRequiredString(record, "id", context)
	if err != nil {
		return BaselineImplementation{}, err
	}
	if !strings.HasPrefix(id, "impl:") {
		return BaselineImplementation{}, baselineCatalogErrorf("%s field id must start with impl:", context)
	}
	name, err := baselineRequiredString(record, "name", context)
	if err != nil {
		return BaselineImplementation{}, err
	}
	description, err := baselineRequiredString(record, "description", context)
	if err != nil {
		return BaselineImplementation{}, err
	}
	kind, err := baselineRequiredString(record, "kind", context)
	if err != nil {
		return BaselineImplementation{}, err
	}
	anchors, err := baselineRequiredAnchors(record, "anchors", context)
	if err != nil {
		return BaselineImplementation{}, err
	}
	sourceObservationIDs, err := baselineRequiredStrings(record, "source_observation_ids", context)
	if err != nil {
		return BaselineImplementation{}, err
	}
	return BaselineImplementation{
		ID:                   id,
		Name:                 name,
		Description:          description,
		Kind:                 kind,
		Anchors:              anchors,
		SourceObservationIDs: sourceObservationIDs,
	}, nil
}

func parseBaselineProtection(record map[string]any, index int) (BaselineProtection, error) {
	context := fmt.Sprintf("protection record %d", index)
	id, err := baselineRequiredString(record, "id", context)
	if err != nil {
		return BaselineProtection{}, err
	}
	if !strings.HasPrefix(id, "prot:") {
		return BaselineProtection{}, baselineCatalogErrorf("%s field id must start with prot:", context)
	}
	assetID, err := baselineRequiredString(record, "asset_id", context)
	if err != nil {
		return BaselineProtection{}, err
	}
	controlID, err := baselineRequiredString(record, "control_id", context)
	if err != nil {
		return BaselineProtection{}, err
	}
	presence, err := baselineRequiredString(record, "presence", context)
	if err != nil {
		return BaselineProtection{}, err
	}
	if presence != "present" && presence != "partial" && presence != "absent" {
		return BaselineProtection{}, baselineCatalogErrorf(
			"%s field presence has unsupported value %q",
			context,
			presence,
		)
	}
	implementationIDs, err := baselineRequiredStrings(record, "implementation_ids", context)
	if err != nil {
		return BaselineProtection{}, err
	}
	description, err := baselineRequiredString(record, "description", context)
	if err != nil {
		return BaselineProtection{}, err
	}
	evidence, err := baselineRequiredAnchors(record, "evidence", context)
	if err != nil {
		return BaselineProtection{}, err
	}
	checked, err := baselineRequiredStrings(record, "checked", context)
	if err != nil {
		return BaselineProtection{}, err
	}
	sourceObservationIDs, err := baselineRequiredStrings(record, "source_observation_ids", context)
	if err != nil {
		return BaselineProtection{}, err
	}
	return BaselineProtection{
		ID:                   id,
		AssetID:              assetID,
		ControlID:            controlID,
		ImplementationIDs:    implementationIDs,
		Presence:             presence,
		Description:          description,
		Evidence:             evidence,
		Checked:              checked,
		SourceObservationIDs: sourceObservationIDs,
	}, nil
}

func validateBaselineUnresolved(records []map[string]any) error {
	for index, record := range records {
		context := fmt.Sprintf("unresolved record %d", index)
		if _, err := baselineRequiredString(record, "observation_id", context); err != nil {
			return err
		}
		if _, err := baselineRequiredString(record, "reason", context); err != nil {
			return err
		}
		if _, err := baselineRequiredStrings(record, "checked", context); err != nil {
			return err
		}
	}
	return nil
}

func baselineNonNegativeIntegerField(
	record map[string]any,
	field, context string,
	required bool,
) error {
	value, exists := record[field]
	if !exists {
		if required {
			return baselineCatalogErrorf("%s is missing required field %s", context, field)
		}
		return nil
	}
	number, err := baselineInt(value)
	if err != nil || number < 0 {
		return baselineCatalogErrorf(
			"%s field %s must be a non-negative integer",
			context,
			field,
		)
	}
	return nil
}

func baselineRecords(value any, field string) ([]map[string]any, error) {
	var values []any
	switch records := value.(type) {
	case []any:
		values = records
	case []map[string]any:
		values = make([]any, len(records))
		for index := range records {
			values[index] = records[index]
		}
	default:
		return nil, baselineCatalogErrorf("baseline field %s must be an array", field)
	}
	result := make([]map[string]any, 0, len(values))
	for index, value := range values {
		record, ok := value.(map[string]any)
		if !ok {
			return nil, baselineCatalogErrorf("baseline field %s record %d must be an object", field, index)
		}
		result = append(result, record)
	}
	return result, nil
}

func baselineRequiredString(record map[string]any, field, context string) (string, error) {
	value, ok := record[field]
	if !ok {
		return "", baselineCatalogErrorf("%s is missing required field %s", context, field)
	}
	text, ok := value.(string)
	if !ok {
		return "", baselineCatalogErrorf("%s field %s must be a string", context, field)
	}
	return text, nil
}

func baselineOptionalString(
	record map[string]any,
	field, context, fallback string,
) (string, error) {
	value, ok := record[field]
	if !ok {
		return fallback, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", baselineCatalogErrorf("%s field %s must be a string", context, field)
	}
	return text, nil
}

func baselineOptionalNullableString(
	record map[string]any,
	field, context, fallback string,
) (string, error) {
	value, ok := record[field]
	if !ok || value == nil {
		return fallback, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", baselineCatalogErrorf("%s field %s must be a string or null", context, field)
	}
	return text, nil
}

func baselineRequiredStrings(record map[string]any, field, context string) ([]string, error) {
	value, ok := record[field]
	if !ok {
		return nil, baselineCatalogErrorf("%s is missing required field %s", context, field)
	}
	var values []any
	switch items := value.(type) {
	case []any:
		values = items
	case []string:
		result := append([]string(nil), items...)
		return result, nil
	default:
		return nil, baselineCatalogErrorf("%s field %s must be an array", context, field)
	}
	result := make([]string, 0, len(values))
	for index, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil, baselineCatalogErrorf(
				"%s field %s item %d must be a string",
				context,
				field,
				index,
			)
		}
		result = append(result, text)
	}
	return result, nil
}

func baselineRequiredAnchors(
	record map[string]any,
	field, context string,
) ([]BaselineAnchor, error) {
	value, ok := record[field]
	if !ok {
		return nil, baselineCatalogErrorf("%s is missing required field %s", context, field)
	}
	records, err := baselineRecords(value, context+" field "+field)
	if err != nil {
		return nil, err
	}
	anchors := make([]BaselineAnchor, 0, len(records))
	for index, anchorRecord := range records {
		anchorContext := fmt.Sprintf("%s field %s item %d", context, field, index)
		decl, requiredErr := baselineRequiredString(anchorRecord, "decl", anchorContext)
		if requiredErr != nil {
			return nil, requiredErr
		}
		quote, requiredErr := baselineRequiredString(anchorRecord, "quote", anchorContext)
		if requiredErr != nil {
			return nil, requiredErr
		}
		anchors = append(anchors, BaselineAnchor{Decl: decl, Quote: quote})
	}
	return anchors, nil
}

func baselineInt(value any) (int, error) {
	var number int64
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int8:
		return int(typed), nil
	case int16:
		return int(typed), nil
	case int32:
		return int(typed), nil
	case int64:
		number = typed
	case uint:
		if uint64(typed) > uint64(maxBaselineInt()) {
			return 0, fmt.Errorf("integer overflow")
		}
		return int(typed), nil
	case uint8:
		return int(typed), nil
	case uint16:
		return int(typed), nil
	case uint32:
		return int(typed), nil
	case uint64:
		if typed > uint64(maxBaselineInt()) {
			return 0, fmt.Errorf("integer overflow")
		}
		return int(typed), nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed {
			return 0, fmt.Errorf("not an integer")
		}
		if typed < float64(minBaselineInt()) || typed > float64(maxBaselineInt()) {
			return 0, fmt.Errorf("integer overflow")
		}
		return int(typed), nil
	case float32:
		value64 := float64(typed)
		if math.Trunc(value64) != value64 {
			return 0, fmt.Errorf("not an integer")
		}
		return int(typed), nil
	case json.Number:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		if err != nil {
			return 0, err
		}
		number = parsed
	default:
		return 0, fmt.Errorf("not a number")
	}
	if number < int64(minBaselineInt()) || number > int64(maxBaselineInt()) {
		return 0, fmt.Errorf("integer overflow")
	}
	return int(number), nil
}

func maxBaselineInt() int { return int(^uint(0) >> 1) }
func minBaselineInt() int { return -maxBaselineInt() - 1 }

func baselineCatalogErrorf(format string, args ...any) error {
	return &BaselineCatalogError{Message: fmt.Sprintf(format, args...)}
}

func isBaselineAssetKind(kind string) bool {
	for _, candidate := range baselineAssetKinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

func isBaselineDetected(presence string) bool {
	return presence == "present" || presence == "partial"
}

func isBaselineSecurityProperty(property string) bool {
	switch property {
	case "authentication", "integrity", "non_repudiation", "confidentiality", "availability", "authorization":
		return true
	default:
		return false
	}
}

func baselinePresenceRank(presence string) int {
	switch presence {
	case "present":
		return 0
	case "partial":
		return 1
	case "absent":
		return 2
	default:
		return 3
	}
}

func baselineFold(value string) string { return strings.ToLower(value) }

func baselineHumanName(value string) string {
	if value == "" {
		return value
	}
	first, size := utf8.DecodeRuneInString(value)
	return string(unicode.ToUpper(first)) + value[size:]
}

func baselineContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sortedBaselineKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneBaselineMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = cloneBaselineValue(item)
	}
	return result
}

func cloneBaselineValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneBaselineMap(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = cloneBaselineValue(item)
		}
		return result
	case []map[string]any:
		result := make([]map[string]any, len(typed))
		for index, item := range typed {
			result[index] = cloneBaselineMap(item)
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func cloneBaselineAsset(asset BaselineAsset) BaselineAsset {
	asset.SourceIDs = append([]string(nil), asset.SourceIDs...)
	asset.Routes = append([]BaselineEndpointRoute(nil), asset.Routes...)
	return asset
}

func cloneBaselineControl(control BaselineControl) BaselineControl {
	control.ASVS = append([]string(nil), control.ASVS...)
	control.SourceObservationIDs = append([]string(nil), control.SourceObservationIDs...)
	return control
}

func cloneBaselineImplementation(implementation BaselineImplementation) BaselineImplementation {
	implementation.Anchors = append([]BaselineAnchor(nil), implementation.Anchors...)
	implementation.SourceObservationIDs = append(
		[]string(nil), implementation.SourceObservationIDs...,
	)
	return implementation
}

func cloneBaselineProtections(protections []BaselineProtection) []BaselineProtection {
	result := make([]BaselineProtection, len(protections))
	for index, protection := range protections {
		protection.ImplementationIDs = append([]string(nil), protection.ImplementationIDs...)
		protection.Evidence = append([]BaselineAnchor(nil), protection.Evidence...)
		protection.Checked = append([]string(nil), protection.Checked...)
		protection.SourceObservationIDs = append(
			[]string(nil), protection.SourceObservationIDs...,
		)
		result[index] = protection
	}
	return result
}
