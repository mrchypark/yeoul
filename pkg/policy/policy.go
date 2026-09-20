package policy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
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

// SchemaError reports a policy file that does not match the recognized schema,
// such as a misspelled structural field. LoadPack returns it so callers fail
// fast; ValidatePack converts it into a validation issue.
type SchemaError struct {
	File    string
	Message string
}

func (e *SchemaError) Error() string {
	return e.Message
}

type Ontology struct {
	Version     int                  `yaml:"version" json:"version"`
	EntityTypes []string             `yaml:"entity_types" json:"entity_types,omitempty"`
	Predicates  []string             `yaml:"predicates" json:"predicates,omitempty"`
	Dedup       map[string]DedupRule `yaml:"dedup" json:"dedup,omitempty"`
	// Extensions carries advisory ontology content that Yeoul Core does not
	// interpret, such as exclusivity hints. It is the only place unknown
	// ontology keys are allowed, so misspelled structural fields fail
	// validation instead of being silently ignored.
	Extensions map[string]any `yaml:"extensions" json:"extensions,omitempty"`
}

type DedupRule struct {
	Keys []string `yaml:"keys" json:"keys,omitempty"`
}

type EpisodeRules struct {
	Version          int            `yaml:"version" json:"version"`
	FactPromotion    *FactPromotion `yaml:"fact_promotion" json:"fact_promotion,omitempty"`
	PromoteToEpisode []EpisodeRule  `yaml:"promote_to_episode" json:"promote_to_episode,omitempty"`
	Drop             []EpisodeRule  `yaml:"drop" json:"drop,omitempty"`
}

type FactPromotion struct {
	PromoteOnly                      []string `yaml:"promote_only" json:"promote_only,omitempty"`
	Candidates                       []string `yaml:"candidates" json:"candidates,omitempty"`
	RequireSupportingEpisode         bool     `yaml:"require_supporting_episode" json:"require_supporting_episode"`
	ClarificationRequiredWhenMissing []string `yaml:"clarification_required_when_missing" json:"clarification_required_when_missing,omitempty"`
	KeepEpisodeOnly                  []string `yaml:"keep_episode_only" json:"keep_episode_only,omitempty"`
}

type EpisodeRule struct {
	Name     string   `yaml:"name" json:"name"`
	When     RuleWhen `yaml:"when" json:"when,omitempty"`
	Priority string   `yaml:"priority" json:"priority,omitempty"`
}

type RuleWhen struct {
	ContainsAny       []string `yaml:"contains_any" json:"contains_any,omitempty"`
	ContainsSubstring []string `yaml:"contains_substring" json:"contains_substring,omitempty"`
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
	// Extensions carries advisory recipe content that Yeoul Core does not
	// interpret. Unknown keys are only allowed here, so misspelled structural
	// fields fail validation instead of being silently ignored.
	Extensions map[string]any `yaml:"extensions" json:"extensions,omitempty"`
}

// supportedRecipeFilterKeys lists the recipe filters that map to real request
// fields. Any other filter is rejected instead of being silently accepted.
var supportedRecipeFilterKeys = []string{"fact_status", "window_days", "predicate"}

// supportedRecipeStrategies lists the strategies the CLI executes.
var supportedRecipeStrategies = []string{"hybrid", "neighborhood", "predicate_subject_lookup"}

// supportedRecipeExpandKeys lists the recipe expand settings that map to real
// request fields. "hops" is accepted as advisory metadata for backward
// compatibility: it is validated when present but never applied to the search
// request, so a recipe that carries it keeps loading.
var supportedRecipeExpandKeys = []string{"entity_types", "hops"}

// supportedRecipeFactStatuses lists the fact lifecycle states a recipe filter
// may select, mirroring the status domain the search runtime accepts.
var supportedRecipeFactStatuses = []string{"active", "superseded", "retracted"}

// expandSettingStrategies maps each supported expand setting to the strategies
// that actually apply it. applySearchRecipe reads entity_types only inside the
// neighborhood branch, so declaring it elsewhere would pass validation while
// execution silently ignored it.
var expandSettingStrategies = map[string][]string{
	"entity_types": {"neighborhood"},
}

