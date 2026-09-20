package yeoul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	json "github.com/goccy/go-json"
	latticedb "github.com/mrchypark/latticedb-go"
)

func TestLatticeIsDefaultAndPersistsGraph(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "yeoul.db")
	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open default engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{
		Kind:    "note",
		Content: "LatticeDB default persistence",
		Source:  SourceInput{Kind: "test", ExternalRef: "lattice-default"},
	})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Project", CanonicalName: "Yeoul"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{
		Predicate:            "USES_STORAGE_ENGINE",
		SubjectID:            entity.ID,
		ValueText:            "LatticeDB",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	reopened, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen default engine: %v", err)
	}
	got, err := reopened.GetFact(ctx, fact.ID)
	if err != nil {
		t.Fatalf("get persisted fact: %v", err)
	}
	if got.SubjectID != entity.ID || got.ValueText != "LatticeDB" {
		t.Fatalf("unexpected persisted fact: %#v", got)
	}
	if err := reopened.Close(ctx); err != nil {
		t.Fatalf("close reopened engine: %v", err)
	}

	db, err := latticedb.Open(dbPath, latticedb.OpenOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("open raw lattice database: %v", err)
	}
	defer db.Close()
	result, err := db.Query(
		"MATCH (f:Fact)-[:SUBJECT]->(e:Entity) RETURN f.id AS fact_id, e.id AS entity_id",
		nil,
	)
	if err != nil {
		t.Fatalf("query lattice graph edge: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["fact_id"] != fact.ID || result.Rows[0]["entity_id"] != entity.ID {
		t.Fatalf("unexpected graph query result: %#v", result.Rows)
	}
}

func TestDefaultOpenMigratesLadybugWithFullStateAndBackup(t *testing.T) {
	previousReaderVersion := legacyMigrationReaderVersion
	legacyMigrationReaderVersion = "v0.13.1"
	t.Cleanup(func() { legacyMigrationReaderVersion = previousReaderVersion })
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.lbug")
	legacy, err := Open(ctx, Config{
		Driver:              StorageDriverLadybug,
		DatabasePath:        dbPath,
		legacyLadybugWrites: true,
		CreateIfMissing:     true,
	})
	if err != nil {
		t.Fatalf("open legacy engine: %v", err)
	}
	episode, err := legacy.IngestEpisode(ctx, EpisodeInput{
		Kind:    "decision",
		Content: "Migrate all lifecycle state",
		Source:  SourceInput{Kind: "test", ExternalRef: "migration"},
	})
	if err != nil {
		t.Fatalf("ingest legacy episode: %v", err)
	}
	entity, err := legacy.UpsertEntity(ctx, EntityInput{Type: "Project", CanonicalName: "Yeoul"})
	if err != nil {
		t.Fatalf("upsert legacy entity: %v", err)
	}
	fact, err := legacy.AssertFact(ctx, FactInput{
		Predicate:            "HAS_STORAGE",
		SubjectID:            entity.ID,
		ValueText:            "Ladybug",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert legacy fact: %v", err)
	}
	if _, err := legacy.SupersedeFact(ctx, fact.ID, FactInput{
		Predicate:            fact.Predicate,
		SubjectID:            entity.ID,
		ValueText:            "LatticeDB",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}, "storage replacement"); err != nil {
		t.Fatalf("supersede legacy fact: %v", err)
	}
	if err := legacy.Close(ctx); err != nil {
		t.Fatalf("close legacy engine: %v", err)
	}
	legacyBaseline, err := Open(ctx, Config{Driver: StorageDriverLadybug, DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen legacy engine for baseline: %v", err)
	}
	before, err := Snapshot(ctx, legacyBaseline)
	if err != nil {
		t.Fatalf("snapshot legacy disk state: %v", err)
	}
	if len(before.FactRevisions) == 0 || len(before.EntityRevisions) == 0 {
		t.Fatalf("expected revision history before migration: %#v", before)
	}
	if err := legacyBaseline.Close(ctx); err != nil {
		t.Fatalf("close legacy baseline engine: %v", err)
	}

	// The conversion is an explicit opt-in for a read-only open, so this test
	// asks for it instead of relying on the open to mutate the source.
	migrated, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true, AllowMigration: true})
	if err != nil {
		t.Fatalf("auto-migrate default open: %v", err)
	}
	after, err := Snapshot(ctx, migrated)
	if err != nil {
		t.Fatalf("snapshot migrated state: %v", err)
	}
	if err := migrated.Close(ctx); err != nil {
		t.Fatalf("close migrated engine: %v", err)
	}
	beforeData, err := json.Marshal(before)
	if err != nil {
		t.Fatalf("encode before snapshot: %v", err)
	}
	afterData, err := json.Marshal(after)
	if err != nil {
		t.Fatalf("encode after snapshot: %v", err)
	}
	if string(beforeData) != string(afterData) {
		t.Fatalf("migration changed persisted state\nbefore: %#v\nafter:  %#v", before, after)
	}
	backups, err := filepath.Glob(dbPath + ".ladybug-backup-*")
	if err != nil {
		t.Fatalf("find migration backup: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one legacy backup, got %v", backups)
	}
	if markers, _ := filepath.Glob(dbPath + ".yeoul-migration.json"); len(markers) != 0 {
		t.Fatalf("migration marker was not cleaned up: %v", markers)
	}
}

