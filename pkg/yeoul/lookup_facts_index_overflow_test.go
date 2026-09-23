package yeoul

import (
	"context"
	"fmt"
	"testing"
)

func TestLookupFactsFanoutOverflowFallsBackWithoutTruncation(t *testing.T) {
	ctx := context.Background()
	eng, err := Open(ctx, Config{DatabasePath: t.TempDir() + "/overflow.db", CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open lattice engine: %v", err)
	}
	defer eng.Close(ctx)
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "overflow", Content: "fanout overflow"})
	if err != nil {
		t.Fatalf("ingest episode: %v", err)
	}
	if _, err := eng.UpsertEntity(ctx, EntityInput{ID: "overflow:subject", Type: "Thing", CanonicalName: "overflow"}); err != nil {
		t.Fatalf("upsert subject: %v", err)
	}
	const factCount = 4097
	facts := make([]FactInput, 0, factCount)
	for i := 0; i < factCount; i++ {
		facts = append(facts, FactInput{ID: fmt.Sprintf("overflow:fact:%05d", i), Predicate: "FANOUT", SubjectID: "overflow:subject", ValueText: fmt.Sprintf("value-%d", i), SupportingEpisodeIDs: []string{episode.EpisodeID}})
	}
	if _, err := eng.IngestBatch(ctx, BatchInput{Facts: facts}); err != nil {
		t.Fatalf("ingest fanout batch: %v", err)
	}
	resp, err := eng.LookupFacts(ctx, FactLookupRequest{SubjectIDs: []string{"overflow:subject"}})
	if err != nil {
		t.Fatalf("lookup after fanout overflow: %v", err)
	}
	if len(resp.Facts) != factCount {
		t.Fatalf("fanout fallback returned %d facts, want %d", len(resp.Facts), factCount)
	}
}
