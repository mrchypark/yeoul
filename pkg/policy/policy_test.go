package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPackAndValidatePack(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
	writeFile(t, filepath.Join(dir, "agent_instructions.md"), "Use Yeoul carefully.\n")
	writeFile(t, filepath.Join(dir, "ontology.yaml"), `
version: 1
entity_types:
  - Project
predicates:
  - USES_STORAGE_ENGINE
dedup:
  Project:
    keys: [canonical_name]
`)
	writeFile(t, filepath.Join(dir, "episode_rules.yaml"), `
version: 1
fact_promotion:
  promote_only:
    - confirmed durable claims
  candidates:
    - decisions
    - stable preferences
  require_supporting_episode: true
  clarification_required_when_missing:
    - subject
    - claim
  keep_episode_only:
    - exploratory context
promote_to_episode:
  - name: retain_decisions
    when:
      contains_any: ["decided"]
`)
	writeFile(t, filepath.Join(dir, "search_recipes.yaml"), `
version: 1
recipes:
  recent_context:
    strategy: hybrid
`)

	pack, err := LoadPack(dir)
	if err != nil {
		t.Fatalf("load pack: %v", err)
	}
	if got := pack.Ontology.Version; got != 1 {
		t.Fatalf("unexpected ontology version: %d", got)
	}
	if pack.EpisodeRules.FactPromotion == nil {
		t.Fatal("expected fact promotion policy")
	}
	if got := pack.EpisodeRules.FactPromotion.Candidates; !contains(got, "stable preferences") {
		t.Fatalf("expected stable preferences candidate, got %v", got)
	}

	result, err := ValidatePack(dir)
	if err != nil {
		t.Fatalf("validate pack: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid pack, got issues=%v warnings=%v", result.Issues, result.Warnings)
	}
}

func TestValidatePackReportsInvalidFields(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ontology.yaml"), "version: 2\n")
	writeFile(t, filepath.Join(dir, "episode_rules.yaml"), "version: 0\n")
	writeFile(t, filepath.Join(dir, "search_recipes.yaml"), "version: 0\nrecipes:\n  broken: {}\n")

	result, err := ValidatePack(dir)
	if err != nil {
		t.Fatalf("validate invalid pack: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid pack")
	}
	joined := strings.Join(result.Issues, "\n")
	for _, expected := range []string{
		"SKILL.md or agent_instructions.md",
		"ontology.yaml must declare version: 1",
		"episode_rules.yaml must declare version: 1",
		"search_recipes.yaml must declare version: 1",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected issue %q in %q", expected, joined)
		}
	}
}

func TestValidatePackReportsInvalidFactPromotion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
	writeFile(t, filepath.Join(dir, "ontology.yaml"), "version: 1\n")
	writeFile(t, filepath.Join(dir, "episode_rules.yaml"), `
version: 1
fact_promotion:
  promote_only: []
  candidates:
    - ""
  clarification_required_when_missing: []
  keep_episode_only:
    - exploratory context
`)
	writeFile(t, filepath.Join(dir, "search_recipes.yaml"), "version: 1\nrecipes: {}\n")

	result, err := ValidatePack(dir)
	if err != nil {
		t.Fatalf("validate invalid fact promotion: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid pack")
	}
	joined := strings.Join(result.Issues, "\n")
	for _, expected := range []string{
		"fact_promotion.promote_only must not be empty",
		"fact_promotion.candidates must not contain empty values",
		"fact_promotion.clarification_required_when_missing must not be empty",
		"fact_promotion.require_supporting_episode must be true",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected issue %q in %q", expected, joined)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func recipeFilter(key string, value any) SearchRecipe {
	return SearchRecipe{Strategy: "hybrid", Filters: map[string]any{key: value}}
}

func recipeExpand(key string, value any) SearchRecipe {
	return SearchRecipe{Strategy: "neighborhood", Expand: map[string]any{key: value}}
}

// Validation must check each supported control's value shape, not only its
// name, so a recognized key with a malformed value cannot pass validation and
// then be silently skipped at execution.
func TestValidateSearchRecipeChecksControlValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		recipe SearchRecipe
		want   string
	}{
		{"window_days rejects non-numeric", recipeFilter("window_days", "bogus"), `invalid filter "window_days"`},
		{"window_days rejects object", recipeFilter("window_days", map[string]any{}), `invalid filter "window_days"`},
		{"window_days rejects list", recipeFilter("window_days", []any{}), `invalid filter "window_days"`},
		{"window_days rejects null", recipeFilter("window_days", nil), `invalid filter "window_days"`},
		{"window_days rejects negative", recipeFilter("window_days", -3), `invalid filter "window_days"`},
		{"window_days rejects fractional", recipeFilter("window_days", 1.5), `invalid filter "window_days"`},
		{"window_days rejects boolean", recipeFilter("window_days", true), `invalid filter "window_days"`},
		{"fact_status rejects unknown status", recipeFilter("fact_status", "archived"), `invalid filter "fact_status"`},
		{"fact_status rejects object", recipeFilter("fact_status", map[string]any{}), `invalid filter "fact_status"`},
		{"fact_status rejects unknown list entry", recipeFilter("fact_status", []any{"active", "archived"}), `invalid filter "fact_status"`},
		{"predicate rejects empty value", recipeFilter("predicate", ""), `invalid filter "predicate"`},
		{"predicate rejects number", recipeFilter("predicate", 7), `invalid filter "predicate"`},
		{"predicate rejects empty list", recipeFilter("predicate", []any{}), `invalid filter "predicate"`},
		{"entity_types rejects object", recipeExpand("entity_types", map[string]any{}), `invalid expand setting "entity_types"`},
		{"entity_types rejects scalar", recipeExpand("entity_types", "Project"), `invalid expand setting "entity_types"`},
		{"entity_types rejects empty list", recipeExpand("entity_types", []any{}), `invalid expand setting "entity_types"`},
		{"entity_types rejects empty entries", recipeExpand("entity_types", []any{"Project", ""}), `invalid expand setting "entity_types"`},
		{"entity_types rejects non-string entries", recipeExpand("entity_types", []any{7}), `invalid expand setting "entity_types"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := ValidateSearchRecipe("recipe", tc.recipe)
			if len(issues) == 0 {
				t.Fatalf("expected malformed control value to be rejected, got no issues")
			}
			if joined := strings.Join(issues, "\n"); !strings.Contains(joined, tc.want) {
				t.Fatalf("expected issue containing %q in %q", tc.want, joined)
			}
		})
	}

	for _, tc := range []struct {
		name   string
		recipe SearchRecipe
	}{
		{"window_days integer", recipeFilter("window_days", 7)},
		{"window_days numeric string", recipeFilter("window_days", "7")},
		{"window_days zero", recipeFilter("window_days", 0)},
		{"fact_status scalar", recipeFilter("fact_status", "active")},
		{"fact_status list", recipeFilter("fact_status", []any{"active", "superseded"})},
		{"predicate scalar", recipeFilter("predicate", "SUPERSEDES")},
		{"predicate list", recipeFilter("predicate", []any{"SUPERSEDES", "CHANGED_TO"})},
		{"entity_types list", recipeExpand("entity_types", []any{"Project", "Task"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if issues := ValidateSearchRecipe("recipe", tc.recipe); len(issues) > 0 {
				t.Fatalf("expected well-formed recipe to validate, got %v", issues)
			}
		})
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
