package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func (c cli) runFact(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul fact get --db PATH --id ID [--json]
  yeoul fact lookup --db PATH [--subject-id IDS] [--predicate PREDS] [--object-id IDS] [--object-text TEXT] [--group-id IDS] [--as-of RFC3339] [--valid-at RFC3339] [--valid-from RFC3339] [--valid-to RFC3339] [--include-inactive] [--limit N] [--cursor CURSOR] [--json]
  yeoul fact assert --db PATH --predicate PRED (--subject-id ID | --upsert-subject --subject-namespace NS --subject-type TYPE --subject-name NAME [--subject-stable-key KEY]) [--object-id ID | --upsert-object --object-namespace NS --object-type TYPE --object-name NAME [--object-stable-key KEY]] [--value-text TEXT] [--observed-at RFC3339] [--valid-from RFC3339] [--valid-to RFC3339] [--cardinality one|many] --supporting-episodes IDS [--json]
  yeoul fact supersede --db PATH --id ID --predicate PRED --subject-id ID [--object-id ID] [--value-text TEXT] [--valid-from RFC3339] [--valid-to RFC3339] --supporting-episodes IDS --reason TEXT [--json]
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
      [--group-id IDS] [--as-of RFC3339] [--valid-at RFC3339] [--valid-from RFC3339] [--valid-to RFC3339] [--include-inactive] [--limit N] [--cursor CURSOR] [--json]
`)

	fs := newFlagSet("fact lookup")
	var dbPath string
	var subjectIDsRaw string
	var predicatesRaw string
	var objectIDsRaw string
	var objectText string
	var groupIDsRaw string
	var asOfRaw string
	var validAtRaw string
	var validFromRaw string
	var validToRaw string
	var includeInactive bool
	var limit int
	var cursor string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&subjectIDsRaw, "subject-id", "", "comma-separated subject IDs")
	fs.StringVar(&predicatesRaw, "predicate", "", "comma-separated predicates")
	fs.StringVar(&objectIDsRaw, "object-id", "", "comma-separated object IDs")
	fs.StringVar(&objectText, "object-text", "", "free-text object/value filter")
	fs.StringVar(&groupIDsRaw, "group-id", "", "comma-separated group IDs")
	fs.StringVar(&asOfRaw, "as-of", "", "knowledge-time view in RFC3339 format")
	fs.StringVar(&validAtRaw, "valid-at", "", "domain-valid point in RFC3339 format")
	fs.StringVar(&validFromRaw, "valid-from", "", "domain-valid interval start in RFC3339 format")
	fs.StringVar(&validToRaw, "valid-to", "", "domain-valid interval end in RFC3339 format")
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
	temporal, err := parseTemporalFlagsFull(asOfRaw, validAtRaw, "", "", validFromRaw, validToRaw, includeInactive)
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
		Scope:      yeoul.ScopeFilter{GroupIDs: splitCSV(groupIDsRaw)},
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
  yeoul fact assert --db PATH --predicate PRED (--subject-id ID | --upsert-subject --subject-namespace NS --subject-type TYPE --subject-name NAME [--subject-stable-key KEY]) [--object-id ID | --upsert-object --object-namespace NS --object-type TYPE --object-name NAME [--object-stable-key KEY]] [--value-text TEXT] [--observed-at RFC3339] [--valid-from RFC3339] [--valid-to RFC3339] [--cardinality one|many] --supporting-episodes IDS [--json]
`)

	fs := newFlagSet("fact assert")
	var dbPath string
	var predicate string
	var subjectID string
	var subjectType string
	var subjectName string
	var subjectNamespace string
	var subjectStableKey string
	var upsertSubject bool
	var objectID string
	var objectType string
	var objectName string
	var objectNamespace string
	var objectStableKey string
	var upsertObject bool
	var valueText string
	var observedAtRaw string
	var validFromRaw string
	var validToRaw string
	var cardinality string
	var supportingEpisodes string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&predicate, "predicate", "", "fact predicate")
	fs.StringVar(&subjectID, "subject-id", "", "subject entity ID")
	fs.StringVar(&subjectType, "subject-type", "", "subject entity type when --upsert-subject is set")
	fs.StringVar(&subjectName, "subject-name", "", "subject canonical name when --upsert-subject is set")
	fs.StringVar(&subjectNamespace, "subject-namespace", "", "subject entity namespace when --upsert-subject is set")
	fs.StringVar(&subjectStableKey, "subject-stable-key", "", "subject stable identity key when --upsert-subject is set")
	fs.BoolVar(&upsertSubject, "upsert-subject", false, "create or update the subject entity before asserting the fact")
	fs.StringVar(&objectID, "object-id", "", "object entity ID")
	fs.StringVar(&objectType, "object-type", "", "object entity type when --upsert-object is set")
	fs.StringVar(&objectName, "object-name", "", "object canonical name when --upsert-object is set")
	fs.StringVar(&objectNamespace, "object-namespace", "", "object entity namespace when --upsert-object is set")
	fs.StringVar(&objectStableKey, "object-stable-key", "", "object stable identity key when --upsert-object is set")
	fs.BoolVar(&upsertObject, "upsert-object", false, "create or update the object entity before asserting the fact")
	fs.StringVar(&valueText, "value-text", "", "value text")
	fs.StringVar(&observedAtRaw, "observed-at", "", "observed time in RFC3339 format")
	fs.StringVar(&validFromRaw, "valid-from", "", "domain-valid interval start in RFC3339 format")
	fs.StringVar(&validToRaw, "valid-to", "", "domain-valid interval end in RFC3339 format")
	fs.StringVar(&cardinality, "cardinality", "", "fact slot cardinality: one or many")
	fs.StringVar(&supportingEpisodes, "supporting-episodes", "", "comma-separated supporting episode IDs")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || predicate == "" || supportingEpisodes == "" {
		return &usageError{message: usage}
	}
	if !upsertSubject && strings.TrimSpace(subjectID) == "" {
		return &usageError{message: usage}
	}
	if upsertSubject && (strings.TrimSpace(subjectType) == "" || strings.TrimSpace(subjectName) == "") {
		return &usageError{message: usage}
	}
	if upsertObject && (strings.TrimSpace(objectType) == "" || strings.TrimSpace(objectName) == "") {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	supportingEpisodeIDs := splitCSV(supportingEpisodes)
	observedAtInfo, err := inferFactObservedAt(ctx, eng, supportingEpisodeIDs, observedAtRaw)
	if err != nil {
		_ = closeEngine(ctx, eng)
		if strings.TrimSpace(observedAtRaw) != "" {
			return &usageError{message: usage}
		}
		return err
	}
	validFrom, err := parseFactTimeFlag(validFromRaw, usage)
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}
	validTo, err := parseFactTimeFlag(validToRaw, usage)
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}

	batch := yeoul.BatchInput{}
	if upsertSubject {
		subjectInput := yeoul.EntityInput{
			ID:            subjectID,
			Namespace:     subjectNamespace,
			Type:          subjectType,
			CanonicalName: subjectName,
			StableKey:     subjectStableKey,
		}
		if strings.TrimSpace(subjectID) == "" {
			subjectID = yeoul.EntityID(subjectNamespace, subjectType, fallbackString(subjectStableKey, subjectName))
			subjectInput.ID = subjectID
		}
		batch.Entities = append(batch.Entities, subjectInput)
	}
	if upsertObject {
		objectInput := yeoul.EntityInput{
			ID:            objectID,
			Namespace:     objectNamespace,
			Type:          objectType,
			CanonicalName: objectName,
			StableKey:     objectStableKey,
		}
		if strings.TrimSpace(objectID) == "" {
			objectID = yeoul.EntityID(objectNamespace, objectType, fallbackString(objectStableKey, objectName))
			objectInput.ID = objectID
		}
		batch.Entities = append(batch.Entities, objectInput)
	}

	factInput := yeoul.FactInput{
		Predicate:            predicate,
		SubjectID:            subjectID,
		ObjectID:             objectID,
		ValueText:            valueText,
		ObservedAt:           observedAtInfo.ObservedAt,
		ValidFrom:            validFrom,
		ValidTo:              validTo,
		SupportingEpisodeIDs: supportingEpisodeIDs,
		Cardinality:          cardinality,
		Metadata:             observedAtInfo.Metadata(),
	}
	var result *yeoul.Fact
	if len(batch.Entities) > 0 {
		batch.Facts = []yeoul.FactInput{factInput}
		batchResult, batchErr := eng.IngestBatch(ctx, batch)
		if batchErr == nil {
			if len(batchResult.FactIDs) != 1 {
				batchErr = fmt.Errorf("fact assert failed: expected 1 fact id, got %d", len(batchResult.FactIDs))
			} else {
				result, batchErr = eng.GetFact(ctx, batchResult.FactIDs[0])
			}
		}
		err = batchErr
	} else {
		result, err = eng.AssertFact(ctx, factInput)
	}
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

