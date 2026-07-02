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

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
