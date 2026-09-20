package yeoul

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestQuiescedNativeDirectoryBackupPreservesState pins the documented backup
// procedure: with every Yeoul process stopped, copying the database's native
// directory (the `.ltdb` directory and its sibling namespace entries) to a new
// path yields a database that reopens with the same counts, revision history,
// and historical query results. `admin export` deliberately refuses
// revision-bearing or lifecycle state, so this native copy is the supported
// full-fidelity backup.
func TestQuiescedNativeDirectoryBackupPreservesState(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "yeoul.ltdb")

	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "native backup note", Source: SourceInput{Kind: "note", ExternalRef: "native-backup"}})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "native backup"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	oldFact, err := eng.AssertFact(ctx, FactInput{ID: "fact:native-old", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "old", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert old fact: %v", err)
	}
	supersede, err := eng.SupersedeFact(ctx, oldFact.ID, FactInput{SubjectID: entity.ID, Predicate: "HAS_STATE", ValueText: "new", SupportingEpisodeIDs: []string{episode.EpisodeID}}, "replaced")
	if err != nil {
		t.Fatalf("supersede old fact: %v", err)
	}
	newFactID := supersede.NewFactID
	beforeSupersede := oldFact.CreatedAt.Add(time.Nanosecond)

	beforeSnapshot, err := Snapshot(ctx, eng)
	if err != nil {
		t.Fatalf("snapshot before: %v", err)
	}
	if len(beforeSnapshot.FactRevisions) == 0 {
		t.Fatal("expected revision history to exist before backup")
	}
	beforeHistorical, err := eng.LookupFacts(ctx, FactLookupRequest{
		SubjectIDs: []string{entity.ID},
		Temporal:   TemporalFilter{AsOf: &beforeSupersede, IncludeInactive: true},
	})
	if err != nil {
		t.Fatalf("historical lookup before: %v", err)
	}
	if len(beforeHistorical.Facts) != 1 || beforeHistorical.Facts[0].ID != oldFact.ID {
		t.Fatalf("expected the pre-supersede fact historically before backup, got %#v", beforeHistorical.Facts)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close source: %v", err)
	}

	backupRoot := t.TempDir()
	backupPath := filepath.Join(backupRoot, "yeoul.ltdb")
	if err := copyNativeDatabase(dbPath, backupPath); err != nil {
		t.Fatalf("copy native database: %v", err)
	}

	backup, err := Open(ctx, Config{DatabasePath: backupPath})
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer func() { _ = backup.Close(ctx) }()

	afterSnapshot, err := Snapshot(ctx, backup)
	if err != nil {
		t.Fatalf("snapshot after: %v", err)
	}
	if len(afterSnapshot.Episodes) != len(beforeSnapshot.Episodes) || len(afterSnapshot.Entities) != len(beforeSnapshot.Entities) || len(afterSnapshot.Facts) != len(beforeSnapshot.Facts) {
		t.Fatalf("expected backup counts to match: before episodes=%d entities=%d facts=%d, after episodes=%d entities=%d facts=%d",
			len(beforeSnapshot.Episodes), len(beforeSnapshot.Entities), len(beforeSnapshot.Facts),
			len(afterSnapshot.Episodes), len(afterSnapshot.Entities), len(afterSnapshot.Facts))
	}
	if len(afterSnapshot.FactRevisions) != len(beforeSnapshot.FactRevisions) {
		t.Fatalf("expected revision history to survive backup: before=%d after=%d", len(beforeSnapshot.FactRevisions), len(afterSnapshot.FactRevisions))
	}
	for id, revision := range beforeSnapshot.FactRevisions {
		restored, ok := afterSnapshot.FactRevisions[id]
		if !ok {
			t.Fatalf("expected fact revision %s to survive backup", id)
		}
		if restored.RevisionKind != revision.RevisionKind || restored.Status != revision.Status {
			t.Fatalf("expected fact revision %s kind %q status %q, got kind %q status %q", id, revision.RevisionKind, revision.Status, restored.RevisionKind, restored.Status)
		}
	}

	afterHistorical, err := backup.LookupFacts(ctx, FactLookupRequest{
		SubjectIDs: []string{entity.ID},
		Temporal:   TemporalFilter{AsOf: &beforeSupersede, IncludeInactive: true},
	})
	if err != nil {
		t.Fatalf("historical lookup after: %v", err)
	}
	if len(afterHistorical.Facts) != 1 || afterHistorical.Facts[0].ID != oldFact.ID {
		t.Fatalf("expected historical query results to survive backup, got %#v", afterHistorical.Facts)
	}
	current, err := backup.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{entity.ID}})
	if err != nil {
		t.Fatalf("current lookup after: %v", err)
	}
	if len(current.Facts) != 1 || current.Facts[0].ID != newFactID {
		t.Fatalf("expected current fact to survive backup, got %#v", current.Facts)
	}
	search, err := backup.Search(ctx, SearchRequest{QueryText: "native backup note"})
	if err != nil {
		t.Fatalf("search backup: %v", err)
	}
	if len(search.Hits) == 0 {
		t.Fatal("expected search results to survive backup")
	}
}

// copyNativeDatabase copies the quiesced native database file set to a new
// path. Ownership lock files are skipped: they are process-scoped and are
// recreated by the next open, so copying a stale lock would be wrong.
func copyNativeDatabase(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if strings.HasSuffix(entry.Name(), ".lock") {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.Create(filepath.Join(dst, rel))
		if err != nil {
			return err
		}
		defer func() { _ = out.Close() }()
		_, err = io.Copy(out, in)
		return err
	})
}
