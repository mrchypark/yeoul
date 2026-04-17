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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
