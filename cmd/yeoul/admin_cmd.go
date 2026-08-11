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
	payload = importableExportPayload(payload)
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

	entityRevisionRows, err := queryRowsAllowMissing(store, "MATCH (r:EntityRevision) RETURN r.id, r.entity_id, r.space_id, r.revision_kind, r.tx_time, r.namespace, r.type, r.canonical_name, r.aliases_json, r.created_at, r.updated_at, r.metadata_json")
	if err != nil {
		return nil, err
	}
	for _, row := range entityRevisionRows {
		payload.EntityRevisions = append(payload.EntityRevisions, yeoul.EntityRevision{
			ID:            rowString(row, "r.id"),
			EntityID:      rowString(row, "r.entity_id"),
			SpaceID:       rowString(row, "r.space_id"),
			RevisionKind:  rowString(row, "r.revision_kind"),
			TxTime:        rowTime(row, "r.tx_time"),
			Namespace:     rowString(row, "r.namespace"),
			Type:          rowString(row, "r.type"),
			CanonicalName: rowString(row, "r.canonical_name"),
			Aliases:       rowStringSlice(row, "r.aliases_json"),
			CreatedAt:     rowTime(row, "r.created_at"),
			UpdatedAt:     rowTime(row, "r.updated_at"),
			Metadata:      rowMap(row, "r.metadata_json"),
		})
	}

	factRevisionRows, err := queryRowsAllowMissing(store, "MATCH (r:FactRevision) RETURN r.id, r.fact_id, r.space_id, r.revision_kind, r.tx_time, r.predicate, r.subject_id, r.object_id, r.value_text, r.confidence, r.status, r.valid_from, r.valid_to, r.observed_at, r.created_at, r.updated_at, r.retracted_at, r.retraction_reason, r.supporting_episode_ids_json, r.metadata_json")
	if err != nil {
		return nil, err
	}
	for _, row := range factRevisionRows {
		payload.FactRevisions = append(payload.FactRevisions, yeoul.FactRevision{
			ID:                   rowString(row, "r.id"),
			FactID:               rowString(row, "r.fact_id"),
			SpaceID:              rowString(row, "r.space_id"),
			RevisionKind:         rowString(row, "r.revision_kind"),
			TxTime:               rowTime(row, "r.tx_time"),
			Predicate:            rowString(row, "r.predicate"),
			SubjectID:            rowString(row, "r.subject_id"),
			ObjectID:             rowString(row, "r.object_id"),
			ValueText:            rowString(row, "r.value_text"),
			Confidence:           rowFloat64(row, "r.confidence"),
			Status:               rowString(row, "r.status"),
			ValidFrom:            rowTime(row, "r.valid_from"),
			ValidTo:              rowTime(row, "r.valid_to"),
			ObservedAt:           rowTime(row, "r.observed_at"),
			CreatedAt:            rowTime(row, "r.created_at"),
			UpdatedAt:            rowTime(row, "r.updated_at"),
			RetractedAt:          rowTime(row, "r.retracted_at"),
			RetractionReason:     rowString(row, "r.retraction_reason"),
			SupportingEpisodeIDs: rowStringSlice(row, "r.supporting_episode_ids_json"),
			Metadata:             rowMap(row, "r.metadata_json"),
		})
	}

	sort.Slice(payload.Episodes, func(i, j int) bool { return payload.Episodes[i].ID < payload.Episodes[j].ID })
	sort.Slice(payload.Entities, func(i, j int) bool { return payload.Entities[i].ID < payload.Entities[j].ID })
	sort.Slice(payload.Facts, func(i, j int) bool { return payload.Facts[i].ID < payload.Facts[j].ID })
	sort.Slice(payload.EntityRevisions, func(i, j int) bool { return payload.EntityRevisions[i].ID < payload.EntityRevisions[j].ID })
	sort.Slice(payload.FactRevisions, func(i, j int) bool { return payload.FactRevisions[i].ID < payload.FactRevisions[j].ID })
	return payload, nil
}

func importableExportPayload(payload *exportFile) *exportFile {
	if payload == nil {
		return nil
	}
	out := *payload
	out.EntityRevisions = nil
	out.FactRevisions = nil
	out.Facts = make([]yeoul.FactInput, 0, len(payload.Facts))
	for _, fact := range payload.Facts {
		if fact.Status != "" && fact.Status != "active" {
			continue
		}
		fact.Status = ""
		fact.Metadata = stripExportFactLifecycleMetadata(fact.Metadata)
		out.Facts = append(out.Facts, fact)
	}
	return &out
}

func stripExportFactLifecycleMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		switch key {
		case "superseded_by", "supersedes", "supersede_reason", "duplicate_of", "_history":
			continue
		default:
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
