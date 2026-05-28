package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

type projectionManifest struct {
	Version         int            `json:"version"`
	DatabasePath    string         `json:"database_path,omitempty"`
	ProjectionFile  string         `json:"projection_file"`
	ProjectionCount int            `json:"projection_count"`
	Counts          map[string]int `json:"counts"`
	BuiltAt         time.Time      `json:"built_at"`
}

type projectionDocument struct {
	ProjectionID string         `json:"projection_id"`
	SearchText   string         `json:"search_text"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	ObservedAtMS *uint64        `json:"observed_at_ms,omitempty"`
}

type raxRawDocument struct {
	DocID       string         `json:"doc_id"`
	Text        string         `json:"text"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	TimestampMS *uint64        `json:"timestamp_ms,omitempty"`
}

type indexBuildResult struct {
	Root            string         `json:"root"`
	ManifestPath    string         `json:"manifest_path"`
	ProjectionPath  string         `json:"projection_path"`
	ProjectionCount int            `json:"projection_count"`
	Counts          map[string]int `json:"counts"`
}

type indexStatusResult struct {
	Root            string         `json:"root"`
	ManifestPath    string         `json:"manifest_path"`
	ProjectionPath  string         `json:"projection_path"`
	ProjectionCount int            `json:"projection_count"`
	Counts          map[string]int `json:"counts"`
}

type indexVerifyResult struct {
	Root            string         `json:"root"`
	ManifestPath    string         `json:"manifest_path"`
	ProjectionPath  string         `json:"projection_path"`
	Valid           bool           `json:"valid"`
	ProjectionCount int            `json:"projection_count"`
	Counts          map[string]int `json:"counts"`
}

type indexPublishRaxResult struct {
	Root             string `json:"root"`
	ProjectionPath   string `json:"projection_path"`
	StorePath        string `json:"store_path"`
	WaxBin           string `json:"wax_bin"`
	Published        bool   `json:"published"`
	RaxDocumentCount int    `json:"rax_document_count"`
}

func (c cli) runIndex(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul index build --db PATH --root DIR [--json]
  yeoul index rebuild --db PATH --root DIR [--json]
  yeoul index status --root DIR [--json]
  yeoul index verify --db PATH --root DIR [--json]
  yeoul index publish-rax --root DIR --store FILE [--wax-bin PATH] [--json]
`)
	if len(args) == 0 {
		return &usageError{message: usage}
	}
	switch args[0] {
	case "build":
		return c.runIndexBuild(ctx, args[1:])
	case "rebuild":
		return c.runIndexBuild(ctx, args[1:])
	case "status":
		return c.runIndexStatus(args[1:])
	case "verify":
		return c.runIndexVerify(ctx, args[1:])
	case "publish-rax":
		return c.runIndexPublishRax(ctx, args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(c.stdout, usage)
		return nil
	default:
		return &usageError{message: usage}
	}
}

func (c cli) runIndexBuild(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul index build --db PATH --root DIR [--json]
`)
	fs := newFlagSet("index build")
	var dbPath string
	var root string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&root, "root", "", "index root directory")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(root) == "" {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	payload, err := exportDatabase(ctx, dbPath)
	if err != nil {
		return err
	}
	projections, manifest := buildProjectionArtifacts(dbPath, payload)
	manifestPath, projectionPath, err := writeProjectionArtifacts(root, projections, manifest)
	if err != nil {
		return err
	}
	result := indexBuildResult{
		Root:            root,
		ManifestPath:    manifestPath,
		ProjectionPath:  projectionPath,
		ProjectionCount: manifest.ProjectionCount,
		Counts:          manifest.Counts,
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "built index %s (%d projections)\n", root, manifest.ProjectionCount)
	return err
}

