package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func (c cli) runAdmin(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul admin checkpoint --db PATH [--json]
  yeoul admin migrate-db --db PATH [--json]
  yeoul admin compact --db PATH [--apply] [--json]
  yeoul admin export --db PATH --out FILE [--json]
  yeoul admin import --db PATH --in FILE [--json] [--confirm]
`)
	if len(args) == 0 {
		return &usageError{message: usage}
	}
	switch args[0] {
	case "checkpoint":
		return c.runAdminCheckpoint(ctx, args[1:])
	case "migrate-db":
		return c.runAdminMigrateDatabase(ctx, args[1:])
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

func (c cli) runAdminMigrateDatabase(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul admin migrate-db --db PATH [--json]
`)
	fs := newFlagSet("admin migrate-db")
	var dbPath string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil || handled {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	result, err := yeoul.MigrateDatabase(ctx, dbPath)
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	if result.Migrated {
		_, err = fmt.Fprintf(c.stdout, "migrated %s (backup: %s)\n", result.DatabasePath, result.BackupPath)
		return err
	}
	_, err = fmt.Fprintf(c.stdout, "already lattice: %s\n", result.DatabasePath)
	return err
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
	if err := yeoul.CheckpointDatabase(ctx, dbPath); err != nil {
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
	if !apply {
		// The dry run only reports candidates, so it stays a read-only preview
		// and does not take the writable ownership an apply needs.
		payload, err := exportDatabase(ctx, dbPath)
		if err != nil {
			return err
		}
		candidates := compactionPlanFromPayload(payload)
		report := map[string]any{
			"database_path":               dbPath,
			"mode":                        "dry-run",
			"entity_duplicate_candidates": len(candidates.EntityCandidates),
			"fact_duplicate_candidates":   len(candidates.FactCandidates),
			"entity_candidates":           candidates.EntityCandidates,
			"fact_candidates":             candidates.FactCandidates,
		}
		if jsonOut {
			return writeJSON(c.stdout, report)
		}
		_, err = fmt.Fprintf(c.stdout, "dry-run entity_candidates=%d fact_candidates=%d\n", len(candidates.EntityCandidates), len(candidates.FactCandidates))
		return err
	}

	// Applying compaction is explicit maintenance, so the whole operation runs
	// inside one maintenance window: the candidates are discovered and applied
	// while this process holds writable ownership, and no other process can
	// change a candidate between the two steps.
	eng, err := openMaintenanceEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	candidates, err := compactionPlanFromEngine(ctx, eng)
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}
	report := map[string]any{
		"database_path":               dbPath,
		"mode":                        "apply",
		"entity_duplicate_candidates": len(candidates.EntityCandidates),
		"fact_duplicate_candidates":   len(candidates.FactCandidates),
		"entity_candidates":           candidates.EntityCandidates,
		"fact_candidates":             candidates.FactCandidates,
	}
	markedEntities := 0
	for _, candidate := range candidates.EntityCandidates {
		marked, err := applyEntityMergeCandidate(ctx, eng, candidate)
		if err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		markedEntities += marked
	}
	retractedFacts := 0
	for _, candidate := range candidates.FactCandidates {
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
	report["entity_marked"] = markedEntities
	report["facts_retracted"] = retractedFacts
	if jsonOut {
		return writeJSON(c.stdout, report)
	}
	_, err = fmt.Fprintf(c.stdout, "applied compaction entity_marked=%d facts_retracted=%d\n", markedEntities, retractedFacts)
	return err
}

// compactionPlan is the set of changes one maintenance window decided to apply.
// It is built from state read while the window holds writable ownership, so the
// records it names cannot change between building and applying it.
type compactionPlan struct {
	EntityCandidates []entityMergeCandidate
	FactCandidates   []factDuplicateCandidate
}

// compactionPlanFromPayload derives the candidates from one snapshot of the
// database.
func compactionPlanFromPayload(payload *exportFile) *compactionPlan {
	return &compactionPlan{
		EntityCandidates: buildEntityMergeCandidates(payload),
		FactCandidates:   buildFactDuplicateCandidates(payload),
	}
}

// compactionPlanFromEngine snapshots the engine it is given and derives the
// compaction candidates from that snapshot. The caller must hold the writable
// ownership of that engine, so the snapshot taken here is the state the plan
// will be applied to.
func compactionPlanFromEngine(ctx context.Context, eng yeoul.Engine) (*compactionPlan, error) {
	payload, err := exportDatabaseFromEngine(ctx, eng)
	if err != nil {
		return nil, err
	}
	return compactionPlanFromPayload(payload), nil
}

// applyEntityMergeCandidate marks one duplicate group: the target records the
// sources it absorbed and each source records the target it duplicates. It
// returns how many sources were marked. The records are re-read from the engine
// so the marks are written onto the state the window owns, not onto a copy taken
// earlier.
func applyEntityMergeCandidate(ctx context.Context, eng yeoul.Engine, candidate entityMergeCandidate) (int, error) {
	marked := time.Now().UTC().Format(time.RFC3339)
	target, err := eng.GetEntity(ctx, candidate.TargetID)
	if err != nil {
		return 0, err
	}
	sources := make([]*yeoul.Entity, 0, len(candidate.SourceIDs))
	for _, sourceID := range candidate.SourceIDs {
		source, err := eng.GetEntity(ctx, sourceID)
		if err != nil {
			return 0, err
		}
		sources = append(sources, source)
	}
	// A candidate that only grouped through recorded drift records the drift on
	// both sides, so the reconciliation is inspectable from the target and from
	// each absorbed duplicate.
	targetDrift := map[string]any{}
	if candidate.DriftNamespace {
		if drift := mergeDriftSummary(target, sources, "merge_drift_namespace"); drift != nil {
			targetDrift["merge_drift_namespace"] = drift
		}
	}
	if candidate.DriftType {
		if drift := mergeDriftSummary(target, sources, "merge_drift_type"); drift != nil {
			targetDrift["merge_drift_type"] = drift
		}
	}
	targetMeta := mergeMaps(target.Metadata, mergeMaps(map[string]any{
		"compaction_entity_duplicates": mergeStringSlices(anyStrings(target.Metadata["compaction_entity_duplicates"]), candidate.SourceIDs),
		"compaction_marked":            marked,
	}, targetDrift))
	if _, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
		ID:            target.ID,
		SpaceID:       target.SpaceID,
		Namespace:     target.Namespace,
		Type:          target.Type,
		CanonicalName: target.CanonicalName,
		Aliases:       target.Aliases,
		Metadata:      targetMeta,
	}); err != nil {
		return 0, err
	}
	count := 0
	for _, source := range sources {
		sourceDrift := map[string]any{}
		if candidate.DriftNamespace && source.Namespace != target.Namespace {
			sourceDrift["merge_drift_namespace"] = fmt.Sprintf("%s -> %s", target.Namespace, source.Namespace)
		}
		if candidate.DriftType && source.Type != target.Type {
			sourceDrift["merge_drift_type"] = fmt.Sprintf("%s -> %s", target.Type, source.Type)
		}
		if _, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
			ID:            source.ID,
			SpaceID:       source.SpaceID,
			Namespace:     source.Namespace,
			Type:          source.Type,
			CanonicalName: source.CanonicalName,
			Aliases:       source.Aliases,
			Metadata: mergeMaps(source.Metadata, mergeMaps(map[string]any{
				"duplicate_of":      target.ID,
				"compaction_marked": marked,
			}, sourceDrift)),
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
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
	if err := validateImportableExportPayload(payload); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := writePrivateFile(outPath, data); err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, map[string]any{"database_path": dbPath, "out": outPath})
	}
	_, err = fmt.Fprintf(c.stdout, "exported %s\n", outPath)
	return err
}

