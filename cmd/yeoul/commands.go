package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	latticedb "github.com/mrchypark/latticedb-go"
	ladybugstorage "github.com/mrchypark/yeoul/internal/storage/ladybug"
	lstorage "github.com/mrchypark/yeoul/internal/storage/lattice"
	"github.com/mrchypark/yeoul/pkg/policy"
	"github.com/mrchypark/yeoul/pkg/retrieval"
	"github.com/mrchypark/yeoul/pkg/yeoul"
)

type initResult struct {
	DatabasePath string `json:"database_path"`
	Created      bool   `json:"created"`
}

type ingestJSONFile struct {
	Episodes        []yeoul.EpisodeInput   `json:"episodes,omitempty"`
	Entities        []yeoul.EntityInput    `json:"entities,omitempty"`
	Facts           []yeoul.FactInput      `json:"facts,omitempty"`
	EntityRevisions []yeoul.EntityRevision `json:"entity_revisions,omitempty"`
	FactRevisions   []yeoul.FactRevision   `json:"fact_revisions,omitempty"`
}

type ingestJSONResult struct {
	DatabasePath string   `json:"database_path"`
	EpisodeIDs   []string `json:"episode_ids,omitempty"`
	EntityIDs    []string `json:"entity_ids,omitempty"`
	FactIDs      []string `json:"fact_ids,omitempty"`
}

type migrateResult struct {
	DatabasePath string `json:"database_path"`
	Migrated     bool   `json:"migrated"`
}

type inspectSchemaResult struct {
	DatabasePath string               `json:"database_path"`
	Version      string               `json:"version,omitempty"`
	Tables       []inspectSchemaTable `json:"tables,omitempty"`
}

