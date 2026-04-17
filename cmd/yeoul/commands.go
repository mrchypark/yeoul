package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	lstore "github.com/mrchypark/yeoul/internal/storage/ladybug"
	"github.com/mrchypark/yeoul/pkg/policy"
	"github.com/mrchypark/yeoul/pkg/yeoul"
)

type initResult struct {
	DatabasePath string `json:"database_path"`
	Created      bool   `json:"created"`
}

type ingestJSONFile struct {
	Episodes []yeoul.EpisodeInput `json:"episodes,omitempty"`
	Entities []yeoul.EntityInput  `json:"entities,omitempty"`
	Facts    []yeoul.FactInput    `json:"facts,omitempty"`
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
	Episodes []yeoul.EpisodeInput `json:"episodes,omitempty"`
	Entities []yeoul.EntityInput  `json:"entities,omitempty"`
	Facts    []yeoul.FactInput    `json:"facts,omitempty"`
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
			if err := os.Remove(dbPath); err != nil {
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
		Driver:          yeoul.StorageDriverLadybug,
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
	if strings.TrimSpace(observedAtRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, observedAtRaw)
		if err != nil {
			return &usageError{message: usage}
		}
		observedAt = parsed
	}

	input := yeoul.EpisodeInput{
		ID:         episodeID,
		Kind:       kind,
		Content:    content,
		SourceID:   sourceID,
		GroupID:    groupID,
		ObservedAt: observedAt,
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
  yeoul search --db PATH --query TEXT [--type fact,episode,entity] [--mode hybrid|keyword|semantic] [--entity ID] [--predicate PREDS] [--min-score N]
      [--as-of RFC3339] [--from RFC3339] [--to RFC3339] [--include-inactive] [--cursor CURSOR]
      [--policy-path PATH] [--recipe NAME] [--limit N] [--include-related] [--json]
`)

	fs := newFlagSet("search")
	var dbPath string
	var query string
	var typesRaw string
	var mode string
	var entityID string
	var predicatesRaw string
	var minScore float64
	var minScoreSet bool
	var asOfRaw string
	var fromRaw string
	var toRaw string
	var includeInactive bool
	var cursor string
	var policyPath string
	var recipeName string
	var limit int
	var includeRelated bool
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&query, "query", "", "query text")
	fs.StringVar(&typesRaw, "type", "", "comma-separated hit types")
	fs.StringVar(&mode, "mode", "hybrid", "search mode: hybrid, keyword, semantic")
	fs.StringVar(&entityID, "entity", "", "entity anchor ID")
	fs.StringVar(&predicatesRaw, "predicate", "", "comma-separated predicates")
	fs.Func("min-score", "minimum score threshold", func(value string) error {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		minScore = parsed
		minScoreSet = true
		return nil
	})
	fs.StringVar(&asOfRaw, "as-of", "", "point-in-time view in RFC3339 format")
	fs.StringVar(&fromRaw, "from", "", "start time in RFC3339 format")
	fs.StringVar(&toRaw, "to", "", "end time in RFC3339 format")
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
	temporal, err := parseTemporalFlags(asOfRaw, fromRaw, toRaw, includeInactive)
	if err != nil {
		return &usageError{message: usage}
	}

	req := yeoul.SearchRequest{
		QueryText:  query,
		Mode:       yeoul.SearchMode(strings.ToLower(strings.TrimSpace(mode))),
		Types:      splitCSV(typesRaw),
		AnchorIDs:  splitCSV(entityID),
		Predicates: splitCSV(predicatesRaw),
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
	resp, err := eng.Search(ctx, req)
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
	_ = ctx
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

	store, err := openRawStore(dbPath, true)
	if err != nil {
		return err
	}
	defer store.Close()

	version, err := singleStringQuery(store, "CALL db_version() RETURN *")
	if err != nil {
		return err
	}
	rows, err := queryRows(store, "CALL show_tables() RETURN *")
	if err != nil {
		return err
	}

	result := inspectSchemaResult{
		DatabasePath: dbPath,
		Version:      version,
		Tables:       make([]inspectSchemaTable, 0, len(rows)),
	}
	for _, row := range rows {
		table := inspectSchemaTable{
			Name:     fmt.Sprint(row["name"]),
			Type:     fmt.Sprint(row["type"]),
			Database: fmt.Sprint(row["database name"]),
			Comment:  fmt.Sprint(row["comment"]),
		}
		result.Tables = append(result.Tables, table)
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
	_ = ctx
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

	store, err := openRawStore(dbPath, true)
	if err != nil {
		return err
	}
	defer store.Close()

	counts := map[string]int{}
	for _, item := range []struct {
		key   string
		query string
	}{
		{key: "sources", query: "MATCH (n:Source) RETURN count(n)"},
		{key: "episodes", query: "MATCH (n:Episode) RETURN count(n)"},
		{key: "entities", query: "MATCH (n:Entity) RETURN count(n)"},
		{key: "facts", query: "MATCH (n:Fact) RETURN count(n)"},
	} {
		value, err := singleIntQuery(store, item.query)
		if err != nil {
			return err
		}
		counts[item.key] = value
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
	var fromRaw string
	var toRaw string
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
	fs.StringVar(&asOfRaw, "as-of", "", "point-in-time view in RFC3339 format")
	fs.StringVar(&fromRaw, "from", "", "start time in RFC3339 format")
	fs.StringVar(&toRaw, "to", "", "end time in RFC3339 format")
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
	temporal, err := parseTemporalFlags(asOfRaw, fromRaw, toRaw, false)
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

func (c cli) runFact(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul fact get --db PATH --id ID [--json]
  yeoul fact lookup --db PATH [--subject-id IDS] [--predicate PREDS] [--object-id IDS] [--object-text TEXT] [--as-of RFC3339] [--include-inactive] [--limit N] [--cursor CURSOR] [--json]
  yeoul fact assert --db PATH --predicate PRED --subject-id ID [--object-id ID] [--value-text TEXT] --supporting-episodes IDS [--json]
  yeoul fact supersede --db PATH --id ID --predicate PRED --subject-id ID [--object-id ID] [--value-text TEXT] --supporting-episodes IDS --reason TEXT [--json]
  yeoul fact retract --db PATH --id ID --reason TEXT [--json]
`)

	if len(args) == 0 {
		return &usageError{message: usage}
	}

	switch args[0] {
	case "get":
		return c.runFactGet(ctx, args[1:])
	case "lookup":
		return c.runFactLookup(ctx, args[1:])
	case "assert":
		return c.runFactAssert(ctx, args[1:])
	case "supersede":
		return c.runFactSupersede(ctx, args[1:])
	case "retract":
		return c.runFactRetract(ctx, args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(c.stdout, usage)
		return nil
	default:
		return &usageError{message: usage}
	}
}

func (c cli) runFactGet(ctx context.Context, args []string) error {
	return c.runInspectRecord(ctx, "fact", args)
}

func (c cli) runFactLookup(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul fact lookup --db PATH [--subject-id IDS] [--predicate PREDS] [--object-id IDS] [--object-text TEXT]
      [--as-of RFC3339] [--include-inactive] [--limit N] [--cursor CURSOR] [--json]
`)

	fs := newFlagSet("fact lookup")
	var dbPath string
	var subjectIDsRaw string
	var predicatesRaw string
	var objectIDsRaw string
	var objectText string
	var asOfRaw string
	var includeInactive bool
	var limit int
	var cursor string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&subjectIDsRaw, "subject-id", "", "comma-separated subject IDs")
	fs.StringVar(&predicatesRaw, "predicate", "", "comma-separated predicates")
	fs.StringVar(&objectIDsRaw, "object-id", "", "comma-separated object IDs")
	fs.StringVar(&objectText, "object-text", "", "free-text object/value filter")
	fs.StringVar(&asOfRaw, "as-of", "", "point-in-time view in RFC3339 format")
	fs.BoolVar(&includeInactive, "include-inactive", false, "include inactive facts")
	fs.IntVar(&limit, "limit", 25, "maximum number of facts")
	fs.StringVar(&cursor, "cursor", "", "opaque pagination cursor")
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
	temporal, err := parseTemporalFlags(asOfRaw, "", "", includeInactive)
	if err != nil {
		return &usageError{message: usage}
	}

	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	resp, err := eng.LookupFacts(ctx, yeoul.FactLookupRequest{
		Temporal:   temporal,
		SubjectIDs: splitCSV(subjectIDsRaw),
		Predicates: splitCSV(predicatesRaw),
		ObjectIDs:  splitCSV(objectIDsRaw),
		ObjectText: objectText,
		Include: yeoul.Include{
			Provenance:         true,
			SupportingEpisodes: true,
			RelatedEntities:    true,
		},
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
	for _, fact := range resp.Facts {
		if _, err := fmt.Fprintf(c.stdout, "%s\t%s\t%s\t%s\t%s\n", fact.ID, fact.Predicate, fact.SubjectID, fallbackString(fact.ObjectID, "-"), fact.Status); err != nil {
			return err
		}
	}
	if resp.Meta.NextCursor != "" {
		_, err = fmt.Fprintf(c.stdout, "next_cursor: %s\n", resp.Meta.NextCursor)
		return err
	}
	return nil
}

func (c cli) runFactAssert(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul fact assert --db PATH --predicate PRED --subject-id ID [--object-id ID] [--value-text TEXT] --supporting-episodes IDS [--json]
`)

	fs := newFlagSet("fact assert")
	var dbPath string
	var predicate string
	var subjectID string
	var objectID string
	var valueText string
	var supportingEpisodes string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&predicate, "predicate", "", "fact predicate")
	fs.StringVar(&subjectID, "subject-id", "", "subject entity ID")
	fs.StringVar(&objectID, "object-id", "", "object entity ID")
	fs.StringVar(&valueText, "value-text", "", "value text")
	fs.StringVar(&supportingEpisodes, "supporting-episodes", "", "comma-separated supporting episode IDs")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || predicate == "" || subjectID == "" || supportingEpisodes == "" {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	result, err := eng.AssertFact(ctx, yeoul.FactInput{
		Predicate:            predicate,
		SubjectID:            subjectID,
		ObjectID:             objectID,
		ValueText:            valueText,
		SupportingEpisodeIDs: splitCSV(supportingEpisodes),
	})
	if closeErr := closeEngine(ctx, eng); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "asserted fact %s\n", result.ID)
	return err
}

