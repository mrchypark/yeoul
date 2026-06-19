package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	"github.com/mrchypark/yeoul/pkg/yeoul"
)

type projectionManifest struct {
	Version         int            `json:"version"`
	DatabasePath    string         `json:"database_path,omitempty"`
	DatabaseSize    int64          `json:"database_size,omitempty"`
	DatabaseModTime time.Time      `json:"database_mod_time,omitempty"`
	ProjectionHash  string         `json:"projection_hash,omitempty"`
	ProjectionFile  string         `json:"projection_file,omitempty"`
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

type raxSearchHit struct {
	DocID string `json:"doc_id"`
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
	RaxRuntime       string `json:"rax_runtime"`
	Published        bool   `json:"published"`
	RaxDocumentCount int    `json:"rax_document_count"`
}

type raxRuntime struct {
	Kind string
	Path string
}

const raxChunkMarker = "#chunk:"
const projectionManifestVersion = 3

func (c cli) runIndex(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul index build --db PATH --root DIR [--json]
  yeoul index rebuild --db PATH --root DIR [--json]
  yeoul index status --root DIR [--json]
  yeoul index verify --db PATH --root DIR [--json]
  yeoul index publish-rax --root DIR --store FILE [--rax-lib PATH] [--rax-bin PATH] [--json]
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
  yeoul index publish-rax --root DIR --store FILE [--rax-lib PATH] [--rax-bin PATH] [--json]
`)
	fs := newFlagSet("index publish-rax")
	var root string
	var storePath string
	var raxLib string
	var raxBin string
	var jsonOut bool
	fs.StringVar(&root, "root", "", "index root directory")
	fs.StringVar(&storePath, "store", "", "rax 0.4.4 .rax retrieval index file")
	fs.StringVar(&raxLib, "rax-lib", "", "rax FFI library path")
	fs.StringVar(&raxBin, "rax-bin", "", "rax CLI binary path")
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

	runtime, ok := lookupRaxRuntime(raxLib, raxBin)
	if !ok {
		return fmt.Errorf("rax publish failed: bundled rax FFI runtime not found; reinstall Yeoul or pass --rax-lib for development")
	}
	if _, err := raxIngestDocs(ctx, runtime, storePath, rawDocsPath); err != nil {
		return fmt.Errorf("rax publish failed: %w", err)
	}
	result := indexPublishRaxResult{
		Root:             root,
		ProjectionPath:   projectionPath,
		StorePath:        storePath,
		RaxRuntime:       runtime.String(),
		Published:        true,
		RaxDocumentCount: len(projections),
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "published %d projections to rax store %s\n", len(projections), storePath)
	return err
}

func (r raxRuntime) String() string {
	if r.Kind == "" || r.Path == "" {
		return ""
	}
	return r.Kind + ":" + r.Path
}

func lookupRaxRuntime(explicitLib, explicitBin string) (raxRuntime, bool) {
	if path, ok := lookupRaxLibrary(explicitLib); ok {
		return raxRuntime{Kind: "ffi", Path: path}, true
	}
	if path, ok := lookupRaxBinary(explicitBin); ok {
		return raxRuntime{Kind: "cli", Path: path}, true
	}
	return raxRuntime{}, false
}

func lookupRaxLibrary(explicit string) (string, bool) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, true
	}
	if envPath := strings.TrimSpace(os.Getenv("YEOUL_RAX_LIB")); envPath != "" {
		return envPath, true
	}
	for _, candidate := range bundledRaxLibraryCandidates() {
		if isRegularFile(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func lookupRaxBinary(explicit string) (string, bool) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, true
	}
	if envPath := strings.TrimSpace(os.Getenv("YEOUL_RAX_BIN")); envPath != "" {
		return envPath, true
	}
	for _, candidate := range bundledRaxBinaryCandidates() {
		if isExecutableFile(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func bundledRaxLibraryCandidates() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	exeDir := filepath.Dir(exe)
	name := raxLibraryName()
	return []string{
		filepath.Join(exeDir, "..", "lib", name),
		filepath.Join(exeDir, name),
		filepath.Join(exeDir, "..", "libexec", "yeoul", name),
	}
}

func bundledRaxBinaryCandidates() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	exeDir := filepath.Dir(exe)
	name := raxExecutableName()
	return []string{
		filepath.Join(exeDir, name),
		filepath.Join(exeDir, "..", "bin", name),
		filepath.Join(exeDir, "..", "libexec", "yeoul", name),
	}
}

func raxLibraryName() string {
	switch runtime.GOOS {
	case "darwin":
		return "librax_ffi.dylib"
	case "linux":
		return "librax_ffi.so"
	case "windows":
		return "rax_ffi.dll"
	default:
		return "librax_ffi"
	}
}

func raxExecutableName() string {
	if runtime.GOOS == "windows" {
		return "rax.exe"
	}
	return "rax"
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

func maybeRerankSearchWithRax(ctx context.Context, dbPath, query, backend, raxLib, raxBin string, limit int, resp *yeoul.SearchResponse) (*yeoul.SearchResponse, error) {
	if backend == "core" || resp == nil || len(resp.Hits) == 0 {
		return resp, nil
	}
	runtime, ok := lookupRaxRuntime(raxLib, raxBin)
	if !ok {
		if backend == "rax" {
			return nil, fmt.Errorf("rax search failed: bundled rax FFI runtime not found; reinstall Yeoul or pass --rax-lib for development")
		}
		return resp, nil
	}
	docIDs, err := runManagedRaxSearch(ctx, dbPath, query, limit, runtime)
	if err != nil {
		if backend == "rax" {
			return nil, err
		}
		return resp, nil
	}
	return rerankSearchResponseByRax(resp, docIDs), nil
}

func runRaxPrimarySearch(ctx context.Context, eng yeoul.Engine, dbPath string, req yeoul.SearchRequest, raxLib, raxBin string) (*yeoul.SearchResponse, error) {
	runtime, ok := lookupRaxRuntime(raxLib, raxBin)
	if !ok {
		return nil, fmt.Errorf("rax search failed: bundled rax FFI runtime not found; reinstall Yeoul or pass --rax-lib for development")
	}
	limit := req.Page.Limit
	if limit <= 0 {
		limit = 10
	}
	docIDs, err := runManagedRaxSearch(ctx, dbPath, req.QueryText, limit*4, runtime)
	if err != nil {
		return nil, err
	}
	return buildRaxPrimarySearchResponse(ctx, eng, req, docIDs)
}

func buildRaxPrimarySearchResponse(ctx context.Context, eng yeoul.Engine, req yeoul.SearchRequest, docIDs []string) (*yeoul.SearchResponse, error) {
	limit := req.Page.Limit
	if limit <= 0 {
		limit = 10
	}
	hits := make([]yeoul.SearchHit, 0, len(docIDs))
	included := yeoul.IncludedRecords{}
	types := compactStrings(req.Types...)
	if len(types) == 0 {
		types = []string{"fact", "episode", "entity"}
	}
	typeOK := map[string]bool{}
	for _, typ := range types {
		typeOK[typ] = true
	}
	seenRecords := map[string]bool{}
	for rank, docID := range docIDs {
		kind, id, ok := raxRecordKindID(docID)
		if !ok || !typeOK[kind] {
			continue
		}
		recordKey := kind + ":" + id
		if seenRecords[recordKey] {
			continue
		}
		seenRecords[recordKey] = true
		record, err := eng.GetRecord(ctx, yeoul.GetRecordRequest{
			Meta:     req.Meta,
			Kind:     kind,
			ID:       id,
			Temporal: req.Temporal,
		})
		if err != nil {
			continue
		}
		if !raxRecordPassesFilters(record.Record, req) {
			continue
		}
		hit := yeoul.SearchHit{
			HitID:       "hit_" + id,
			HitType:     kind,
			RecordID:    id,
			Score:       1 / float64(rank+1),
			MatchedText: raxMatchedText(record.Record),
			Reasons:     []string{fmt.Sprintf("rax_candidate_rank:%d", rank+1)},
		}
		if req.MinScore != nil && hit.Score < *req.MinScore {
			continue
		}
		hits = append(hits, hit)
		if len(hits) >= limit {
			break
		}
		if req.Include.Provenance || req.Include.SupportingEpisodes || req.Include.RelatedEntities || req.Include.Snippets {
			addIncludedRecord(&included, record.Record)
		}
	}
	now := time.Now().UTC()
	spaceID := strings.TrimSpace(req.Meta.SpaceID)
	if spaceID == "" {
		spaceID = "default"
	}
	resp := &yeoul.SearchResponse{
		Meta: yeoul.QueryResponseMeta{SpaceID: spaceID, SnapshotAt: &now},
		Hits: hits,
	}
	if req.Include.Provenance || req.Include.SupportingEpisodes || req.Include.RelatedEntities || req.Include.Snippets {
		resp.Included = included
	}
	return resp, nil
}

func runManagedRaxSearch(ctx context.Context, dbPath, query string, limit int, runtime raxRuntime) ([]string, error) {
	storePath, err := ensureManagedRaxStore(ctx, dbPath, runtime)
	if err != nil {
		return nil, err
	}
	topK := limit
	if topK <= 0 {
		topK = 10
	}
	output, err := raxSearchText(ctx, runtime, storePath, query, topK)
	if err != nil {
		return nil, fmt.Errorf("rax search failed: %w", err)
	}
	return parseRaxDocIDs(output)
}

func ensureManagedRaxStore(ctx context.Context, dbPath string, runtime raxRuntime) (string, error) {
	root, storePath, err := managedRaxIndexPaths(dbPath)
	if err != nil {
		return "", err
	}
	if ok, err := managedRaxStoreFresh(root, storePath, dbPath); err != nil {
		return "", err
	} else if !ok {
		payload, err := exportDatabase(ctx, dbPath)
		if err != nil {
			return "", err
		}
		projections, manifest := buildProjectionArtifacts(dbPath, payload)
		if err := rebuildManagedRaxStore(ctx, root, storePath, runtime, projections, manifest); err != nil {
			return "", err
		}
	}
	return storePath, nil
}

func parseRaxDocIDs(output []byte) ([]string, error) {
	var hits []raxSearchHit
	if err := json.Unmarshal(output, &hits); err == nil {
		docIDs := make([]string, 0, len(hits))
		seen := map[string]bool{}
		for _, hit := range hits {
			docID := strings.TrimSpace(hit.DocID)
			if docID == "" || seen[docID] {
				continue
			}
			seen[docID] = true
			docIDs = append(docIDs, docID)
		}
		return docIDs, nil
	}

	var rawDocIDs []string
	if err := json.Unmarshal(output, &rawDocIDs); err != nil {
		return nil, fmt.Errorf("rax search failed: invalid JSON output: %w", err)
	}
	docIDs := make([]string, 0, len(rawDocIDs))
	seen := map[string]bool{}
	for _, rawDocID := range rawDocIDs {
		docID := strings.TrimSpace(rawDocID)
		if docID == "" || seen[docID] {
			continue
		}
		seen[docID] = true
		docIDs = append(docIDs, docID)
	}
	return docIDs, nil
}

func managedRaxStoreFresh(root, storePath, dbPath string) (bool, error) {
	if _, err := os.Stat(storePath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	manifest, err := readProjectionManifest(root)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, nil
	}
	stat, err := os.Stat(dbPath)
	if err != nil {
		return false, err
	}
	// ponytail: file stat avoids exporting the DB on every search; use a DB
	// revision if Ladybug exposes one.
	return manifest.Version == projectionManifestVersion &&
		manifest.DatabaseSize > 0 &&
		manifest.DatabaseSize == stat.Size() &&
		!manifest.DatabaseModTime.IsZero() &&
		manifest.DatabaseModTime.Equal(stat.ModTime()), nil
}

func rebuildManagedRaxStore(ctx context.Context, root, storePath string, runtime raxRuntime, projections []projectionDocument, manifest projectionManifest) error {
	if err := os.Remove(storePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if runtime.Kind == "ffi" {
		manifest.ProjectionFile = ""
		if _, err := writeProjectionManifest(root, manifest); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(root, "projection.ndjson"))
		jsonl, err := raxRawDocumentsJSONL(projections)
		if err != nil {
			return err
		}
		if _, err := raxFFIIngestDocs(runtime.Path, storePath, jsonl); err != nil {
			return fmt.Errorf("rax ingest failed: %w", err)
		}
		return nil
	}
	if _, _, err := writeProjectionArtifacts(root, projections, manifest); err != nil {
		return err
	}
	rawDocsPath, err := writeTemporaryRaxRawDocuments(root, projections)
	if err != nil {
		return err
	}
	defer os.Remove(rawDocsPath)
	if _, err := raxIngestDocs(ctx, runtime, storePath, rawDocsPath); err != nil {
		return fmt.Errorf("rax ingest failed: %w", err)
	}
	return nil
}

func raxIngestDocs(ctx context.Context, runtime raxRuntime, storePath, rawDocsPath string) ([]byte, error) {
	switch runtime.Kind {
	case "ffi":
		jsonl, err := os.ReadFile(rawDocsPath)
		if err != nil {
			return nil, err
		}
		return raxFFIIngestDocs(runtime.Path, storePath, jsonl)
	case "cli":
		cmd := exec.CommandContext(ctx, runtime.Path, "ingest", "docs", "--store", storePath, "--input", rawDocsPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
		}
		return output, nil
	default:
		return nil, fmt.Errorf("unknown rax runtime kind %q", runtime.Kind)
	}
}

func raxSearchText(ctx context.Context, runtime raxRuntime, storePath, query string, topK int) ([]byte, error) {
	switch runtime.Kind {
	case "ffi":
		searcher, err := openRaxFFISearcher(runtime.Path, storePath)
		if err != nil {
			return nil, err
		}
		defer searcher.close()
		return searcher.searchText(storePath, query, topK)
	case "cli":
		cmd := exec.CommandContext(ctx, runtime.Path, "search", "--store", storePath, "--mode", "text", "--text", query, "--top-k", fmt.Sprint(topK))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
		}
		return output, nil
	default:
		return nil, fmt.Errorf("unknown rax runtime kind %q", runtime.Kind)
	}
}

func managedRaxIndexPaths(dbPath string) (string, string, error) {
	absDBPath, err := filepath.Abs(dbPath)
	if err != nil {
		return "", "", err
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		cacheRoot = os.TempDir()
	}
	sum := sha256.Sum256([]byte(absDBPath))
	key := hex.EncodeToString(sum[:])[:16]
	root := filepath.Join(cacheRoot, "yeoul", "rax", key)
	return root, filepath.Join(root, "projection.rax"), nil
}

func rerankSearchResponseByRax(resp *yeoul.SearchResponse, docIDs []string) *yeoul.SearchResponse {
	if resp == nil || len(resp.Hits) == 0 || len(docIDs) == 0 {
		return resp
	}
	rank := make(map[string]int, len(docIDs))
	for i, docID := range docIDs {
		if projectionID, ok := raxRecordProjectionID(docID); ok {
			rank[projectionID] = i
		}
	}
	out := *resp
	out.Hits = append([]yeoul.SearchHit(nil), resp.Hits...)
	sort.SliceStable(out.Hits, func(i, j int) bool {
		leftRank, leftOK := rank[raxProjectionIDForHit(out.Hits[i])]
		rightRank, rightOK := rank[raxProjectionIDForHit(out.Hits[j])]
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK {
			return leftRank < rightRank
		}
		return false
	})
	for i := range out.Hits {
		if rank, ok := rank[raxProjectionIDForHit(out.Hits[i])]; ok {
			out.Hits[i].Reasons = append(out.Hits[i].Reasons, fmt.Sprintf("rax_candidate_rank:%d", rank+1))
		}
	}
	return &out
}

func raxProjectionIDForHit(hit yeoul.SearchHit) string {
	return strings.TrimSpace(hit.HitType) + ":" + strings.TrimSpace(hit.RecordID)
}

func raxRecordProjectionID(docID string) (string, bool) {
	kind, id, ok := raxRecordKindID(docID)
	if !ok {
		return "", false
	}
	return kind + ":" + id, true
}

func raxRecordKindID(docID string) (string, string, bool) {
	kind, id, ok := strings.Cut(strings.TrimSpace(docID), ":")
	if !ok || kind == "" || id == "" {
		return "", "", false
	}
	if before, _, ok := strings.Cut(id, raxChunkMarker); ok {
		id = before
	}
	if id == "" {
		return "", "", false
	}
	return kind, id, true
}

func raxRecordPassesFilters(record any, req yeoul.SearchRequest) bool {
	switch value := record.(type) {
	case *yeoul.Fact:
		if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, value.Predicate) {
			return false
		}
		return true
	case *yeoul.Episode, *yeoul.Entity:
		return len(req.Predicates) == 0
	default:
		return false
	}
}

func raxMatchedText(record any) string {
	switch value := record.(type) {
	case *yeoul.Fact:
		return value.ValueText
	case *yeoul.Episode:
		return value.Content
	case *yeoul.Entity:
		return value.CanonicalName
	default:
		return ""
	}
}

func addIncludedRecord(included *yeoul.IncludedRecords, record any) {
	switch value := record.(type) {
	case *yeoul.Fact:
		included.Facts = append(included.Facts, *value)
	case *yeoul.Episode:
		included.Episodes = append(included.Episodes, *value)
	case *yeoul.Entity:
		included.Entities = append(included.Entities, *value)
	}
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
			ts := projectionTimestampMS(episode.ObservedAt)
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
			ts := projectionTimestampMS(fact.ObservedAt)
			doc.ObservedAtMS = &ts
		}
		projections = append(projections, doc)
	}

	sort.Slice(projections, func(i, j int) bool { return projections[i].ProjectionID < projections[j].ProjectionID })
	manifest := projectionManifest{
		Version:         projectionManifestVersion,
		DatabasePath:    dbPath,
		ProjectionFile:  "projection.ndjson",
		ProjectionCount: len(projections),
		Counts:          counts,
		BuiltAt:         time.Now().UTC(),
	}
	if stat, err := os.Stat(dbPath); err == nil {
		manifest.DatabaseSize = stat.Size()
		manifest.DatabaseModTime = stat.ModTime()
	}
	manifest.ProjectionHash = projectionHash(projections)
	return projections, manifest
}

func projectionHash(projections []projectionDocument) string {
	hash := sha256.New()
	for _, projection := range projections {
		_, _ = io.WriteString(hash, projection.ProjectionID)
		_, _ = io.WriteString(hash, "\x00")
		_, _ = io.WriteString(hash, projection.SearchText)
		_, _ = io.WriteString(hash, "\x00")
		if projection.ObservedAtMS != nil {
			_, _ = fmt.Fprintf(hash, "%d", *projection.ObservedAtMS)
		}
		_, _ = io.WriteString(hash, "\n")
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func projectionTimestampMS(observedAt time.Time) uint64 {
	milli := observedAt.UTC().UnixMilli()
	if milli < 0 {
		return 0
	}
	return uint64(milli)
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
	manifestFileOpen := true
	defer func() {
		if manifestFileOpen {
			manifestFile.Close()
		}
	}()
	if n, err := manifestFile.Write(data); err != nil {
		return "", "", err
	} else if n != len(data) {
		return "", "", io.ErrShortWrite
	}
	if err := manifestFile.Close(); err != nil {
		return "", "", err
	}
	manifestFileOpen = false
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

func writeProjectionManifest(root string, manifest projectionManifest) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	manifestPath := filepath.Join(root, "yeoul-index.json")
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(root, ".yeoul-index-*.json")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := replaceProjectionFile(tempPath, manifestPath); err != nil {
		return "", err
	}
	removeTemp = false
	return manifestPath, nil
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
	manifest, err := readProjectionManifest(root)
	if err != nil {
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

func readProjectionManifest(root string) (projectionManifest, error) {
	data, err := os.ReadFile(filepath.Join(root, "yeoul-index.json"))
	if err != nil {
		return projectionManifest{}, err
	}
	var manifest projectionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return projectionManifest{}, err
	}
	return manifest, nil
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
	fileOpen := true
	defer func() {
		if fileOpen {
			file.Close()
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
			return "", err
		}
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	fileOpen = false
	removeOnError = false
	return path, nil
}

func raxRawDocumentsJSONL(projections []projectionDocument) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for _, projection := range projections {
		rawDocument := raxRawDocument{
			DocID:       projection.ProjectionID,
			Text:        projection.SearchText,
			Metadata:    projection.Metadata,
			TimestampMS: projection.ObservedAtMS,
		}
		if err := encoder.Encode(rawDocument); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
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
		actualData, exists := actualByID[id]
		if !exists {
			return false, nil
		}
		same, err := sameProjectionDocument(expectedData, actualData)
		if err != nil || !same {
			return same, err
		}
	}
	return true, nil
}

func sameProjectionDocument(expected, actual projectionDocument) (bool, error) {
	if expected.ProjectionID != actual.ProjectionID || expected.SearchText != actual.SearchText {
		return false, nil
	}
	if (expected.ObservedAtMS == nil) != (actual.ObservedAtMS == nil) {
		return false, nil
	}
	if expected.ObservedAtMS != nil && *expected.ObservedAtMS != *actual.ObservedAtMS {
		return false, nil
	}
	expectedMetadata, err := json.Marshal(expected.Metadata)
	if err != nil {
		return false, err
	}
	actualMetadata, err := json.Marshal(actual.Metadata)
	if err != nil {
		return false, err
	}
	if string(expectedMetadata) != string(actualMetadata) {
		return false, nil
	}
	return true, nil
}

func projectionDocumentMap(projections []projectionDocument) (map[string]projectionDocument, bool, error) {
	byID := make(map[string]projectionDocument, len(projections))
	for _, projection := range projections {
		if _, exists := byID[projection.ProjectionID]; exists {
			return nil, false, nil
		}
		byID[projection.ProjectionID] = projection
	}
	return byID, true, nil
}
