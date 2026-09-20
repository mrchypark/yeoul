package yeoul

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lstore "github.com/mrchypark/yeoul/internal/storage/ladybug"
)

// legacyFixtureIDs records the identifiers a strict-reader fixture needs to
// corrupt a specific record or edge.
type legacyFixtureIDs struct {
	episodeID string
	sourceID  string
	entityID  string
	factID    string
}

// writeStrictLegacyFixture builds a complete legacy Ladybug database with a
// source, episode, entity (with aliases and metadata) and supported fact, so a
// migration test can corrupt one field or table and prove the strict reader
// rejects it.
func writeStrictLegacyFixture(t *testing.T, dbPath string) legacyFixtureIDs {
	t.Helper()
	ctx := context.Background()
	eng, err := Open(ctx, Config{
		Driver:              StorageDriverLadybug,
		DatabasePath:        dbPath,
		CreateIfMissing:     true,
		legacyLadybugWrites: true,
	})
	if err != nil {
		t.Fatalf("open legacy fixture engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:       "ep-strict",
		Kind:     "note",
		Content:  "strict reader fixture",
		Source:   SourceInput{ID: "src-strict", Kind: "test", ExternalRef: "strict", Metadata: map[string]any{"channel": "fixture"}},
		Metadata: map[string]any{"origin": "fixture"},
	})
	if err != nil {
		t.Fatalf("ingest legacy fixture episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{
		ID:            "ent-strict",
		Type:          "Project",
		CanonicalName: "Yeoul",
		Aliases:       []string{"yeoul-project"},
		Metadata:      map[string]any{"tier": "core"},
	})
	if err != nil {
		t.Fatalf("upsert legacy fixture entity: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact-strict",
		Predicate:            "HAS_STORAGE",
		SubjectID:            entity.ID,
		ValueText:            "Ladybug",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert legacy fixture fact: %v", err)
	}
	if _, err := eng.SupersedeFact(ctx, fact.ID, FactInput{
		ID:                   "fact-strict-new",
		Predicate:            "HAS_STORAGE",
		SubjectID:            entity.ID,
		ValueText:            "Lattice",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}, "storage engine moved"); err != nil {
		t.Fatalf("supersede legacy fixture fact: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close legacy fixture engine: %v", err)
	}
	return legacyFixtureIDs{episodeID: episode.EpisodeID, sourceID: episode.SourceID, entityID: entity.ID, factID: fact.ID}
}

// execLegacyStatements opens the fixture with the raw adapter and applies
// statements, so a test can drop a table or overwrite a stored JSON column.
func execLegacyStatements(t *testing.T, dbPath string, statements ...string) {
	t.Helper()
	store, err := lstore.Open(dbPath, false)
	if err != nil {
		t.Fatalf("open raw legacy database: %v", err)
	}
	defer store.Close()
	if err := store.ExecuteStatements(statements); err != nil {
		t.Fatalf("apply raw legacy statements: %v", err)
	}
}

// addStrictLegacySource ingests a second episode with its own source into an
// existing strict fixture. It gives a test an existing-but-wrong Source to
// retarget a FROM_SOURCE edge onto and a second Episode to attach an extra
// ASSERTS edge to.
func addStrictLegacySource(t *testing.T, dbPath, episodeID, sourceID string) {
	t.Helper()
	ctx := context.Background()
	eng, err := Open(ctx, Config{
		Driver:              StorageDriverLadybug,
		DatabasePath:        dbPath,
		legacyLadybugWrites: true,
	})
	if err != nil {
		t.Fatalf("reopen legacy fixture engine: %v", err)
	}
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{
		ID:      episodeID,
		Kind:    "note",
		Content: "second source fixture",
		Source:  SourceInput{ID: sourceID, Kind: "test", ExternalRef: "strict-other"},
	}); err != nil {
		t.Fatalf("ingest second legacy fixture episode: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close second legacy fixture engine: %v", err)
	}
}