type inspectSchemaTable struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Database string `json:"database,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

type inspectCountsResult struct {
	DatabasePath string         `json:"database_path"`
	Counts       map[string]int `json:"counts"`
}

type exportFile struct {
	Episodes        []yeoul.EpisodeInput   `json:"episodes,omitempty"`
	Entities        []yeoul.EntityInput    `json:"entities,omitempty"`
	Facts           []yeoul.FactInput      `json:"facts,omitempty"`
	EntityRevisions []yeoul.EntityRevision `json:"entity_revisions,omitempty"`
	FactRevisions   []yeoul.FactRevision   `json:"fact_revisions,omitempty"`
}

type benchIngestResult struct {
	DatabasePath      string  `json:"database_path"`
	Episodes          int     `json:"episodes"`
	FactsPerEpisode   int     `json:"facts_per_episode"`
	ElapsedSeconds    float64 `json:"elapsed_seconds"`
	EpisodesPerSecond float64 `json:"episodes_per_second"`
	FactsPerSecond    float64 `json:"facts_per_second"`
}

type benchQueryResult struct {
	DatabasePath string             `json:"database_path"`
	Query        string             `json:"query"`
	Iterations   int                `json:"iterations"`
	Metrics      map[string]latency `json:"metrics"`
}

type latency struct {
	P50Millis float64 `json:"p50_ms"`
	P95Millis float64 `json:"p95_ms"`
	P99Millis float64 `json:"p99_ms"`
}

type benchLifecycleResult struct {
	DatabasePath    string  `json:"database_path"`
	Iterations      int     `json:"iterations"`
	ElapsedSeconds  float64 `json:"elapsed_seconds"`
	OpsPerSecond    float64 `json:"ops_per_second"`
	SupersedeCount  int     `json:"supersede_count"`
	RetractionCount int     `json:"retraction_count"`
}

type entityMergeCandidate struct {
	TargetID      string   `json:"target_id"`
	SourceIDs     []string `json:"source_ids"`
	Namespace     string   `json:"namespace,omitempty"`
	Type          string   `json:"type"`
	CanonicalName string   `json:"canonical_name"`
}

type factDuplicateCandidate struct {
	TargetID  string   `json:"target_id"`
	SourceIDs []string `json:"source_ids"`
	Predicate string   `json:"predicate"`
	SubjectID string   `json:"subject_id"`
	ObjectID  string   `json:"object_id,omitempty"`
	ValueText string   `json:"value_text,omitempty"`
}

const (
	observedAtBasisKey               = "observed_at_basis"
	observedAtSupportingEpisodeIDKey = "observed_at_supporting_episode_id"
	observedAtBasisExplicit          = "explicit"
	observedAtBasisSupportingEpisode = "supporting_episode"
	observedAtBasisSystemTimeDefault = "system_time_default"
)

// ensureForceInitTarget verifies that dbPath names a recognizable Yeoul
// database that no other process currently owns. init --force replaces the
// database, so the ownership and shape checks must run before any recursive
// removal.
func ensureForceInitTarget(dbPath string) error {
	info, err := os.Stat(dbPath)
	if err != nil {
		return fmt.Errorf("stat database: %w", err)
	}
	if info.IsDir() {
		store, err := lstorage.Open(dbPath, false, false)
		if err != nil {
			if errors.Is(err, latticedb.ErrDatabaseLocked) {
				return fmt.Errorf("refusing to replace %s: the database is in use by another process", dbPath)
			}
			return fmt.Errorf("refusing to remove %s: not a recognized Lattice database: %w", dbPath, err)
		}
		return store.Close()
	}
	legacy, err := ladybugstorage.Open(dbPath, true)
	if err != nil {
		return fmt.Errorf("refusing to remove %s: not a recognized Ladybug database: %w", dbPath, err)
	}
	legacy.Close()
	return nil
}

func (c cli) runInit(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul init --db PATH [--force] [--json]
`)

	fs := newFlagSet("init")
	var dbPath string
	var force bool
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.BoolVar(&force, "force", false, "replace an existing database file")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	created := false
	if _, err := os.Stat(dbPath); err == nil {
		if force {
			if !c.confirm {
				return &usageError{message: usage + "\n\nThis operation is destructive. Re-run with --confirm."}
			}
			if err := ensureForceInitTarget(dbPath); err != nil {
				return err
			}
			if err := os.RemoveAll(dbPath); err != nil {
				return fmt.Errorf("remove existing database: %w", err)
			}
			created = true
		}
	} else if os.IsNotExist(err) {
		created = true
	} else {
		return fmt.Errorf("stat database: %w", err)
	}

	eng, err := yeoul.Open(ctx, yeoul.Config{
		DatabasePath:    dbPath,
		CreateIfMissing: true,
	})
	if err != nil {
		return err
	}
	if err := closeEngine(ctx, eng); err != nil {
		return err
	}

	result := initResult{
		DatabasePath: dbPath,
		Created:      created,
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	if created {
		_, err = fmt.Fprintf(c.stdout, "initialized %s\n", dbPath)
		return err
	}
	_, err = fmt.Fprintf(c.stdout, "database already exists at %s\n", dbPath)
	return err
}

func (c cli) runMigrate(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul migrate --db PATH [--json]
`)

	fs := newFlagSet("migrate")
	var dbPath string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	if err := closeEngine(ctx, eng); err != nil {
		return err
	}

	result := migrateResult{
		DatabasePath: dbPath,
		Migrated:     true,
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "migrated %s\n", dbPath)
	return err
}

func (c cli) runIngest(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul ingest episode --db PATH --kind KIND (--content TEXT | --content-file FILE) [flags]
  yeoul ingest file --db PATH --kind KIND --file FILE [flags]
  yeoul ingest json --db PATH --file FILE [--json]
  yeoul ingest batch --db PATH --file FILE [--json]
`)

	if len(args) == 0 {
		return &usageError{message: usage}
	}

	switch args[0] {
	case "episode":
		return c.runIngestEpisode(ctx, args[1:])
	case "file":
		return c.runIngestFile(ctx, args[1:])
	case "json":
		return c.runIngestJSON(ctx, args[1:])
	case "batch":
		return c.runIngestJSON(ctx, args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(c.stdout, usage)
		return nil
	default:
		return &usageError{message: usage}
	}
}

