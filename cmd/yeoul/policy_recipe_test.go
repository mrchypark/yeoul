package main

import (
	"slices"
	"strings"
	"testing"

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

func TestApplySearchRecipeRejectsInertControls(t *testing.T) {
	pack := recipePack(map[string]policy.SearchRecipe{
		"ranked":      {Strategy: "hybrid", Ranking: map[string]float64{"recency": 0.4}},
		"hopped":      {Strategy: "neighborhood", Expand: map[string]any{"hops": 2}},
		"unknown":     {Strategy: "hybrid", Filters: map[string]any{"unsupported_filter": "x"}},
		"unsupported": {Strategy: "custom_planner"},
	})

	for _, name := range []string{"ranked", "hopped", "unknown", "unsupported"} {
		t.Run(name, func(t *testing.T) {
			if _, err := applySearchRecipe(pack, name, yeoul.SearchRequest{}); err == nil {
				t.Fatalf("expected recipe %q to be rejected", name)
			}
		})
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

// Unsupported knobs must also fail policy validation, not only recipe use.
func TestValidatePackRejectsInertRecipeControls(t *testing.T) {
	dir := t.TempDir()
	writePolicyFile(t, dir+"/SKILL.md", "# Skill\n")
	writePolicyFile(t, dir+"/ontology.yaml", "version: 1\n")
	writePolicyFile(t, dir+"/episode_rules.yaml", "version: 1\n")
	writePolicyFile(t, dir+"/search_recipes.yaml", "version: 1\nrecipes:\n  ranked:\n    strategy: hybrid\n    ranking:\n      recency: 0.4\n  hopped:\n    strategy: neighborhood\n    expand:\n      hops: 2\n")

	result, err := policy.ValidatePack(dir)
	if err != nil {
		t.Fatalf("validate pack: %v", err)
	}
	if result.Valid {
		t.Fatal("expected inert recipe controls to invalidate the pack")
	}
	joined := strings.Join(result.Issues, "\n")
	for _, want := range []string{"ranking weights", "unsupported expand setting"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected issue containing %q in %q", want, joined)
		}
	}
}
