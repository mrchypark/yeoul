//go:build linux || darwin

package yeoul

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	rhizastore "github.com/mrchypark/yeoul/internal/storage/rhiza"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRhizaEngineWriteReopenLifecycleAndFullText(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory")
	eng, err := Open(ctx, Config{DatabasePath: path, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.(*engine).store.(*ownershipStore).stateStore.(*rhizaStore); !ok {
		t.Fatalf("default store type = %T, want Rhiza", eng.(*engine).store)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{ID: "ep-rhiza", Kind: "note", Content: "comet orbit observation", GroupID: "scope-a"})
	if err != nil {
		t.Fatal(err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{ID: "project:rhiza", Type: "Project", CanonicalName: "Comet Archive"})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{ID: "fact-rhiza", Predicate: "HAS_NOTE", SubjectID: entity.ID, ValueText: "comet orbit record", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.SupersedeFact(ctx, fact.ID, FactInput{ID: "fact-rhiza-new", Predicate: fact.Predicate, SubjectID: entity.ID, ValueText: "comet orbit revised", SupportingEpisodeIDs: []string{episode.EpisodeID}}, "revision"); err != nil {
		t.Fatal(err)
	}
	want, err := Snapshot(ctx, eng)
	if err != nil {
		t.Fatal(err)
	}
	fullText, err := eng.Search(ctx, SearchRequest{QueryText: "comet orbit", Mode: SearchModeFullText, Types: []string{"episode", "fact", "entity"}})
	if err != nil {
		t.Fatalf("Rhiza full-text search: %v", err)
	}
	if len(fullText.Hits) < 3 {
		t.Fatalf("full-text hits = %v", hitIDs(fullText.Hits))
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, Config{DatabasePath: path, ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen Rhiza: %v", err)
	}
	got, err := Snapshot(ctx, reopened)
	if err != nil {
		t.Fatal(err)
	}
	if !equalPersistedJSON(want, got) {
		t.Fatal("Rhiza state changed across reopen")
	}
	search, err := reopened.Search(ctx, SearchRequest{QueryText: "comet orbit", Mode: SearchModeFullText, Types: []string{"episode", "fact", "entity"}})
	if err != nil {
		t.Fatalf("reopened Rhiza full-text search: %v", err)
	}
	if !reflect.DeepEqual(hitIDs(fullText.Hits), hitIDs(search.Hits)) {
		t.Fatalf("full-text hits changed on reopen: before=%v after=%v", hitIDs(fullText.Hits), hitIDs(search.Hits))
	}
	if err := reopened.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRhizaOversizedDeltaRollsBackAtomically(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory")
	eng, err := Open(ctx, Config{DatabasePath: path, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.IngestEpisode(ctx, EpisodeInput{ID: "too-large", Kind: "note", Content: strings.Repeat("x", 60_000)})
	if err == nil || !hasErrorCode(err, ErrInputInvalid) {
		t.Fatalf("oversized mutation error = %v", err)
	}
	if got := len(eng.(*engine).episodes); got != 0 {
		t.Fatalf("episodes after rejected mutation = %d", got)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, Config{DatabasePath: path, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(ctx)
	state, err := Snapshot(ctx, reopened)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Episodes) != 0 || len(state.Sources) != 0 {
		t.Fatalf("state after rejected mutation = episodes %d sources %d", len(state.Episodes), len(state.Sources))
	}
}

func TestRhizaMigrationPreservesStateAndRefusesExistingOrPendingTargets(t *testing.T) {
	if !storageMigrationDurable() {
		t.Skip("Windows Go APIs cannot durably fsync the pending migration marker directory")
	}
	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source")
	targetPath := filepath.Join(root, "target")
	if err := MigrateLatticeToRhiza(ctx, sourcePath, sourcePath); !hasErrorCode(err, ErrConfigInvalid) {
		t.Fatalf("same-path migration error = %v", err)
	}
	if err := MigrateLatticeToRhiza(ctx, sourcePath, filepath.Join(sourcePath, "nested")); !hasErrorCode(err, ErrConfigInvalid) {
		t.Fatalf("nested-target migration error = %v", err)
	}
	source, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: sourcePath, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	episode, err := source.IngestEpisode(ctx, EpisodeInput{ID: "migration-episode", Kind: "note", Content: "migration payload"})
	if err != nil {
		t.Fatal(err)
	}
	entity, err := source.UpsertEntity(ctx, EntityInput{ID: "migration-entity", Type: "Project", CanonicalName: "Migration"})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := source.AssertFact(ctx, FactInput{ID: "migration-fact", Predicate: "HAS_NOTE", SubjectID: entity.ID, ValueText: "original", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.SupersedeFact(ctx, fact.ID, FactInput{ID: "migration-fact-next", Predicate: fact.Predicate, SubjectID: entity.ID, ValueText: "replacement", SupportingEpisodeIDs: []string{episode.EpisodeID}}, "migration-test"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("migration-batch-%02d", i)
		if _, err := source.IngestEpisode(ctx, EpisodeInput{ID: id, Kind: "note", Content: strings.Repeat("m", 4<<10)}); err != nil {
			t.Fatalf("seed migration batch record %s: %v", id, err)
		}
	}
	want, err := Snapshot(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Close(ctx); err != nil {
		t.Fatal(err)
	}
	sourceFilesBefore, err := hashStorageTree(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	blockedSourceOpen := false
	blockedSourceOpenErr := error(nil)
	previousPublishBarrier := migrationBeforePublishBarrier
	migrationBeforePublishBarrier = func() {
		writer, openErr := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: sourcePath})
		if openErr == nil {
			_ = writer.Close(ctx)
			return
		}
		blockedSourceOpenErr = openErr
		blockedSourceOpen = strings.Contains(openErr.Error(), "database migration is in progress")
	}
	t.Cleanup(func() { migrationBeforePublishBarrier = previousPublishBarrier })
	if err := MigrateLatticeToRhiza(ctx, sourcePath, targetPath); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !blockedSourceOpen {
		t.Fatalf("source writer was not refused for ownership contention: %v", blockedSourceOpenErr)
	}
	sourceFilesAfter, err := hashStorageTree(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sourceFilesBefore, sourceFilesAfter) {
		t.Fatal("migration modified the Lattice source files")
	}
	writer, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: sourcePath})
	if err != nil {
		t.Fatalf("source remained locked after migration completed: %v", err)
	}
	if err := writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(targetPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completion marker stat error = %v", err)
	}
	target, err := Open(ctx, Config{DatabasePath: targetPath, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Snapshot(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if !equalPersistedJSON(want, got) {
		t.Fatal("migrated state differs from Lattice source")
	}
	if err := target.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := MigrateLatticeToRhiza(ctx, sourcePath, targetPath); err == nil {
		t.Fatal("migration overwrote existing target")
	}

	pendingTarget := filepath.Join(root, "pending")
	if err := os.WriteFile(databaseMigrationMarkerPath(pendingTarget), []byte(`{"state":"pending"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, Config{DatabasePath: pendingTarget, CreateIfMissing: true}); err == nil {
		t.Fatal("open accepted pending migration marker")
	}
}

func TestRhizaMigrationCancellationBeforePublishRetainsPendingMarker(t *testing.T) {
	if !storageMigrationDurable() {
		t.Skip("durable migration markers unavailable")
	}
	ctx := context.Background()
	root := t.TempDir()
	sourcePath, targetPath := filepath.Join(root, "source"), filepath.Join(root, "target")
	source, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: sourcePath, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Close(ctx); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	previous := migrationBeforePublishBarrier
	migrationBeforePublishBarrier = cancel
	t.Cleanup(func() { migrationBeforePublishBarrier = previous })
	if err := MigrateLatticeToRhiza(canceled, sourcePath, targetPath); !errors.Is(err, context.Canceled) {
		t.Fatalf("migration error = %v", err)
	}
	if _, err := os.Stat(databaseMigrationMarkerPath(targetPath)); err != nil {
		t.Fatalf("pending marker missing: %v", err)
	}
	writer, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: sourcePath})
	if err != nil {
		t.Fatalf("source ownership remained held after pre-publish cancellation: %v", err)
	}
	if err := writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRhizaMigrationPublishFailureReleasesSourceOwnership(t *testing.T) {
	if !storageMigrationDurable() {
		t.Skip("durable migration markers unavailable")
	}
	ctx := context.Background()
	root := t.TempDir()
	sourcePath, targetPath := filepath.Join(root, "source"), filepath.Join(root, "target")
	source, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: sourcePath, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Close(ctx); err != nil {
		t.Fatal(err)
	}
	previous := migrationBeforePublishBarrier
	migrationBeforePublishBarrier = func() { _ = os.Remove(databaseMigrationMarkerPath(targetPath)) }
	t.Cleanup(func() { migrationBeforePublishBarrier = previous })
	if err := MigrateLatticeToRhiza(ctx, sourcePath, targetPath); err == nil {
		t.Fatal("migration unexpectedly published without its pending marker")
	}
	writer, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: sourcePath})
	if err != nil {
		t.Fatalf("source ownership remained held after publish failure: %v", err)
	}
	if err := writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPublishMigratedDatabaseCloseFailuresKeepMarker(t *testing.T) {
	for _, test := range []string{"close error", "canceled during close"} {
		t.Run(test, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "pending")
			if err := os.WriteFile(marker, []byte("pending"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			closeErr := error(nil)
			closeSource := func() error {
				if test == "close error" {
					closeErr = errors.New("close failed")
				} else {
					cancel()
				}
				return closeErr
			}
			err := publishMigratedDatabase(ctx, marker, "source", closeSource)
			if test == "close error" {
				var structured *Error
				if !errors.As(err, &structured) || structured.Code != ErrStorageFailed {
					t.Fatalf("error = %T %v, want structured storage failure", err, err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context canceled", err)
			}
			if _, statErr := os.Stat(marker); statErr != nil {
				t.Fatalf("pending marker missing: %v", statErr)
			}
		})
	}
}

func TestRhizaLoadValidationErrorsAreStructured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rhiza")
	storeValue, err := newRhizaStore(Config{Driver: StorageDriverRhiza, DatabasePath: path, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	store := storeValue.(*rhizaStore)
	defer store.Close()
	if err := store.db.Apply(context.Background(), "invalid-record", []rhizastore.Record{{Key: "Fact:YWJj", Kind: "Fact", ID: "abc", Payload: "{"}}); err != nil {
		t.Fatal(err)
	}
	_, err = store.Load()
	var structured *Error
	if !errors.As(err, &structured) || structured.Code != ErrStorageFailed {
		t.Fatalf("Load error = %T %v, want structured storage failure", err, err)
	}
}

func TestRhizaMigrationOversizedRecordLeavesPendingMarkerAndSourceUntouched(t *testing.T) {
	if !storageMigrationDurable() {
		t.Skip("Windows migration is refused before writes because directory fsync is unavailable")
	}
	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "large-source")
	targetPath := filepath.Join(root, "large-target")
	source, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: sourcePath, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.IngestEpisode(ctx, EpisodeInput{ID: "large-legacy", Kind: "note", Content: strings.Repeat("x", 70<<10)}); err != nil {
		t.Fatalf("seed legacy oversized record: %v", err)
	}
	if err := source.Close(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := hashStorageTree(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	err = MigrateLatticeToRhiza(ctx, sourcePath, targetPath)
	if err == nil {
		t.Fatal("migration accepted a record larger than a bounded migration delta")
	}
	if _, err := os.Lstat(databaseMigrationMarkerPath(targetPath)); err != nil {
		t.Fatalf("failed migration did not retain pending marker: %v", err)
	}
	after, err := hashStorageTree(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rejected migration modified the Lattice source")
	}
	if _, err := Open(ctx, Config{DatabasePath: targetPath}); err == nil {
		t.Fatal("open accepted incomplete target after oversized-record failure")
	}
}

func hashStorageTree(root string) (map[string][32]byte, error) {
	hashes := make(map[string][32]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		hashes[rel] = sha256.Sum256(data)
		return nil
	})
	return hashes, err
}

func TestStorageDriverMarkersFailClosed(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	unknown := filepath.Join(root, "unknown")
	if err := os.Mkdir(unknown, 0o700); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(unknown, "keep.txt")
	if err := os.WriteFile(junk, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, Config{Driver: StorageDriverRhiza, DatabasePath: unknown, CreateIfMissing: true}); err == nil {
		t.Fatal("explicit Rhiza initialized an unknown nonempty directory")
	}
	if content, err := os.ReadFile(junk); err != nil || string(content) != "keep" {
		t.Fatalf("unknown directory changed: content=%q err=%v", content, err)
	}

	symlinkRoot := filepath.Join(root, "symlink-marker")
	if err := os.Mkdir(symlinkRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	markerTarget := filepath.Join(root, "marker-target")
	if err := os.WriteFile(markerTarget, []byte(rhizaDriverMarkerValue), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(markerTarget, filepath.Join(symlinkRoot, rhizaDriverMarker)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Open(ctx, Config{Driver: StorageDriverRhiza, DatabasePath: symlinkRoot, CreateIfMissing: true}); err == nil {
		t.Fatal("Rhiza followed a symlink storage marker")
	}
	if _, err := os.Stat(filepath.Join(symlinkRoot, "qlog")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink marker opened native Rhiza data: %v", err)
	}
	oversizedMarker := filepath.Join(root, "oversized-marker")
	if err := os.Mkdir(oversizedMarker, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oversizedMarker, rhizaDriverMarker), []byte(strings.Repeat("x", 1<<20)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readRhizaDriverMarker(oversizedMarker); err == nil {
		t.Fatal("oversized storage marker was accepted")
	}

	markerOnly := filepath.Join(root, "marker-only")
	if err := os.Mkdir(markerOnly, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(markerOnly, rhizaDriverMarker), []byte(rhizaDriverMarkerValue), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, Config{DatabasePath: markerOnly}); err == nil {
		t.Fatal("marker-only Rhiza root was treated as an existing database")
	}
	eng, err := Open(ctx, Config{DatabasePath: markerOnly, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("explicit empty-root recovery: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatal(err)
	}

	latticePath := filepath.Join(root, "lattice")
	lattice, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: latticePath, CreateIfMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := lattice.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, Config{Driver: StorageDriverRhiza, DatabasePath: latticePath, CreateIfMissing: true}); err == nil {
		t.Fatal("explicit Rhiza accepted Lattice signatures")
	}
	if _, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: markerOnly, CreateIfMissing: true}); err == nil {
		t.Fatal("explicit Lattice accepted Rhiza signatures")
	}
	untouchedRhiza := filepath.Join(root, "untouched-rhiza")
	if err := os.Mkdir(untouchedRhiza, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(untouchedRhiza, rhizaDriverMarker), []byte(rhizaDriverMarkerValue), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, Config{Driver: StorageDriverLattice, DatabasePath: untouchedRhiza, CreateIfMissing: true}); err == nil {
		t.Fatal("explicit Lattice accepted an uninitialized Rhiza marker")
	}
	if _, err := os.Lstat(databaseOwnershipPath(untouchedRhiza)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("driver conflict created ownership sidecar: %v", err)
	}
}

func equalPersistedJSON(left, right any) bool {
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && reflect.DeepEqual(a, b)
}