// writeStrictSharedSourceFixture builds a legacy database with the legitimate
// shapes the writer actually produces for these relationships: two episodes
// that share one source and one fact supported by both episodes.
func writeStrictSharedSourceFixture(t *testing.T, dbPath string) (string, []string, string) {
	t.Helper()
	ctx := context.Background()
	eng, err := Open(ctx, Config{
		Driver:              StorageDriverLadybug,
		DatabasePath:        dbPath,
		CreateIfMissing:     true,
		legacyLadybugWrites: true,
	})
	if err != nil {
		t.Fatalf("open shared-source fixture engine: %v", err)
	}
	source := SourceInput{ID: "src-shared", Kind: "test", ExternalRef: "shared"}
	episodeIDs := make([]string, 0, 2)
	for index, content := range []string{"shared source a", "shared source b"} {
		episode, err := eng.IngestEpisode(ctx, EpisodeInput{
			ID:      fmt.Sprintf("ep-shared-%d", index),
			Kind:    "note",
			Content: content,
			Source:  source,
		})
		if err != nil {
			t.Fatalf("ingest shared-source episode: %v", err)
		}
		episodeIDs = append(episodeIDs, episode.EpisodeID)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "ent-shared", Type: "Project", CanonicalName: "Yeoul"})
	if err != nil {
		t.Fatalf("upsert shared-source entity: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{
		ID:                   "fact-shared",
		Predicate:            "HAS_STORAGE",
		SubjectID:            entity.ID,
		ValueText:            "Ladybug",
		SupportingEpisodeIDs: episodeIDs,
	})
	if err != nil {
		t.Fatalf("assert shared-source fact: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close shared-source fixture engine: %v", err)
	}
	return "src-shared", episodeIDs, fact.ID
}

// legacyDatabaseFileSet hashes every data file that belongs to one database
// file (the main file plus its sidecars) so a test can prove a failed migration
// did not touch the source on disk. Ownership lock files are excluded: taking
// the migration lock creates them regardless of whether the read succeeds.
func legacyDatabaseFileSet(t *testing.T, dbPath string) map[string]string {
	t.Helper()
	parent := filepath.Dir(dbPath)
	base := filepath.Base(dbPath)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read database parent: %v", err)
	}
	sums := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if name != base && !strings.HasPrefix(name, base+".") {
			continue
		}
		if strings.HasSuffix(name, ".lock") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(parent, name))
		if err != nil {
			t.Fatalf("read database file %s: %v", name, err)
		}
		sums[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return sums
}

// assertNoMigrationArtifacts checks that a failed migration left the source
// untouched: no backup namespace entry, no staging directory, and no marker.
func assertNoMigrationArtifacts(t *testing.T, dbPath string) {
	t.Helper()
	if backups := migrationBackupNamespaceEntries(t, dbPath); len(backups) != 0 {
		t.Fatalf("failed migration must not create a backup, got %v", backups)
	}
	if staging, err := filepath.Glob(dbPath + ".lattice-migrate-*"); err != nil {
		t.Fatalf("glob staging directories: %v", err)
	} else if len(staging) != 0 {
		t.Fatalf("failed migration must not create a staging directory, got %v", staging)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); !os.IsNotExist(err) {
		t.Fatalf("failed migration must not leave a marker, got %v", err)
	}
}

// useInProcessMigration makes MigrateDatabase take the in-process strict reader
// path instead of delegating to the version-pinned helper binary.
func useInProcessMigration(t *testing.T) {
	t.Helper()
	previousReaderVersion := legacyMigrationReaderVersion
	legacyMigrationReaderVersion = "v0.13.1"
	t.Cleanup(func() { legacyMigrationReaderVersion = previousReaderVersion })
}

func TestMigrateDatabaseStrictReaderAcceptsValidFixture(t *testing.T) {
	useInProcessMigration(t)
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "valid.lbug")
	ids := writeStrictLegacyFixture(t, dbPath)

	result, err := MigrateDatabase(ctx, dbPath)
	if err != nil {
		t.Fatalf("migrate valid legacy database: %v", err)
	}
	if !result.Migrated || result.BackupPath == "" {
		t.Fatalf("unexpected migration result: %#v", result)
	}

	migrated, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer func() { _ = migrated.Close(ctx) }()
	episode, err := migrated.GetEpisode(ctx, ids.episodeID)
	if err != nil {
		t.Fatalf("read migrated episode: %v", err)
	}
	if episode.Content != "strict reader fixture" {
		t.Fatalf("unexpected migrated episode content %q", episode.Content)
	}
	entity, err := migrated.GetEntity(ctx, ids.entityID)
	if err != nil {
		t.Fatalf("read migrated entity: %v", err)
	}
	if len(entity.Aliases) != 1 || entity.Aliases[0] != "yeoul-project" {
		t.Fatalf("unexpected migrated entity aliases %v", entity.Aliases)
	}
}

