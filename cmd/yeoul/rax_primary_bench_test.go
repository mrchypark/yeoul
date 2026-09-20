package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

// H-03 measurement harness.
//
// The hypothesis is that primary Rax retrieval does not avoid core work: the
// response builder calls raxCoreRerankScores, which runs a full core
// eng.Search over the whole corpus regardless of how many native candidates
// Rax returned, and only the already selected core page is reranked. The
// benchmarks below separate the two dimensions that could drive the cost: the
// size of the corpus the core search scans, and the number of Rax candidates
// the builder is handed.
//
// The Rax FFI runtime is not required here: the measured functions are the
// pure-Go hydration and rerank path, which runs after native retrieval and is
// where the suspected core search lives.

// newRaxBenchEngine builds an in-memory corpus of `facts` facts over `entities`
// entities, all sharing the query token so every record is a plausible hit.
func newRaxBenchEngine(b *testing.B, entities, facts int) yeoul.Engine {
	b.Helper()
	ctx := context.Background()
	eng, err := yeoul.Open(ctx, yeoul.Config{InMemory: true})
	if err != nil {
		b.Fatalf("open engine: %v", err)
	}
	b.Cleanup(func() { _ = eng.Close(ctx) })
	episode, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{ID: "rax:bench:episode", Kind: "bench", Content: "rax bench corpus needle"})
	if err != nil {
		b.Fatalf("ingest episode: %v", err)
	}
	entityIDs := make([]string, 0, entities)
	for i := 0; i < entities; i++ {
		entity, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
			ID:            fmt.Sprintf("rax:bench:entity:%04d", i),
			Type:          "Bench",
			CanonicalName: fmt.Sprintf("needle entity %04d", i),
		})
		if err != nil {
			b.Fatalf("upsert entity %d: %v", i, err)
		}
		entityIDs = append(entityIDs, entity.ID)
	}
	for i := 0; i < facts; i++ {
		if _, err := eng.AssertFact(ctx, yeoul.FactInput{
			ID:                   fmt.Sprintf("rax:bench:fact:%05d", i),
			Predicate:            "HAS_NEEDLE",
			SubjectID:            entityIDs[i%len(entityIDs)],
			ValueText:            fmt.Sprintf("needle value %05d", i),
			SupportingEpisodeIDs: []string{episode.EpisodeID},
		}); err != nil {
			b.Fatalf("assert fact %d: %v", i, err)
		}
	}
	return eng
}

// raxBenchDocIDs returns `count` fact document ids in the shape Rax returns
// them.
func raxBenchDocIDs(count int) []string {
	docIDs := make([]string, 0, count)
	for i := 0; i < count; i++ {
		docIDs = append(docIDs, fmt.Sprintf("fact:rax:bench:fact:%05d", i))
	}
	return docIDs
}

func raxBenchRequest(limit int) yeoul.SearchRequest {
	return yeoul.SearchRequest{
		Meta:      yeoul.QueryMeta{SpaceID: "default"},
		QueryText: "needle",
		Types:     []string{"fact"},
		Page:      yeoul.Page{Limit: limit},
	}
}

// BenchmarkRaxPrimaryCoreRerankScaling isolates the full core search the
// primary Rax path runs to obtain rerank scores. The candidate count is fixed,
// so any growth is the core search scanning a larger corpus rather than the
// number of native candidates.
func BenchmarkRaxPrimaryCoreRerankScaling(b *testing.B) {
	ctx := context.Background()
	for _, corpus := range []int{250, 500, 1000, 2000} {
		b.Run(fmt.Sprintf("corpus=%d", corpus), func(b *testing.B) {
			eng := newRaxBenchEngine(b, corpus/8, corpus)
			req := raxBenchRequest(10)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if scores := raxCoreRerankScores(ctx, eng, req, 20); len(scores) == 0 {
					b.Fatal("expected rerank scores")
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(corpus), "corpus")
		})
	}
}

// BenchmarkRaxPrimaryResponseScaling measures the whole primary Rax response
// build with a fixed ten-candidate set as the corpus grows. A path that only
// hydrated and rechecked the native candidates would stay flat in corpus size;
// growth here is the full core search the builder runs on top of them.
func BenchmarkRaxPrimaryResponseScaling(b *testing.B) {
	ctx := context.Background()
	for _, corpus := range []int{250, 500, 1000, 2000} {
		b.Run(fmt.Sprintf("corpus=%d", corpus), func(b *testing.B) {
			eng := newRaxBenchEngine(b, corpus/8, corpus)
			req := raxBenchRequest(10)
			docIDs := raxBenchDocIDs(10)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := buildRaxPrimarySearchResponse(ctx, eng, req, docIDs); err != nil {
					b.Fatalf("build response: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(corpus), "corpus")
		})
	}
}

// BenchmarkRaxPrimaryResponseCandidateScaling measures the same build with a
// fixed corpus as the native candidate set grows. Comparing it with the corpus
// variant shows which dimension actually drives the cost: a full core search
// follows the corpus, while candidate hydration follows the candidate count.
func BenchmarkRaxPrimaryResponseCandidateScaling(b *testing.B) {
	ctx := context.Background()
	const corpus = 1000
	for _, candidates := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("candidates=%d", candidates), func(b *testing.B) {
			eng := newRaxBenchEngine(b, corpus/8, corpus)
			req := raxBenchRequest(10)
			docIDs := raxBenchDocIDs(candidates)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := buildRaxPrimarySearchResponse(ctx, eng, req, docIDs); err != nil {
					b.Fatalf("build response: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(candidates), "candidates")
		})
	}
}