func (c cli) runFactSupersede(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul fact supersede --db PATH --id ID --predicate PRED --subject-id ID [--object-id ID] [--value-text TEXT] --supporting-episodes IDS --reason TEXT [--json]
`)

	fs := newFlagSet("fact supersede")
	var dbPath string
	var factID string
	var predicate string
	var subjectID string
	var objectID string
	var valueText string
	var supportingEpisodes string
	var reason string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&factID, "id", "", "fact ID to supersede")
	fs.StringVar(&predicate, "predicate", "", "new fact predicate")
	fs.StringVar(&subjectID, "subject-id", "", "subject entity ID")
	fs.StringVar(&objectID, "object-id", "", "object entity ID")
	fs.StringVar(&valueText, "value-text", "", "value text")
	fs.StringVar(&supportingEpisodes, "supporting-episodes", "", "comma-separated supporting episode IDs")
	fs.StringVar(&reason, "reason", "", "supersede reason")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || factID == "" || predicate == "" || subjectID == "" || supportingEpisodes == "" || reason == "" {
		return &usageError{message: usage}
	}
	if !c.confirm {
		return &usageError{message: usage + "\n\nThis operation is destructive. Re-run with --confirm."}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	result, err := eng.SupersedeFact(ctx, factID, yeoul.FactInput{
		Predicate:            predicate,
		SubjectID:            subjectID,
		ObjectID:             objectID,
		ValueText:            valueText,
		SupportingEpisodeIDs: splitCSV(supportingEpisodes),
	}, reason)
	if closeErr := closeEngine(ctx, eng); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "superseded fact %s -> %s\n", result.OldFactID, result.NewFactID)
	return err
}

func (c cli) runFactRetract(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul fact retract --db PATH --id ID --reason TEXT [--json]
`)

	fs := newFlagSet("fact retract")
	var dbPath string
	var factID string
	var reason string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&factID, "id", "", "fact ID")
	fs.StringVar(&reason, "reason", "", "retraction reason")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || factID == "" || reason == "" {
		return &usageError{message: usage}
	}
	if !c.confirm {
		return &usageError{message: usage + "\n\nThis operation is destructive. Re-run with --confirm."}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	result, err := eng.RetractFact(ctx, factID, reason)
	if closeErr := closeEngine(ctx, eng); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "retracted fact %s (%s)\n", result.FactID, result.Status)
	return err
}

