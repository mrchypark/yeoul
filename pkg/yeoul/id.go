package yeoul

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
)

func normalizeSourceID(spaceID, kind, externalRef string) string {
	return "src:" + readableIdentitySlug(spaceID, kind, externalRef) + "~" + identityFingerprint(spaceID, kind, externalRef)
}

func legacyEntityID(namespace, entityType, canonical string) string {
	return readableIdentitySlug(namespace, entityType, canonical)
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
