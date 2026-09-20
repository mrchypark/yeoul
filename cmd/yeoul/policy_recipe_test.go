package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mrchypark/yeoul/pkg/policy"
	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func recipePack(recipes map[string]policy.SearchRecipe) *policy.Pack {
	return &policy.Pack{SearchRecipes: policy.SearchRecipes{Version: 1, Recipes: recipes}}
}

// Every shipped recipe control must change the executed request, and inert
// controls must be rejected instead of silently ignored.
func TestApplySearchRecipeAppliesSupportedControls(t *testing.T) {
	pack := recipePack(map[string]policy.SearchRecipe{
		"hybrid": {
			Strategy: "hybrid",
			Filters:  map[string]any{"fact_status": "active", "window_days": 30},
		},
		"neighborhood": {
			Strategy: "neighborhood",
			Filters:  map[string]any{"fact_status": "active"},
			Expand:   map[string]any{"entity_types": []any{"Project", "Task"}},
		},
		"lookup": {
			Strategy: "predicate_subject_lookup",
			Filters:  map[string]any{"fact_status": "active", "predicate": []any{"SUPERSEDES", "CHANGED_TO"}},
		},
	})

	hybrid, err := applySearchRecipe(pack, "hybrid", yeoul.SearchRequest{})
	if err != nil {
		t.Fatalf("apply hybrid recipe: %v", err)
	}
	if !slices.Contains(hybrid.Scope.FactStatus, "active") {
		t.Fatalf("expected fact_status filter to reach the request, got %#v", hybrid.Scope.FactStatus)
	}
	if hybrid.Temporal.ObservedFrom == nil {
		t.Fatal("expected window_days to set a temporal lower bound")
	}
	if !hybrid.Include.RelatedEntities || !hybrid.Include.SupportingEpisodes {
		t.Fatalf("expected hybrid include flags, got %#v", hybrid.Include)
	}

	neighborhood, err := applySearchRecipe(pack, "neighborhood", yeoul.SearchRequest{})
	if err != nil {
		t.Fatalf("apply neighborhood recipe: %v", err)
	}
	if !neighborhood.Include.RelatedEntities || !neighborhood.Include.Provenance {
		t.Fatalf("expected neighborhood include flags, got %#v", neighborhood.Include)
	}
	for _, want := range []string{"Project", "Task"} {
		if !slices.Contains(neighborhood.Scope.EntityTypes, want) {
			t.Fatalf("expected entity type %q in %#v", want, neighborhood.Scope.EntityTypes)
		}
	}

	lookup, err := applySearchRecipe(pack, "lookup", yeoul.SearchRequest{})
	if err != nil {
		t.Fatalf("apply predicate_subject_lookup recipe: %v", err)
	}
	if len(lookup.Types) != 1 || lookup.Types[0] != "fact" {
		t.Fatalf("expected fact-only hit types, got %#v", lookup.Types)
	}
	if !slices.Contains(lookup.Predicates, "SUPERSEDES") || !slices.Contains(lookup.Predicates, "CHANGED_TO") {
		t.Fatalf("expected predicate filter to reach the request, got %#v", lookup.Predicates)
	}
}

// A recognized control whose value cannot be applied must fail execution
// instead of being silently skipped. Validation and execution must agree, so
// every one of these shapes is rejected by ValidateSearchRecipe as well.
func TestApplySearchRecipeRejectsMalformedControlValues(t *testing.T) {
	pack := recipePack(map[string]policy.SearchRecipe{
		"window_bogus":      {Strategy: "hybrid", Filters: map[string]any{"window_days": "bogus"}},
		"window_object":     {Strategy: "hybrid", Filters: map[string]any{"window_days": map[string]any{}}},
		"window_list":       {Strategy: "hybrid", Filters: map[string]any{"window_days": []any{}}},
		"window_negative":   {Strategy: "hybrid", Filters: map[string]any{"window_days": -5}},
		"window_fractional": {Strategy: "hybrid", Filters: map[string]any{"window_days": 1.5}},
		"status_unknown":    {Strategy: "hybrid", Filters: map[string]any{"fact_status": "archived"}},
		"status_object":     {Strategy: "hybrid", Filters: map[string]any{"fact_status": map[string]any{}}},
		"predicate_empty":   {Strategy: "hybrid", Filters: map[string]any{"predicate": ""}},
		"entity_object":     {Strategy: "neighborhood", Expand: map[string]any{"entity_types": map[string]any{}}},
		"entity_scalar":     {Strategy: "neighborhood", Expand: map[string]any{"entity_types": "Project"}},
		"entity_empty":      {Strategy: "neighborhood", Expand: map[string]any{"entity_types": []any{}}},
	})

	for name := range pack.SearchRecipes.Recipes {
		t.Run(name, func(t *testing.T) {
			req, err := applySearchRecipe(pack, name, yeoul.SearchRequest{})
			if err == nil {
				t.Fatalf("expected recipe %q to be rejected, got %#v", name, req)
			}
		})
	}
}

