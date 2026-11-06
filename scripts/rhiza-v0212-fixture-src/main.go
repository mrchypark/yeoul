package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fmt.Errorf("build information unavailable")
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/mrchypark/rhiza" && dep.Version == "v0.21.2" && dep.Replace == nil {
			return create(filepath.Clean(os.Getenv("YEOUL_RHIZA_FIXTURE_DB")))
		}
	}
	return fmt.Errorf("fixture must use github.com/mrchypark/rhiza v0.21.2 without replacements")
}

func create(path string) error {
	if !filepath.IsAbs(path) || path == string(filepath.Separator) {
		return fmt.Errorf("YEOUL_RHIZA_FIXTURE_DB must be an absolute, non-root path")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return fmt.Errorf("refusing to use non-empty fixture path %q: %v", path, err)
	}
	ctx := context.Background()
	eng, err := yeoul.Open(ctx, yeoul.Config{Driver: yeoul.StorageDriverRhiza, DatabasePath: path, CreateIfMissing: true})
	if err != nil {
		return fmt.Errorf("open v0.14.0 fixture database: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = eng.Close(ctx)
		}
	}()
	episode, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{
		ID: "ep:rhiza-v0212-fixture", Kind: "note", Content: "cross-version archive observatory marker",
	})
	if err != nil {
		return fmt.Errorf("ingest fixture episode: %w", err)
	}
	entity, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
		ID: "entity:rhiza-v0212-fixture", Type: "Project", CanonicalName: "Old Archive",
		Metadata: map[string]any{"generation": "21.2"},
	})
	if err != nil {
		return fmt.Errorf("create fixture entity: %w", err)
	}
	if _, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
		ID: entity.ID, Type: "Project", CanonicalName: "Revised Archive",
		Metadata: map[string]any{"generation": "21.2", "revision": "second"},
	}); err != nil {
		return fmt.Errorf("revise fixture entity: %w", err)
	}
	old, err := eng.AssertFact(ctx, yeoul.FactInput{
		ID: "fact:rhiza-v0212-old", Predicate: "HAS_NOTE", SubjectID: entity.ID,
		ValueText: "historical value", SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	if err != nil {
		return fmt.Errorf("create historical fixture fact: %w", err)
	}
	if _, err := eng.SupersedeFact(ctx, old.ID, yeoul.FactInput{
		ID: "fact:rhiza-v0212-current", Predicate: old.Predicate, SubjectID: entity.ID,
		ValueText: "current value", SupportingEpisodeIDs: []string{episode.EpisodeID},
	}, "fixture revision"); err != nil {
		return fmt.Errorf("supersede fixture fact: %w", err)
	}
	if err := eng.Close(ctx); err != nil {
		return fmt.Errorf("close v0.14.0 fixture database: %w", err)
	}
	closed = true
	fmt.Println("fixture created with github.com/mrchypark/yeoul v0.14.0 and github.com/mrchypark/rhiza v0.21.2")
	return nil
}
