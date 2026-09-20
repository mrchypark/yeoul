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

func TestEpisodeRuleAcceptsSubstringOnlyMatching(t *testing.T) {
	cases := []struct {
		name  string
		rules string
		valid bool
	}{
		{
			name:  "substring only",
			rules: "version: 1\ndrop:\n  - name: sub\n    when:\n      contains_substring: [\"ignore me\"]\n",
			valid: true,
		},
		{
			name:  "contains_any only",
			rules: "version: 1\ndrop:\n  - name: any\n    when:\n      contains_any: [\"ok\"]\n",
			valid: true,
		},
		{
			name:  "both lists",
			rules: "version: 1\ndrop:\n  - name: both\n    when:\n      contains_any: [\"ok\"]\n      contains_substring: [\"ignore me\"]\n",
			valid: true,
		},
		{
			name:  "neither list",
			rules: "version: 1\ndrop:\n  - name: none\n    when:\n      contains_any: []\n",
			valid: false,
		},
		{
			name:  "blank substring token",
			rules: "version: 1\ndrop:\n  - name: blank\n    when:\n      contains_substring: [\"\"]\n",
			valid: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
			writeFile(t, filepath.Join(dir, "ontology.yaml"), "version: 1\n")
			writeFile(t, filepath.Join(dir, "episode_rules.yaml"), tc.rules)
			writeFile(t, filepath.Join(dir, "search_recipes.yaml"), "version: 1\nrecipes: {}\n")

			result, err := ValidatePack(dir)
			if err != nil {
				t.Fatalf("validate pack: %v", err)
			}
			if result.Valid != tc.valid {
				t.Fatalf("expected valid=%v, got valid=%v issues=%v", tc.valid, result.Valid, result.Issues)
			}
		})
	}
}

func TestRecipeAcceptsAdvisoryHops(t *testing.T) {
	cases := []struct {
		name   string
		recipe string
		valid  bool
	}{
		{
			name:   "entity_types with advisory hops",
			recipe: "    strategy: neighborhood\n    expand:\n      entity_types: [Project]\n      hops: 2\n",
			valid:  true,
		},
		{
			name:   "hops alone",
			recipe: "    strategy: neighborhood\n    expand:\n      hops: 2\n",
			valid:  true,
		},
		{
			name:   "hops as word",
			recipe: "    strategy: neighborhood\n    expand:\n      hops: two\n",
			valid:  false,
		},
		{
			name:   "hops as fraction",
			recipe: "    strategy: neighborhood\n    expand:\n      hops: 1.5\n",
			valid:  false,
		},
		{
			name:   "hops negative",
			recipe: "    strategy: neighborhood\n    expand:\n      hops: -1\n",
			valid:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
			writeFile(t, filepath.Join(dir, "ontology.yaml"), "version: 1\n")
			writeFile(t, filepath.Join(dir, "episode_rules.yaml"), "version: 1\n")
			writeFile(t, filepath.Join(dir, "search_recipes.yaml"),
				"version: 1\nrecipes:\n  hops_recipe:\n"+tc.recipe)

			result, err := ValidatePack(dir)
			if err != nil {
				t.Fatalf("validate pack: %v", err)
			}
			if result.Valid != tc.valid {
				t.Fatalf("expected valid=%v, got valid=%v issues=%v", tc.valid, result.Valid, result.Issues)
			}

			// ValidateSearchRecipe must agree with ValidatePack so a recipe the
			// pack accepts is never rejected at search execution time.
			pack, err := LoadPack(dir)
			if err != nil {
				t.Fatalf("load pack: %v", err)
			}
			recipeIssues := ValidateSearchRecipe("hops_recipe", pack.SearchRecipes.Recipes["hops_recipe"])
			if (len(recipeIssues) == 0) != tc.valid {
				t.Fatalf("ValidateSearchRecipe disagreed with ValidatePack: issues=%v", recipeIssues)
			}
		})
	}
}

