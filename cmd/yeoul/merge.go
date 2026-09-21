package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

// entityIdentityKey is the exact identity used by automatic compaction. Keep
// this grouping conservative: drift belongs to explicit merge-preview and
// merge, never to destructive compaction.
type entityIdentityKey struct {
	SpaceID       string
	Namespace     string
	Type          string
	CanonicalName string
	StableKey     string
}

func entityIdentityOf(entity yeoul.EntityInput, stableKey string) entityIdentityKey {
	return entityIdentityKey{
		SpaceID:       entity.SpaceID,
		Namespace:     entity.Namespace,
		Type:          entity.Type,
		CanonicalName: entity.CanonicalName,
		StableKey:     stableKey,
	}
}

// partitionEntityDriftGroups splits one identity group into the subgroups that
// may be merged together. Two populated namespaces that differ after folding
// stay in separate subgroups, while a blank namespace joins the populated
// subgroup so an empty-vs-populated pair is reported as one drifted candidate.
func partitionEntityDriftGroups(entities []yeoul.EntityInput) [][]yeoul.EntityInput {
	buckets := make(map[string][]yeoul.EntityInput)
	blank := make([]yeoul.EntityInput, 0)
	for _, entity := range entities {
		folded := normalizeKey(entity.Namespace)
		if folded == "" {
			blank = append(blank, entity)
			continue
		}
		buckets[folded] = append(buckets[folded], entity)
	}
	if len(buckets) == 0 {
		if len(blank) == 0 {
			return nil
		}
		return [][]yeoul.EntityInput{blank}
	}
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	subgroups := make([][]yeoul.EntityInput, 0, len(keys))
	for _, key := range keys {
		subgroup := buckets[key]
		if len(buckets) == 1 {
			subgroup = append(append([]yeoul.EntityInput{}, blank...), subgroup...)
		}
		subgroups = append(subgroups, subgroup)
	}
	if len(buckets) > 1 && len(blank) > 0 {
		subgroups = append(subgroups, blank)
	}
	return subgroups
}

// entityStableKey returns the stored stable key and whether the entity is
// eligible for automatic merging at all. The key is used exactly as stored; a
// non-string legacy value cannot be compared exactly, so such entities are
// excluded instead of being treated as unkeyed.
func entityStableKey(metadata map[string]any) (string, bool) {
	if metadata == nil {
		return "", true
	}
	value, ok := metadata["stable_key"]
	if !ok {
		return "", true
	}
	text, isString := value.(string)
	if !isString {
		return "", false
	}
	if strings.TrimSpace(text) == "" {
		return "", true
	}
	return text, true
}

func buildEntityMergeCandidates(payload *exportFile) []entityMergeCandidate {
	groups := make(map[entityIdentityKey][]yeoul.EntityInput)
	for _, entity := range payload.Entities {
		if duplicateOf(entity.Metadata) != "" {
			continue
		}
		stableKey, eligible := entityStableKey(entity.Metadata)
		if !eligible {
			continue
		}
		key := entityIdentityOf(entity, stableKey)
		groups[key] = append(groups[key], entity)
	}
	candidates := make([]entityMergeCandidate, 0)
	for _, entities := range groups {
		if len(entities) < 2 {
			continue
		}
		if len(entities) < 2 {
			continue
		}
		sort.Slice(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })
		sourceIDs := make([]string, 0, len(entities)-1)
		for _, entity := range entities[1:] {
			sourceIDs = append(sourceIDs, entity.ID)
		}
		candidates = append(candidates, entityMergeCandidate{
			TargetID:      entities[0].ID,
			SourceIDs:     sourceIDs,
			Namespace:     entities[0].Namespace,
			Type:          entities[0].Type,
			CanonicalName: entities[0].CanonicalName,
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].TargetID < candidates[j].TargetID })
	return candidates
}

// entityDriftGroupKey groups entities for the user-facing drift preview. The
// stable key is deliberately absent: a keyed entity and its unkeyed drift form
// must land in the same group even though the strict compaction key would keep
// them apart. The type and canonical name are folded so a case-only difference
// groups together.
type entityDriftGroupKey struct {
	SpaceID       string
	Type          string
	CanonicalName string
}

// buildEntityDriftCandidates reports the identity drift that the strict admin
// compaction path (buildEntityMergeCandidates) cannot act on: a keyed entity
// paired with its unkeyed drift form, and a stable-key conflict that still
// shares a display identity. Admin compaction keeps its strict grouping; only
// entity merge-preview reads this wider view.
func buildEntityDriftCandidates(payload *exportFile) []entityMergeCandidate {
	groups := make(map[entityDriftGroupKey][]yeoul.EntityInput)
	for _, entity := range payload.Entities {
		if duplicateOf(entity.Metadata) != "" {
			continue
		}
		if _, eligible := entityStableKey(entity.Metadata); !eligible {
			continue
		}
		key := entityDriftGroupKey{
			SpaceID:       entity.SpaceID,
			Type:          normalizeKey(entity.Type),
			CanonicalName: normalizeKey(entity.CanonicalName),
		}
		groups[key] = append(groups[key], entity)
	}
	candidates := make([]entityMergeCandidate, 0)
	for _, entities := range groups {
		for _, subgroup := range partitionEntityDriftGroups(entities) {
			candidates = append(candidates, entityDriftCandidatesForSubgroup(subgroup)...)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].TargetID < candidates[j].TargetID })
	return candidates
}

