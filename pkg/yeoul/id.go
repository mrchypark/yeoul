package yeoul

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
)

// Record kind labels used when reporting explicit ID conflicts. They match the
// user-facing record names rather than the internal map names.
const (
	kindSource             = "source"
	kindEpisode            = "episode"
	kindEntity             = "entity"
	kindFact               = "fact"
	kindFactRevision       = "fact_revision"
	kindEntityRevision     = "entity_revision"
	kindMigrationWatermark = "migration_watermark"
)

func normalizeSourceID(spaceID, kind, externalRef string) string {
	return "src:" + readableIdentitySlug(spaceID, kind, externalRef) + "~" + identityFingerprint(spaceID, kind, externalRef)
}

func legacyEntityID(namespace, entityType, canonical string) string {
	return readableIdentitySlug(namespace, entityType, canonical)
}

// LegacyEntityID returns the pre-fingerprint entity ID used for compatibility
// reuse. Callers must still verify the full identity tuple and space.
func LegacyEntityID(namespace, entityType, canonical string) string {
	return legacyEntityID(namespace, entityType, canonical)
}

// legacySourceID is the source ID format written by releases before the
// fingerprint suffix (space-qualified readable slug) and stays a checked
// compatibility lookup target.
func legacySourceID(spaceID, kind, externalRef string) string {
	parts := []string{"src", normalizeIDPart(spaceID), normalizeIDPart(kind), normalizeIDPart(externalRef)}
	return strings.Trim(strings.Join(parts, ":"), ":")
}

func normalizeLegacySourceID(kind, externalRef string) string {
	return readableIdentitySlug("src", kind, externalRef)
}

func sourceMatches(source Source, spaceID, kind, externalRef string) bool {
	return source.SpaceID == spaceID && source.Kind == kind && source.ExternalRef == externalRef
}

func normalizeSpaceID(spaceID string) string {
	return firstNonEmpty(spaceID, "default")
}

func normalizeIDPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, "/", "-")
	return value
}

// readableIdentitySlug keeps IDs human-readable by reusing the historical
// display normalization. It is only a label: the identity fingerprint below is
// what makes derived IDs exact.
func readableIdentitySlug(parts ...string) string {
	slugs := make([]string, 0, len(parts))
	for _, part := range parts {
		slugs = append(slugs, normalizeIDPart(part))
	}
	return strings.Trim(strings.Join(slugs, ":"), ":")
}

// identityFingerprint returns a short stable digest of the exact identity
// tuple, distinguishing values that the readable slug merges (case, "/" vs
// "-", spaces, and other separators).
func identityFingerprint(parts ...string) string {
	hasher := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(hasher, "%d:%s|", len(part), part)
	}
	return hex.EncodeToString(hasher.Sum(nil)[:8])
}

func entityIdentityMatches(entity Entity, input EntityInput) bool {
	if entity.Namespace != input.Namespace || entity.Type != input.Type {
		return false
	}
	incomingKey := input.StableKey
	storedKey := metadataStableKey(entity.Metadata)
	if strings.TrimSpace(incomingKey) != "" || strings.TrimSpace(storedKey) != "" {
		return incomingKey == storedKey
	}
	return entity.CanonicalName == input.CanonicalName
}

// EntityInputsMatchIdentity applies the same tolerant identity rules used by
// entity resolution to two pending automatic entity inputs. It is used by
// callers that must validate a batch before either input exists in storage.
func EntityInputsMatchIdentity(left, right EntityInput) bool {
	if normalizeSpaceID(left.SpaceID) != normalizeSpaceID(right.SpaceID) {
		return false
	}
	match := func(stored Entity, request EntityInput) bool {
		matched, _ := entityMatchesResolveRequest(stored, EntityResolveRequest{
			Namespace:       request.Namespace,
			Type:            request.Type,
			CanonicalName:   request.CanonicalName,
			StableKey:       request.StableKey,
			IncludeKeyDrift: true,
		})
		return matched
	}
	return match(entityFromInput(left), right) || match(entityFromInput(right), left)
}

func entityFromInput(input EntityInput) Entity {
	metadata := map[string]any{}
	if strings.TrimSpace(input.StableKey) != "" {
		metadata["stable_key"] = input.StableKey
	}
	return Entity{
		Namespace:     input.Namespace,
		Type:          input.Type,
		CanonicalName: input.CanonicalName,
		Aliases:       input.Aliases,
		Metadata:      metadata,
	}
}

