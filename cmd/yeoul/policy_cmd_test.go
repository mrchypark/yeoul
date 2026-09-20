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

func writePolicyFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
