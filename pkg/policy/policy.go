package policy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Pack struct {
	Path              string        `json:"path"`
	Skill             string        `json:"skill,omitempty"`
	AgentInstructions string        `json:"agent_instructions,omitempty"`
	Ontology          Ontology      `json:"ontology,omitempty"`
	EpisodeRules      EpisodeRules  `json:"episode_rules,omitempty"`
	SearchRecipes     SearchRecipes `json:"search_recipes,omitempty"`
}

type ValidationResult struct {
	Path     string   `json:"path"`
	Valid    bool     `json:"valid"`
	Issues   []string `json:"issues,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type Ontology struct {
	Version     int                  `yaml:"version" json:"version"`
	EntityTypes []string             `yaml:"entity_types" json:"entity_types,omitempty"`
	Predicates  []string             `yaml:"predicates" json:"predicates,omitempty"`
	Dedup       map[string]DedupRule `yaml:"dedup" json:"dedup,omitempty"`
}

type DedupRule struct {
	Keys []string `yaml:"keys" json:"keys,omitempty"`
}

type EpisodeRules struct {
	Version          int           `yaml:"version" json:"version"`
	PromoteToEpisode []EpisodeRule `yaml:"promote_to_episode" json:"promote_to_episode,omitempty"`
	Drop             []EpisodeRule `yaml:"drop" json:"drop,omitempty"`
}

type EpisodeRule struct {
	Name     string   `yaml:"name" json:"name"`
	When     RuleWhen `yaml:"when" json:"when,omitempty"`
	Priority string   `yaml:"priority" json:"priority,omitempty"`
}

type RuleWhen struct {
	ContainsAny []string `yaml:"contains_any" json:"contains_any,omitempty"`
}

type SearchRecipes struct {
	Version int                     `yaml:"version" json:"version"`
	Recipes map[string]SearchRecipe `yaml:"recipes" json:"recipes,omitempty"`
}

type SearchRecipe struct {
	Description string             `yaml:"description" json:"description,omitempty"`
	Strategy    string             `yaml:"strategy" json:"strategy,omitempty"`
	Filters     map[string]any     `yaml:"filters" json:"filters,omitempty"`
	Ranking     map[string]float64 `yaml:"ranking" json:"ranking,omitempty"`
	Expand      map[string]any     `yaml:"expand" json:"expand,omitempty"`
}

func LoadPack(path string) (*Pack, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat policy path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("policy path must be a directory")
	}

	pack := &Pack{Path: path}
	if data, err := os.ReadFile(filepath.Join(path, "SKILL.md")); err == nil {
		pack.Skill = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}
	if data, err := os.ReadFile(filepath.Join(path, "agent_instructions.md")); err == nil {
		pack.AgentInstructions = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read agent_instructions.md: %w", err)
	}
	if err := loadYAML(filepath.Join(path, "ontology.yaml"), &pack.Ontology); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read ontology.yaml: %w", err)
	}
	if err := loadYAML(filepath.Join(path, "episode_rules.yaml"), &pack.EpisodeRules); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read episode_rules.yaml: %w", err)
	}
	if err := loadYAML(filepath.Join(path, "search_recipes.yaml"), &pack.SearchRecipes); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read search_recipes.yaml: %w", err)
	}
	return pack, nil
}

func ValidatePack(path string) (*ValidationResult, error) {
	pack, err := LoadPack(path)
	if err != nil {
		return nil, err
	}

	result := &ValidationResult{
		Path:  path,
		Valid: true,
	}
	addIssue := func(msg string) {
		result.Valid = false
		result.Issues = append(result.Issues, msg)
	}
	addWarning := func(msg string) {
		result.Warnings = append(result.Warnings, msg)
	}

	if strings.TrimSpace(pack.Skill) == "" && strings.TrimSpace(pack.AgentInstructions) == "" {
		addIssue("pack must contain at least one of SKILL.md or agent_instructions.md")
	}
	if pack.Ontology.Version != 1 {
		addIssue("ontology.yaml must declare version: 1")
	}
	if len(pack.Ontology.EntityTypes) == 0 {
		addWarning("ontology.yaml does not declare any entity types")
	}
	if len(pack.Ontology.Predicates) == 0 {
		addWarning("ontology.yaml does not declare any predicates")
	}
	for entityType, rule := range pack.Ontology.Dedup {
		if len(rule.Keys) == 0 {
			addIssue(fmt.Sprintf("ontology dedup rule for %s must declare keys", entityType))
		}
	}

	if pack.EpisodeRules.Version != 1 {
		addIssue("episode_rules.yaml must declare version: 1")
	}
	for _, rule := range append(append([]EpisodeRule{}, pack.EpisodeRules.PromoteToEpisode...), pack.EpisodeRules.Drop...) {
		if strings.TrimSpace(rule.Name) == "" {
			addIssue("episode rule name must not be empty")
		}
		if len(rule.When.ContainsAny) == 0 {
			addIssue(fmt.Sprintf("episode rule %q must declare when.contains_any", rule.Name))
		}
	}

	if pack.SearchRecipes.Version != 1 {
		addIssue("search_recipes.yaml must declare version: 1")
	}
	if len(pack.SearchRecipes.Recipes) == 0 {
		addWarning("search_recipes.yaml does not declare any recipes")
	}
	for name, recipe := range pack.SearchRecipes.Recipes {
		if strings.TrimSpace(recipe.Strategy) == "" {
			addIssue(fmt.Sprintf("recipe %q must declare strategy", name))
		}
	}

	return result, nil
}

func loadYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}
