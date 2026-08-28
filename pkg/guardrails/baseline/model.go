// Package baseline reads, validates, indexes, and selects locally stored
// Guardrails baseline artifacts. It never infers a repository from the current
// working directory.
package baseline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const SchemaVersion = 1

// Status is the durable lifecycle state recorded in baseline.json.
type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// RunMetadata is the small stable subset needed to select and summarize runs.
type RunMetadata struct {
	ID              string
	Status          Status
	StartedAt       string
	CompletedAt     string
	DurationSeconds float64
}

// GitMetadata identifies the exact source snapshot without consulting Git.
type GitMetadata struct {
	Commit string
	Branch string
	Dirty  bool
}

// CodebaseMetadata identifies the scanned codebase without consulting the cwd.
type CodebaseMetadata struct {
	Name string
	Path string
	Git  GitMetadata
}

// Counts summarizes every repeatable section in one artifact.
type Counts struct {
	Classes             int
	Routes              int
	Resources           int
	Roles               int
	AssetObservations   int
	ControlObservations int
	Assets              int
	Controls            int
	Implementations     int
	Unresolved          int
}

// Document is one validated baseline.json. Raw and section accessors return
// independent copies so callers cannot mutate the indexes built from it.
type Document struct {
	SchemaVersion int
	Run           RunMetadata
	Codebase      CodebaseMetadata
	Counts        Counts

	raw                 map[string]any
	sections            map[Collection][]map[string]any
	assetControls       map[string][]map[string]any
	controlObservations []map[string]any
	assetObservations   []map[string]any
}

// Parse validates a canonical schema-version 1 baseline artifact.
func Parse(data []byte) (*Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, artifactError("baseline.json", "invalid JSON: %v", err)
	}
	if raw == nil {
		return nil, artifactError("baseline.json", "top level must be an object")
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return nil, artifactError("baseline.json", "%v", err)
	}
	return parseDocument(raw)
}

// Load reads and validates one regular, non-symlinked baseline.json file.
func Load(path string) (*Document, error) {
	data, err := readRegularFile(path)
	if err != nil {
		return nil, artifactError(path, "could not read artifact: %v", err)
	}
	document, err := Parse(data)
	if baselineErr, ok := err.(*Error); ok {
		baselineErr.Path = path
	}
	return document, err
}

// Raw returns the full additive artifact, including fields unknown to this reader.
func (d *Document) Raw() map[string]any {
	if d == nil {
		return nil
	}
	return cloneMap(d.raw)
}

// Index returns the complete deterministic index section.
func (d *Document) Index() map[string]any {
	if d == nil {
		return nil
	}
	value, _ := d.raw["index"].(map[string]any)
	return cloneMap(value)
}

// Section returns a repeatable top-level collection. Observation collections
// are addressed as CollectionAssetObservations and CollectionControlObservations.
func (d *Document) Section(collection Collection) ([]map[string]any, error) {
	if d == nil {
		return nil, artifactError("baseline.json", "document is nil")
	}
	values, ok := d.sections[collection]
	if !ok {
		return nil, fmt.Errorf("unknown baseline collection %q", collection)
	}
	return cloneRecords(values), nil
}