func TestRecipeExpandHopsIsAdvisoryOnly(t *testing.T) {
	recipe := SearchRecipe{
		Strategy: "neighborhood",
		Expand:   map[string]any{"entity_types": []any{"Project"}, "hops": 2},
	}
	if issues := ValidateSearchRecipe("advisory", recipe); len(issues) != 0 {
		t.Fatalf("expected advisory hops to validate, got %v", issues)
	}
	hops, ok, err := RecipeHops(recipe)
	if err != nil {
		t.Fatalf("recipe hops: %v", err)
	}
	if !ok || hops != 2 {
		t.Fatalf("expected hops=2 declared, got hops=%d declared=%v", hops, ok)
	}
	if _, ok, _ := RecipeHops(SearchRecipe{Expand: map[string]any{}}); ok {
		t.Fatal("expected hops to be undeclared when absent")
	}
}

func TestDecodeYAMLRejectsTrailingDocument(t *testing.T) {
	multi := "version: 1\nrecipes: {}\n---\nrecipes:\n  x:\n    stratgy: hybrid\n"
	var out map[string]any
	if err := decodeYAML([]byte(multi), &out); err == nil {
		t.Fatal("expected trailing YAML document to be rejected")
	}

	var single map[string]any
	if err := decodeYAML([]byte("version: 1\nrecipes: {}\n"), &single); err != nil {
		t.Fatalf("expected single document to decode: %v", err)
	}
}

func TestValidatePackRejectsTrailingDocument(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
	writeFile(t, filepath.Join(dir, "ontology.yaml"), "version: 1\n")
	writeFile(t, filepath.Join(dir, "episode_rules.yaml"), "version: 1\n")
	writeFile(t, filepath.Join(dir, "search_recipes.yaml"),
		"version: 1\nrecipes: {}\n---\nrecipes:\n  x:\n    stratgy: hybrid\n")

	if _, err := LoadPack(dir); err == nil {
		t.Fatal("expected LoadPack to reject a multi-document policy file")
	}
	result, err := ValidatePack(dir)
	if err != nil {
		t.Fatalf("validate pack: %v", err)
	}
	if result.Valid {
		t.Fatal("expected ValidatePack to reject a multi-document policy file")
	}
}

func TestValidatePackRejectsBlankEpisodeRuleTokens(t *testing.T) {
	cases := []struct {
		name   string
		rules  string
		issues []string
	}{
		{
			name:   "empty token",
			rules:  "version: 1\ndrop:\n  - name: blank\n    when:\n      contains_any: [\"\"]\n",
			issues: []string{"episode rule \"blank\" when.contains_any must not contain blank tokens"},
		},
		{
			name:   "whitespace token",
			rules:  "version: 1\ndrop:\n  - name: blank\n    when:\n      contains_any: [\" \"]\n",
			issues: []string{"episode rule \"blank\" when.contains_any must not contain blank tokens"},
		},
		{
			name:   "mixed valid and blank",
			rules:  "version: 1\npromote_to_episode:\n  - name: mixed\n    when:\n      contains_any: [\"decided\", \"  \"]\n",
			issues: []string{"episode rule \"mixed\" when.contains_any must not contain blank tokens"},
		},
		{
			name:   "blank substring token",
			rules:  "version: 1\ndrop:\n  - name: blank_sub\n    when:\n      contains_any: [\"ok\"]\n      contains_substring: [\"\"]\n",
			issues: []string{"episode rule \"blank_sub\" when.contains_substring must not contain blank tokens"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
			writeFile(t, filepath.Join(dir, "ontology.yaml"), "version: 1\n")
			writeFile(t, filepath.Join(dir, "episode_rules.yaml"), tc.rules)
			writeFile(t, filepath.Join(dir, "search_recipes.yaml"), "version: 1\nrecipes: {}\n")

			result, err := ValidatePack(dir)
			if err != nil {
				t.Fatalf("validate blank token pack: %v", err)
			}
			if result.Valid {
				t.Fatalf("expected invalid pack, got issues=%v", result.Issues)
			}
			joined := strings.Join(result.Issues, "\n")
			for _, expected := range tc.issues {
				if !strings.Contains(joined, expected) {
					t.Fatalf("expected issue %q in %q", expected, joined)
				}
			}
		})
	}
}