type factObservedAtInfo struct {
	ObservedAt          time.Time
	Basis               string
	SupportingEpisodeID string
}

func (info factObservedAtInfo) Metadata() map[string]any {
	metadata := map[string]any{
		observedAtBasisKey: info.Basis,
	}
	if info.SupportingEpisodeID != "" {
		metadata[observedAtSupportingEpisodeIDKey] = info.SupportingEpisodeID
	}
	return metadata
}

func inferFactObservedAt(ctx context.Context, eng yeoul.Engine, supportingEpisodeIDs []string, observedAtRaw string) (factObservedAtInfo, error) {
	if strings.TrimSpace(observedAtRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, observedAtRaw)
		if err != nil {
			return factObservedAtInfo{}, err
		}
		return factObservedAtInfo{ObservedAt: parsed, Basis: observedAtBasisExplicit}, nil
	}
	for _, episodeID := range supportingEpisodeIDs {
		episode, err := eng.GetEpisode(ctx, episodeID)
		if err != nil {
			return factObservedAtInfo{}, err
		}
		if !episode.ObservedAt.IsZero() {
			return factObservedAtInfo{
				ObservedAt:          episode.ObservedAt,
				Basis:               observedAtBasisSupportingEpisode,
				SupportingEpisodeID: episode.ID,
			}, nil
		}
	}
	return factObservedAtInfo{
		ObservedAt: time.Now().UTC(),
		Basis:      observedAtBasisSystemTimeDefault,
	}, nil
}

