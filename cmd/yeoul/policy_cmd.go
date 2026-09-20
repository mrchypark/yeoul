package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mrchypark/yeoul/pkg/policy"
	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func (c cli) runPolicy(ctx context.Context, args []string) error {
	_ = ctx
	usage := strings.TrimSpace(`
Usage:
  yeoul policy validate --path PATH [--json]
  yeoul policy show --path PATH [--json]
  yeoul policy list-recipes --path PATH [--json]
`)
	if len(args) == 0 {
		return &usageError{message: usage}
	}
	switch args[0] {
	case "validate":
		return c.runPolicyValidate(args[1:])
	case "show":
		return c.runPolicyShow(args[1:])
	case "list-recipes":
		return c.runPolicyListRecipes(args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(c.stdout, usage)
		return nil
	default:
		return &usageError{message: usage}
	}
}

func (c cli) runPolicyValidate(args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul policy validate --path PATH [--json]
`)
	fs := newFlagSet("policy validate")
	var path string
	var jsonOut bool
	fs.StringVar(&path, "path", "", "policy pack path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(path) == "" {
		return &usageError{message: usage}
	}
	result, err := policy.ValidatePack(path)
	if err != nil {
		return err
	}
	if jsonOut {
		if err := writeJSON(c.stdout, result); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(c.stdout, "valid: %t\n", result.Valid); err != nil {
			return err
		}
		for _, issue := range result.Issues {
			if _, err := fmt.Fprintf(c.stdout, "issue: %s\n", issue); err != nil {
				return err
			}
		}
		for _, warning := range result.Warnings {
			if _, err := fmt.Fprintf(c.stdout, "warning: %s\n", warning); err != nil {
				return err
			}
		}
	}
	// The result is reported first so callers keep the diagnostics, then an
	// invalid pack fails the command instead of exiting successfully.
	if !result.Valid {
		return &yeoul.Error{
			Code:    yeoul.ErrInputInvalid,
			Message: fmt.Sprintf("policy pack %s is invalid", path),
			Details: map[string]any{
				"path":   path,
				"issues": result.Issues,
			},
		}
	}
	return nil
}

func (c cli) runPolicyShow(args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul policy show --path PATH [--json]
`)
	fs := newFlagSet("policy show")
	var path string
	var jsonOut bool
	fs.StringVar(&path, "path", "", "policy pack path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(path) == "" {
		return &usageError{message: usage}
	}
	pack, err := policy.LoadPack(path)
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(c.stdout, pack)
	}
	_, err = fmt.Fprintf(
		c.stdout,
		"path: %s\nentity_types: %d\npredicates: %d\nfact_candidates: %d\nrecipes: %d\n",
		pack.Path,
		len(pack.Ontology.EntityTypes),
		len(pack.Ontology.Predicates),
		len(factPromotionCandidates(pack)),
		len(pack.SearchRecipes.Recipes),
	)
	return err
}

func factPromotionCandidates(pack *policy.Pack) []string {
	if pack == nil || pack.EpisodeRules.FactPromotion == nil {
		return nil
	}
	return pack.EpisodeRules.FactPromotion.Candidates
}

func (c cli) runPolicyListRecipes(args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul policy list-recipes --path PATH [--json]
`)
	fs := newFlagSet("policy list-recipes")
	var path string
	var jsonOut bool
	fs.StringVar(&path, "path", "", "policy pack path")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || strings.TrimSpace(path) == "" {
		return &usageError{message: usage}
	}
	pack, err := policy.LoadPack(path)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(pack.SearchRecipes.Recipes))
	for name := range pack.SearchRecipes.Recipes {
		names = append(names, name)
	}
	sort.Strings(names)
	if jsonOut {
		return writeJSON(c.stdout, names)
	}
	for _, name := range names {
		recipe := pack.SearchRecipes.Recipes[name]
		if _, err := fmt.Fprintf(c.stdout, "%s\t%s\n", name, recipe.Strategy); err != nil {
			return err
		}
	}
	return nil
}

func shouldDropEpisode(pack *policy.Pack, content string) bool {
	content = strings.ToLower(content)
	for _, rule := range pack.EpisodeRules.Drop {
		for _, token := range rule.When.ContainsAny {
			if strings.Contains(content, strings.ToLower(strings.TrimSpace(token))) {
				return true
			}
		}
	}
	return false
}

func applySearchRecipe(pack *policy.Pack, recipeName string, req yeoul.SearchRequest) (yeoul.SearchRequest, error) {
	recipe, ok := pack.SearchRecipes.Recipes[recipeName]
	if !ok {
		return req, fmt.Errorf("search recipe %q not found", recipeName)
	}
	if issues := policy.ValidateSearchRecipe(recipeName, recipe); len(issues) > 0 {
		return req, fmt.Errorf("search recipe %q is not executable: %s", recipeName, strings.Join(issues, "; "))
	}
	// Scope must be populated before the strategy switch: the
	// predicate_subject_lookup strategy narrows the hit types below.
	if status, ok := recipe.Filters["fact_status"]; ok {
		req.Scope.FactStatus = mergeStringSlices(req.Scope.FactStatus, splitCSV(fmt.Sprint(status)))
	}
	if predicates, ok := recipe.Filters["predicate"]; ok {
		values, isList := stringSliceFromAny(predicates)
		if !isList {
			values = splitCSV(fmt.Sprint(predicates))
		}
		req.Predicates = mergeStringSlices(req.Predicates, values)
	}
	if windowDays, ok := intFromAny(recipe.Filters["window_days"]); ok {
		from := time.Now().UTC().Add(-time.Duration(windowDays) * 24 * time.Hour)
		if req.Temporal.ObservedFrom == nil || req.Temporal.ObservedFrom.Before(from) {
			req.Temporal.ObservedFrom = &from
		}
	}
	switch recipe.Strategy {
	case "hybrid":
		req.Include.RelatedEntities = true
		req.Include.SupportingEpisodes = true
	case "neighborhood":
		req.Include.RelatedEntities = true
		req.Include.Provenance = true
		if types, ok := stringSliceFromAny(recipe.Expand["entity_types"]); ok {
			req.Scope.EntityTypes = mergeStringSlices(req.Scope.EntityTypes, types)
		}
	case "predicate_subject_lookup":
		req.Types = []string{"fact"}
	default:
		return req, fmt.Errorf("unsupported recipe strategy %q", recipe.Strategy)
	}
	return req, nil
}