// TestReadOnlyOpenDoesNotMigrateLegacyDatabaseWithoutOptIn proves the default
// read-only open is a no-mutation open: a legacy database it cannot read is
// reported as requiring migration, and every source path and format stays
// byte-for-byte unchanged. The same open with the explicit opt-in still
// converts, so the gate is a permission boundary rather than a loss of the
// migration path.
func TestReadOnlyOpenDoesNotMigrateLegacyDatabaseWithoutOptIn(t *testing.T) {
	useInProcessMigration(t)
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy-no-mutation.lbug")
	legacy, err := Open(ctx, Config{
		Driver:              StorageDriverLadybug,
		DatabasePath:        dbPath,
		legacyLadybugWrites: true,
		CreateIfMissing:     true,
	})
	if err != nil {
		t.Fatalf("open legacy engine: %v", err)
	}
	episode, err := legacy.IngestEpisode(ctx, EpisodeInput{
		ID:      "ep-no-mutation",
		Kind:    "note",
		Content: "legacy source that must not be converted",
		Source:  SourceInput{Kind: "test", ExternalRef: "no-mutation"},
	})
	if err != nil {
		t.Fatalf("ingest legacy episode: %v", err)
	}
	if err := legacy.Close(ctx); err != nil {
		t.Fatalf("close legacy engine: %v", err)
	}

	before := legacyDatabaseFileSet(t, dbPath)
	if len(before) == 0 {
		t.Fatal("expected the legacy fixture to write at least one data file")
	}

	eng, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err == nil {
		_ = eng.Close(ctx)
		t.Fatal("expected a read-only open of a legacy database to report a migration requirement")
	}
	if !errors.Is(err, errMigrationRequired) {
		t.Fatalf("expected the migration-required sentinel, got %v", err)
	}
	var yeoulErr *Error
	if !errors.As(err, &yeoulErr) {
		t.Fatalf("expected a typed yeoul error, got %T", err)
	}
	if yeoulErr.Code != ErrNotSupported {
		t.Fatalf("unexpected error code: %v", yeoulErr.Code)
	}
	if detail, _ := yeoulErr.Details["database_path"].(string); filepath.Base(detail) != filepath.Base(dbPath) {
		t.Fatalf("unexpected database_path detail: %v", yeoulErr.Details["database_path"])
	}

	// The refused open must leave every source path and format unchanged: the
	// same files with the same bytes, no backup, no staging directory, no
	// marker.
	if after := legacyDatabaseFileSet(t, dbPath); !equalStringMaps(before, after) {
		t.Fatalf("refused read-only open changed the database file set\nbefore: %v\nafter:  %v", before, after)
	}
	assertNoMigrationArtifacts(t, dbPath)

	// The explicit opt-in keeps the migration path available to a caller that
	// accepted the conversion.
	migrated, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true, AllowMigration: true})
	if err != nil {
		t.Fatalf("read-only open with allow_migration: %v", err)
	}
	defer func() { _ = migrated.Close(ctx) }()
	got, err := migrated.GetEpisode(ctx, episode.EpisodeID)
	if err != nil {
		t.Fatalf("read migrated episode: %v", err)
	}
	if got.Content != "legacy source that must not be converted" {
		t.Fatalf("unexpected migrated episode content %q", got.Content)
	}
}

func equalStringMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func TestMigrateDatabaseFailsClosedWithoutLegacyHelper(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.lbug")
	legacy, err := Open(ctx, Config{
		Driver:              StorageDriverLadybug,
		DatabasePath:        dbPath,
		legacyLadybugWrites: true,
		CreateIfMissing:     true,
	})
	if err != nil {
		t.Fatalf("open legacy engine: %v", err)
	}
	if err := legacy.Close(ctx); err != nil {
		t.Fatalf("close legacy engine: %v", err)
	}
	before, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat legacy database: %v", err)
	}
	t.Setenv(legacyMigrationHelperEnv, filepath.Join(t.TempDir(), "missing-helper"))

	if _, err := MigrateDatabase(ctx, dbPath); err == nil {
		t.Fatal("expected migration to fail when the version-pinned helper is missing")
	}
	after, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("legacy database was not preserved: %v", err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("legacy database size changed: before=%d after=%d", before.Size(), after.Size())
	}
	if backups, _ := filepath.Glob(dbPath + ".ladybug-backup-*"); len(backups) != 0 {
		t.Fatalf("unexpected backup after failed helper lookup: %v", backups)
	}
}

func TestLegacyMigrationHelperEnvironmentRemovesInjectedRuntimes(t *testing.T) {
	got := legacyMigrationHelperEnvironment([]string{
		"PATH=/usr/bin",
		"LD_LIBRARY_PATH=/main/lib",
		"LD_PRELOAD=/tmp/injected.so",
		"DYLD_LIBRARY_PATH=/main/lib",
		"DYLD_FALLBACK_LIBRARY_PATH=/fallback/lib",
		"DYLD_INSERT_LIBRARIES=/tmp/injected.dylib",
		"YEOUL_DB=/memory.ltdb",
	})
	// The helper owns the database itself, so the environment carries no
	// ownership marker: the parent releases the lock before spawning it.
	want := []string{"PATH=/usr/bin", "YEOUL_DB=/memory.ltdb"}
	if len(got) != len(want) {
		t.Fatalf("unexpected helper environment: %v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected helper environment: got=%v want=%v", got, want)
		}
	}
}

func TestMigrateDatabaseIsIdempotentForLattice(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "current.db")
	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open lattice engine: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close lattice engine: %v", err)
	}
	result, err := MigrateDatabase(ctx, dbPath)
	if err != nil {
		t.Fatalf("migrate current lattice database: %v", err)
	}
	if result.Migrated || result.SourceDriver != string(StorageDriverLattice) || result.BackupPath != "" {
		t.Fatalf("unexpected idempotent migration result: %#v", result)
	}
}

func TestRecoverDatabaseMigrationResumesAfterBackupRename(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "memory.db")
	backupPath := dbPath + ".ladybug-backup-test"
	stagingPath := dbPath + ".lattice-migrate-test"
	if err := os.Mkdir(backupPath, 0o755); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	// The staged database is a real LatticeDB database: recovery installs it
	// only after proving that this build can read it.
	writeLatticeStateFixture(t, stagingPath, strconv.Itoa(currentStateVersion), false, nil)
	marker := databaseMigrationMarker{
		Phase:        migrationPhasePrepared,
		DatabasePath: dbPath,
		BackupPath:   backupPath,
		StagingPath:  stagingPath,
	}
	if err := writeDatabaseMigrationMarker(marker); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if err := recoverDatabaseMigration(dbPath); err != nil {
		t.Fatalf("recover migration: %v", err)
	}
	if info, err := os.Stat(dbPath); err != nil || !info.IsDir() {
		t.Fatalf("staging database was not installed: info=%v err=%v", info, err)
	}
	if info, err := os.Stat(backupPath); err != nil || !info.IsDir() {
		t.Fatalf("backup was not retained: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(dbPath)); !os.IsNotExist(err) {
		t.Fatalf("migration marker remains after recovery: %v", err)
	}
}