func TestMigrateDatabaseStrictReaderAllowsAbsentOptionalTables(t *testing.T) {
	useInProcessMigration(t)
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "older.lbug")
	ids := writeStrictLegacyFixture(t, dbPath)

	// Older databases predate the bitemporal tables, so their absence is legal.
	execLegacyStatements(t, dbPath,
		"DROP TABLE YeoulMigration",
		"DROP TABLE EntityRevision",
		"DROP TABLE FactRevision",
	)

	result, err := MigrateDatabase(ctx, dbPath)
	if err != nil {
		t.Fatalf("migrate database without optional tables: %v", err)
	}
	if !result.Migrated {
		t.Fatalf("expected migration to proceed without optional tables, got %#v", result)
	}
	migrated, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer func() { _ = migrated.Close(ctx) }()
	if _, err := migrated.GetEpisode(ctx, ids.episodeID); err != nil {
		t.Fatalf("read migrated episode: %v", err)
	}
}

func TestMigrateDatabaseStrictReaderRejectsMissingMandatoryTable(t *testing.T) {
	useInProcessMigration(t)
	ctx := context.Background()
	cases := []struct {
		name       string
		statements []string
	}{
		{
			name: "queried table",
			// Entity is referenced by two relationship tables, so drop those first.
			statements: []string{"DROP TABLE SUBJECT", "DROP TABLE OBJECT_ENTITY", "DROP TABLE Entity"},
		},
		{
			// FROM_SOURCE is never read by a loader, so only a catalog check can
			// catch this truncated schema.
			name:       "unqueried table",
			statements: []string{"DROP TABLE FROM_SOURCE"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "missing.lbug")
			writeStrictLegacyFixture(t, dbPath)
			execLegacyStatements(t, dbPath, tc.statements...)
			before := legacyDatabaseFileSet(t, dbPath)

			_, err := MigrateDatabase(ctx, dbPath)
			if err == nil {
				t.Fatal("expected migration to reject a database missing a mandatory table")
			}
			yeoulErr := unwrapYeoulError(err)
			if yeoulErr == nil || yeoulErr.Code != ErrStorageFailed {
				t.Fatalf("expected %s error, got %v", ErrStorageFailed, err)
			}
			if !strings.Contains(yeoulErr.Message, "mandatory table") {
				t.Fatalf("expected a missing-mandatory-table error, got %v", err)
			}
			assertNoMigrationArtifacts(t, dbPath)
			if after := legacyDatabaseFileSet(t, dbPath); !mapsEqual(before, after) {
				t.Fatalf("failed migration changed the source file set\nbefore: %v\nafter:  %v", before, after)
			}
		})
	}
}