func parseFactTimeFlag(raw, usage string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, &usageError{message: usage}
	}
	return parsed, nil
}

func (c cli) runFactSupersede(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul fact supersede --db PATH --id ID --predicate PRED --subject-id ID [--object-id ID] [--value-text TEXT] [--valid-from RFC3339] [--valid-to RFC3339] --supporting-episodes IDS --reason TEXT [--json]
`)

	fs := newFlagSet("fact supersede")
	var dbPath string
	var factID string
	var predicate string
	var subjectID string
	var objectID string
	var valueText string
	var validFromRaw string
	var validToRaw string
	var supportingEpisodes string
	var reason string
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&factID, "id", "", "fact ID to supersede")
	fs.StringVar(&predicate, "predicate", "", "new fact predicate")
	fs.StringVar(&subjectID, "subject-id", "", "subject entity ID")
	fs.StringVar(&objectID, "object-id", "", "object entity ID")
	fs.StringVar(&valueText, "value-text", "", "value text")
	fs.StringVar(&validFromRaw, "valid-from", "", "domain-valid interval start in RFC3339 format")
	fs.StringVar(&validToRaw, "valid-to", "", "domain-valid interval end in RFC3339 format")
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
	validFrom, err := parseFactTimeFlag(validFromRaw, usage)
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}
	validTo, err := parseFactTimeFlag(validToRaw, usage)
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}
	result, err := eng.SupersedeFact(ctx, factID, yeoul.FactInput{
		Predicate:            predicate,
		SubjectID:            subjectID,
		ObjectID:             objectID,
		ValueText:            valueText,
		ValidFrom:            validFrom,
		ValidTo:              validTo,
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
