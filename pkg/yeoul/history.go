package yeoul

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

func entityMarkedDuplicate(entity Entity) bool {
	if len(entity.Metadata) == 0 {
		return false
	}
	value, ok := entity.Metadata["duplicate_of"]
	if !ok {
		return false
	}
	return strings.TrimSpace(fmt.Sprint(value)) != ""
}

// canonicalEntityIDLocked follows the current duplicate chain. Historical
// callers must use canonicalEntityIDAtLocked so a later merge is not visible
// before its transaction time.
func (e *engine) canonicalEntityIDLocked(id string) string {
	return e.canonicalEntityIDAtLocked(id, TemporalFilter{}, nil)
}

// canonicalEntityIDAtLocked resolves a duplicate chain in the entity version
// visible to filter. Any dangling, cross-space, invalid, cyclic, or bounded
// chain fails closed to the original raw ID; returning a partial endpoint could
// expose facts through an untrusted redirect.
func (e *engine) canonicalEntityIDAtLocked(id string, filter TemporalFilter, index *temporalIndex) string {
	original := id
	if strings.TrimSpace(id) == "" {
		return id
	}
	if index == nil {
		index = newTemporalIndex(e)
	}
	current := id
	visited := map[string]struct{}{current: {}}
	const maxHops = 64
	for hop := 0; hop < maxHops; hop++ {
		stored, ok := e.entities[current]
		if !ok {
			return original
		}
		entity := e.entityVersionAt(stored, filter, index)
		if entity == nil {
			return original
		}
		rawNext, marked := entity.Metadata["duplicate_of"]
		if !marked {
			return current
		}
		next, ok := rawNext.(string)
		if !ok || strings.TrimSpace(next) == "" || next == current {
			return original
		}
		targetStored, ok := e.entities[next]
		if !ok {
			return original
		}
		target := e.entityVersionAt(targetStored, filter, index)
		if target == nil || normalizeSpaceID(target.SpaceID) != normalizeSpaceID(entity.SpaceID) {
			return original
		}
		if _, seen := visited[next]; seen {
			return original
		}
		visited[next] = struct{}{}
		current = next
	}
	return original
}

// canonicalEntityIDsLocked maps every id through canonicalEntityIDLocked so a
// caller-supplied anchor set and a stored fact endpoint are compared in the
// same canonical namespace.
func (e *engine) canonicalEntityIDsLocked(ids []string) []string {
	return e.canonicalEntityIDsAtLocked(ids, TemporalFilter{}, nil)
}

func (e *engine) canonicalEntityIDsAtLocked(ids []string, filter TemporalFilter, index *temporalIndex) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, e.canonicalEntityIDAtLocked(id, filter, index))
	}
	return out
}

func factProvenanceMeta(fact Fact) map[string]any {
	meta := map[string]any{
		"status": fact.Status,
	}
	if !fact.ValidFrom.IsZero() {
		meta["valid_from"] = fact.ValidFrom
	}
	if !fact.ValidTo.IsZero() {
		meta["valid_to"] = fact.ValidTo
	}
	if !fact.RetractedAt.IsZero() {
		meta["retracted_at"] = fact.RetractedAt
	}
	if fact.RetractionReason != "" {
		meta["retraction_reason"] = fact.RetractionReason
	}
	if supersedes := metadataStringIDs(fact.Metadata["supersedes"]); len(supersedes) == 1 {
		meta["supersedes"] = supersedes[0]
	} else if len(supersedes) > 1 {
		meta["supersedes"] = supersedes
	}
	if supersededBy, _ := fact.Metadata["superseded_by"].(string); supersededBy != "" {
		meta["superseded_by"] = supersededBy
	}
	if reason, _ := fact.Metadata["supersede_reason"].(string); reason != "" {
		meta["supersede_reason"] = reason
	}
	return meta
}

func metadataStringIDs(value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{typed}
	case []string:
		return dedupeStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return dedupeStrings(out)
	default:
		return nil
	}
}

type entityHistorySnapshot struct {
	ChangedAt     time.Time
	SpaceID       string
	Namespace     string
	Type          string
	CanonicalName string
	Aliases       []string
	Metadata      map[string]any
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func entitySnapshotEntry(entity Entity, changedAt time.Time) map[string]any {
	return map[string]any{
		"changed_at":     changedAt.UTC().Format(time.RFC3339Nano),
		"space_id":       entity.SpaceID,
		"namespace":      entity.Namespace,
		"type":           entity.Type,
		"canonical_name": entity.CanonicalName,
		"aliases":        slices.Clone(entity.Aliases),
		"metadata":       metadataWithoutEntityHistory(entity.Metadata),
		"created_at":     entity.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":     entity.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func appendEntityHistory(metadata map[string]any, entry map[string]any) map[string]any {
	out := metadataWithoutEntityHistory(metadata)
	history := make([]any, 0, 1)
	if metadata != nil {
		if raw, ok := metadata[entityHistoryKey].([]any); ok {
			history = append(history, raw...)
		}
	}
	history = append(history, entry)
	if out == nil {
		out = make(map[string]any, 1)
	}
	out[entityHistoryKey] = history
	return out
}

func metadataWithoutEntityHistory(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := cloneAnyMap(metadata)
	delete(out, entityHistoryKey)
	if len(out) == 0 {
		return nil
	}
	return out
}

func entityHistorySnapshots(metadata map[string]any) []entityHistorySnapshot {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata[entityHistoryKey].([]any)
	if !ok {
		return nil
	}
	out := make([]entityHistorySnapshot, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, entityHistorySnapshot{
			ChangedAt:     parseMaybeTime(entry["changed_at"]),
			SpaceID:       fmt.Sprint(entry["space_id"]),
			Namespace:     fmt.Sprint(entry["namespace"]),
			Type:          fmt.Sprint(entry["type"]),
			CanonicalName: fmt.Sprint(entry["canonical_name"]),
			Aliases:       anyStringsFromMetadata(entry["aliases"]),
			Metadata:      anyMapFromMetadata(entry["metadata"]),
			CreatedAt:     parseMaybeTime(entry["created_at"]),
			UpdatedAt:     parseMaybeTime(entry["updated_at"]),
		})
	}
	return out
}

func (s entityHistorySnapshot) toEntity(id string) *Entity {
	return &Entity{
		ID:            id,
		SpaceID:       s.SpaceID,
		Namespace:     s.Namespace,
		Type:          s.Type,
		CanonicalName: s.CanonicalName,
		Aliases:       slices.Clone(s.Aliases),
		Metadata:      cloneAnyMap(s.Metadata),
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

func anyStringsFromMetadata(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func anyMapFromMetadata(value any) map[string]any {
	entry, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return cloneAnyMap(entry)
}