func (c cli) runIndexStatus(args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul index status --root DIR [--json]
`)
	fs := newFlagSet("index status")
	var root string
	var jsonOut bool
	fs.StringVar(&root, "root", "", "index root directory")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(root) == "" {
		return &usageError{message: usage}
	}
	manifestPath, projectionPath, manifest, err := loadProjectionManifest(root)
	if err != nil {
		return err
	}
	result := indexStatusResult{
		Root:            root,
		ManifestPath:    manifestPath,
		ProjectionPath:  projectionPath,
		ProjectionCount: manifest.ProjectionCount,
		Counts:          manifest.Counts,
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "index %s projections=%d\n", root, manifest.ProjectionCount)
	return err
}

func (c cli) runIndexVerify(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul index verify --db PATH --root DIR [--json]
`)
	fs := newFlagSet("index verify")
	var dbPath string
	var root string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&root, "root", "", "index root directory")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(root) == "" {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	manifestPath, projectionPath, manifest, err := loadProjectionManifest(root)
	if err != nil {
		return err
	}
	actualProjections, err := loadProjectionDocuments(projectionPath)
	if err != nil {
		return err
	}
	payload, err := exportDatabase(ctx, dbPath)
	if err != nil {
		return err
	}
	expectedProjections, expected := buildProjectionArtifacts(dbPath, payload)
	if len(actualProjections) != manifest.ProjectionCount {
		return fmt.Errorf("index verification failed: projection count %d does not match manifest count %d", len(actualProjections), manifest.ProjectionCount)
	}
	if !sameCounts(expected.Counts, manifest.Counts) {
		return fmt.Errorf("index verification failed: database counts %v do not match manifest counts %v", expected.Counts, manifest.Counts)
	}
	same, err := sameProjectionDocuments(expectedProjections, actualProjections)
	if err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("index verification failed: projection documents do not match database records")
	}
	result := indexVerifyResult{
		Root:            root,
		ManifestPath:    manifestPath,
		ProjectionPath:  projectionPath,
		Valid:           true,
		ProjectionCount: manifest.ProjectionCount,
		Counts:          manifest.Counts,
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "verified index %s (%d projections)\n", root, manifest.ProjectionCount)
	return err
}