func (c cli) runEntity(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul entity get --db PATH --id ID [--json]
  yeoul entity merge-preview --db PATH [--json]
  yeoul entity merge --db PATH --target ID --source IDS --reason TEXT [--json] [--confirm]
`)
	if len(args) == 0 {
		return &usageError{message: usage}
	}
	switch args[0] {
	case "get":
		return c.runInspectRecord(ctx, "entity", args[1:])
	case "merge-preview":
		return c.runEntityMergePreview(ctx, args[1:])
	case "merge":
		return c.runEntityMerge(ctx, args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(c.stdout, usage)
		return nil
	default:
		return &usageError{message: usage}
	}
}

func (c cli) runEntityMergePreview(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul entity merge-preview --db PATH [--json]
`)
	fs := newFlagSet("entity merge-preview")
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
	payload, err := exportDatabase(ctx, dbPath)
	if err != nil {
		return err
	}
	candidates := buildEntityMergeCandidates(payload)
	if jsonOut {
		return writeJSON(c.stdout, candidates)
	}
	if len(candidates) == 0 {
		_, err = fmt.Fprintln(c.stdout, "no exact duplicate entity candidates")
		return err
	}
	for _, candidate := range candidates {
		if _, err := fmt.Fprintf(c.stdout, "target=%s sources=%s key=%s/%s/%s\n", candidate.TargetID, strings.Join(candidate.SourceIDs, ","), candidate.Namespace, candidate.Type, candidate.CanonicalName); err != nil {
			return err
		}
	}
	return nil
}

func (c cli) runEntityMerge(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul entity merge --db PATH --target ID --source IDS --reason TEXT [--json]
`)
	fs := newFlagSet("entity merge")
	var dbPath string
	var targetID string
	var sourceIDsRaw string
	var reason string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&targetID, "target", "", "target entity ID")
	fs.StringVar(&sourceIDsRaw, "source", "", "comma-separated source entity IDs")
	fs.StringVar(&reason, "reason", "", "merge reason")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || targetID == "" || sourceIDsRaw == "" || reason == "" {
		return &usageError{message: usage}
	}
	if !c.confirm {
		return &usageError{message: usage + "\n\nThis operation is destructive. Re-run with --confirm."}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	sourceIDs := splitCSV(sourceIDsRaw)
	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}

	target, err := eng.GetEntity(ctx, targetID)
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}
	mergedFrom := make([]string, 0, len(sourceIDs))
	aliases := append([]string{}, target.Aliases...)
	for _, sourceID := range sourceIDs {
		if sourceID == targetID {
			continue
		}
		source, err := eng.GetEntity(ctx, sourceID)
		if err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		mergedFrom = append(mergedFrom, source.ID)
		aliases = append(aliases, source.CanonicalName)
		aliases = append(aliases, source.Aliases...)
		if _, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
			ID:            source.ID,
			SpaceID:       source.SpaceID,
			Namespace:     source.Namespace,
			Type:          source.Type,
			CanonicalName: source.CanonicalName,
			Aliases:       source.Aliases,
			Metadata: mergeMaps(source.Metadata, map[string]any{
				"duplicate_of": targetID,
				"merge_reason": reason,
				"merge_marked": time.Now().UTC().Format(time.RFC3339),
				"merge_target": targetID,
			}),
		}); err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
	}
	targetMeta := mergeMaps(target.Metadata, map[string]any{
		"merged_from":  mergeStringSlices(anyStrings(target.Metadata["merged_from"]), mergedFrom),
		"merge_reason": reason,
		"merge_marked": time.Now().UTC().Format(time.RFC3339),
	})
	updated, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
		ID:            target.ID,
		SpaceID:       target.SpaceID,
		Namespace:     target.Namespace,
		Type:          target.Type,
		CanonicalName: target.CanonicalName,
		Aliases:       aliases,
		Metadata:      targetMeta,
	})
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}
	if err := closeEngine(ctx, eng); err != nil {
		return err
	}
	result := map[string]any{
		"target_id":    updated.ID,
		"source_ids":   mergedFrom,
		"merge_reason": reason,
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "marked duplicates %s -> %s\n", strings.Join(mergedFrom, ","), updated.ID)
	return err
}

func (c cli) runPolicy(ctx context.Context, args []string) error {
	_ = ctx
	usage := strings.TrimSpace(`
Usage:
  yeoul policy validate --path PATH [--json]
  yeoul policy show --path PATH [--json]
  yeoul policy list-recipes --path PATH [--json]
`)
	if len(args) == 0 {
		return &usageError{message: usage}
	}
	switch args[0] {
	case "validate":
		return c.runPolicyValidate(args[1:])
	case "show":
		return c.runPolicyShow(args[1:])
	case "list-recipes":
		return c.runPolicyListRecipes(args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(c.stdout, usage)
		return nil
	default:
		return &usageError{message: usage}
	}
}

func (c cli) runPolicyValidate(args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul policy validate --path PATH [--json]
`)
	fs := newFlagSet("policy validate")
	var path string
	var jsonOut bool
	fs.StringVar(&path, "path", "", "policy pack path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(path) == "" {
		return &usageError{message: usage}
	}
	result, err := policy.ValidatePack(path)
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	if _, err := fmt.Fprintf(c.stdout, "valid: %t\n", result.Valid); err != nil {
		return err
	}
	for _, issue := range result.Issues {
		if _, err := fmt.Fprintf(c.stdout, "issue: %s\n", issue); err != nil {
			return err
		}
	}
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintf(c.stdout, "warning: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func (c cli) runPolicyShow(args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul policy show --path PATH [--json]
`)
	fs := newFlagSet("policy show")
	var path string
	var jsonOut bool
	fs.StringVar(&path, "path", "", "policy pack path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(path) == "" {
		return &usageError{message: usage}
	}
	pack, err := policy.LoadPack(path)
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, pack)
	}
	_, err = fmt.Fprintf(
		c.stdout,
		"path: %s\nentity_types: %d\npredicates: %d\nrecipes: %d\n",
		pack.Path,
		len(pack.Ontology.EntityTypes),
		len(pack.Ontology.Predicates),
		len(pack.SearchRecipes.Recipes),
	)
	return err
}

func (c cli) runPolicyListRecipes(args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul policy list-recipes --path PATH [--json]
`)
	fs := newFlagSet("policy list-recipes")
	var path string
	var jsonOut bool
	fs.StringVar(&path, "path", "", "policy pack path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(path) == "" {
		return &usageError{message: usage}
	}
	pack, err := policy.LoadPack(path)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(pack.SearchRecipes.Recipes))
	for name := range pack.SearchRecipes.Recipes {
		names = append(names, name)
	}
	sort.Strings(names)
	if jsonOut {
		return writeJSON(c.stdout, names)
	}
	for _, name := range names {
		recipe := pack.SearchRecipes.Recipes[name]
		if _, err := fmt.Fprintf(c.stdout, "%s\t%s\n", name, recipe.Strategy); err != nil {
			return err
		}
	}
	return nil
}