func parseDocument(raw map[string]any) (*Document, error) {
	version, err := requiredInteger(raw, "schema_version", "baseline")
	if err != nil {
		return nil, err
	}
	if version != SchemaVersion {
		return nil, &Error{
			Code:    ErrorUnsupportedSchema,
			Path:    "baseline.json",
			Message: fmt.Sprintf("unsupported schema_version %d", version),
		}
	}

	runObject, err := requiredObject(raw, "run", "baseline")
	if err != nil {
		return nil, err
	}
	run, err := parseRun(runObject)
	if err != nil {
		return nil, err
	}
	codebaseObject, err := requiredObject(raw, "codebase", "baseline")
	if err != nil {
		return nil, err
	}
	codebase, err := parseCodebase(codebaseObject)
	if err != nil {
		return nil, err
	}
	if _, err := requiredObject(raw, "index", "baseline"); err != nil {
		return nil, err
	}

	document := &Document{
		SchemaVersion: version,
		Run:           run,
		Codebase:      codebase,
		raw:           raw,
		sections:      make(map[Collection][]map[string]any),
		assetControls: make(map[string][]map[string]any),
	}
	for _, collection := range topLevelCollections {
		records, recordsErr := requiredRecords(raw, string(collection), "baseline")
		if recordsErr != nil {
			return nil, recordsErr
		}
		document.sections[collection] = records
	}

	observations, err := requiredObject(raw, "observations", "baseline")
	if err != nil {
		return nil, err
	}
	document.assetObservations, err = requiredRecords(observations, "assets", "baseline.observations")
	if err != nil {
		return nil, err
	}
	document.controlObservations, err = requiredRecords(observations, "controls", "baseline.observations")
	if err != nil {
		return nil, err
	}
	document.sections[CollectionAssetObservations] = document.assetObservations
	document.sections[CollectionControlObservations] = document.controlObservations

	if err := validateDocument(document); err != nil {
		return nil, err
	}
	document.Counts = Counts{
		Classes:             len(document.sections[CollectionClasses]),
		Routes:              len(document.sections[CollectionRoutes]),
		Resources:           len(document.sections[CollectionResources]),
		Roles:               len(document.sections[CollectionRoles]),
		AssetObservations:   len(document.assetObservations),
		ControlObservations: len(document.controlObservations),
		Assets:              len(document.sections[CollectionAssets]),
		Controls:            len(document.sections[CollectionControls]),
		Implementations:     len(document.sections[CollectionImplementations]),
		Unresolved:          len(document.sections[CollectionUnresolved]),
	}
	return document, nil
}

func parseRun(raw map[string]any) (RunMetadata, error) {
	id, err := requiredString(raw, "id", "baseline.run")
	if err != nil {
		return RunMetadata{}, err
	}
	if !safeRunID(id) {
		return RunMetadata{}, artifactError("baseline.run.id", "must be a single safe path component")
	}
	statusValue, err := requiredString(raw, "status", "baseline.run")
	if err != nil {
		return RunMetadata{}, err
	}
	status := Status(statusValue)
	if !validStatus(status) {
		return RunMetadata{}, artifactError("baseline.run.status", "unsupported value %q", status)
	}
	startedAt, err := requiredTimestamp(raw, "started_at", "baseline.run")
	if err != nil {
		return RunMetadata{}, err
	}
	completedAt, err := optionalTimestamp(raw, "completed_at", "baseline.run")
	if err != nil {
		return RunMetadata{}, err
	}
	if status == StatusCompleted && completedAt == "" {
		return RunMetadata{}, artifactError("baseline.run.completed_at", "is required for a completed run")
	}
	duration, err := requiredNonNegativeNumber(raw, "duration_seconds", "baseline.run")
	if err != nil {
		return RunMetadata{}, err
	}
	return RunMetadata{
		ID:              id,
		Status:          status,
		StartedAt:       startedAt,
		CompletedAt:     completedAt,
		DurationSeconds: duration,
	}, nil
}