func legacyEntityIdentityMatches(entity Entity, input EntityInput) bool {
	if entity.Namespace != input.Namespace || entity.Type != input.Type {
		return false
	}
	incomingKey := input.StableKey
	storedKey := metadataStableKey(entity.Metadata)
	if strings.TrimSpace(incomingKey) != "" {
		return incomingKey == storedKey
	}
	if strings.TrimSpace(storedKey) != "" {
		return false
	}
	return entity.CanonicalName == input.CanonicalName
}

func metadataStableKey(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata["stable_key"].(string)
	if !ok {
		return ""
	}
	return value
}

func (e *engine) newIDLocked(prefix string) string {
	for {
		next := atomic.AddUint64(&e.sequence, 1)
		id := fmt.Sprintf("%s_%06d", prefix, next)
		if !e.idExistsLocked(id) {
			return id
		}
	}
}

// Revision ID prefixes generated by newIDLocked. The numeric suffix of a
// generated revision ID is the engine sequence at creation time.
const (
	factRevisionIDPrefix   = "factrev"
	entityRevisionIDPrefix = "entityrev"
)

// revisionOrder recovers the numeric revision order from a generated revision
// ID. Generated revision IDs embed the engine's monotonically increasing
// sequence ("factrev_000000042"), so the numeric suffix is the revision's true
// order. Comparing the raw ID text instead flips the order at digit
// boundaries: "factrev_999999" sorts after "factrev_1000000", which would let
// a boundary-crossing historical read pick the older representation.
//
// Revisions this engine did not generate carry no recovered order and report
// zero, i.e. they sort before any generated revision sharing their transaction
// time. Migration seeds (IDs like "seed:<fact id>:current") describe state
// older than any generated revision, and imported revisions have no engine
// sequence to recover.
func revisionOrder(id string) uint64 {
	var suffix string
	switch {
	case strings.HasPrefix(id, factRevisionIDPrefix+"_"):
		suffix = id[len(factRevisionIDPrefix)+1:]
	case strings.HasPrefix(id, entityRevisionIDPrefix+"_"):
		suffix = id[len(entityRevisionIDPrefix)+1:]
	default:
		return 0
	}
	order, err := strconv.ParseUint(suffix, 10, 64)
	if err != nil {
		return 0
	}
	return order
}

func (e *engine) idExistsLocked(id string) bool {
	if _, ok := e.sources[id]; ok {
		return true
	}
	if _, ok := e.episodes[id]; ok {
		return true
	}
	if _, ok := e.entities[id]; ok {
		return true
	}
	if _, ok := e.facts[id]; ok {
		return true
	}
	if _, ok := e.factRevisions[id]; ok {
		return true
	}
	if _, ok := e.entityRevisions[id]; ok {
		return true
	}
	if _, ok := e.migrationWatermarks[id]; ok {
		return true
	}
	return false
}

// ensureGlobalIDAvailableLocked rejects an ID that is already owned by a
// different record kind. Generated IDs are already checked for global
// uniqueness by newIDLocked, but explicit IDs (and derived source IDs) were
// only checked within their own kind, so an episode and an entity could both be
// named "shared" and neighborhood/provenance lookups, which resolve nodes by
// raw ID, would collapse or mistype them. ownKind is the kind being written;
// an existing record of the same kind is left to the caller's own replay or
// conflict handling.
func (e *engine) ensureGlobalIDAvailableLocked(id, ownKind string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	conflicts := e.conflictingIDKindsLocked(id, ownKind)
	if len(conflicts) == 0 {
		return nil
	}
	return errorf(ErrInputInvalid, fmt.Sprintf("id %q already exists as %s", id, strings.Join(conflicts, ", ")), map[string]any{
		"id":                id,
		"kind":              ownKind,
		"conflicting_kinds": conflicts,
	}, nil)
}