func (c cli) runIngestEpisode(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul ingest episode --db PATH --kind KIND (--content TEXT | --content-file FILE) [--id ID] [--group-id GROUP]
      [--source-id ID] [--source-kind KIND] [--source-uri URI] [--source-external-ref REF]
      [--observed-at RFC3339] [--policy-path PATH] [--json]
`)

	fs := newFlagSet("ingest episode")
	var dbPath string
	var jsonOut bool
	var episodeID string
	var kind string
	var content string
	var contentFile string
	var groupID string
	var sourceID string
	var sourceKind string
	var sourceURI string
	var sourceExternalRef string
	var observedAtRaw string
	var policyPath string
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	fs.StringVar(&episodeID, "id", "", "episode ID")
	fs.StringVar(&kind, "kind", "", "episode kind")
	fs.StringVar(&content, "content", "", "episode content")
	fs.StringVar(&contentFile, "content-file", "", "path to a content file")
	fs.StringVar(&groupID, "group-id", "", "group ID")
	fs.StringVar(&sourceID, "source-id", "", "source ID")
	fs.StringVar(&sourceKind, "source-kind", "", "source kind")
	fs.StringVar(&sourceURI, "source-uri", "", "source URI")
	fs.StringVar(&sourceExternalRef, "source-external-ref", "", "source external ref")
	fs.StringVar(&observedAtRaw, "observed-at", "", "observed time in RFC3339 format")
	fs.StringVar(&policyPath, "policy-path", "", "policy pack path")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	if strings.TrimSpace(kind) == "" {
		return &usageError{message: usage}
	}
	if strings.TrimSpace(content) != "" && strings.TrimSpace(contentFile) != "" {
		return &usageError{message: usage}
	}
	if strings.TrimSpace(contentFile) != "" {
		fileContent, err := readFile(contentFile)
		if err != nil {
			return fmt.Errorf("read content file: %w", err)
		}
		content = fileContent
	}
	if strings.TrimSpace(content) == "" {
		return &usageError{message: usage}
	}
	if policyPath != "" {
		pack, err := policy.LoadPack(policyPath)
		if err != nil {
			return err
		}
		if shouldDropEpisode(pack, content) {
			result := map[string]any{
				"database_path": dbPath,
				"skipped":       true,
				"reason":        "policy_drop",
			}
			if jsonOut {
				return writeJSON(c.stdout, result)
			}
			_, err := fmt.Fprintln(c.stdout, "skipped episode by policy drop rule")
			return err
		}
	}

	var observedAt time.Time
	observedAtBasis := observedAtBasisSystemTimeDefault
	if strings.TrimSpace(observedAtRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, observedAtRaw)
		if err != nil {
			return &usageError{message: usage}
		}
		observedAt = parsed
		observedAtBasis = observedAtBasisExplicit
	} else {
		observedAt = time.Now().UTC()
	}

	input := yeoul.EpisodeInput{
		ID:         episodeID,
		Kind:       kind,
		Content:    content,
		SourceID:   sourceID,
		GroupID:    groupID,
		ObservedAt: observedAt,
		Metadata: map[string]any{
			observedAtBasisKey: observedAtBasis,
		},
	}
	if sourceKind != "" || sourceURI != "" || sourceExternalRef != "" {
		input.Source = yeoul.SourceInput{
			Kind:        sourceKind,
			URI:         sourceURI,
			ExternalRef: sourceExternalRef,
		}
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}

	result, err := eng.IngestEpisode(ctx, input)
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}
	if err := closeEngine(ctx, eng); err != nil {
		return err
	}

	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(
		c.stdout,
		"ingested episode %s (source=%s created=%t)\n",
		result.EpisodeID,
		result.SourceID,
		result.Created,
	)
	return err
}

func (c cli) runIngestFile(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul ingest file --db PATH --kind KIND --file FILE [--id ID] [--group-id GROUP]
      [--source-id ID] [--source-kind KIND] [--source-uri URI] [--source-external-ref REF]
      [--observed-at RFC3339] [--json]
`)

	rewritten := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		if args[i] == "--file" && i+1 < len(args) {
			rewritten = append(rewritten, "--content-file", args[i+1])
			i++
			continue
		}
		rewritten = append(rewritten, args[i])
	}
	hasContentFile := false
	for _, arg := range rewritten {
		if arg == "--content-file" {
			hasContentFile = true
			break
		}
	}
	if !hasContentFile {
		return &usageError{message: usage}
	}
	return c.runIngestEpisode(ctx, rewritten)
}

