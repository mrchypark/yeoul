package yeoul

import (
	"slices"
	"strings"
)

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
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
	return out
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = cloneAny(v)
	}
	return dst
}

func cloneAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneAnyMap(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = cloneAny(v[i])
		}
		return out
	case []string:
		return slices.Clone(v)
	default:
		return v
	}
}

func mergeAnyMap(base, incoming map[string]any) map[string]any {
	if len(base) == 0 && len(incoming) == 0 {
		return nil
	}
	out := cloneAnyMap(base)
	if out == nil {
		out = make(map[string]any, len(incoming))
	}
	for k, v := range incoming {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func cloneEpisode(src Episode) *Episode {
	dst := src
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneEntity(src Entity) *Entity {
	dst := src
	dst.Aliases = slices.Clone(src.Aliases)
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneFact(src Fact) *Fact {
	dst := src
	dst.SupportingEpisodeIDs = slices.Clone(src.SupportingEpisodeIDs)
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneFactRevision(src FactRevision) *FactRevision {
	dst := src
	dst.SupportingEpisodeIDs = slices.Clone(src.SupportingEpisodeIDs)
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneEntityRevision(src EntityRevision) *EntityRevision {
	dst := src
	dst.Aliases = slices.Clone(src.Aliases)
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneMigrationWatermark(src MigrationWatermark) *MigrationWatermark {
	dst := src
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}

func cloneSource(src Source) *Source {
	dst := src
	dst.Metadata = cloneAnyMap(src.Metadata)
	return &dst
}