// conflictingIDKindsLocked reports every record kind other than excludeKind
// that already stores id. The result is deduplicated and stable-ordered.
func (e *engine) conflictingIDKindsLocked(id, excludeKind string) []string {
	owners := []struct {
		kind   string
		exists bool
	}{
		{kindSource, hasKey(e.sources, id)},
		{kindEpisode, hasKey(e.episodes, id)},
		{kindEntity, hasKey(e.entities, id)},
		{kindFact, hasKey(e.facts, id)},
		{kindFactRevision, hasKey(e.factRevisions, id)},
		{kindEntityRevision, hasKey(e.entityRevisions, id)},
		{kindMigrationWatermark, hasKey(e.migrationWatermarks, id)},
	}
	conflicts := make([]string, 0, len(owners))
	for _, owner := range owners {
		if owner.exists && owner.kind != excludeKind && !slices.Contains(conflicts, owner.kind) {
			conflicts = append(conflicts, owner.kind)
		}
	}
	return conflicts
}

func hasKey[V any](records map[string]V, id string) bool {
	_, ok := records[id]
	return ok
}

// validateGlobalRecordIDs fails closed when persisted state stores the same raw
// record id under more than one kind.
//
// Write acceptance points keep newly written state unique, but databases
// produced by affected releases can already hold a collision. The lattice
// loader only rejects duplicates keyed by label plus id, so the same id under
// two labels loads successfully, and Neighborhood indexes nodes by raw id, so
// such a pair keeps overwriting or mistyping nodes after an upgrade. Opening
// must therefore reject the database instead of serving it silently corrupt.
// Nothing is rewritten: the operator gets the colliding id and the kinds that
// claim it, and can repair the database deliberately.
//
// colliding_ids is capped so a badly damaged database cannot turn the error
// into an unbounded payload.
func validateGlobalRecordIDs(state persistedState) error {
	groups := []struct {
		kind string
		ids  []string
	}{
		{kindSource, sortedRecordIDs(state.Sources)},
		{kindEpisode, sortedRecordIDs(state.Episodes)},
		{kindEntity, sortedRecordIDs(state.Entities)},
		{kindFact, sortedRecordIDs(state.Facts)},
		{kindFactRevision, sortedRecordIDs(state.FactRevisions)},
		{kindEntityRevision, sortedRecordIDs(state.EntityRevisions)},
		{kindMigrationWatermark, sortedRecordIDs(state.MigrationWatermarks)},
	}
	owners := make(map[string][]string)
	for _, group := range groups {
		for _, id := range group.ids {
			if strings.TrimSpace(id) == "" || slices.Contains(owners[id], group.kind) {
				continue
			}
			owners[id] = append(owners[id], group.kind)
		}
	}
	collisions := make([]string, 0)
	for id, kinds := range owners {
		if len(kinds) > 1 {
			collisions = append(collisions, id)
		}
	}
	if len(collisions) == 0 {
		return nil
	}
	slices.Sort(collisions)
	collidingID := collisions[0]
	collidingKinds := owners[collidingID]
	return errorf(ErrStorageFailed, fmt.Sprintf("database record id %q is shared by multiple record kinds: %s", collidingID, strings.Join(collidingKinds, ", ")), map[string]any{
		"id":            collidingID,
		"kinds":         collidingKinds,
		"colliding_ids": capStrings(collisions, 16),
	}, nil)
}

func sortedRecordIDs[V any](records map[string]V) []string {
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func capStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

// compositeID builds an unambiguous composite identifier from a prefix and
// ordered parts. Plain delimiter concatenation is ambiguous: a fact "f"
// supporting episode "e1:e2" and a fact "f:e1" supporting episode "e2" both
// produced "edge:f:e1:e2", so their edges aliased. Each part is length-prefixed
// as "<len>:<part>", making the encoding self-delimiting: a decoder reads digits
// up to the first ":", then exactly that many bytes, so no ID value can forge a
// different part split. The prefix is a fixed literal and never caller data.
//
// Callers building relationship identities must pass the relationship type as
// the first part and the endpoints after it, as in
// compositeID("edge", "SUBJECT", factID, subjectID). Fact and episode IDs alone
// are not a unique identity: a fact "f" supported by an episode whose explicit
// ID is literally "subject" yields the SUBJECT edge f -> subject and the ASSERTS
// edge subject -> f, and without the type part both encode to "edge:1:f:7:subject".
// Edges are stored by ID, so the later one would silently overwrite the earlier
// and a valid graph would lose an edge.
func compositeID(prefix string, parts ...string) string {
	var builder strings.Builder
	builder.WriteString(prefix)
	for _, part := range parts {
		builder.WriteByte(':')
		builder.WriteString(strconv.Itoa(len(part)))
		builder.WriteByte(':')
		builder.WriteString(part)
	}
	return builder.String()
}