func (c cli) runIndexPublishRax(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul index publish-rax --root DIR --store FILE [--wax-bin PATH] [--json]
`)
	fs := newFlagSet("index publish-rax")
	var root string
	var storePath string
	var waxBin string
	var jsonOut bool
	fs.StringVar(&root, "root", "", "index root directory")
	fs.StringVar(&storePath, "store", "", "rax direct .wax store path")
	fs.StringVar(&waxBin, "wax-bin", "wax", "wax CLI binary path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(root) == "" || strings.TrimSpace(storePath) == "" {
		return &usageError{message: usage}
	}
	_, projectionPath, manifest, err := loadProjectionManifest(root)
	if err != nil {
		return err
	}
	projections, err := loadProjectionDocuments(projectionPath)
	if err != nil {
		return err
	}
	if len(projections) != manifest.ProjectionCount {
		return fmt.Errorf("rax publish failed: projection count %d does not match manifest count %d", len(projections), manifest.ProjectionCount)
	}

	rawDocsPath, err := writeTemporaryRaxRawDocuments(root, projections)
	if err != nil {
		return err
	}
	defer os.Remove(rawDocsPath)

	cmd := exec.CommandContext(ctx, waxBin, "ingest", "docs", "--store", storePath, "--input", rawDocsPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rax publish failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	result := indexPublishRaxResult{
		Root:             root,
		ProjectionPath:   projectionPath,
		StorePath:        storePath,
		WaxBin:           waxBin,
		Published:        true,
		RaxDocumentCount: len(projections),
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "published %d projections to rax store %s\n", len(projections), storePath)
	return err
}

func buildProjectionArtifacts(dbPath string, payload *exportFile) ([]projectionDocument, projectionManifest) {
	projections := make([]projectionDocument, 0, len(payload.Episodes)+len(payload.Entities)+len(payload.Facts))
	counts := map[string]int{
		"episodes": len(payload.Episodes),
		"entities": len(payload.Entities),
		"facts":    len(payload.Facts),
	}

	for _, episode := range payload.Episodes {
		meta := map[string]any{
			"projection_type": "episode",
			"record_id":       episode.ID,
			"space_id":        episode.SpaceID,
			"kind":            episode.Kind,
			"source_id":       episode.SourceID,
			"group_id":        episode.GroupID,
		}
		if !episode.ObservedAt.IsZero() {
			meta["observed_at"] = episode.ObservedAt.UTC().Format(time.RFC3339)
		}
		if len(episode.Metadata) > 0 {
			meta["record_metadata"] = episode.Metadata
		}
		doc := projectionDocument{
			ProjectionID: "episode:" + episode.ID,
			SearchText:   strings.TrimSpace(episode.Content),
			Metadata:     meta,
		}
		if !episode.ObservedAt.IsZero() {
			ts := uint64(episode.ObservedAt.UTC().UnixMilli())
			doc.ObservedAtMS = &ts
		}
		projections = append(projections, doc)
	}

	for _, entity := range payload.Entities {
		meta := map[string]any{
			"projection_type": "entity",
			"record_id":       entity.ID,
			"space_id":        entity.SpaceID,
			"namespace":       entity.Namespace,
			"entity_type":     entity.Type,
			"canonical_name":  entity.CanonicalName,
			"aliases":         entity.Aliases,
		}
		if len(entity.Metadata) > 0 {
			meta["record_metadata"] = entity.Metadata
		}
		text := strings.TrimSpace(strings.Join(compactStrings(entity.CanonicalName, strings.Join(entity.Aliases, " ")), " "))
		projections = append(projections, projectionDocument{
			ProjectionID: "entity:" + entity.ID,
			SearchText:   text,
			Metadata:     meta,
		})
	}

	for _, fact := range payload.Facts {
		meta := map[string]any{
			"projection_type":        "fact",
			"record_id":              fact.ID,
			"space_id":               fact.SpaceID,
			"predicate":              fact.Predicate,
			"subject_id":             fact.SubjectID,
			"object_id":              fact.ObjectID,
			"status":                 fact.Status,
			"supporting_episode_ids": fact.SupportingEpisodeIDs,
		}
		if !fact.ObservedAt.IsZero() {
			meta["observed_at"] = fact.ObservedAt.UTC().Format(time.RFC3339)
		}
		if !fact.ValidFrom.IsZero() {
			meta["valid_from"] = fact.ValidFrom.UTC().Format(time.RFC3339)
		}
		if !fact.ValidTo.IsZero() {
			meta["valid_to"] = fact.ValidTo.UTC().Format(time.RFC3339)
		}
		if len(fact.Metadata) > 0 {
			meta["record_metadata"] = fact.Metadata
		}
		text := strings.TrimSpace(strings.Join(compactStrings(fact.Predicate, fact.SubjectID, fact.ObjectID, fact.ValueText), " "))
		doc := projectionDocument{
			ProjectionID: "fact:" + fact.ID,
			SearchText:   text,
			Metadata:     meta,
		}
		if !fact.ObservedAt.IsZero() {
			ts := uint64(fact.ObservedAt.UTC().UnixMilli())
			doc.ObservedAtMS = &ts
		}
		projections = append(projections, doc)
	}

	sort.Slice(projections, func(i, j int) bool { return projections[i].ProjectionID < projections[j].ProjectionID })
	manifest := projectionManifest{
		Version:         1,
		DatabasePath:    dbPath,
		ProjectionFile:  "projection.ndjson",
		ProjectionCount: len(projections),
		Counts:          counts,
		BuiltAt:         time.Now().UTC(),
	}
	return projections, manifest
}

func writeProjectionArtifacts(root string, projections []projectionDocument, manifest projectionManifest) (string, string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", "", err
	}
	manifestPath := filepath.Join(root, "yeoul-index.json")
	projectionPath := filepath.Join(root, manifest.ProjectionFile)

	projectionFile, err := os.CreateTemp(root, ".projection-*.ndjson")
	if err != nil {
		return "", "", err
	}
	projectionTempPath := projectionFile.Name()
	removeProjectionTemp := true
	defer func() {
		if removeProjectionTemp {
			os.Remove(projectionTempPath)
		}
	}()
	projectionFileOpen := true
	defer func() {
		if projectionFileOpen {
			projectionFile.Close()
		}
	}()

	encoder := json.NewEncoder(projectionFile)
	for _, projection := range projections {
		if err := encoder.Encode(projection); err != nil {
			return "", "", err
		}
	}
	if err := projectionFile.Close(); err != nil {
		return "", "", err
	}
	projectionFileOpen = false

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", "", err
	}
	manifestFile, err := os.CreateTemp(root, ".yeoul-index-*.json")
	if err != nil {
		return "", "", err
	}
	manifestTempPath := manifestFile.Name()
	removeManifestTemp := true
	defer func() {
		if removeManifestTemp {
			os.Remove(manifestTempPath)
		}
	}()
	if n, err := manifestFile.Write(data); err != nil {
		manifestFile.Close()
		return "", "", err
	} else if n != len(data) {
		manifestFile.Close()
		return "", "", io.ErrShortWrite
	}
	if err := manifestFile.Close(); err != nil {
		return "", "", err
	}
	if err := replaceProjectionFile(projectionTempPath, projectionPath); err != nil {
		return "", "", err
	}
	removeProjectionTemp = false
	if err := replaceProjectionFile(manifestTempPath, manifestPath); err != nil {
		return "", "", err
	}
	removeManifestTemp = false
	return manifestPath, projectionPath, nil
}

func replaceProjectionFile(tempPath, finalPath string) error {
	if err := os.Rename(tempPath, finalPath); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	} else if _, statErr := os.Stat(tempPath); statErr != nil {
		return err
	}
	if err := os.Remove(finalPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tempPath, finalPath)
}

func loadProjectionManifest(root string) (string, string, projectionManifest, error) {
	manifestPath := filepath.Join(root, "yeoul-index.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", "", projectionManifest{}, err
	}
	var manifest projectionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", "", projectionManifest{}, err
	}
	projectionFileName, err := safeProjectionFileName(manifest.ProjectionFile)
	if err != nil {
		return "", "", projectionManifest{}, err
	}
	projectionPath := filepath.Join(root, projectionFileName)
	if _, err := os.Stat(projectionPath); err != nil {
		return "", "", projectionManifest{}, err
	}
	return manifestPath, projectionPath, manifest, nil
}

func safeProjectionFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) || filepath.Clean(name) != name {
		return "", fmt.Errorf("invalid projection file in manifest: %q", name)
	}
	return name, nil
}

func loadProjectionDocuments(path string) ([]projectionDocument, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	projections := make([]projectionDocument, 0)
	for {
		var projection projectionDocument
		if err := decoder.Decode(&projection); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if strings.TrimSpace(projection.ProjectionID) == "" {
			return nil, fmt.Errorf("projection missing projection_id")
		}
		projections = append(projections, projection)
	}
	return projections, nil
}

func writeTemporaryRaxRawDocuments(root string, projections []projectionDocument) (string, error) {
	file, err := os.CreateTemp(root, "rax-projection-*.jsonl")
	if err != nil {
		return "", err
	}
	path := file.Name()
	removeOnError := true
	defer func() {
		if removeOnError {
			os.Remove(path)
		}
	}()

	encoder := json.NewEncoder(file)
	for _, projection := range projections {
		rawDocument := raxRawDocument{
			DocID:       projection.ProjectionID,
			Text:        projection.SearchText,
			Metadata:    projection.Metadata,
			TimestampMS: projection.ObservedAtMS,
		}
		if err := encoder.Encode(rawDocument); err != nil {
			file.Close()
			return "", err
		}
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	removeOnError = false
	return path, nil
}

func sameCounts(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func sameProjectionDocuments(expected, actual []projectionDocument) (bool, error) {
	if len(expected) != len(actual) {
		return false, nil
	}
	expectedByID, ok, err := projectionDocumentMap(expected)
	if err != nil || !ok {
		return ok, err
	}
	actualByID, ok, err := projectionDocumentMap(actual)
	if err != nil || !ok {
		return ok, err
	}
	for id, expectedData := range expectedByID {
		if actualByID[id] != expectedData {
			return false, nil
		}
	}
	return true, nil
}

func projectionDocumentMap(projections []projectionDocument) (map[string]string, bool, error) {
	byID := make(map[string]string, len(projections))
	for _, projection := range projections {
		if _, exists := byID[projection.ProjectionID]; exists {
			return nil, false, nil
		}
		data, err := json.Marshal(projection)
		if err != nil {
			return nil, false, err
		}
		byID[projection.ProjectionID] = string(data)
	}
	return byID, true, nil
}