// ValidateSearchRecipe reports recipe controls that the runtime does not
// implement. Every accepted setting must change the executed request, so a
// recipe can never advertise behavior the CLI ignores. A control is accepted
// only when its value also has an applicable shape; a recognized key with a
// malformed value is rejected here and never silently skipped at execution.
func ValidateSearchRecipe(name string, recipe SearchRecipe) []string {
	var issues []string
	if len(recipe.Ranking) > 0 {
		issues = append(issues, fmt.Sprintf("recipe %q declares ranking weights, which are not supported", name))
	}
	for _, key := range sortedKeys(recipe.Filters) {
		if !slices.Contains(supportedRecipeFilterKeys, key) {
			issues = append(issues, fmt.Sprintf("recipe %q declares unsupported filter %q", name, key))
			continue
		}
		if err := validateRecipeFilterValue(key, recipe); err != nil {
			issues = append(issues, fmt.Sprintf("recipe %q declares invalid filter %q: %v", name, key, err))
		}
	}
	for _, key := range sortedKeys(recipe.Expand) {
		if !slices.Contains(supportedRecipeExpandKeys, key) {
			issues = append(issues, fmt.Sprintf("recipe %q declares unsupported expand setting %q", name, key))
			continue
		}
		if err := validateRecipeExpandValue(key, recipe); err != nil {
			issues = append(issues, fmt.Sprintf("recipe %q declares invalid expand setting %q: %v", name, key, err))
			continue
		}
		// Only keys that map to a real request field are strategy-checked.
		// Advisory keys such as hops are validated for shape but never applied.
		strategies, applied := expandSettingStrategies[key]
		if applied && !slices.Contains(strategies, recipe.Strategy) {
			issues = append(issues, fmt.Sprintf("recipe %q declares expand setting %q, which the %q strategy does not apply", name, key, recipe.Strategy))
		}
	}
	return issues
}

// validateRecipeExpandValue checks the value shape of a supported expand key.
// The accepted shapes match what applySearchRecipe can execute, so validation
// and execution can never disagree about a control. Advisory keys such as
// hops are still shape-checked even though execution ignores their value.
func validateRecipeExpandValue(key string, recipe SearchRecipe) error {
	switch key {
	case "entity_types":
		_, _, err := RecipeEntityTypes(recipe)
		return err
	case "hops":
		_, _, err := RecipeHops(recipe)
		return err
	default:
		return nil
	}
}

// validateRecipeFilterValue checks the value shape of a supported filter key.
// The accepted shapes match what applySearchRecipe can execute, so validation
// and execution can never disagree about a control.
func validateRecipeFilterValue(key string, recipe SearchRecipe) error {
	switch key {
	case "fact_status":
		_, _, err := RecipeFactStatuses(recipe)
		return err
	case "window_days":
		_, _, err := RecipeWindowDays(recipe)
		return err
	case "predicate":
		_, _, err := RecipePredicates(recipe)
		return err
	default:
		return nil
	}
}

// RecipeFactStatuses returns the fact_status filter values declared by the
// recipe. The bool reports whether the recipe declares the filter at all. The
// values are an explicit string list where every entry is a known status.
func RecipeFactStatuses(recipe SearchRecipe) ([]string, bool, error) {
	raw, ok := recipe.Filters["fact_status"]
	if !ok {
		return nil, false, nil
	}
	values, err := recipeStringList(raw, true)
	if err != nil {
		return nil, true, err
	}
	for _, value := range values {
		if !slices.Contains(supportedRecipeFactStatuses, value) {
			return nil, true, fmt.Errorf("must contain only %s, found %q", strings.Join(supportedRecipeFactStatuses, ", "), value)
		}
	}
	return values, true, nil
}

// RecipePredicates returns the predicate filter values declared by the recipe.
// The bool reports whether the recipe declares the filter at all.
func RecipePredicates(recipe SearchRecipe) ([]string, bool, error) {
	raw, ok := recipe.Filters["predicate"]
	if !ok {
		return nil, false, nil
	}
	values, err := recipeStringList(raw, true)
	if err != nil {
		return nil, true, err
	}
	return values, true, nil
}

// RecipeWindowDays returns the window_days filter value declared by the recipe.
// The bool reports whether the recipe declares the filter at all. The value
// must be a whole, non-negative integer; non-scalar shapes are rejected.
func RecipeWindowDays(recipe SearchRecipe) (int, bool, error) {
	raw, ok := recipe.Filters["window_days"]
	if !ok {
		return 0, false, nil
	}
	days, err := recipeInteger(raw)
	if err != nil {
		return 0, true, err
	}
	if days < 0 {
		return 0, true, fmt.Errorf("must not be negative, found %d", days)
	}
	return days, true, nil
}

// RecipeEntityTypes returns the expand.entity_types list declared by the
// recipe. The bool reports whether the recipe declares the setting at all. The
// value must be a non-empty list of non-empty strings.
func RecipeEntityTypes(recipe SearchRecipe) ([]string, bool, error) {
	raw, ok := recipe.Expand["entity_types"]
	if !ok {
		return nil, false, nil
	}
	values, err := recipeStringList(raw, false)
	if err != nil {
		return nil, true, err
	}
	return values, true, nil
}