// entityDriftCandidatesForSubgroup buckets one namespace subgroup by exact
// stable key and pairs a lone keyed bucket with the unkeyed bucket. Two or more
// distinct keys stay apart, because a conflicting strong key proves distinct
// identity. Buckets with fewer than two entities are dropped.
func entityDriftCandidatesForSubgroup(subgroup []yeoul.EntityInput) []entityMergeCandidate {
	keyed := make(map[string][]yeoul.EntityInput)
	unkeyed := make([]yeoul.EntityInput, 0, len(subgroup))
	for _, entity := range subgroup {
		key, _ := entityStableKey(entity.Metadata)
		if key == "" {
			unkeyed = append(unkeyed, entity)
			continue
		}
		keyed[key] = append(keyed[key], entity)
	}
	keys := make([]string, 0, len(keyed))
	for key := range keyed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	buckets := make([][]yeoul.EntityInput, 0, len(keys)+1)
	if len(keys) == 1 {
		// Exactly one keyed identity, so its unkeyed drift form joins it in one
		// candidate rather than standing alone.
		buckets = append(buckets, append(append([]yeoul.EntityInput{}, keyed[keys[0]]...), unkeyed...))
	} else {
		for _, key := range keys {
			buckets = append(buckets, keyed[key])
		}
		buckets = append(buckets, unkeyed)
	}

	candidates := make([]entityMergeCandidate, 0, len(buckets))
	for _, bucket := range buckets {
		if len(bucket) < 2 {
			continue
		}
		sort.Slice(bucket, func(i, j int) bool { return bucket[i].ID < bucket[j].ID })
		target := bucket[0]
		sourceIDs := make([]string, 0, len(bucket)-1)
		driftNamespace := false
		driftType := false
		driftName := false
		for _, entity := range bucket[1:] {
			sourceIDs = append(sourceIDs, entity.ID)
			if entity.Namespace != target.Namespace {
				driftNamespace = true
			}
			if entity.Type != target.Type {
				driftType = true
			}
			if entity.CanonicalName != target.CanonicalName {
				driftName = true
			}
		}
		candidates = append(candidates, entityMergeCandidate{
			TargetID:       target.ID,
			SourceIDs:      sourceIDs,
			Namespace:      target.Namespace,
			Type:           target.Type,
			CanonicalName:  target.CanonicalName,
			DriftNamespace: driftNamespace,
			DriftType:      driftType,
			DriftName:      driftName,
		})
	}
	return candidates
}

// factIdentityKey is a comparable identity for duplicate-fact detection.
// Fields are compared exactly (no case folding), and supporting episode IDs are
// length-prefixed so values containing separators cannot collide.
type factIdentityKey struct {
	SpaceID    string
	Predicate  string
	SubjectID  string
	ObjectID   string
	ValueText  string
	Supporting string
	ValidFrom  string
	ValidTo    string
	Metadata   string
}

func factIdentityOf(fact yeoul.FactInput) factIdentityKey {
	return factIdentityKey{
		SpaceID:    fact.SpaceID,
		Predicate:  fact.Predicate,
		SubjectID:  fact.SubjectID,
		ObjectID:   fact.ObjectID,
		ValueText:  fact.ValueText,
		Supporting: encodeIdentityParts(sortedStrings(fact.SupportingEpisodeIDs)),
		ValidFrom:  fact.ValidFrom.UTC().Format(time.RFC3339Nano),
		ValidTo:    fact.ValidTo.UTC().Format(time.RFC3339Nano),
		Metadata:   canonicalMetadata(fact.Metadata),
	}
}

// canonicalMetadata renders metadata structurally so that values which differ
// in type or in key/value boundaries cannot share an identity. Plain map
// formatting collapses {"x":"1"} with {"x":1} and {"a":"b c:d"} with
// {"a":"b","c":"d"}.
func canonicalMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		parts = append(parts, key, canonicalValue(metadata[key]))
	}
	return "map[" + encodeIdentityParts(parts) + "]"
}

func canonicalValue(value any) string {
	if value == nil {
		return "nil"
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Map:
		keys := make([]string, 0, rv.Len())
		byKey := make(map[string]string, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := fmt.Sprintf("%v", iter.Key().Interface())
			keys = append(keys, key)
			byKey[key] = canonicalValue(iter.Value().Interface())
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys)*2)
		for _, key := range keys {
			parts = append(parts, key, byKey[key])
		}
		return "map[" + encodeIdentityParts(parts) + "]"
	case reflect.Slice, reflect.Array:
		parts := make([]string, 0, rv.Len())
		for index := 0; index < rv.Len(); index++ {
			parts = append(parts, canonicalValue(rv.Index(index).Interface()))
		}
		return "list[" + encodeIdentityParts(parts) + "]"
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return "nil"
		}
		return canonicalValue(rv.Elem().Interface())
	default:
		return fmt.Sprintf("%T:%v", value, value)
	}
}