func (c cli) runIngestJSON(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul ingest json --db PATH --file FILE [--json]
`)

	fs := newFlagSet("ingest json")
	var dbPath string
	var filePath string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&filePath, "file", "", "path to a JSON ingest file")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	if strings.TrimSpace(filePath) == "" {
		return &usageError{message: usage}
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read ingest file: %w", err)
	}

	var payload ingestJSONFile
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("decode ingest file: %w", err)
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}

	batch, err := eng.IngestBatch(ctx, yeoul.BatchInput{
		Episodes: payload.Episodes,
		Entities: payload.Entities,
		Facts:    payload.Facts,
	})
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}
	if err := closeEngine(ctx, eng); err != nil {
		return err
	}

	result := ingestJSONResult{
		DatabasePath: dbPath,
		EpisodeIDs:   batch.EpisodeIDs,
		EntityIDs:    batch.EntityIDs,
		FactIDs:      batch.FactIDs,
	}

	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(
		c.stdout,
		"ingested %d episodes, %d entities, %d facts into %s\n",
		len(result.EpisodeIDs),
		len(result.EntityIDs),
		len(result.FactIDs),
		dbPath,
	)
	return err
}

func (c cli) runGet(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul get --db PATH --kind episode|entity|fact|source --id ID [--as-of RFC3339] [--json]
`)

	fs := newFlagSet("get")
	var dbPath string
	var kind string
	var id string
	var asOfRaw string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&kind, "kind", "", "record kind")
	fs.StringVar(&id, "id", "", "record ID")
	fs.StringVar(&asOfRaw, "as-of", "", "point-in-time view in RFC3339 format")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(id) == "" {
		return &usageError{message: usage}
	}
	temporal, err := parseTemporalFlags(asOfRaw, "", "", true)
	if err != nil {
		return &usageError{message: usage}
	}

	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	resp, err := eng.GetRecord(ctx, yeoul.GetRecordRequest{
		Kind:     kind,
		ID:       id,
		Temporal: temporal,
	})
	if closeErr := closeEngine(ctx, eng); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	if jsonOut {
		return writeJSON(c.stdout, resp)
	}
	return writeJSON(c.stdout, resp.Record)
}

