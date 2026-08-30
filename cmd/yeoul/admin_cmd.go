package main

import (
	"context"
	"fmt"
	"os"
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
	if err := validateImportableExportPayload(payload); err != nil {
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
	if err := json.Unmarshal(data, &payload); err != nil {
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
	for _, key := range []string{"superseded_by", "supersedes", "supersede_reason", "duplicate_of", "_history"} {
		if _, ok := metadata[key]; ok {
			return true
		}
	}
	return false
}