func TestMigrateDatabaseStrictReaderRejectsMalformedFields(t *testing.T) {
	useInProcessMigration(t)
	ctx := context.Background()
	cases := []struct {
		name      string
		statement func(legacyFixtureIDs) string
	}{
		{
			name: "entity metadata_json",
			statement: func(ids legacyFixtureIDs) string {
				return fmt.Sprintf("MATCH (n:Entity {id: %s}) SET n.metadata_json = 'not-json{'", lstore.StringLiteral(ids.entityID))
			},
		},
		{
			name: "entity aliases_json",
			statement: func(ids legacyFixtureIDs) string {
				return fmt.Sprintf("MATCH (n:Entity {id: %s}) SET n.aliases_json = '[1,2'", lstore.StringLiteral(ids.entityID))
			},
		},
		{
			name: "source metadata_json",
			statement: func(ids legacyFixtureIDs) string {
				return fmt.Sprintf("MATCH (n:Source {id: %s}) SET n.metadata_json = '{bad'", lstore.StringLiteral(ids.sourceID))
			},
		},
		{
			name: "episode metadata_json",
			statement: func(ids legacyFixtureIDs) string {
				return fmt.Sprintf("MATCH (n:Episode {id: %s}) SET n.metadata_json = 'not-json'", lstore.StringLiteral(ids.episodeID))
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "malformed.lbug")
			ids := writeStrictLegacyFixture(t, dbPath)
			execLegacyStatements(t, dbPath, tc.statement(ids))
			before := legacyDatabaseFileSet(t, dbPath)

			_, err := MigrateDatabase(ctx, dbPath)
			if err == nil {
				t.Fatal("expected migration to reject a malformed legacy field")
			}
			yeoulErr := unwrapYeoulError(err)
			if yeoulErr == nil || yeoulErr.Code != ErrStorageFailed {
				t.Fatalf("expected %s error, got %v", ErrStorageFailed, err)
			}
			if !strings.Contains(yeoulErr.Message, "malformed") {
				t.Fatalf("expected a malformed-field error, got %v", err)
			}
			assertNoMigrationArtifacts(t, dbPath)
			if after := legacyDatabaseFileSet(t, dbPath); !mapsEqual(before, after) {
				t.Fatalf("failed migration changed the source file set\nbefore: %v\nafter:  %v", before, after)
			}
		})
	}
}

func TestMigrateDatabaseStrictReaderRejectsForeignSchema(t *testing.T) {
	useInProcessMigration(t)
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "foreign.lbug")
	store, err := lstore.Open(dbPath, false)
	if err != nil {
		t.Fatalf("open foreign database: %v", err)
	}
	if err := store.ExecuteStatements([]string{"CREATE NODE TABLE IF NOT EXISTS Widget(id STRING, PRIMARY KEY(id))"}); err != nil {
		store.Close()
		t.Fatalf("create foreign table: %v", err)
	}
	store.Close()
	before := legacyDatabaseFileSet(t, dbPath)

	_, err = MigrateDatabase(ctx, dbPath)
	if err == nil {
		t.Fatal("expected migration to reject a foreign-schema database")
	}
	if yeoulErr := unwrapYeoulError(err); yeoulErr == nil || yeoulErr.Code != ErrStorageFailed {
		t.Fatalf("expected %s error, got %v", ErrStorageFailed, err)
	}
	assertNoMigrationArtifacts(t, dbPath)
	if after := legacyDatabaseFileSet(t, dbPath); !mapsEqual(before, after) {
		t.Fatalf("failed migration changed the foreign source file set\nbefore: %v\nafter:  %v", before, after)
	}
}