// RecipeHops returns the expand.hops value declared by the recipe. The bool
// reports whether the recipe declares the setting at all. hops is advisory
// metadata: execution never applies it, but a declared value must still be a
// non-negative integer so a malformed recipe cannot pass validation.
func RecipeHops(recipe SearchRecipe) (int, bool, error) {
	raw, ok := recipe.Expand["hops"]
	if !ok {
		return 0, false, nil
	}
	hops, err := recipeInteger(raw)
	if err != nil {
		return 0, true, err
	}
	if hops < 0 {
		return 0, true, fmt.Errorf("must not be negative, found %d", hops)
	}
	return hops, true, nil
}

// recipeStringList coerces a scalar string or a string list into a non-empty
// list of trimmed, non-empty strings. When allowScalar is false the value must
// already be a list, so a bare string is rejected instead of being applied.
func recipeStringList(raw any, allowScalar bool) ([]string, error) {
	var items []any
	switch value := raw.(type) {
	case string:
		if !allowScalar {
			return nil, fmt.Errorf("must be a list of strings, found a string")
		}
		for _, part := range strings.Split(value, ",") {
			items = append(items, part)
		}
	case []string:
		for _, item := range value {
			items = append(items, item)
		}
	case []any:
		items = append(items, value...)
	default:
		if allowScalar {
			return nil, fmt.Errorf("must be a string or list of strings, found %s", describeRecipeValue(raw))
		}
		return nil, fmt.Errorf("must be a list of strings, found %s", describeRecipeValue(raw))
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("must contain only strings, found %s", describeRecipeValue(item))
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, fmt.Errorf("must not contain empty values")
		}
		values = append(values, text)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("must not be empty")
	}
	return values, nil
}

// recipeInteger coerces a whole-number scalar into an int. Non-integral
// numbers, non-numeric strings, and non-scalar shapes are rejected.
func recipeInteger(raw any) (int, error) {
	switch value := raw.(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		if math.Trunc(value) != value {
			return 0, fmt.Errorf("must be a whole number, found %v", value)
		}
		return int(value), nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0, fmt.Errorf("must be an integer, found %q", value)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("must be an integer, found %s", describeRecipeValue(raw))
	}
}

func describeRecipeValue(raw any) string {
	switch raw.(type) {
	case map[string]any, map[any]any:
		return "an object"
	case []any, []string:
		return "a list"
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case int, int64, float64:
		return "a number"
	default:
		return fmt.Sprintf("%T", raw)
	}
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func LoadPack(path string) (*Pack, error) {
	return loadPack(path, true)
}

// loadPack reads a policy directory. When sanitize is true, blank episode-rule
// tokens are removed from the returned rules; validation uses sanitize=false so
// it can report the blank tokens it is meant to reject.
func loadPack(path string, sanitize bool) (*Pack, error) {
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
		return nil, schemaError("ontology.yaml", err)
	}
	if err := loadYAML(filepath.Join(path, "episode_rules.yaml"), &pack.EpisodeRules); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, schemaError("episode_rules.yaml", err)
	}
	if sanitize {
		dropBlankTokens(&pack.EpisodeRules)
	}
	if err := loadYAML(filepath.Join(path, "search_recipes.yaml"), &pack.SearchRecipes); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, schemaError("search_recipes.yaml", err)
	}
	return pack, nil
}

// schemaError wraps a decode failure. Read failures stay plain errors; only
// schema mismatches become SchemaError so validation can report them as issues.
func schemaError(file string, err error) error {
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) || errors.Is(err, errMultipleDocuments) || strings.Contains(err.Error(), "not found in type") {
		return &SchemaError{File: file, Message: fmt.Sprintf("%s: %v", file, err)}
	}
	return fmt.Errorf("read %s: %w", file, err)
}

// dropBlankTokens defensively removes blank episode-rule tokens after load.
// Validation rejects such packs, but direct LoadPack callers still use the
// returned rules; dropping the tokens keeps a blank entry from matching every
// episode and suppressing all captures.
func dropBlankTokens(rules *EpisodeRules) {
	if rules == nil {
		return
	}
	all := make([][]EpisodeRule, 0, 2)
	all = append(all, rules.PromoteToEpisode, rules.Drop)
	for _, group := range all {
		for i := range group {
			group[i].When.ContainsAny = trimBlankTokens(group[i].When.ContainsAny)
			group[i].When.ContainsSubstring = trimBlankTokens(group[i].When.ContainsSubstring)
		}
	}
}

