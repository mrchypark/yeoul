package yeoul

import (
	"reflect"
	"slices"
	"strings"
)

// cloneVisitKey identifies a pointer-backed container that is already being
// cloned, so metadata which references itself (directly or through a chain of
// maps, slices, pointers, and interfaces) terminates instead of recursing
// forever. The length and capacity are part of the key so two different slice
// views that happen to share a backing array are not collapsed into one clone.
type cloneVisitKey struct {
	kind reflect.Kind
	typ  reflect.Type
	ptr  uintptr
	len  int
	cap  int
}

// cloneVisitKeyOf reports the visit key for a pointer-backed kind. Scalars and
// nil containers cannot form a cycle and are not tracked.
func cloneVisitKeyOf(rv reflect.Value) (cloneVisitKey, bool) {
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map:
		if rv.IsNil() {
			return cloneVisitKey{}, false
		}
		return cloneVisitKey{kind: rv.Kind(), typ: rv.Type(), ptr: rv.Pointer()}, true
	case reflect.Slice:
		if rv.IsNil() {
			return cloneVisitKey{}, false
		}
		return cloneVisitKey{kind: rv.Kind(), typ: rv.Type(), ptr: rv.Pointer(), len: rv.Len(), cap: rv.Cap()}, true
	default:
		return cloneVisitKey{}, false
	}
}

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
	return cloneAnyMapValue(src, make(map[cloneVisitKey]reflect.Value))
}

func cloneAny(value any) any {
	return cloneAnyValue(value, make(map[cloneVisitKey]reflect.Value))
}

func cloneAnyValue(value any, visited map[cloneVisitKey]reflect.Value) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return cloneAnyMapValue(typed, visited)
	case []any:
		return cloneAnySliceValue(typed, visited)
	case []string:
		return slices.Clone(typed)
	default:
		return cloneReflect(value, visited)
	}
}

func cloneAnyMapValue(src map[string]any, visited map[cloneVisitKey]reflect.Value) map[string]any {
	if len(src) == 0 {
		return nil
	}
	key, cacheable := cloneVisitKeyOf(reflect.ValueOf(src))
	if cacheable {
		if cached, ok := visited[key]; ok {
			if out, ok := cached.Interface().(map[string]any); ok {
				return out
			}
		}
	}
	out := make(map[string]any, len(src))
	if cacheable {
		visited[key] = reflect.ValueOf(out)
	}
	for k, v := range src {
		out[k] = cloneAnyValue(v, visited)
	}
	return out
}

func cloneAnySliceValue(src []any, visited map[cloneVisitKey]reflect.Value) []any {
	out := make([]any, len(src))
	key, cacheable := cloneVisitKeyOf(reflect.ValueOf(src))
	if cacheable {
		if cached, ok := visited[key]; ok {
			if prior, ok := cached.Interface().([]any); ok {
				return prior
			}
		}
		visited[key] = reflect.ValueOf(out)
	}
	for i := range src {
		out[i] = cloneAnyValue(src[i], visited)
	}
	return out
}

// cloneReflect deep-copies a JSON-compatible value that is not one of the
// concrete container shapes handled above.
func cloneReflect(value any, visited map[cloneVisitKey]reflect.Value) any {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return value
	}
	cloned := cloneReflectValue(rv, visited)
	if !cloned.IsValid() {
		return value
	}
	return cloned.Interface()
}

// cloneReflectValue deep-copies a JSON-compatible container value that is typed
// more specifically than map[string]any/[]any/[]string. The public API accepts
// map[string]any metadata, so callers can place any JSON-compatible Go value
// inside it: map[string]string, []int, []map[string]any, map[string]int,
// arrays, structs holding those containers, pointers to them, and nested
// combinations of all of those. Without this, such values stayed shared between
// caller input and engine state, so mutating caller memory after an ingest
// silently mutated engine state without a lock, revision, or persistence write.
//
// Interfaces and pointers are followed, maps, slices, and arrays are rebuilt
// recursively, and structs are copied in two steps: the whole struct is copied
// first so unexported fields survive, then every field reflection can set is
// deep-cloned. Unexported fields keep their copied value, which matches the
// JSON-compatible metadata contract because they are never serialized anyway.
// Values are copied directly rather than via a JSON round-trip so numeric types
// and precision are preserved exactly. Map keys are reused as-is: keys are
// comparable values whose identity must be preserved for lookups.
func cloneReflectValue(rv reflect.Value, visited map[cloneVisitKey]reflect.Value) reflect.Value {
	switch rv.Kind() {
	case reflect.Interface:
		if rv.IsNil() {
			return rv
		}
		out := reflect.New(rv.Type()).Elem()
		out.Set(cloneReflectValue(rv.Elem(), visited))
		return out
	case reflect.Pointer:
		if rv.IsNil() {
			return rv
		}
		key, cacheable := cloneVisitKeyOf(rv)
		if cacheable {
			if cached, ok := visited[key]; ok {
				return cached
			}
		}
		out := reflect.New(rv.Type().Elem())
		if cacheable {
			visited[key] = out
		}
		out.Elem().Set(cloneReflectValue(rv.Elem(), visited))
		return out
	case reflect.Map:
		if rv.IsNil() {
			return rv
		}
		key, cacheable := cloneVisitKeyOf(rv)
		if cacheable {
			if cached, ok := visited[key]; ok {
				return cached
			}
		}
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		if cacheable {
			visited[key] = out
		}
		iter := rv.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cloneReflectValue(iter.Value(), visited))
		}
		return out
	case reflect.Slice:
		if rv.IsNil() {
			return rv
		}
		key, cacheable := cloneVisitKeyOf(rv)
		if cacheable {
			if cached, ok := visited[key]; ok {
				return cached
			}
		}
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		if cacheable {
			visited[key] = out
		}
		for i := 0; i < rv.Len(); i++ {
			out.Index(i).Set(cloneReflectValue(rv.Index(i), visited))
		}
		return out
	case reflect.Array:
		out := reflect.New(rv.Type()).Elem()
		for i := 0; i < rv.Len(); i++ {
			out.Index(i).Set(cloneReflectValue(rv.Index(i), visited))
		}
		return out
	case reflect.Struct:
		out := reflect.New(rv.Type()).Elem()
		out.Set(rv)
		for i := 0; i < rv.NumField(); i++ {
			if !out.Field(i).CanSet() {
				continue
			}
			out.Field(i).Set(cloneReflectValue(rv.Field(i), visited))
		}
		return out
	default:
		return rv
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