func TestLatticeLoadRejectsCorruptRecordIdentity(t *testing.T) {
	tests := []struct {
		name  string
		nodes []map[string]any
	}{
		{
			name: "payload id mismatch",
			nodes: []map[string]any{{
				"id":      "ep-indexed",
				"payload": `{"id":"ep-payload","kind":"note","content":"corrupt"}`,
			}},
		},
		{
			name: "duplicate indexed id",
			nodes: []map[string]any{
				{"id": "ep-duplicate", "payload": `{"id":"ep-duplicate","kind":"note","content":"one"}`},
				{"id": "ep-duplicate", "payload": `{"id":"ep-duplicate","kind":"note","content":"two"}`},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "corrupt.db")
			db, err := latticedb.Open(dbPath, latticedb.OpenOptions{Create: true})
			if err != nil {
				t.Fatalf("create lattice database: %v", err)
			}
			if err := db.Update(func(tx *latticedb.Tx) error {
				if err := tx.PutAppMetadata([]byte(latticeMetaVersion), []byte(strconv.Itoa(currentStateVersion))); err != nil {
					return err
				}
				for _, properties := range test.nodes {
					if _, err := tx.CreateNode(latticedb.CreateNodeOptions{Labels: []string{"Episode"}, Properties: properties}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatalf("write corrupt lattice database: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close corrupt lattice database: %v", err)
			}
			if _, err := Open(context.Background(), Config{Driver: StorageDriverLattice, DatabasePath: dbPath, ReadOnly: true}); err == nil {
				t.Fatal("expected corrupt lattice record identity to be rejected")
			}
		})
	}
}

// TestLatticeOpenRejectsCrossKindRecordIDCollision covers the databases an
// affected release could already contain: the same raw id stored under two
// different kinds. The lattice loader keys duplicates by label plus id, so this
// shape loads without complaint, and Neighborhood indexes nodes by raw id, so
// the collision would silently mistype nodes. Open must fail closed and name
// both the id and the kinds that claim it. The database is written directly to
// bypass the write-time uniqueness checks that guard new data.
func TestLatticeOpenRejectsCrossKindRecordIDCollision(t *testing.T) {
	const sharedID = "shared-cross-kind-id"
	dbPath := filepath.Join(t.TempDir(), "collision.db")
	db, err := latticedb.Open(dbPath, latticedb.OpenOptions{Create: true})
	if err != nil {
		t.Fatalf("create lattice database: %v", err)
	}
	if err := db.Update(func(tx *latticedb.Tx) error {
		if err := tx.PutAppMetadata([]byte(latticeMetaVersion), []byte(strconv.Itoa(currentStateVersion))); err != nil {
			return err
		}
		if _, err := tx.CreateNode(latticedb.CreateNodeOptions{Labels: []string{"Entity"}, Properties: map[string]any{
			"id": sharedID, "payload": `{"id":"shared-cross-kind-id","type":"Thing","canonical_name":"Shared"}`,
		}}); err != nil {
			return err
		}
		if _, err := tx.CreateNode(latticedb.CreateNodeOptions{Labels: []string{"Episode"}, Properties: map[string]any{
			"id": sharedID, "payload": `{"id":"shared-cross-kind-id","kind":"note","content":"shared"}`,
		}}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("write colliding lattice database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close colliding lattice database: %v", err)
	}

	_, err = Open(context.Background(), Config{Driver: StorageDriverLattice, DatabasePath: dbPath, ReadOnly: true})
	if err == nil {
		t.Fatal("expected a cross-kind record id collision to be rejected at open")
	}
	structured := unwrapYeoulError(err)
	if structured == nil {
		t.Fatalf("expected a structured error, got %v", err)
	}
	if structured.Code != ErrStorageFailed {
		t.Fatalf("expected %s, got %s (%v)", ErrStorageFailed, structured.Code, err)
	}
	if got := structured.Details["id"]; got != sharedID {
		t.Fatalf("expected the error to name colliding id %q, got %#v", sharedID, got)
	}
	kinds, ok := structured.Details["kinds"].([]string)
	if !ok {
		t.Fatalf("expected kinds in error details, got %#v", structured.Details)
	}
	for _, want := range []string{kindEntity, kindEpisode} {
		if !slices.Contains(kinds, want) {
			t.Fatalf("expected kind %q in error details, got %#v", want, kinds)
		}
	}
}