func (c cli) runAdmin(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul admin checkpoint --db PATH [--json]
  yeoul admin compact --db PATH [--apply] [--json]
  yeoul admin export --db PATH --out FILE [--json]
  yeoul admin import --db PATH --in FILE [--json]
`)
	if len(args) == 0 {
		return &usageError{message: usage}
	}
	switch args[0] {
	case "checkpoint":
		return c.runAdminCheckpoint(ctx, args[1:])
	case "compact":
		return c.runAdminCompact(ctx, args[1:])
	case "export":
		return c.runAdminExport(ctx, args[1:])
	case "import":
		return c.runAdminImport(ctx, args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(c.stdout, usage)
		return nil
	default:
		return &usageError{message: usage}
	}
}

func (c cli) runAdminCheckpoint(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul admin checkpoint --db PATH [--json]
`)
	fs := newFlagSet("admin checkpoint")
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
	result := map[string]any{
		"database_path": dbPath,
		"checkpointed":  true,
		"at":            time.Now().UTC().Format(time.RFC3339),
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "checkpointed %s\n", dbPath)
	return err
}

func (c cli) runAdminCompact(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul admin compact --db PATH [--apply] [--json]
`)
	fs := newFlagSet("admin compact")
	var dbPath string
	var apply bool
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.BoolVar(&apply, "apply", false, "apply safe compaction actions")
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
	if apply && !c.confirm {
		return &usageError{message: usage + "\n\nThis operation is destructive. Re-run with --confirm."}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	payload, err := exportDatabase(ctx, dbPath)
	if err != nil {
		return err
	}
	entityCandidates := buildEntityMergeCandidates(payload)
	factCandidates := buildFactDuplicateCandidates(payload)
	report := map[string]any{
		"database_path":               dbPath,
		"mode":                        "dry-run",
		"entity_duplicate_candidates": len(entityCandidates),
		"fact_duplicate_candidates":   len(factCandidates),
		"entity_candidates":           entityCandidates,
		"fact_candidates":             factCandidates,
	}
	if !apply {
		if jsonOut {
			return writeJSON(c.stdout, report)
		}
		_, err = fmt.Fprintf(c.stdout, "dry-run entity_candidates=%d fact_candidates=%d\n", len(entityCandidates), len(factCandidates))
		return err
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	markedEntities := 0
	for _, candidate := range entityCandidates {
		target, err := eng.GetEntity(ctx, candidate.TargetID)
		if err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		targetMeta := mergeMaps(target.Metadata, map[string]any{
			"compaction_entity_duplicates": mergeStringSlices(anyStrings(target.Metadata["compaction_entity_duplicates"]), candidate.SourceIDs),
			"compaction_marked":            time.Now().UTC().Format(time.RFC3339),
		})
		if _, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
			ID:            target.ID,
			SpaceID:       target.SpaceID,
			Namespace:     target.Namespace,
			Type:          target.Type,
			CanonicalName: target.CanonicalName,
			Aliases:       target.Aliases,
			Metadata:      targetMeta,
		}); err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		for _, sourceID := range candidate.SourceIDs {
			source, err := eng.GetEntity(ctx, sourceID)
			if err != nil {
				_ = closeEngine(ctx, eng)
				return err
			}
			if _, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
				ID:            source.ID,
				SpaceID:       source.SpaceID,
				Namespace:     source.Namespace,
				Type:          source.Type,
				CanonicalName: source.CanonicalName,
				Aliases:       source.Aliases,
				Metadata: mergeMaps(source.Metadata, map[string]any{
					"duplicate_of":      target.ID,
					"compaction_marked": time.Now().UTC().Format(time.RFC3339),
				}),
			}); err != nil {
				_ = closeEngine(ctx, eng)
				return err
			}
			markedEntities++
		}
	}
	retractedFacts := 0
	for _, candidate := range factCandidates {
		for _, factID := range candidate.SourceIDs {
			if _, err := eng.RetractFact(ctx, factID, "duplicate_of:"+candidate.TargetID); err != nil {
				_ = closeEngine(ctx, eng)
				return err
			}
			retractedFacts++
		}
	}
	if err := closeEngine(ctx, eng); err != nil {
		return err
	}
	report["mode"] = "apply"
	report["entity_marked"] = markedEntities
	report["facts_retracted"] = retractedFacts
	if jsonOut {
		return writeJSON(c.stdout, report)
	}
	_, err = fmt.Fprintf(c.stdout, "applied compaction entity_marked=%d facts_retracted=%d\n", markedEntities, retractedFacts)
	return err
}

func (c cli) runAdminExport(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul admin export --db PATH --out FILE [--json]
`)
	fs := newFlagSet("admin export")
	var dbPath string
	var outPath string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&outPath, "out", "", "output file path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(outPath) == "" {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	payload, err := exportDatabase(ctx, dbPath)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, map[string]any{"database_path": dbPath, "out": outPath})
	}
	_, err = fmt.Fprintf(c.stdout, "exported %s\n", outPath)
	return err
}

