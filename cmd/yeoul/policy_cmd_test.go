package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Invalid policy packs must fail the command in every output mode so CI and
// shell gates cannot treat semantic invalidity as success.
func TestCLIPolicyValidateFailsOnInvalidPack(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writePolicyFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
	writePolicyFile(t, filepath.Join(dir, "ontology.yaml"), "version: 2\n")
	writePolicyFile(t, filepath.Join(dir, "episode_rules.yaml"), "version: 1\n")
	writePolicyFile(t, filepath.Join(dir, "search_recipes.yaml"), "version: 1\nrecipes: {}\n")

	for _, mode := range []struct {
		name string
		args []string
		want string
	}{
		{"text", []string{"policy", "validate", "--path", dir}, "valid: false"},
		{"json", []string{"policy", "validate", "--path", dir, "--json"}, `"valid": false`},
	} {
		t.Run(mode.name, func(t *testing.T) {
			var stdout strings.Builder
			var stderr strings.Builder
			err := run(ctx, mode.args, &stdout, &stderr)
			if err == nil {
				t.Fatalf("expected invalid pack to fail the command, stdout=%q", stdout.String())
			}
			if code := exitCode(err); code == 0 {
				t.Fatalf("expected a nonzero exit code, got %d for %v", code, err)
			}
			if !strings.Contains(stdout.String(), mode.want) {
				t.Fatalf("expected result output %q, got %q", mode.want, stdout.String())
			}
		})
	}
}

// A pack that only produces warnings stays valid and must keep exiting 0.
func TestCLIPolicyValidateWarningsStillSucceed(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writePolicyFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
	writePolicyFile(t, filepath.Join(dir, "ontology.yaml"), "version: 1\n")
	writePolicyFile(t, filepath.Join(dir, "episode_rules.yaml"), "version: 1\n")
	writePolicyFile(t, filepath.Join(dir, "search_recipes.yaml"), "version: 1\nrecipes: {}\n")

	for _, args := range [][]string{
		{"policy", "validate", "--path", dir},
		{"policy", "validate", "--path", dir, "--json"},
	} {
		var stdout strings.Builder
		var stderr strings.Builder
		if err := run(ctx, args, &stdout, &stderr); err != nil {
			t.Fatalf("run %v: %v\nstdout=%s", args, err, stdout.String())
		}
		if !strings.Contains(stdout.String(), "warning:") && !strings.Contains(stdout.String(), `"warnings"`) {
			t.Fatalf("expected warning-only output for %v, got %q", args, stdout.String())
		}
	}
}

// A pack whose recipe control values cannot be executed must fail
// `policy validate`, not only recipe use, so shipped or user packs cannot
// report valid: true while declaring inert behavior.
func TestCLIPolicyValidateRejectsMalformedRecipeValues(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writePolicyFile(t, filepath.Join(dir, "SKILL.md"), "# Skill\n")
	writePolicyFile(t, filepath.Join(dir, "ontology.yaml"), "version: 1\n")
	writePolicyFile(t, filepath.Join(dir, "episode_rules.yaml"), "version: 1\n")
	writePolicyFile(t, filepath.Join(dir, "search_recipes.yaml"), "version: 1\nrecipes:\n  window_days_bogus:\n    strategy: hybrid\n    filters:\n      window_days: bogus\n  window_days_object:\n    strategy: hybrid\n    filters:\n      window_days: {}\n  entity_types_object:\n    strategy: neighborhood\n    expand:\n      entity_types: {}\n")

	var stdout strings.Builder
	var stderr strings.Builder
	err := run(ctx, []string{"policy", "validate", "--path", dir}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected malformed recipe values to fail validation, stdout=%q", stdout.String())
	}
	if code := exitCode(err); code == 0 {
		t.Fatalf("expected a nonzero exit code, got %d for %v", code, err)
	}
	for _, want := range []string{"valid: false", `invalid filter "window_days"`, `invalid expand setting "entity_types"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected output containing %q, got %q", want, stdout.String())
		}
	}
}

func writePolicyFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