// writePrivateFile writes data to path with 0600 permissions. Exports contain
// complete episode contents and source metadata, so neither a permissive umask
// nor a previously permissive export at the same path may leave the payload
// readable by other local accounts.
//
// The payload is written inside a staging directory that is hardened before it
// receives any data: the empty directory is created, its inherited ACL grants
// are removed, and only then is the export file created inside it. A file
// created there inherits no grants for other accounts, so no window exists in
// which another local account can open the payload and keep a read handle
// across a later permission change. The finished file is published with a
// rename, so a failed write leaves any previous export intact, and the
// containing directory is synced so the rename itself survives a crash.
func writePrivateFile(path string, data []byte) error {
	if err := ensurePrivateFileSupported(); err != nil {
		return err
	}
	return writePrivateFileStaged(path, data)
}

// writePrivateFileStaged performs the platform-independent staging and publish
// steps. It is separate from the support check so the durability behavior can
// be exercised on platforms where export is refused.
func writePrivateFileStaged(path string, data []byte) error {
	stageDir, err := os.MkdirTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp(stageDir, "export-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	// Clean up without recursion: in a shared writable parent the staging
	// directory entry can be substituted with an unrelated directory, and a
	// recursive delete would then destroy data this process does not own.
	defer func() {
		_ = os.Remove(tempPath)
		_ = os.Remove(stageDir)
	}()

	// Bind the permission change to the open descriptor: a path-based chmod
	// would follow a substituted symlink in a shared writable parent.
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if err := writePrivateFileContents(temp, data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return syncPrivateFileDir(filepath.Dir(path))
}

// writePrivateFileContents is a seam for tests to inject a partial-write
// failure and prove the previous export survives it.
var writePrivateFileContents = func(file *os.File, data []byte) error {
	_, err := file.Write(data)
	return err
}

// syncPrivateFileDir flushes the directory entry created by the publish
// rename so the replacement export is durable, not only the file contents.
func syncPrivateFileDir(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		// Directory fsync is not portable: Windows cannot flush a directory
		// handle (FlushFileBuffers is refused with "Access is denied"), so the
		// durability barrier is a documented no-op there, matching the migration
		// protocol's syncDirectory. The publish above already renamed the
		// complete file into place, so only the platform's refusal is tolerated:
		// every other platform still takes the barrier and reports a genuine
		// failure, and a non-Windows sync failure still fails the export.
		if runtime.GOOS == "windows" {
			return closeErr
		}
		return syncErr
	}
	return closeErr
}

func (c cli) runAdminImport(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul admin import --db PATH --in FILE [--json] [--confirm]
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
	if err := decodeSingleJSON(data, &payload); err != nil {
		return err
	}
	if len(payload.EntityRevisions) > 0 || len(payload.FactRevisions) > 0 {
		return fmt.Errorf("admin import does not restore revision history; import episodes/entities/facts only")
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

func exportDatabase(ctx context.Context, dbPath string) (*exportFile, error) {
	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeEngine(ctx, eng) }()
	return exportDatabaseFromEngine(ctx, eng)
}

func exportDatabaseFromEngine(ctx context.Context, eng yeoul.Engine) (*exportFile, error) {
	snapshot, err := yeoul.Snapshot(ctx, eng)
	if err != nil {
		return nil, err
	}
	sourceMap := make(map[string]yeoul.SourceInput, len(snapshot.Sources))
	for id, source := range snapshot.Sources {
		sourceMap[id] = yeoul.SourceInput{
			ID:          source.ID,
			SpaceID:     source.SpaceID,
			Kind:        source.Kind,
			URI:         source.URI,
			ExternalRef: source.ExternalRef,
			Metadata:    source.Metadata,
		}
	}

	payload := &exportFile{}
	for _, record := range snapshot.Episodes {
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
	for _, record := range snapshot.Entities {
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

	for _, record := range snapshot.Facts {
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

	for _, revision := range snapshot.EntityRevisions {
		payload.EntityRevisions = append(payload.EntityRevisions, revision)
	}
	for _, revision := range snapshot.FactRevisions {
		payload.FactRevisions = append(payload.FactRevisions, revision)
	}

	sort.Slice(payload.Episodes, func(i, j int) bool { return payload.Episodes[i].ID < payload.Episodes[j].ID })
	sort.Slice(payload.Entities, func(i, j int) bool { return payload.Entities[i].ID < payload.Entities[j].ID })
	sort.Slice(payload.Facts, func(i, j int) bool { return payload.Facts[i].ID < payload.Facts[j].ID })
	sort.Slice(payload.EntityRevisions, func(i, j int) bool { return payload.EntityRevisions[i].ID < payload.EntityRevisions[j].ID })
	sort.Slice(payload.FactRevisions, func(i, j int) bool { return payload.FactRevisions[i].ID < payload.FactRevisions[j].ID })
	return payload, nil
}

func validateImportableExportPayload(payload *exportFile) error {
	if payload == nil {
		return nil
	}
	for _, fact := range payload.Facts {
		if fact.Status != "" && fact.Status != "active" {
			return fmt.Errorf("admin export cannot create an importable snapshot: inactive fact %q has status %q; full-fidelity restore is not implemented", fact.ID, fact.Status)
		}
		if hasFactLifecycleMetadata(fact.Metadata) {
			return fmt.Errorf("admin export cannot create an importable snapshot: fact %q contains lifecycle metadata; full-fidelity restore is not implemented", fact.ID)
		}
	}
	if len(payload.EntityRevisions) > 0 || len(payload.FactRevisions) > 0 {
		return fmt.Errorf("admin export cannot create an importable snapshot: database contains revision history; full-fidelity restore is not implemented")
	}
	return nil
}

func hasFactLifecycleMetadata(metadata map[string]any) bool {
	for _, key := range []string{"superseded_by", "supersedes", "supersede_reason", "duplicate_of", "_history", "history_inferred"} {
		if _, ok := metadata[key]; ok {
			return true
		}
	}
	return false
}
