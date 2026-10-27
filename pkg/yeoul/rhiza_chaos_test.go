//go:build linux || darwin

package yeoul

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	rhizaChaosRootEnv = "YEOUL_RHIZA_CHAOS_ROOT"
	rhizaChaosRootTag = "yeoul-rhiza-chaos-test-v1"
	rhizaChaosKillAck = "yeoul-rhiza-chaos-container-kill-v1\n"
)

// TestRhizaChaosContainerKillReopensCanonicalState is opt-in. Mount one
// dedicated volume at the same absolute path across container restarts.
func TestRhizaChaosContainerKillReopensCanonicalState(t *testing.T) {
	root := os.Getenv(rhizaChaosRootEnv)
	if root == "" {
		t.Skip("set YEOUL_RHIZA_CHAOS_ROOT to a dedicated, pre-marked persistent test volume")
	}
	if err := validateRhizaChaosRoot(root); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, "memory")
	expectedPath := filepath.Join(root, "expected.json")
	donePath := filepath.Join(root, "recovered")
	if _, err := os.Stat(donePath); err == nil {
		fmt.Println("YEOUL_RHIZA_CHAOS_RECOVERED")
		for {
			time.Sleep(time.Hour)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("check recovery completion marker: %v", err)
	}

	ctx := context.Background()
	if _, err := os.Stat(expectedPath); errors.Is(err, os.ErrNotExist) {
		if err := requireAbsentRhizaChaosDB(dbPath); err != nil {
			t.Fatal(err)
		}
		eng, err := Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverRhiza, CreateIfMissing: true})
		if err != nil {
			t.Fatalf("open chaos writer: %v", err)
		}
		seedChaosRecoveryOracle(t, ctx, eng)
		expected, err := Snapshot(ctx, eng)
		if err != nil {
			t.Fatalf("snapshot chaos recovery oracle: %v", err)
		}
		if err := persistRhizaChaosExpected(root, expectedPath, expected); err != nil {
			t.Fatalf("persist chaos recovery oracle: %v", err)
		}
		fmt.Println("YEOUL_RHIZA_CHAOS_READY")
		for {
			time.Sleep(time.Hour)
		}
	} else if err != nil {
		t.Fatalf("check expected snapshot: %v", err)
	}
	ackPath := filepath.Join(root, "container-kill-confirmed")
	if err := waitForRhizaChaosKillAck(ackPath, 120*time.Second); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read expected snapshot: %v", err)
	}
	var expected DatabaseSnapshot
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatalf("decode expected snapshot: %v", err)
	}
	eng, err := Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverRhiza})
	if err != nil {
		t.Fatalf("reopen Rhiza after container kill: %v", err)
	}
	assertRecoveryOracle(t, ctx, eng, &expected)
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{ID: "ep:after-chaos-kill", Kind: "note", Content: "write after container kill recovery"}); err != nil {
		t.Fatalf("write after recovery: %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close database after recovery write: %v", err)
	}
	eng, err = Open(ctx, Config{DatabasePath: dbPath, Driver: StorageDriverRhiza})
	if err != nil {
		t.Fatalf("reopen database after recovery write: %v", err)
	}
	state, err := Snapshot(ctx, eng)
	if err != nil {
		_ = eng.Close(ctx)
		t.Fatalf("snapshot after recovery write and reopen: %v", err)
	}
	if _, ok := state.Episodes["ep:after-chaos-kill"]; !ok {
		_ = eng.Close(ctx)
		t.Fatal("post-recovery write is missing after close and reopen")
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close database after verification: %v", err)
	}
	if err := writeRhizaChaosMarker(root, donePath, "recovered\n"); err != nil {
		t.Fatalf("persist recovery completion marker: %v", err)
	}
	fmt.Println("YEOUL_RHIZA_CHAOS_RECOVERED")
	for {
		time.Sleep(time.Hour)
	}
}

func validateRhizaChaosKillAck(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read external container-kill acknowledgment %q: %w", path, err)
	}
	if string(data) != rhizaChaosKillAck {
		return fmt.Errorf("external container-kill acknowledgment %q has unexpected content", path)
	}
	return nil
}

func requireAbsentRhizaChaosDB(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("refusing to use pre-existing database path %q", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check database path before initialization: %w", err)
	}
	return nil
}