func parseCodebase(raw map[string]any) (CodebaseMetadata, error) {
	name, err := requiredString(raw, "name", "baseline.codebase")
	if err != nil {
		return CodebaseMetadata{}, err
	}
	path, err := requiredString(raw, "path", "baseline.codebase")
	if err != nil {
		return CodebaseMetadata{}, err
	}
	if !filepath.IsAbs(path) {
		return CodebaseMetadata{}, artifactError("baseline.codebase.path", "must be absolute")
	}
	git := GitMetadata{}
	if value, exists := raw["git"]; exists {
		gitObject, ok := value.(map[string]any)
		if !ok {
			return CodebaseMetadata{}, artifactError("baseline.codebase.git", "must be an object")
		}
		git.Commit, err = optionalString(gitObject, "commit", "baseline.codebase.git")
		if err != nil {
			return CodebaseMetadata{}, err
		}
		git.Branch, err = optionalString(gitObject, "branch", "baseline.codebase.git")
		if err != nil {
			return CodebaseMetadata{}, err
		}
		if dirty, exists := gitObject["dirty"]; exists {
			var ok bool
			git.Dirty, ok = dirty.(bool)
			if !ok {
				return CodebaseMetadata{}, artifactError("baseline.codebase.git.dirty", "must be a boolean")
			}
		}
	}
	return CodebaseMetadata{Name: name, Path: filepath.Clean(path), Git: git}, nil
}

func validStatus(status Status) bool {
	switch status {
	case StatusRunning, StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("invalid trailing data: %w", err)
	}
	return fmt.Errorf("contains more than one JSON value")
}

func requiredObject(record map[string]any, field, context string) (map[string]any, error) {
	value, exists := record[field]
	if !exists {
		return nil, artifactError(context+"."+field, "is required")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, artifactError(context+"."+field, "must be an object")
	}
	return object, nil
}

func requiredRecords(record map[string]any, field, context string) ([]map[string]any, error) {
	value, exists := record[field]
	if !exists {
		return nil, artifactError(context+"."+field, "is required")
	}
	array, ok := value.([]any)
	if !ok {
		return nil, artifactError(context+"."+field, "must be an array")
	}
	records := make([]map[string]any, len(array))
	for index, item := range array {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, artifactError(
				fmt.Sprintf("%s.%s[%d]", context, field, index),
				"must be an object",
			)
		}
		records[index] = object
	}
	return records, nil
}

func requiredString(record map[string]any, field, context string) (string, error) {
	value, exists := record[field]
	if !exists {
		return "", artifactError(context+"."+field, "is required")
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", artifactError(context+"."+field, "must be a non-empty string")
	}
	return strings.TrimSpace(text), nil
}

func optionalString(record map[string]any, field, context string) (string, error) {
	value, exists := record[field]
	if !exists || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", artifactError(context+"."+field, "must be a string")
	}
	return strings.TrimSpace(text), nil
}

func requiredInteger(record map[string]any, field, context string) (int, error) {
	value, exists := record[field]
	if !exists {
		return 0, artifactError(context+"."+field, "is required")
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, artifactError(context+"."+field, "must be an integer")
	}
	integer, err := strconv.Atoi(number.String())
	if err != nil {
		return 0, artifactError(context+"."+field, "must be an integer")
	}
	return integer, nil
}

func requiredNonNegativeNumber(record map[string]any, field, context string) (float64, error) {
	value, exists := record[field]
	if !exists {
		return 0, artifactError(context+"."+field, "is required")
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, artifactError(context+"."+field, "must be a non-negative number")
	}
	numeric, err := strconv.ParseFloat(number.String(), 64)
	if err != nil || numeric < 0 {
		return 0, artifactError(context+"."+field, "must be a non-negative number")
	}
	return numeric, nil
}

func requiredTimestamp(record map[string]any, field, context string) (string, error) {
	value, err := requiredString(record, field, context)
	if err != nil {
		return "", err
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return "", artifactError(context+"."+field, "must be an RFC3339 timestamp")
	}
	return value, nil
}

func optionalTimestamp(record map[string]any, field, context string) (string, error) {
	value, err := optionalString(record, field, context)
	if err != nil || value == "" {
		return value, err
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return "", artifactError(context+"."+field, "must be an RFC3339 timestamp")
	}
	return value, nil
}

func cloneRecords(records []map[string]any) []map[string]any {
	result := make([]map[string]any, len(records))
	for index, record := range records {
		result[index] = cloneMap(record)
	}
	return result
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = cloneValue(item)
	}
	return result
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = cloneValue(item)
		}
		return result
	default:
		return value
	}
}