func (c cli) runSearch(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul search --db PATH --query TEXT [--backend auto|core|rax] [--rax-lib PATH] [--rax-bin PATH] [--type fact,episode,entity] [--mode hybrid|keyword|semantic] [--entity ID] [--predicate PREDS] [--min-score N]
      [--group-id IDS] [--as-of RFC3339] [--valid-at RFC3339] [--from RFC3339] [--to RFC3339] [--valid-from RFC3339] [--valid-to RFC3339] [--include-inactive] [--cursor CURSOR]
      [--policy-path PATH] [--recipe NAME] [--limit N] [--include-related] [--json]
`)

	fs := newFlagSet("search")
	var dbPath string
	var query string
	var backend string
	var raxLib string
	var raxBin string
	var typesRaw string
	var mode string
	var entityID string
	var predicatesRaw string
	var groupIDsRaw string
	var minScore float64
	var minScoreSet bool
	var asOfRaw string
	var validAtRaw string
	var fromRaw string
	var toRaw string
	var validFromRaw string
	var validToRaw string
	var includeInactive bool
	var cursor string
	var policyPath string
	var recipeName string
	var limit int
	var includeRelated bool
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&query, "query", "", "query text")
	fs.StringVar(&backend, "backend", "auto", "search backend: auto, core, rax")
	fs.StringVar(&raxLib, "rax-lib", "", "rax FFI library path")
	fs.StringVar(&raxBin, "rax-bin", "", "rax CLI binary path")
	fs.StringVar(&typesRaw, "type", "", "comma-separated hit types")
	fs.StringVar(&mode, "mode", "hybrid", "search mode: hybrid, keyword, semantic")
	fs.StringVar(&entityID, "entity", "", "entity anchor ID")
	fs.StringVar(&predicatesRaw, "predicate", "", "comma-separated predicates")
	fs.StringVar(&groupIDsRaw, "group-id", "", "comma-separated group IDs")
	fs.Func("min-score", "minimum score threshold", func(value string) error {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		minScore = parsed
		minScoreSet = true
		return nil
	})
	fs.StringVar(&asOfRaw, "as-of", "", "knowledge-time view in RFC3339 format")
	fs.StringVar(&validAtRaw, "valid-at", "", "domain-valid point in RFC3339 format")
	fs.StringVar(&fromRaw, "from", "", "observed start time in RFC3339 format")
	fs.StringVar(&toRaw, "to", "", "observed end time in RFC3339 format")
	fs.StringVar(&validFromRaw, "valid-from", "", "domain-valid interval start in RFC3339 format")
	fs.StringVar(&validToRaw, "valid-to", "", "domain-valid interval end in RFC3339 format")
	fs.BoolVar(&includeInactive, "include-inactive", false, "include inactive facts")
	fs.StringVar(&cursor, "cursor", "", "opaque pagination cursor")
	fs.StringVar(&policyPath, "policy-path", "", "policy pack path")
	fs.StringVar(&recipeName, "recipe", "", "search recipe name")
	fs.IntVar(&limit, "limit", 10, "maximum number of hits")
	fs.BoolVar(&includeRelated, "include-related", false, "include related records")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	if strings.TrimSpace(query) == "" {
		return &usageError{message: usage}
	}
	backend = strings.ToLower(strings.TrimSpace(backend))
	if backend != "auto" && backend != "core" && backend != "rax" {
		return &usageError{message: usage}
	}
	temporal, err := parseTemporalFlagsFull(asOfRaw, validAtRaw, fromRaw, toRaw, validFromRaw, validToRaw, includeInactive)
	if err != nil {
		return &usageError{message: usage}
	}

	req := yeoul.SearchRequest{
		QueryText:  query,
		Mode:       yeoul.SearchMode(strings.ToLower(strings.TrimSpace(mode))),
		Types:      splitCSV(typesRaw),
		AnchorIDs:  splitCSV(entityID),
		Predicates: splitCSV(predicatesRaw),
		Scope:      yeoul.ScopeFilter{GroupIDs: splitCSV(groupIDsRaw)},
		Temporal:   temporal,
		Page: yeoul.Page{
			Limit:  limit,
			Cursor: cursor,
		},
	}
	if minScoreSet {
		req.MinScore = &minScore
	}
	if policyPath != "" || recipeName != "" {
		if policyPath == "" || recipeName == "" {
			return &usageError{message: usage}
		}
		pack, err := policy.LoadPack(policyPath)
		if err != nil {
			return err
		}
		req, err = applySearchRecipe(pack, recipeName, req)
		if err != nil {
			return err
		}
	}
	if includeRelated {
		req.Include = yeoul.Include{
			Provenance:         true,
			SupportingEpisodes: true,
			RelatedEntities:    true,
			Snippets:           true,
		}
	}

	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	var resp *yeoul.SearchResponse
	if backend == "rax" {
		resp, err = runRaxPrimarySearch(ctx, eng, dbPath, req, raxLib, raxBin)
	} else {
		resp, err = eng.Search(ctx, req)
		if err == nil {
			resp, err = maybeRerankSearchWithRax(ctx, eng, dbPath, query, backend, raxLib, raxBin, limit, resp)
		}
	}
	if closeErr := closeEngine(ctx, eng); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	if jsonOut {
		return writeJSON(c.stdout, resp)
	}
	if len(resp.Hits) == 0 {
		_, err = fmt.Fprintln(c.stdout, "no hits")
		return err
	}

	for _, hit := range resp.Hits {
		if _, err := fmt.Fprintf(c.stdout, "[%s] %s score=%.2f\n", hit.HitType, hit.RecordID, hit.Score); err != nil {
			return err
		}
		if text := shorten(hit.MatchedText, 96); text != "" {
			if _, err := fmt.Fprintf(c.stdout, "  %s\n", text); err != nil {
				return err
			}
		}
	}
	if includeRelated {
		_, err = fmt.Fprintf(
			c.stdout,
			"included: %d episodes, %d entities, %d facts, %d sources\n",
			len(resp.Included.Episodes),
			len(resp.Included.Entities),
			len(resp.Included.Facts),
			len(resp.Included.Sources),
		)
		return err
	}
	return nil
}

func (c cli) runContext(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul context --db PATH --query TEXT [--type fact,episode,entity] [--entity ID] [--limit N] [--max-blocks N] [--max-text-runes N] [--json]
`)
	fs := newFlagSet("context")
	var dbPath string
	var query string
	var typesRaw string
	var entityID string
	var limit int
	var maxBlocks int
	var maxTextRunes int
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&query, "query", "", "query text")
	fs.StringVar(&typesRaw, "type", "", "comma-separated hit types")
	fs.StringVar(&entityID, "entity", "", "entity anchor ID")
	fs.IntVar(&limit, "limit", 10, "maximum number of hits")
	fs.IntVar(&maxBlocks, "max-blocks", 16, "maximum context blocks")
	fs.IntVar(&maxTextRunes, "max-text-runes", 512, "maximum runes per block")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(query) == "" {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	searchResp, err := eng.Search(ctx, yeoul.SearchRequest{
		QueryText: query,
		Types:     splitCSV(typesRaw),
		AnchorIDs: splitCSV(entityID),
		Include: yeoul.Include{
			SupportingEpisodes: true,
			RelatedEntities:    true,
		},
		Page: yeoul.Page{Limit: limit},
	})
	if closeErr := closeEngine(ctx, eng); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	bundle := retrieval.BuildContext(*searchResp, retrieval.ContextOptions{MaxBlocks: maxBlocks, MaxTextRunes: maxTextRunes})
	if jsonOut {
		return writeJSON(c.stdout, bundle)
	}
	for _, block := range bundle.Blocks {
		if _, err := fmt.Fprintf(c.stdout, "[%s] %s %s\n", block.Kind, block.Title, block.Text); err != nil {
			return err
		}
	}
	return nil
}