func (c cli) runAdminImport(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul admin import --db PATH --in FILE [--json]
`)
	fs := newFlagSet("admin import")
	var dbPath string
	var inPath string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&inPath, "in", "", "input file path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(inPath) == "" {
		return &usageError{message: usage}
	}
	if !c.confirm {
		return &usageError{message: usage + "\n\nThis operation is destructive. Re-run with --confirm."}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	data, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}
	var payload ingestJSONFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	if _, err := eng.IngestBatch(ctx, yeoul.BatchInput{
		Episodes: payload.Episodes,
		Entities: payload.Entities,
		Facts:    payload.Facts,
	}); err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}
	if err := closeEngine(ctx, eng); err != nil {
		return err
	}

	if jsonOut {
		return writeJSON(c.stdout, map[string]any{"database_path": dbPath, "in": inPath})
	}
	_, err = fmt.Fprintf(c.stdout, "imported %s\n", inPath)
	return err
}

func (c cli) runBench(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul bench ingest --db PATH --episodes N [--facts-per-episode N] [--json]
  yeoul bench query --db PATH --query TEXT [--entity ID] [--fact ID] [--iterations N] [--json]
  yeoul bench lifecycle --db PATH --iterations N [--json]
`)
	if len(args) == 0 {
		return &usageError{message: usage}
	}
	switch args[0] {
	case "ingest":
		return c.runBenchIngest(ctx, args[1:])
	case "query":
		return c.runBenchQuery(ctx, args[1:])
	case "lifecycle":
		return c.runBenchLifecycle(ctx, args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(c.stdout, usage)
		return nil
	default:
		return &usageError{message: usage}
	}
}