// Well-formed values must still validate and still change the executed
// request, so the stricter validation does not break supported recipes.
func TestApplySearchRecipeAppliesWellFormedControlValues(t *testing.T) {
	pack := recipePack(map[string]policy.SearchRecipe{
		"window": {Strategy: "hybrid", Filters: map[string]any{"window_days": 7}},
		"scoped": {Strategy: "neighborhood", Expand: map[string]any{"entity_types": []any{"Project", "Task"}}},
	})

	window, err := applySearchRecipe(pack, "window", yeoul.SearchRequest{})
	if err != nil {
		t.Fatalf("apply window recipe: %v", err)
	}
	if window.Temporal.ObservedFrom == nil {
		t.Fatal("expected window_days to set a temporal lower bound")
	}
	if got := time.Since(*window.Temporal.ObservedFrom); got < 6*24*time.Hour || got > 8*24*time.Hour {
		t.Fatalf("expected a 7 day window, got %s", got)
	}

	scoped, err := applySearchRecipe(pack, "scoped", yeoul.SearchRequest{})
	if err != nil {
		t.Fatalf("apply scoped recipe: %v", err)
	}
	for _, want := range []string{"Project", "Task"} {
		if !slices.Contains(scoped.Scope.EntityTypes, want) {
			t.Fatalf("expected entity type %q in %#v", want, scoped.Scope.EntityTypes)
		}
	}
}

func TestApplySearchRecipeRejectsInertControls(t *testing.T) {
	pack := recipePack(map[string]policy.SearchRecipe{
		"ranked":      {Strategy: "hybrid", Ranking: map[string]float64{"recency": 0.4}},
		"unknown":     {Strategy: "hybrid", Filters: map[string]any{"unsupported_filter": "x"}},
		"unsupported": {Strategy: "custom_planner"},
	})

	for _, name := range []string{"ranked", "unknown", "unsupported"} {
		t.Run(name, func(t *testing.T) {
			if _, err := applySearchRecipe(pack, name, yeoul.SearchRequest{}); err == nil {
				t.Fatalf("expected recipe %q to be rejected", name)
			}
		})
	}
}

// hops stays accepted as advisory metadata for backward compatibility: it
// must validate and run, but it must not change the executed request.
func TestApplySearchRecipeAcceptsAdvisoryHops(t *testing.T) {
	pack := recipePack(map[string]policy.SearchRecipe{
		"hopped": {
			Strategy: "neighborhood",
			Expand:   map[string]any{"entity_types": []any{"Project"}, "hops": 2},
		},
	})

	plain, err := applySearchRecipe(pack, "hopped", yeoul.SearchRequest{})
	if err != nil {
		t.Fatalf("apply hopped recipe: %v", err)
	}
	if !slices.Contains(plain.Scope.EntityTypes, "Project") {
		t.Fatalf("expected entity_types to reach the request, got %#v", plain.Scope.EntityTypes)
	}

	baseline := recipePack(map[string]policy.SearchRecipe{
		"hopped": {Strategy: "neighborhood", Expand: map[string]any{"entity_types": []any{"Project"}}},
	})
	without, err := applySearchRecipe(baseline, "hopped", yeoul.SearchRequest{})
	if err != nil {
		t.Fatalf("apply baseline recipe: %v", err)
	}
	if !reflect.DeepEqual(plain, without) {
		t.Fatalf("advisory hops changed the request: with=%#v without=%#v", plain, without)
	}
}

// The shipped agent pack must only advertise executable controls, and it must
// stay valid so policy validate continues to accept it.
func TestShippedSearchRecipesAreExecutable(t *testing.T) {
	pack, err := policy.LoadPack("../../agent-pack")
	if err != nil {
		t.Fatalf("load agent pack: %v", err)
	}
	if len(pack.SearchRecipes.Recipes) == 0 {
		t.Fatal("expected shipped search recipes")
	}
	for name := range pack.SearchRecipes.Recipes {
		if _, err := applySearchRecipe(pack, name, yeoul.SearchRequest{}); err != nil {
			t.Fatalf("shipped recipe %q is not executable: %v", name, err)
		}
	}
	result, err := policy.ValidatePack("../../agent-pack")
	if err != nil {
		t.Fatalf("validate agent pack: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected shipped pack to stay valid, issues=%v", result.Issues)
	}
}