func waitForRhizaChaosKillAck(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := validateRhizaChaosKillAck(path)
		if err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func TestValidateRhizaChaosKillAck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "container-kill-confirmed")
	if err := validateRhizaChaosKillAck(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing acknowledgment error = %v, want os.ErrNotExist", err)
	}
	if err := os.WriteFile(path, []byte("not confirmed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRhizaChaosKillAck(path); err == nil {
		t.Fatal("accepted invalid acknowledgment content")
	}
	if err := os.WriteFile(path, []byte(rhizaChaosKillAck), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRhizaChaosKillAck(path); err != nil {
		t.Fatalf("rejected exact acknowledgment: %v", err)
	}
}

func TestRequireAbsentRhizaChaosDB(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "memory")
	if err := requireAbsentRhizaChaosDB(dbPath); err != nil {
		t.Fatalf("rejected absent database path: %v", err)
	}
	if err := os.Mkdir(dbPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := requireAbsentRhizaChaosDB(dbPath); err == nil {
		t.Fatal("accepted pre-existing neighboring database directory")
	}
}

func validateRhizaChaosRoot(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == string(filepath.Separator) {
		return fmt.Errorf("%s must be a clean absolute path to a dedicated test mount", rhizaChaosRootEnv)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", rhizaChaosRootEnv, err)
	}
	home, homeErr := os.UserHomeDir()
	if homeErr == nil {
		home, _ = filepath.EvalSymlinks(home)
	}
	if homeErr == nil && (resolved == home || strings.HasPrefix(resolved, filepath.Clean(home)+string(filepath.Separator))) {
		return fmt.Errorf("%s must not point inside the user home directory", rhizaChaosRootEnv)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%s must already exist as a dedicated directory", rhizaChaosRootEnv)
	}
	tag, err := os.ReadFile(filepath.Join(root, ".yeoul-rhiza-chaos-test-root"))
	if err != nil || strings.TrimSpace(string(tag)) != rhizaChaosRootTag {
		return fmt.Errorf("%s lacks the required .yeoul-rhiza-chaos-test-root marker", rhizaChaosRootEnv)
	}
	return nil
}

func TestValidateRhizaChaosRoot(t *testing.T) {
	root := t.TempDir()
	if err := validateRhizaChaosRoot(root); err == nil {
		t.Fatal("accepted unmarked directory")
	}
	if err := os.WriteFile(filepath.Join(root, ".yeoul-rhiza-chaos-test-root"), []byte("wrong\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRhizaChaosRoot(root); err == nil {
		t.Fatal("accepted incorrect mount marker")
	}
	if err := os.WriteFile(filepath.Join(root, ".yeoul-rhiza-chaos-test-root"), []byte(rhizaChaosRootTag+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateRhizaChaosRoot(root); err != nil {
		t.Fatalf("rejected marked test directory: %v", err)
	}
	if err := validateRhizaChaosRoot("relative/path"); err == nil {
		t.Fatal("accepted relative path")
	}
	if home, err := os.UserHomeDir(); err == nil {
		if err := validateRhizaChaosRoot(home); err == nil {
			t.Fatal("accepted user home as test volume")
		}
	}
}

func seedChaosRecoveryOracle(t *testing.T, ctx context.Context, eng Engine) {
	t.Helper()
	entity, episode, oldFact := seedRecoveryOracle(t, ctx, eng)
	if _, err := eng.SupersedeFact(ctx, oldFact.ID, FactInput{
		ID: "fact:successor", Predicate: oldFact.Predicate, SubjectID: entity.ID,
		ValueText: "comet archive revised", SupportingEpisodeIDs: []string{episode.EpisodeID},
	}, "verified supersession"); err != nil {
		t.Fatalf("supersede oracle fact: %v", err)
	}
	retracted, err := eng.AssertFact(ctx, FactInput{
		ID: "fact:retracted", Predicate: "HAS_NOTE", SubjectID: entity.ID,
		ValueText: "obsolete comet note", SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		t.Fatalf("assert oracle fact to retract: %v", err)
	}
	if _, err := eng.RetractFact(ctx, retracted.ID, "no longer valid"); err != nil {
		t.Fatalf("retract oracle fact: %v", err)
	}
	if _, err := eng.AssertFact(ctx, FactInput{
		ID: "fact:active", Predicate: "HAS_NOTE", SubjectID: entity.ID,
		ValueText: "active comet archive", SupportingEpisodeIDs: []string{episode.EpisodeID},
	}); err != nil {
		t.Fatalf("assert active oracle fact: %v", err)
	}
}

func persistRhizaChaosExpected(root, path string, expected *DatabaseSnapshot) error {
	data, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	return writeRhizaChaosMarker(root, path, string(data))
}

func writeRhizaChaosMarker(root, path, content string) error {
	tmp, err := os.CreateTemp(root, ".yeoul-chaos-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