func (c cli) runInspect(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul inspect schema --db PATH [--json]
  yeoul inspect counts --db PATH [--json]
  yeoul inspect entity --db PATH --id ID [--json]
  yeoul inspect fact --db PATH --id ID [--json]
  yeoul inspect episode --db PATH --id ID [--json]
  yeoul inspect source --db PATH --id ID [--json]
`)

	if len(args) == 0 {
		return &usageError{message: usage}
	}

	switch args[0] {
	case "schema":
		return c.runInspectSchema(ctx, args[1:])
	case "counts":
		return c.runInspectCounts(ctx, args[1:])
	case "entity", "fact", "episode", "source":
		return c.runInspectRecord(ctx, args[0], args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(c.stdout, usage)
		return nil
	default:
		return &usageError{message: usage}
	}
}

func (c cli) runInspectSchema(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul inspect schema --db PATH [--json]
`)

	fs := newFlagSet("inspect schema")
	var dbPath string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = closeEngine(ctx, eng) }()

	result := inspectSchemaResult{
		DatabasePath: dbPath,
		Version:      latticedb.Version(),
		Tables:       make([]inspectSchemaTable, 0, 7),
	}
	for _, label := range []string{"Source", "Episode", "Entity", "Fact", "FactRevision", "EntityRevision", "YeoulMigration"} {
		result.Tables = append(result.Tables, inspectSchemaTable{Name: label, Type: "NODE"})
	}

	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	if _, err := fmt.Fprintf(c.stdout, "version: %s\n", result.Version); err != nil {
		return err
	}
	for _, table := range result.Tables {
		if _, err := fmt.Fprintf(c.stdout, "%s\t%s\n", table.Type, table.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c cli) runInspectCounts(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul inspect counts --db PATH [--json]
`)

	fs := newFlagSet("inspect counts")
	var dbPath string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = closeEngine(ctx, eng) }()
	snapshot, err := yeoul.Snapshot(ctx, eng)
	if err != nil {
		return err
	}
	counts := map[string]int{
		"sources":  len(snapshot.Sources),
		"episodes": len(snapshot.Episodes),
		"entities": len(snapshot.Entities),
		"facts":    len(snapshot.Facts),
	}

	result := inspectCountsResult{
		DatabasePath: dbPath,
		Counts:       counts,
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	for _, key := range []string{"sources", "episodes", "entities", "facts"} {
		if _, err := fmt.Fprintf(c.stdout, "%s: %d\n", key, counts[key]); err != nil {
			return err
		}
	}
	return nil
}

func (c cli) runInspectRecord(ctx context.Context, kind string, args []string) error {
	usage := strings.TrimSpace(fmt.Sprintf(`
Usage:
  yeoul inspect %s --db PATH --id ID [--json]
`, kind))

	fs := newFlagSet("inspect " + kind)
	var dbPath string
	var id string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&id, "id", "", "record ID")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(id) == "" {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	resp, err := eng.GetRecord(ctx, yeoul.GetRecordRequest{Kind: kind, ID: id})
	if closeErr := closeEngine(ctx, eng); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, resp)
	}
	return writeJSON(c.stdout, resp.Record)
}

func (c cli) runTimeline(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul timeline --db PATH [--entity ID | --fact ID | --episode ID | --source ID] [--event-type TYPES]
      [--as-of RFC3339] [--from RFC3339] [--to RFC3339] [--descending] [--cursor CURSOR] [--limit N] [--json]
`)

	fs := newFlagSet("timeline")
	var dbPath string
	var entityID string
	var factID string
	var episodeID string
	var sourceID string
	var eventTypesRaw string
	var asOfRaw string
	var validAtRaw string
	var fromRaw string
	var toRaw string
	var validFromRaw string
	var validToRaw string
	var descending bool
	var cursor string
	var limit int
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&entityID, "entity", "", "entity anchor ID")
	fs.StringVar(&factID, "fact", "", "fact anchor ID")
	fs.StringVar(&episodeID, "episode", "", "episode anchor ID")
	fs.StringVar(&sourceID, "source", "", "source anchor ID")
	fs.StringVar(&eventTypesRaw, "event-type", "", "comma-separated event types")
	fs.StringVar(&asOfRaw, "as-of", "", "knowledge-time view in RFC3339 format")
	fs.StringVar(&validAtRaw, "valid-at", "", "domain-valid point in RFC3339 format")
	fs.StringVar(&fromRaw, "from", "", "observed start time in RFC3339 format")
	fs.StringVar(&toRaw, "to", "", "observed end time in RFC3339 format")
	fs.StringVar(&validFromRaw, "valid-from", "", "domain-valid interval start in RFC3339 format")
	fs.StringVar(&validToRaw, "valid-to", "", "domain-valid interval end in RFC3339 format")
	fs.BoolVar(&descending, "descending", false, "sort descending by timestamp")
	fs.StringVar(&cursor, "cursor", "", "opaque pagination cursor")
	fs.IntVar(&limit, "limit", 25, "maximum number of events")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	temporal, err := parseTemporalFlagsFull(asOfRaw, validAtRaw, fromRaw, toRaw, validFromRaw, validToRaw, false)
	if err != nil {
		return &usageError{message: usage}
	}
	anchors := compactStrings(entityID, factID, episodeID, sourceID)

	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	resp, err := eng.Timeline(ctx, yeoul.TimelineRequest{
		AnchorIDs:  anchors,
		EventTypes: splitCSV(eventTypesRaw),
		Temporal:   temporal,
		Descending: descending,
		Page: yeoul.Page{
			Limit:  limit,
			Cursor: cursor,
		},
	})
	if closeErr := closeEngine(ctx, eng); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, resp)
	}
	for _, event := range resp.Events {
		if _, err := fmt.Fprintf(c.stdout, "%s\t%s\t%s\t%s\n", event.Timestamp.Format(time.RFC3339), event.EventType, event.RecordID, shorten(event.Summary, 80)); err != nil {
			return err
		}
	}
	if resp.Meta.NextCursor != "" {
		_, err = fmt.Fprintf(c.stdout, "next_cursor: %s\n", resp.Meta.NextCursor)
		return err
	}
	return nil
}