func (c cli) runBenchIngest(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul bench ingest --db PATH --episodes N [--facts-per-episode N] [--json]
`)
	fs := newFlagSet("bench ingest")
	var dbPath string
	var episodes int
	var factsPerEpisode int
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.IntVar(&episodes, "episodes", 0, "number of synthetic episodes")
	fs.IntVar(&factsPerEpisode, "facts-per-episode", 1, "facts per episode")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || episodes <= 0 || factsPerEpisode <= 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	start := time.Now()
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < episodes; i++ {
		episodeID := fmt.Sprintf("bench-ep-%06d", i)
		entityID := fmt.Sprintf("bench-entity-%06d", i)
		result, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{
			ID:      episodeID,
			SpaceID: "default",
			Kind:    "bench",
			Content: fmt.Sprintf("synthetic episode %d", i),
			Source: yeoul.SourceInput{
				Kind:        "bench",
				ExternalRef: fmt.Sprintf("bench-source-%06d", i),
			},
		})
		if err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		if _, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
			ID:            entityID,
			SpaceID:       "default",
			Type:          "BenchEntity",
			CanonicalName: fmt.Sprintf("bench-%06d", i),
		}); err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		for j := 0; j < factsPerEpisode; j++ {
			if _, err := eng.AssertFact(ctx, yeoul.FactInput{
				ID:                   fmt.Sprintf("bench-fact-%06d-%02d", i, j),
				SpaceID:              "default",
				Predicate:            "HAS_BENCH_VALUE",
				SubjectID:            entityID,
				ValueText:            fmt.Sprintf("v-%d-%d-%d", i, j, rng.Intn(1000)),
				SupportingEpisodeIDs: []string{result.EpisodeID},
			}); err != nil {
				_ = closeEngine(ctx, eng)
				return err
			}
		}
	}
	if err := closeEngine(ctx, eng); err != nil {
		return err
	}
	elapsed := time.Since(start)
	totalFacts := episodes * factsPerEpisode
	result := benchIngestResult{
		DatabasePath:      dbPath,
		Episodes:          episodes,
		FactsPerEpisode:   factsPerEpisode,
		ElapsedSeconds:    elapsed.Seconds(),
		EpisodesPerSecond: float64(episodes) / elapsed.Seconds(),
		FactsPerSecond:    float64(totalFacts) / elapsed.Seconds(),
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(
		c.stdout,
		"episodes=%d facts=%d elapsed=%.3fs eps=%.2f fps=%.2f\n",
		episodes, totalFacts, result.ElapsedSeconds, result.EpisodesPerSecond, result.FactsPerSecond,
	)
	return err
}

func (c cli) runBenchQuery(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul bench query --db PATH --query TEXT [--entity ID] [--fact ID] [--iterations N] [--json]
`)
	fs := newFlagSet("bench query")
	var dbPath string
	var query string
	var entityID string
	var factID string
	var iterations int
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&query, "query", "", "query text")
	fs.StringVar(&entityID, "entity", "", "entity anchor ID")
	fs.StringVar(&factID, "fact", "", "fact ID for provenance")
	fs.IntVar(&iterations, "iterations", 10, "number of iterations")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || query == "" || iterations <= 0 {
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

	if entityID == "" || factID == "" {
		payload, err := exportDatabase(ctx, dbPath)
		if err != nil {
			return err
		}
		if entityID == "" && len(payload.Entities) > 0 {
			entityID = payload.Entities[0].ID
		}
		if factID == "" && len(payload.Facts) > 0 {
			factID = payload.Facts[0].ID
		}
	}

	metrics := map[string][]time.Duration{
		"search":       {},
		"neighborhood": {},
		"timeline":     {},
		"provenance":   {},
	}
	for i := 0; i < iterations; i++ {
		start := time.Now()
		if _, err := eng.Search(ctx, yeoul.SearchRequest{
			QueryText: query,
			AnchorIDs: compactStrings(entityID),
			Page:      yeoul.Page{Limit: 10},
		}); err != nil {
			return err
		}
		metrics["search"] = append(metrics["search"], time.Since(start))

		if entityID != "" {
			start = time.Now()
			if _, err := eng.Neighborhood(ctx, yeoul.NeighborhoodRequest{
				AnchorIDs: []string{entityID},
				MaxHops:   2,
				MaxNodes:  100,
			}); err != nil {
				return err
			}
			metrics["neighborhood"] = append(metrics["neighborhood"], time.Since(start))

			start = time.Now()
			if _, err := eng.Timeline(ctx, yeoul.TimelineRequest{
				AnchorIDs: []string{entityID},
				Page:      yeoul.Page{Limit: 25},
			}); err != nil {
				return err
			}
			metrics["timeline"] = append(metrics["timeline"], time.Since(start))
		}

		if factID != "" {
			start = time.Now()
			if _, err := eng.Provenance(ctx, yeoul.ProvenanceRequest{
				Kind:     "fact",
				ID:       factID,
				MaxDepth: 2,
			}); err != nil {
				return err
			}
			metrics["provenance"] = append(metrics["provenance"], time.Since(start))
		}
	}

	result := benchQueryResult{
		DatabasePath: dbPath,
		Query:        query,
		Iterations:   iterations,
		Metrics:      make(map[string]latency),
	}
	for key, samples := range metrics {
		if len(samples) == 0 {
			continue
		}
		result.Metrics[key] = summarizeLatencies(samples)
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	for _, key := range []string{"search", "neighborhood", "timeline", "provenance"} {
		metric, ok := result.Metrics[key]
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(c.stdout, "%s p50=%.3fms p95=%.3fms p99=%.3fms\n", key, metric.P50Millis, metric.P95Millis, metric.P99Millis); err != nil {
			return err
		}
	}
	return nil
}

func (c cli) runBenchLifecycle(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul bench lifecycle --db PATH --iterations N [--json]
`)
	fs := newFlagSet("bench lifecycle")
	var dbPath string
	var iterations int
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.IntVar(&iterations, "iterations", 10, "number of lifecycle iterations")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || iterations <= 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	episode, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{
		ID:      "bench-lifecycle-episode",
		SpaceID: "default",
		Kind:    "bench",
		Content: "lifecycle bench seed",
		Source:  yeoul.SourceInput{Kind: "bench", ExternalRef: "lifecycle"},
	})
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}
	entity, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
		ID:            "bench:lifecycle:entity",
		SpaceID:       "default",
		Type:          "BenchEntity",
		CanonicalName: "Lifecycle",
	})
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}

	start := time.Now()
	supersedeCount := 0
	retractCount := 0
	for i := 0; i < iterations; i++ {
		fact, err := eng.AssertFact(ctx, yeoul.FactInput{
			ID:                   fmt.Sprintf("bench-lifecycle-fact-%06d", i),
			SpaceID:              "default",
			Predicate:            "HAS_LIFECYCLE_VALUE",
			SubjectID:            entity.ID,
			ValueText:            fmt.Sprintf("v-%d", i),
			SupportingEpisodeIDs: []string{episode.EpisodeID},
		})
		if err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		superseded, err := eng.SupersedeFact(ctx, fact.ID, yeoul.FactInput{
			ID:                   fmt.Sprintf("bench-lifecycle-fact-%06d-next", i),
			SpaceID:              "default",
			Predicate:            "HAS_LIFECYCLE_VALUE",
			SubjectID:            entity.ID,
			ValueText:            fmt.Sprintf("v-%d-next", i),
			SupportingEpisodeIDs: []string{episode.EpisodeID},
		}, "bench")
		if err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		supersedeCount++
		if _, err := eng.RetractFact(ctx, superseded.NewFactID, "bench"); err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		retractCount++
	}
	if err := closeEngine(ctx, eng); err != nil {
		return err
	}
	elapsed := time.Since(start)
	result := benchLifecycleResult{
		DatabasePath:    dbPath,
		Iterations:      iterations,
		ElapsedSeconds:  elapsed.Seconds(),
		OpsPerSecond:    float64(supersedeCount+retractCount) / elapsed.Seconds(),
		SupersedeCount:  supersedeCount,
		RetractionCount: retractCount,
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "iterations=%d elapsed=%.3fs ops/sec=%.2f supersedes=%d retracts=%d\n", iterations, result.ElapsedSeconds, result.OpsPerSecond, supersedeCount, retractCount)
	return err
}

func exportDatabase(ctx context.Context, dbPath string) (*exportFile, error) {
	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeEngine(ctx, eng) }()

	store, err := openRawStore(dbPath, true)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	sourceRows, err := queryRows(store, "MATCH (s:Source) RETURN s.id, s.kind, s.uri, s.external_ref")
	if err != nil {
		return nil, err
	}
	sourceMap := make(map[string]yeoul.SourceInput, len(sourceRows))
	for _, row := range sourceRows {
		id := fmt.Sprint(row["s.id"])
		sourceMap[id] = yeoul.SourceInput{
			ID:          id,
			Kind:        fmt.Sprint(row["s.kind"]),
			URI:         fmt.Sprint(row["s.uri"]),
			ExternalRef: fmt.Sprint(row["s.external_ref"]),
		}
	}

	payload := &exportFile{}

	episodeRows, err := queryRows(store, "MATCH (e:Episode) RETURN e.id")
	if err != nil {
		return nil, err
	}
	for _, row := range episodeRows {
		id := fmt.Sprint(row["e.id"])
		record, err := eng.GetEpisode(ctx, id)
		if err != nil {
			return nil, err
		}
		input := yeoul.EpisodeInput{
			ID:         record.ID,
			SpaceID:    record.SpaceID,
			Kind:       record.Kind,
			Content:    record.Content,
			SourceID:   record.SourceID,
			GroupID:    record.GroupID,
			ObservedAt: record.ObservedAt,
			Metadata:   record.Metadata,
		}
		if source, ok := sourceMap[record.SourceID]; ok {
			input.Source = source
		}
		payload.Episodes = append(payload.Episodes, input)
	}

	entityRows, err := queryRows(store, "MATCH (e:Entity) RETURN e.id")
	if err != nil {
		return nil, err
	}
	for _, row := range entityRows {
		id := fmt.Sprint(row["e.id"])
		record, err := eng.GetEntity(ctx, id)
		if err != nil {
			return nil, err
		}
		payload.Entities = append(payload.Entities, yeoul.EntityInput{
			ID:            record.ID,
			SpaceID:       record.SpaceID,
			Namespace:     record.Namespace,
			Type:          record.Type,
			CanonicalName: record.CanonicalName,
			Aliases:       record.Aliases,
			Metadata:      record.Metadata,
		})
	}

	factRows, err := queryRows(store, "MATCH (f:Fact) RETURN f.id")
	if err != nil {
		return nil, err
	}
	for _, row := range factRows {
		id := fmt.Sprint(row["f.id"])
		record, err := eng.GetFact(ctx, id)
		if err != nil {
			return nil, err
		}
		payload.Facts = append(payload.Facts, yeoul.FactInput{
			ID:                   record.ID,
			SpaceID:              record.SpaceID,
			Predicate:            record.Predicate,
			SubjectID:            record.SubjectID,
			ObjectID:             record.ObjectID,
			ValueText:            record.ValueText,
			Confidence:           record.Confidence,
			Status:               record.Status,
			ValidFrom:            record.ValidFrom,
			ValidTo:              record.ValidTo,
			ObservedAt:           record.ObservedAt,
			SupportingEpisodeIDs: record.SupportingEpisodeIDs,
			Metadata:             record.Metadata,
		})
	}

	sort.Slice(payload.Episodes, func(i, j int) bool { return payload.Episodes[i].ID < payload.Episodes[j].ID })
	sort.Slice(payload.Entities, func(i, j int) bool { return payload.Entities[i].ID < payload.Entities[j].ID })
	sort.Slice(payload.Facts, func(i, j int) bool { return payload.Facts[i].ID < payload.Facts[j].ID })
	return payload, nil
}

func queryRows(store *lstore.Store, query string) ([]map[string]any, error) {
	result, err := store.Query(query)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	rows := make([]map[string]any, 0)
	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			return nil, err
		}
		row, err := tuple.GetAsMap()
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func singleStringQuery(store *lstore.Store, query string) (string, error) {
	rows, err := queryRows(store, query)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	for _, value := range rows[0] {
		return fmt.Sprint(value), nil
	}
	return "", nil
}

func singleIntQuery(store *lstore.Store, query string) (int, error) {
	rows, err := queryRows(store, query)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	for _, value := range rows[0] {
		switch v := value.(type) {
		case int:
			return v, nil
		case int64:
			return int(v), nil
		case float64:
			return int(v), nil
		default:
			return 0, fmt.Errorf("unexpected count type %T", value)
		}
	}
	return 0, nil
}

func parseTemporalFlags(asOfRaw, fromRaw, toRaw string, includeInactive bool) (yeoul.TemporalFilter, error) {
	var filter yeoul.TemporalFilter
	parseOne := func(raw string) (*time.Time, error) {
		if strings.TrimSpace(raw) == "" {
			return nil, nil
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, err
		}
		return &parsed, nil
	}
	asOf, err := parseOne(asOfRaw)
	if err != nil {
		return filter, err
	}
	from, err := parseOne(fromRaw)
	if err != nil {
		return filter, err
	}
	to, err := parseOne(toRaw)
	if err != nil {
		return filter, err
	}
	filter.AsOf = asOf
	filter.ObservedFrom = from
	filter.ObservedTo = to
	filter.IncludeInactive = includeInactive
	return filter, nil
}

func compactStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func buildEntityMergeCandidates(payload *exportFile) []entityMergeCandidate {
	groups := make(map[string][]yeoul.EntityInput)
	for _, entity := range payload.Entities {
		if duplicateOf(entity.Metadata) != "" {
			continue
		}
		key := strings.Join([]string{
			normalizeKey(entity.SpaceID),
			normalizeKey(entity.Namespace),
			normalizeKey(entity.Type),
			normalizeKey(entity.CanonicalName),
		}, "|")
		groups[key] = append(groups[key], entity)
	}
	candidates := make([]entityMergeCandidate, 0)
	for _, entities := range groups {
		if len(entities) < 2 {
			continue
		}
		sort.Slice(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })
		sourceIDs := make([]string, 0, len(entities)-1)
		for _, entity := range entities[1:] {
			sourceIDs = append(sourceIDs, entity.ID)
		}
		candidates = append(candidates, entityMergeCandidate{
			TargetID:      entities[0].ID,
			SourceIDs:     sourceIDs,
			Namespace:     entities[0].Namespace,
			Type:          entities[0].Type,
			CanonicalName: entities[0].CanonicalName,
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].TargetID < candidates[j].TargetID })
	return candidates
}

func buildFactDuplicateCandidates(payload *exportFile) []factDuplicateCandidate {
	groups := make(map[string][]yeoul.FactInput)
	for _, fact := range payload.Facts {
		if strings.EqualFold(fact.Status, "retracted") {
			continue
		}
		key := strings.Join([]string{
			normalizeKey(fact.SpaceID),
			normalizeKey(fact.Predicate),
			normalizeKey(fact.SubjectID),
			normalizeKey(fact.ObjectID),
			normalizeKey(fact.ValueText),
			strings.Join(sortedStrings(fact.SupportingEpisodeIDs), ","),
			fact.ValidFrom.UTC().Format(time.RFC3339Nano),
			fact.ValidTo.UTC().Format(time.RFC3339Nano),
		}, "|")
		groups[key] = append(groups[key], fact)
	}
	candidates := make([]factDuplicateCandidate, 0)
	for _, facts := range groups {
		if len(facts) < 2 {
			continue
		}
		sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
		sourceIDs := make([]string, 0, len(facts)-1)
		for _, fact := range facts[1:] {
			sourceIDs = append(sourceIDs, fact.ID)
		}
		candidates = append(candidates, factDuplicateCandidate{
			TargetID:  facts[0].ID,
			SourceIDs: sourceIDs,
			Predicate: facts[0].Predicate,
			SubjectID: facts[0].SubjectID,
			ObjectID:  facts[0].ObjectID,
			ValueText: facts[0].ValueText,
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].TargetID < candidates[j].TargetID })
	return candidates
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func duplicateOf(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata["duplicate_of"]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func mergeMaps(base, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func anyStrings(value any) []string {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...)
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return nil
	}
}

func mergeStringSlices(left, right []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(left)+len(right))
	for _, value := range append(append([]string(nil), left...), right...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func shouldDropEpisode(pack *policy.Pack, content string) bool {
	content = strings.ToLower(content)
	for _, rule := range pack.EpisodeRules.Drop {
		for _, token := range rule.When.ContainsAny {
			if strings.Contains(content, strings.ToLower(strings.TrimSpace(token))) {
				return true
			}
		}
	}
	return false
}

func applySearchRecipe(pack *policy.Pack, recipeName string, req yeoul.SearchRequest) (yeoul.SearchRequest, error) {
	recipe, ok := pack.SearchRecipes.Recipes[recipeName]
	if !ok {
		return req, fmt.Errorf("search recipe %q not found", recipeName)
	}
	if status, ok := recipe.Filters["fact_status"]; ok {
		req.Scope.FactStatus = mergeStringSlices(req.Scope.FactStatus, splitCSV(fmt.Sprint(status)))
	}
	if windowDays, ok := intFromAny(recipe.Filters["window_days"]); ok {
		from := time.Now().UTC().Add(-time.Duration(windowDays) * 24 * time.Hour)
		if req.Temporal.ObservedFrom == nil || req.Temporal.ObservedFrom.Before(from) {
			req.Temporal.ObservedFrom = &from
		}
	}
	switch recipe.Strategy {
	case "hybrid":
		req.Include.RelatedEntities = true
		req.Include.SupportingEpisodes = true
	case "neighborhood":
		req.Include.RelatedEntities = true
		req.Include.Provenance = true
		if types, ok := stringSliceFromAny(recipe.Expand["entity_types"]); ok {
			req.Scope.EntityTypes = mergeStringSlices(req.Scope.EntityTypes, types)
		}
	case "predicate_subject_lookup":
		req.Types = []string{"fact"}
	default:
		return req, fmt.Errorf("unsupported recipe strategy %q", recipe.Strategy)
	}
	return req, nil
}

func intFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func stringSliceFromAny(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...), true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out, true
	default:
		return nil, false
	}
}

func summarizeLatencies(samples []time.Duration) latency {
	if len(samples) == 0 {
		return latency{}
	}
	values := append([]time.Duration(nil), samples...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return latency{
		P50Millis: durationAtPercentile(values, 0.50).Seconds() * 1000,
		P95Millis: durationAtPercentile(values, 0.95).Seconds() * 1000,
		P99Millis: durationAtPercentile(values, 0.99).Seconds() * 1000,
	}
}

func durationAtPercentile(values []time.Duration, percentile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(percentile * float64(len(values)-1))
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}
