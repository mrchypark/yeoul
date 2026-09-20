package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func buildEntityMergeCandidates(payload *exportFile) []entityMergeCandidate {
	groups := make(map[string][]yeoul.EntityInput)
	for _, entity := range payload.Entities {
		if duplicateOf(entity.Metadata) != "" {
			continue
		}
		key := strings.Join([]string{
			normalizeKey(entity.SpaceID),
			normalizeKey(entity.Namespace),
			normalizeKey(entity.Type),
			normalizeKey(entity.CanonicalName),
		}, "|")
		groups[key] = append(groups[key], entity)
	}
	candidates := make([]entityMergeCandidate, 0)
	for _, entities := range groups {
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
		SpaceID:    strings.TrimSpace(fact.SpaceID),
		Predicate:  strings.TrimSpace(fact.Predicate),
		SubjectID:  strings.TrimSpace(fact.SubjectID),
		ObjectID:   strings.TrimSpace(fact.ObjectID),
		ValueText:  strings.TrimSpace(fact.ValueText),
		Supporting: encodeIdentityParts(sortedStrings(fact.SupportingEpisodeIDs)),
		ValidFrom:  fact.ValidFrom.UTC().Format(time.RFC3339Nano),
		ValidTo:    fact.ValidTo.UTC().Format(time.RFC3339Nano),
		Metadata:   fmt.Sprintf("%v", fact.Metadata),
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

func intFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func stringSliceFromAny(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...), true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out, true
	default:
		return nil, false
	}
}