// TestMigrateDatabaseStrictReaderRejectsDisagreeingRelationships covers the two
// mandatory relationship tables the strict reader previously only checked for
// existence. FROM_SOURCE duplicates Episode.source_id and ASSERTS duplicates the
// fact's supporting-episode set, so a row that disagrees with the decoded state
// is corruption the migration must reject rather than silently discard. Each
// case corrupts exactly one relationship table and leaves the other
// representation valid.
func TestMigrateDatabaseStrictReaderRejectsDisagreeingRelationships(t *testing.T) {
	useInProcessMigration(t)
	ctx := context.Background()
	cases := []struct {
		name       string
		prepare    func(t *testing.T, dbPath string, ids legacyFixtureIDs)
		statements func(ids legacyFixtureIDs) []string
	}{
		{
			name: "from_source retargeted",
			prepare: func(t *testing.T, dbPath string, ids legacyFixtureIDs) {
				addStrictLegacySource(t, dbPath, "ep-other", "src-other")
			},
			// Episode.source_id still points at src-strict; only the edge moves.
			statements: func(ids legacyFixtureIDs) []string {
				return []string{
					fmt.Sprintf("MATCH (e:Episode {id: %s})-[r:FROM_SOURCE]->(:Source) DELETE r", lstore.StringLiteral(ids.episodeID)),
					fmt.Sprintf("MATCH (e:Episode {id: %s}), (s:Source {id: 'src-other'}) CREATE (e)-[:FROM_SOURCE {created_at: timestamp('2024-01-01T00:00:00Z')}]->(s)", lstore.StringLiteral(ids.episodeID)),
				}
			},
		},
		{
			name: "from_source removed",
			statements: func(ids legacyFixtureIDs) []string {
				return []string{fmt.Sprintf("MATCH (e:Episode {id: %s})-[r:FROM_SOURCE]->(:Source) DELETE r", lstore.StringLiteral(ids.episodeID))}
			},
		},
		{
			name: "from_source duplicated",
			prepare: func(t *testing.T, dbPath string, ids legacyFixtureIDs) {
				addStrictLegacySource(t, dbPath, "ep-other", "src-other")
			},
			// The original edge stays; a second edge to another existing source
			// makes the table disagree with source_id by multiplicity.
			statements: func(ids legacyFixtureIDs) []string {
				return []string{fmt.Sprintf("MATCH (e:Episode {id: %s}), (s:Source {id: 'src-other'}) CREATE (e)-[:FROM_SOURCE {created_at: timestamp('2024-01-01T00:00:00Z')}]->(s)", lstore.StringLiteral(ids.episodeID))}
			},
		},
		{
			name: "asserts removed",
			statements: func(ids legacyFixtureIDs) []string {
				return []string{fmt.Sprintf("MATCH (:Episode)-[r:ASSERTS]->(:Fact {id: %s}) DELETE r", lstore.StringLiteral(ids.factID))}
			},
		},
		{
			name: "asserts added for an unsupporting episode",
			prepare: func(t *testing.T, dbPath string, ids legacyFixtureIDs) {
				addStrictLegacySource(t, dbPath, "ep-other", "src-other")
			},
			statements: func(ids legacyFixtureIDs) []string {
				return []string{fmt.Sprintf("MATCH (e:Episode {id: 'ep-other'}), (f:Fact {id: %s}) CREATE (e)-[:ASSERTS {created_at: timestamp('2024-01-01T00:00:00Z')}]->(f)", lstore.StringLiteral(ids.factID))}
			},
		},
		{
			name: "asserts duplicated",
			statements: func(ids legacyFixtureIDs) []string {
				return []string{fmt.Sprintf("MATCH (e:Episode {id: %s}), (f:Fact {id: %s}) CREATE (e)-[:ASSERTS {created_at: timestamp('2024-01-01T00:00:00Z')}]->(f)", lstore.StringLiteral(ids.episodeID), lstore.StringLiteral(ids.factID))}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "disagreeing.lbug")
			ids := writeStrictLegacyFixture(t, dbPath)
			if tc.prepare != nil {
				tc.prepare(t, dbPath, ids)
			}
			execLegacyStatements(t, dbPath, tc.statements(ids)...)
			before := legacyDatabaseFileSet(t, dbPath)

			_, err := MigrateDatabase(ctx, dbPath)
			if err == nil {
				t.Fatal("expected migration to reject a relationship table that disagrees with the decoded state")
			}
			yeoulErr := unwrapYeoulError(err)
			if yeoulErr == nil || yeoulErr.Code != ErrStorageFailed {
				t.Fatalf("expected %s error, got %v", ErrStorageFailed, err)
			}
			assertNoMigrationArtifacts(t, dbPath)
			if after := legacyDatabaseFileSet(t, dbPath); !mapsEqual(before, after) {
				t.Fatalf("failed migration changed the source file set\nbefore: %v\nafter:  %v", before, after)
			}
		})
	}
}