func (c cli) runProvenance(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul provenance --db PATH (--kind KIND --id ID | --entity ID | --fact ID | --episode ID) [--as-of RFC3339] [--max-depth N] [--json]
`)

	fs := newFlagSet("provenance")
	var dbPath string
	var kind string
	var id string
	var entityID string
	var factID string
	var episodeID string
	var asOfRaw string
	var maxDepth int
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&kind, "kind", "", "record kind")
	fs.StringVar(&id, "id", "", "record ID")
	fs.StringVar(&entityID, "entity", "", "entity ID")
	fs.StringVar(&factID, "fact", "", "fact ID")
	fs.StringVar(&episodeID, "episode", "", "episode ID")
	fs.StringVar(&asOfRaw, "as-of", "", "point-in-time view in RFC3339 format")
	fs.IntVar(&maxDepth, "max-depth", 8, "maximum expansion depth")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	if entityID != "" {
		kind, id = "entity", entityID
	}
	if factID != "" {
		kind, id = "fact", factID
	}
	if episodeID != "" {
		kind, id = "episode", episodeID
	}
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(id) == "" {
		return &usageError{message: usage}
	}
	temporal, err := parseTemporalFlags(asOfRaw, "", "", true)
	if err != nil {
		return &usageError{message: usage}
	}

	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	resp, err := eng.Provenance(ctx, yeoul.ProvenanceRequest{
		Kind:     kind,
		ID:       id,
		Temporal: temporal,
		MaxDepth: maxDepth,
	})
	if closeErr := closeEngine(ctx, eng); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, resp)
	}
	if _, err := fmt.Fprintf(c.stdout, "root: [%s] %s %s\n", resp.Root.Type, resp.Root.ID, resp.Root.Label); err != nil {
		return err
	}
	for _, edge := range resp.Edges {
		if _, err := fmt.Fprintf(c.stdout, "%s\t%s -> %s\n", edge.Type, edge.FromID, edge.ToID); err != nil {
			return err
		}
	}
	return nil
}

func (c cli) runNeighborhood(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul neighborhood --db PATH (--entity ID | --fact ID | --episode ID) [--hops N] [--max-nodes N] [--json]
`)

	fs := newFlagSet("neighborhood")
	var dbPath string
	var entityID string
	var factID string
	var episodeID string
	var hops int
	var maxNodes int
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&entityID, "entity", "", "entity anchor ID")
	fs.StringVar(&factID, "fact", "", "fact anchor ID")
	fs.StringVar(&episodeID, "episode", "", "episode anchor ID")
	fs.IntVar(&hops, "hops", 1, "maximum hop count")
	fs.IntVar(&maxNodes, "max-nodes", 50, "maximum number of nodes")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	anchors := make([]string, 0, 1)
	for _, value := range []string{entityID, factID, episodeID} {
		if strings.TrimSpace(value) != "" {
			anchors = append(anchors, value)
		}
	}
	if len(anchors) != 1 {
		return &usageError{message: usage}
	}

	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	resp, err := eng.Neighborhood(ctx, yeoul.NeighborhoodRequest{
		AnchorIDs: anchors,
		MaxHops:   hops,
		MaxNodes:  maxNodes,
	})
	if closeErr := closeEngine(ctx, eng); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	if jsonOut {
		return writeJSON(c.stdout, resp)
	}
	if _, err := fmt.Fprintf(c.stdout, "nodes: %d edges: %d\n", len(resp.Nodes), len(resp.Edges)); err != nil {
		return err
	}
	for _, node := range resp.Nodes {
		if _, err := fmt.Fprintf(c.stdout, "[%s] %s %s\n", node.Type, node.ID, node.Label); err != nil {
			return err
		}
	}
	return nil
}