func encodeIdentityParts(parts []string) string {
	var builder strings.Builder
	for _, part := range parts {
		fmt.Fprintf(&builder, "%d:%s|", len(part), part)
	}
	return builder.String()
}

// preferFactSurvivor returns the index of the duplicate that should survive
// compaction: the highest confidence, then the most recent observation, then
// the lowest ID. Only active facts reach this point, so a superseded or
// retracted fact can never displace the active successor.
func preferFactSurvivor(facts []yeoul.FactInput) int {
	best := 0
	for index := 1; index < len(facts); index++ {
		if factSurvivorBefore(facts[index], facts[best]) {
			best = index
		}
	}
	return best
}

func factSurvivorBefore(left, right yeoul.FactInput) bool {
	if left.Confidence != right.Confidence {
		return left.Confidence > right.Confidence
	}
	if !left.ObservedAt.Equal(right.ObservedAt) {
		return left.ObservedAt.After(right.ObservedAt)
	}
	return left.ID < right.ID
}

func buildFactDuplicateCandidates(payload *exportFile) []factDuplicateCandidate {
	groups := make(map[factIdentityKey][]yeoul.FactInput)
	for _, fact := range payload.Facts {
		status := strings.TrimSpace(fact.Status)
		if status != "" && !strings.EqualFold(status, "active") {
			continue
		}
		key := factIdentityOf(fact)
		groups[key] = append(groups[key], fact)
	}
	candidates := make([]factDuplicateCandidate, 0)
	for _, facts := range groups {
		if len(facts) < 2 {
			continue
		}
		sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
		targetIndex := preferFactSurvivor(facts)
		target := facts[targetIndex]
		sourceIDs := make([]string, 0, len(facts)-1)
		for index, fact := range facts {
			if index == targetIndex {
				continue
			}
			sourceIDs = append(sourceIDs, fact.ID)
		}
		candidates = append(candidates, factDuplicateCandidate{
			TargetID:  target.ID,
			SourceIDs: sourceIDs,
			Predicate: target.Predicate,
			SubjectID: target.SubjectID,
			ObjectID:  target.ObjectID,
			ValueText: target.ValueText,
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].TargetID < candidates[j].TargetID })
	return candidates
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func duplicateOf(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata["duplicate_of"]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

// entityNamesOverlap reports whether two entities agree on their display
// identity: the canonical name or any alias, compared case-insensitively
// because a case-only display-name difference is recorded drift. A conflicting
// stable key still proves distinct identity even when the names agree, so two
// populated-but-different keys never overlap; an equal key confirms the
// overlap.
func entityNamesOverlap(left, right *yeoul.Entity) bool {
	leftKey := yeoulStableKey(left.Metadata)
	rightKey := yeoulStableKey(right.Metadata)
	if leftKey != "" && rightKey != "" {
		return leftKey == rightKey
	}
	names := make(map[string]struct{}, len(left.Aliases)+1)
	names[normalizeKey(left.CanonicalName)] = struct{}{}
	for _, alias := range left.Aliases {
		names[normalizeKey(alias)] = struct{}{}
	}
	if _, ok := names[normalizeKey(right.CanonicalName)]; ok {
		return true
	}
	for _, alias := range right.Aliases {
		if _, ok := names[normalizeKey(alias)]; ok {
			return true
		}
	}
	return false
}

// yeoulStableKey reads the stored stable key exactly as written. A non-string
// legacy value carries no comparable identity, so it reads as blank.
func yeoulStableKey(metadata map[string]any) string {
	value, ok := metadata["stable_key"].(string)
	if !ok {
		return ""
	}
	return value
}

// mergeDriftSummary renders the drift a target absorbed from its sources for
// one field, as "target value -> source value". A single drift is a string and
// several are a string list, so the summary stays readable in JSON output. It
// returns nil when the field did not drift at all.
func mergeDriftSummary(target *yeoul.Entity, sources []*yeoul.Entity, field string) any {
	values := make([]string, 0, len(sources))
	for _, source := range sources {
		var targetValue, sourceValue string
		switch field {
		case "merge_drift_namespace":
			targetValue, sourceValue = target.Namespace, source.Namespace
		case "merge_drift_type":
			targetValue, sourceValue = target.Type, source.Type
		}
		if targetValue == sourceValue {
			continue
		}
		values = append(values, fmt.Sprintf("%s -> %s", targetValue, sourceValue))
	}
	switch len(values) {
	case 0:
		return nil
	case 1:
		return values[0]
	default:
		return values
	}
}

func mergeMaps(base, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func anyStrings(value any) []string {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...)
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return nil
	}
}

func mergeStringSlices(left, right []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(left)+len(right))
	for _, value := range append(append([]string(nil), left...), right...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