// TestStrictReaderRejectsMalformedRelationshipColumns checks the row shape of
// TestMigrateDatabaseStrictReaderRejectsDuplicateSupportedByEdge covers the
// strict reader's SUPPORTED_BY multiplicity rule: the writer emits exactly one
// edge per (fact, episode) pair, so a repeated row is corruption even when the
// single ASSERTS edge still agrees with the decoded supporting-episode set. The
// reader must not collapse the duplicate before the agreement check runs and
// then migrate as if the source were intact.
func TestMigrateDatabaseStrictReaderRejectsDuplicateSupportedByEdge(t *testing.T) {
	useInProcessMigration(t)
	ctx := context.Background()

	t.Run("uncorrupted fixture migrates", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "supported-by-valid.lbug")
		writeStrictLegacyFixture(t, dbPath)

		result, err := MigrateDatabase(ctx, dbPath)
		if err != nil {
			t.Fatalf("migrate an uncorrupted fixture: %v", err)
		}
		if !result.Migrated || result.BackupPath == "" {
			t.Fatalf("unexpected migration result: %#v", result)
		}
	})

	t.Run("duplicate supported_by edge is rejected", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "supported-by-duplicate.lbug")
		ids := writeStrictLegacyFixture(t, dbPath)
		// The original SUPPORTED_BY and ASSERTS edges stay intact; only a second
		// SUPPORTED_BY row for the same pair is added.
		execLegacyStatements(t, dbPath, fmt.Sprintf("MATCH (f:Fact {id: %s}), (e:Episode {id: %s}) CREATE (f)-[:SUPPORTED_BY {support_kind: 'observed', created_at: timestamp('2024-01-01T00:00:00Z')}]->(e)", lstore.StringLiteral(ids.factID), lstore.StringLiteral(ids.episodeID)))
		before := legacyDatabaseFileSet(t, dbPath)

		_, err := MigrateDatabase(ctx, dbPath)
		if err == nil {
			t.Fatal("expected migration to reject a duplicated SUPPORTED_BY edge")
		}
		yeoulErr := unwrapYeoulError(err)
		if yeoulErr == nil || yeoulErr.Code != ErrStorageFailed {
			t.Fatalf("expected %s error, got %v", ErrStorageFailed, err)
		}
		if yeoulErr.Details["table"] != "SUPPORTED_BY" {
			t.Fatalf("expected the error to name the SUPPORTED_BY table, got %#v", yeoulErr.Details)
		}
		assertNoMigrationArtifacts(t, dbPath)
		if after := legacyDatabaseFileSet(t, dbPath); !mapsEqual(before, after) {
			t.Fatalf("failed migration changed the source file set\nbefore: %v\nafter:  %v", before, after)
		}
	})
}

// TestStrictReaderRejectsMalformedRelationshipColumns checks the row shape of
// the two mandatory relationship tables. Their ids are projected from typed node
// columns, so a non-string or missing id column cannot be resolved to a record
// and must fail the same way the other strict-reader tables do. The check runs
// through the same helper the loaders use, in strict mode only.
func TestStrictReaderRejectsMalformedRelationshipColumns(t *testing.T) {
	strict := &ladybugStore{cfg: Config{DatabasePath: "strict.lbug", legacyStrictRead: true}}
	lenient := &ladybugStore{cfg: Config{DatabasePath: "lenient.lbug"}}
	cases := []struct {
		name   string
		table  string
		values []any
	}{
		{name: "from_source typed episode id", table: "FROM_SOURCE", values: []any{int64(1), "src-strict"}},
		{name: "from_source typed source id", table: "FROM_SOURCE", values: []any{"ep-strict", int64(2)}},
		{name: "from_source missing column", table: "FROM_SOURCE", values: []any{"ep-strict"}},
		{name: "asserts typed episode id", table: "ASSERTS", values: []any{int64(1), "fact-strict"}},
		{name: "asserts typed fact id", table: "ASSERTS", values: []any{"ep-strict", int64(2)}},
		{name: "asserts missing column", table: "ASSERTS", values: []any{"ep-strict"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := strict.checkEdgeRow(tc.table, tc.values, 2, 2)
			if err == nil {
				t.Fatal("expected the strict reader to reject a malformed relationship row")
			}
			yeoulErr := unwrapYeoulError(err)
			if yeoulErr == nil || yeoulErr.Code != ErrStorageFailed {
				t.Fatalf("expected %s error, got %v", ErrStorageFailed, err)
			}
			if yeoulErr.Details["table"] != tc.table {
				t.Fatalf("expected the error to name table %s, got %#v", tc.table, yeoulErr.Details)
			}
			if err := lenient.checkEdgeRow(tc.table, tc.values, 2, 2); err != nil {
				t.Fatalf("lenient mode must keep tolerating the row, got %v", err)
			}
		})
	}
}

