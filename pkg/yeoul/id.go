package yeoul

import (
	"fmt"
	"strings"
	"sync/atomic"
)

func normalizeEntityID(namespace, entityType, canonical string) string {
	return EntityID(namespace, entityType, canonical)
}

func normalizeSourceID(spaceID, kind, externalRef string) string {
	parts := []string{"src", normalizeIDPart(spaceID), normalizeIDPart(kind), normalizeIDPart(externalRef)}
	return strings.Trim(strings.Join(parts, ":"), ":")
}

func normalizeLegacySourceID(kind, externalRef string) string {
	parts := []string{"src", normalizeIDPart(kind), normalizeIDPart(externalRef)}
	return strings.Trim(strings.Join(parts, ":"), ":")
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

func (e *engine) newIDLocked(prefix string) string {
	for {
		next := atomic.AddUint64(&e.sequence, 1)
		id := fmt.Sprintf("%s_%06d", prefix, next)
		if !e.idExistsLocked(id) {
			return id
		}
	}
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