func TestShippedRecipeControlsStayExecutable(t *testing.T) {
	pack, err := policy.LoadPack("../../agent-pack")
	if err != nil {
		t.Fatalf("load agent pack: %v", err)
	}
	recent, err := applySearchRecipe(pack, "recent_context", yeoul.SearchRequest{})
	if err != nil {
		t.Fatalf("apply recent_context: %v", err)
	}
	if !slices.Contains(recent.Scope.FactStatus, "active") || recent.Temporal.ObservedFrom == nil {
		t.Fatalf("expected recent_context filters to reach the request, got scope=%#v temporal=%#v", recent.Scope, recent.Temporal)
	}
	contradiction, err := applySearchRecipe(pack, "contradiction_check", yeoul.SearchRequest{})
	if err != nil {
		t.Fatalf("apply contradiction_check: %v", err)
	}
	if len(contradiction.Predicates) == 0 {
		t.Fatal("expected contradiction_check to restrict predicates")
	}
	if !slices.Contains(contradiction.Types, "fact") {
		t.Fatalf("expected contradiction_check to restrict hit types, got %#v", contradiction.Types)
	}
}

// Execution must consume the representation the validator accepted. A recipe
// that declares a list filter is validated as a list, so applying it must not
// collapse that list into a single scalar: a decoded YAML list would otherwise
// reach the request as "[active superseded]".
func TestApplySearchRecipeAppliesListFiltersAsLists(t *testing.T) {
	pack := recipePack(map[string]policy.SearchRecipe{
		"listed": {
			Strategy: "hybrid",
			Filters: map[string]any{
				"fact_status": []any{"active", "superseded"},
				"predicate":   []any{"SUPERSEDES", "CHANGED_TO"},
			},
		},
		"scalar": {
			Strategy: "hybrid",
			Filters:  map[string]any{"fact_status": "active, superseded"},
		},
	})

	if issues := policy.ValidateSearchRecipe("listed", pack.SearchRecipes.Recipes["listed"]); len(issues) > 0 {
		t.Fatalf("expected the list recipe to validate, issues=%v", issues)
	}
	applied, err := applySearchRecipe(pack, "listed", yeoul.SearchRequest{})
	if err != nil {
		t.Fatalf("apply list recipe: %v", err)
	}
	if !reflect.DeepEqual(applied.Scope.FactStatus, []string{"active", "superseded"}) {
		t.Fatalf("expected both declared statuses in the request, got %#v", applied.Scope.FactStatus)
	}
	// mergeStringSlices sorts and dedupes, so compare membership rather than order.
	if !reflect.DeepEqual(applied.Predicates, []string{"CHANGED_TO", "SUPERSEDES"}) {
		t.Fatalf("expected both declared predicates in the request, got %#v", applied.Predicates)
	}

	scalar, err := applySearchRecipe(pack, "scalar", yeoul.SearchRequest{})
	if err != nil {
		t.Fatalf("apply scalar recipe: %v", err)
	}
	if !reflect.DeepEqual(scalar.Scope.FactStatus, []string{"active", "superseded"}) {
		t.Fatalf("expected the comma separated scalar to split into both statuses, got %#v", scalar.Scope.FactStatus)
	}
}

// Unsupported knobs must also fail policy validation, not only recipe use.
func TestValidatePackRejectsInertRecipeControls(t *testing.T) {
	dir := t.TempDir()
	writePolicyFile(t, dir+"/SKILL.md", "# Skill\n")
	writePolicyFile(t, dir+"/ontology.yaml", "version: 1\n")
	writePolicyFile(t, dir+"/episode_rules.yaml", "version: 1\n")
	writePolicyFile(t, dir+"/search_recipes.yaml", "version: 1\nrecipes:\n  ranked:\n    strategy: hybrid\n    ranking:\n      recency: 0.4\n")

	result, err := policy.ValidatePack(dir)
	if err != nil {
		t.Fatalf("validate pack: %v", err)
	}
	if result.Valid {
		t.Fatal("expected inert recipe controls to invalidate the pack")
	}
	joined := strings.Join(result.Issues, "\n")
	for _, want := range []string{"ranking weights"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected issue containing %q in %q", want, joined)
		}
	}
}

// Advisory hops must not invalidate a pack: the shipped agent pack and the
// documented compatibility contract both rely on it staying accepted.
func TestValidatePackAcceptsAdvisoryHops(t *testing.T) {
	dir := t.TempDir()
	writePolicyFile(t, dir+"/SKILL.md", "# Skill\n")
	writePolicyFile(t, dir+"/ontology.yaml", "version: 1\n")
	writePolicyFile(t, dir+"/episode_rules.yaml", "version: 1\n")
	writePolicyFile(t, dir+"/search_recipes.yaml", "version: 1\nrecipes:\n  hopped:\n    strategy: neighborhood\n    expand:\n      entity_types: [Project]\n      hops: 2\n")

	result, err := policy.ValidatePack(dir)
	if err != nil {
		t.Fatalf("validate pack: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected advisory hops to stay valid, issues=%v", result.Issues)
	}
}