// TestMigrateDatabaseStrictReaderAcceptsSharedSourceAndMultiSupport proves the
// agreement checks do not invent stricter rules than the writer produces: a
// source shared by two episodes and a fact supported by both episodes must still
// migrate.
func TestMigrateDatabaseStrictReaderAcceptsSharedSourceAndMultiSupport(t *testing.T) {
	useInProcessMigration(t)
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "shared.lbug")
	sourceID, episodeIDs, factID := writeStrictSharedSourceFixture(t, dbPath)

	result, err := MigrateDatabase(ctx, dbPath)
	if err != nil {
		t.Fatalf("migrate a valid shared-source database: %v", err)
	}
	if !result.Migrated || result.BackupPath == "" {
		t.Fatalf("unexpected migration result: %#v", result)
	}

	migrated, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer func() { _ = migrated.Close(ctx) }()
	for _, episodeID := range episodeIDs {
		episode, err := migrated.GetEpisode(ctx, episodeID)
		if err != nil {
			t.Fatalf("read migrated episode %s: %v", episodeID, err)
		}
		if episode.SourceID != sourceID {
			t.Fatalf("episode %s lost its shared source: %q", episodeID, episode.SourceID)
		}
	}
	fact, err := migrated.GetFact(ctx, factID)
	if err != nil {
		t.Fatalf("read migrated fact: %v", err)
	}
	if !sameStringSet(fact.SupportingEpisodeIDs, episodeIDs) {
		t.Fatalf("migrated fact supporting episodes %v, want %v", fact.SupportingEpisodeIDs, episodeIDs)
	}
}

// TestLenientLegacyReadIgnoresDisagreeingRelationships keeps the tolerance
// boundary explicit: the ordinary read path still reconstructs state from
// Episode.source_id and SUPPORTED_BY, so a disagreeing FROM_SOURCE edge is not a
// read failure outside migration.
func TestLenientLegacyReadIgnoresDisagreeingRelationships(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "lenient-relationship.lbug")
	ids := writeStrictLegacyFixture(t, dbPath)
	addStrictLegacySource(t, dbPath, "ep-other", "src-other")
	execLegacyStatements(t, dbPath,
		fmt.Sprintf("MATCH (e:Episode {id: %s})-[r:FROM_SOURCE]->(:Source) DELETE r", lstore.StringLiteral(ids.episodeID)),
		fmt.Sprintf("MATCH (e:Episode {id: %s}), (s:Source {id: 'src-other'}) CREATE (e)-[:FROM_SOURCE {created_at: timestamp('2024-01-01T00:00:00Z')}]->(s)", lstore.StringLiteral(ids.episodeID)))

	eng, err := Open(ctx, Config{Driver: StorageDriverLadybug, DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("lenient read must tolerate a disagreeing relationship row: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()
	episode, err := eng.GetEpisode(ctx, ids.episodeID)
	if err != nil {
		t.Fatalf("read episode through the lenient path: %v", err)
	}
	if episode.SourceID != ids.sourceID {
		t.Fatalf("lenient read should reconstruct the source from source_id, got %q", episode.SourceID)
	}
}

// TestLenientLegacyReadToleratesMalformedData guards requirement #1: the
// ordinary read path must keep loading data users already read successfully,
// so strictness stays confined to the migration reader.
func TestLenientLegacyReadToleratesMalformedData(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "lenient.lbug")
	ids := writeStrictLegacyFixture(t, dbPath)
	execLegacyStatements(t, dbPath, fmt.Sprintf("MATCH (n:Entity {id: %s}) SET n.metadata_json = 'not-json{'", lstore.StringLiteral(ids.entityID)))

	eng, err := Open(ctx, Config{Driver: StorageDriverLadybug, DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("lenient read must tolerate malformed data: %v", err)
	}
	defer func() { _ = eng.Close(ctx) }()
	snapshot, err := Snapshot(ctx, eng)
	if err != nil {
		t.Fatalf("snapshot lenient database: %v", err)
	}
	entity, ok := snapshot.Entities[ids.entityID]
	if !ok {
		t.Fatalf("expected the malformed entity to still load, got %#v", snapshot.Entities)
	}
	if entity.Metadata != nil {
		t.Fatalf("lenient read should drop the malformed metadata, got %#v", entity.Metadata)
	}
}

func mapsEqual(left, right map[string]string) bool {
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