func trimBlankTokens(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func ValidatePack(path string) (*ValidationResult, error) {
	pack, err := loadPack(path, false)
	if err != nil {
		var schemaErr *SchemaError
		if errors.As(err, &schemaErr) {
			return &ValidationResult{
				Path:   path,
				Valid:  false,
				Issues: []string{schemaErr.Message},
			}, nil
		}
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
	if pack.EpisodeRules.FactPromotion != nil {
		validateNonEmptyList(addIssue, "fact_promotion.promote_only", pack.EpisodeRules.FactPromotion.PromoteOnly)
		validateNonEmptyList(addIssue, "fact_promotion.candidates", pack.EpisodeRules.FactPromotion.Candidates)
		validateNonEmptyList(addIssue, "fact_promotion.clarification_required_when_missing", pack.EpisodeRules.FactPromotion.ClarificationRequiredWhenMissing)
		validateNonEmptyList(addIssue, "fact_promotion.keep_episode_only", pack.EpisodeRules.FactPromotion.KeepEpisodeOnly)
		if !pack.EpisodeRules.FactPromotion.RequireSupportingEpisode {
			addIssue("episode_rules.yaml fact_promotion.require_supporting_episode must be true")
		}
	}
	for _, rule := range append(append([]EpisodeRule{}, pack.EpisodeRules.PromoteToEpisode...), pack.EpisodeRules.Drop...) {
		if strings.TrimSpace(rule.Name) == "" {
			addIssue("episode rule name must not be empty")
		}
		if len(rule.When.ContainsAny) == 0 && len(rule.When.ContainsSubstring) == 0 {
			addIssue(fmt.Sprintf("episode rule %q must declare when.contains_any or when.contains_substring", rule.Name))
		}
		validateNonBlankTokens(addIssue, fmt.Sprintf("episode rule %q when.contains_any", rule.Name), rule.When.ContainsAny)
		validateNonBlankTokens(addIssue, fmt.Sprintf("episode rule %q when.contains_substring", rule.Name), rule.When.ContainsSubstring)
	}

	if pack.SearchRecipes.Version != 1 {
		addIssue("search_recipes.yaml must declare version: 1")
	}
	if len(pack.SearchRecipes.Recipes) == 0 {
		addWarning("search_recipes.yaml does not declare any recipes")
	}
	for _, name := range sortedRecipeNames(pack.SearchRecipes.Recipes) {
		recipe := pack.SearchRecipes.Recipes[name]
		if strings.TrimSpace(recipe.Strategy) == "" {
			addIssue(fmt.Sprintf("recipe %q must declare strategy", name))
		} else if !slices.Contains(supportedRecipeStrategies, recipe.Strategy) {
			addIssue(fmt.Sprintf("recipe %q declares unsupported strategy %q", name, recipe.Strategy))
		}
		for _, issue := range ValidateSearchRecipe(name, recipe) {
			addIssue(issue)
		}
	}

	return result, nil
}

func sortedRecipeNames(recipes map[string]SearchRecipe) []string {
	names := make([]string, 0, len(recipes))
	for name := range recipes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func validateNonEmptyList(addIssue func(string), field string, values []string) {
	if len(values) == 0 {
		addIssue(fmt.Sprintf("episode_rules.yaml %s must not be empty", field))
		return
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			addIssue(fmt.Sprintf("episode_rules.yaml %s must not contain empty values", field))
			return
		}
	}
}

// validateNonBlankTokens rejects empty and whitespace-only tokens. A blank
// token would otherwise become an empty substring at match time and suppress
// every episode, so a pack that relies on intentional match-all behavior must
// say so explicitly instead of hiding it in a blank list entry.
func validateNonBlankTokens(addIssue func(string), field string, values []string) {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			addIssue(fmt.Sprintf("%s must not contain blank tokens", field))
			return
		}
	}
}

func loadYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return decodeYAML(data, out)
}

// decodeYAML decodes YAML while rejecting keys that do not map to a struct
// field. Permissive decoding silently dropped misspelled structural fields, so
// a pack could report valid while excluding no episodes or failing later at
// search time.
func decodeYAML(data []byte, out any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	// A second document would escape KnownFields strictness and be silently
	// ignored, so trailing content is rejected instead of accepted.
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errMultipleDocuments
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// errMultipleDocuments marks policy files that carry more than one YAML
// document. Only the first document would be validated, so the extra content
// must fail loudly instead of being silently dropped.
var errMultipleDocuments = errors.New("must contain a single YAML document, found additional content")
