package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runStorageBoundaryCheck(t *testing.T, files map[string]string) (string, error) {
	t.Helper()
	root := t.TempDir()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	checker := filepath.Join(root, "scripts", "ci", "check-storage-boundaries.sh")
	sourceChecker := filepath.Join(repoRoot(t), "scripts", "ci", "check-storage-boundaries.sh")
	data, err := os.ReadFile(sourceChecker)
	if err != nil {
		t.Fatalf("read storage boundary checker: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(checker), 0o755); err != nil {
		t.Fatalf("create checker directory: %v", err)
	}
	if err := os.WriteFile(checker, data, 0o755); err != nil {
		t.Fatalf("write storage boundary checker: %v", err)
	}

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	git("init", "-q")
	git("add", ".")

	cmd := exec.Command("sh", checker)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func rawCypherFixture(path string) map[string]string {
	return map[string]string{
		path: "package fixture\n\nfunc query() { _ = \"MATCH (e:Entity) RETURN e\" }\n",
	}
}

func TestStorageBoundaryCheckerAllowsOnlyAuthorizedAdapters(t *testing.T) {
	for _, path := range []string{
		"internal/storage/ladybug/adapter.go",
		"internal/storage/lattice/adapter.go",
	} {
		t.Run("allows "+path, func(t *testing.T) {
			if out, err := runStorageBoundaryCheck(t, rawCypherFixture(path)); err != nil {
				t.Fatalf("authorized adapter rejected: %v\n%s", err, out)
			}
		})
	}
}

func TestStorageBoundaryCheckerRejectsOtherAdapterPaths(t *testing.T) {
	for _, path := range []string{
		"pkg/yeoul/adapter.go",
		"cmd/yeoul/adapter.go",
		"internal/storage/other/adapter.go",
	} {
		t.Run("rejects "+path, func(t *testing.T) {
			out, err := runStorageBoundaryCheck(t, rawCypherFixture(path))
			if err == nil {
				t.Fatalf("unauthorized adapter accepted; output: %s", out)
			}
			if !strings.Contains(out, path) {
				t.Fatalf("rejection did not identify %s: %s", path, out)
			}
		})
	}
}