func TestLoadPackDropsBlankEpisodeRuleTokens(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
	writeFile(t, filepath.Join(dir, "ontology.yaml"), "version: 1\n")
	writeFile(t, filepath.Join(dir, "episode_rules.yaml"),
		"version: 1\ndrop:\n  - name: blank\n    when:\n      contains_any: [\" \", \"ok\"]\n")
	writeFile(t, filepath.Join(dir, "search_recipes.yaml"), "version: 1\nrecipes: {}\n")

	pack, err := LoadPack(dir)
	if err != nil {
		t.Fatalf("load pack: %v", err)
	}
	if len(pack.EpisodeRules.Drop) != 1 {
		t.Fatalf("expected one drop rule, got %d", len(pack.EpisodeRules.Drop))
	}
	tokens := pack.EpisodeRules.Drop[0].When.ContainsAny
	if contains(tokens, " ") {
		t.Fatalf("expected blank token to be dropped, got %v", tokens)
	}
	if !contains(tokens, "ok") {
		t.Fatalf("expected valid token to survive, got %v", tokens)
	}
}

func TestValidatePackRejectsUnknownAndUnsupportedRecipeFields(t *testing.T) {
	cases := []struct {
		name   string
		recipe string
		issues []string
	}{
		{
			name:   "misspelled recipe field",
			recipe: "version: 1\nrecipes:\n  recent_context:\n    strategy: hybrid\n    stratgy: hybrid\n",
			issues: []string{"stratgy"},
		},
		{
			name:   "unsupported strategy",
			recipe: "version: 1\nrecipes:\n  recent_context:\n    strategy: vector_only\n",
			issues: []string{`recipe "recent_context" declares unsupported strategy "vector_only"`},
		},
		{
			name:   "unsupported filter",
			recipe: "version: 1\nrecipes:\n  recent_context:\n    strategy: hybrid\n    filters:\n      confidence_min: 0.5\n",
			issues: []string{`recipe "recent_context" declares unsupported filter "confidence_min"`},
		},
		{
			name:   "wrong window_days type",
			recipe: "version: 1\nrecipes:\n  recent_context:\n    strategy: hybrid\n    filters:\n      window_days: soon\n",
			issues: []string{`recipe "recent_context" declares invalid filter "window_days"`},
		},
		{
			name:   "unsupported expand key",
			recipe: "version: 1\nrecipes:\n  recent_context:\n    strategy: hybrid\n    expand:\n      edges: [SUPERSEDES]\n",
			issues: []string{`recipe "recent_context" declares unsupported expand setting "edges"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
			writeFile(t, filepath.Join(dir, "ontology.yaml"), "version: 1\n")
			writeFile(t, filepath.Join(dir, "episode_rules.yaml"), "version: 1\n")
			writeFile(t, filepath.Join(dir, "search_recipes.yaml"), tc.recipe)

			result, err := ValidatePack(dir)
			if err != nil {
				t.Fatalf("validate recipe pack: %v", err)
			}
			if result.Valid {
				t.Fatalf("expected invalid pack, got issues=%v", result.Issues)
			}
			joined := strings.Join(result.Issues, "\n")
			for _, expected := range tc.issues {
				if !strings.Contains(joined, expected) {
					t.Fatalf("expected issue %q in %q", expected, joined)
				}
			}
		})
	}
}

func TestValidatePackRejectsMisspelledStructuralFields(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
	writeFile(t, filepath.Join(dir, "ontology.yaml"), "version: 1\n")
	writeFile(t, filepath.Join(dir, "episode_rules.yaml"),
		"version: 1\ndrop:\n  - name: low_signal\n    when:\n      contains_anyy: [\"ok\"]\n")
	writeFile(t, filepath.Join(dir, "search_recipes.yaml"), "version: 1\nrecipes: {}\n")

	result, err := ValidatePack(dir)
	if err != nil {
		t.Fatalf("validate misspelled pack: %v", err)
	}
	if result.Valid {
		t.Fatal("expected misspelled structural field to fail validation")
	}
	joined := strings.Join(result.Issues, "\n")
	if !strings.Contains(joined, "contains_anyy") {
		t.Fatalf("expected issue to name the unknown field, got %q", joined)
	}

	if _, err := LoadPack(dir); err == nil {
		t.Fatal("expected LoadPack to reject a misspelled structural field")
	}
}

func TestValidatePackAcceptsOntologyExtensions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
	writeFile(t, filepath.Join(dir, "ontology.yaml"),
		"version: 1\npredicates: [OWNS]\nextensions:\n  exclusive_predicates: [CURRENT_OWNER]\n")
	writeFile(t, filepath.Join(dir, "episode_rules.yaml"), "version: 1\n")
	writeFile(t, filepath.Join(dir, "search_recipes.yaml"), "version: 1\nrecipes: {}\n")

	result, err := ValidatePack(dir)
	if err != nil {
		t.Fatalf("validate ontology extension pack: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid pack, got issues=%v", result.Issues)
	}

	pack, err := LoadPack(dir)
	if err != nil {
		t.Fatalf("load ontology extension pack: %v", err)
	}
	if pack.Ontology.Extensions == nil {
		t.Fatal("expected ontology extensions to be preserved")
	}
}

// TestDocumentedOntologyExampleDecodes ties the accepted ontology reference to
// the implementation: the documented example must decode without unknown-field
// errors and keep its supported meaning, with advisory content confined to
// extensions.
func TestDocumentedOntologyExampleDecodes(t *testing.T) {
	docPath := filepath.Join("..", "..", "docs", "10-examples", "example-ontology.md")
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}
	yamlBlock := extractYAMLBlock(t, string(data))

	var ontology Ontology
	if err := decodeYAML([]byte(yamlBlock), &ontology); err != nil {
		t.Fatalf("decode documented ontology example: %v", err)
	}
	if ontology.Version != 1 {
		t.Fatalf("expected version 1, got %d", ontology.Version)
	}
	if !contains(ontology.EntityTypes, "Repository") {
		t.Fatalf("expected Repository entity type, got %v", ontology.EntityTypes)
	}
	if !contains(ontology.Predicates, "DEPENDS_ON") {
		t.Fatalf("expected DEPENDS_ON predicate, got %v", ontology.Predicates)
	}
	if len(ontology.Dedup["Repository"].Keys) == 0 {
		t.Fatalf("expected Repository dedup keys, got %v", ontology.Dedup)
	}
	if _, ok := ontology.Extensions["exclusive_predicates"]; !ok {
		t.Fatalf("expected advisory exclusive_predicates under extensions, got %v", ontology.Extensions)
	}
}

func extractYAMLBlock(t *testing.T, markdown string) string {
	t.Helper()
	lines := strings.Split(markdown, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "```yaml" {
			start = i + 1
			break
		}
	}
	if start == -1 {
		t.Fatal("documented example is missing a yaml block")
	}
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "```" {
			return strings.Join(lines[start:i], "\n")
		}
	}
	t.Fatal("documented example yaml block is not closed")
	return ""
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
		{"entity_types rejects the hybrid strategy", SearchRecipe{Strategy: "hybrid", Expand: map[string]any{"entity_types": []any{"Project"}}}, `expand setting "entity_types", which the "hybrid" strategy does not apply`},
		{"entity_types rejects the predicate_subject_lookup strategy", SearchRecipe{Strategy: "predicate_subject_lookup", Expand: map[string]any{"entity_types": []any{"Project"}}}, `expand setting "entity_types", which the "predicate_subject_lookup" strategy does not apply`},
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
