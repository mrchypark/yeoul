package main

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func (c cli) runBench(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul bench ingest --db PATH --episodes N [--facts-per-episode N] [--json]
  yeoul bench query --db PATH --query TEXT [--entity ID] [--fact ID] [--iterations N] [--json]
  yeoul bench lifecycle --db PATH --iterations N [--json]
`)
	if len(args) == 0 {
		return &usageError{message: usage}
	}
	switch args[0] {
	case "ingest":
		return c.runBenchIngest(ctx, args[1:])
	case "query":
		return c.runBenchQuery(ctx, args[1:])
	case "lifecycle":
		return c.runBenchLifecycle(ctx, args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(c.stdout, usage)
		return nil
	default:
		return &usageError{message: usage}
	}
}

func (c cli) runBenchIngest(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul bench ingest --db PATH --episodes N [--facts-per-episode N] [--json]
`)
	fs := newFlagSet("bench ingest")
	var dbPath string
	var episodes int
	var factsPerEpisode int
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.IntVar(&episodes, "episodes", 0, "number of synthetic episodes")
	fs.IntVar(&factsPerEpisode, "facts-per-episode", 1, "facts per episode")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || episodes <= 0 || factsPerEpisode <= 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	start := time.Now()
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < episodes; i++ {
		episodeID := fmt.Sprintf("bench-ep-%06d", i)
		entityID := fmt.Sprintf("bench-entity-%06d", i)
		result, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{
			ID:      episodeID,
			SpaceID: "default",
			Kind:    "bench",
			Content: fmt.Sprintf("synthetic episode %d", i),
			Source: yeoul.SourceInput{
				Kind:        "bench",
				ExternalRef: fmt.Sprintf("bench-source-%06d", i),
			},
		})
		if err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		if _, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
			ID:            entityID,
			SpaceID:       "default",
			Type:          "BenchEntity",
			CanonicalName: fmt.Sprintf("bench-%06d", i),
		}); err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		for j := 0; j < factsPerEpisode; j++ {
			if _, err := eng.AssertFact(ctx, yeoul.FactInput{
				ID:                   fmt.Sprintf("bench-fact-%06d-%02d", i, j),
				SpaceID:              "default",
				Predicate:            "HAS_BENCH_VALUE",
				SubjectID:            entityID,
				ValueText:            fmt.Sprintf("v-%d-%d-%d", i, j, rng.Intn(1000)),
				SupportingEpisodeIDs: []string{result.EpisodeID},
			}); err != nil {
				_ = closeEngine(ctx, eng)
				return err
			}
		}
	}
	if err := closeEngine(ctx, eng); err != nil {
		return err
	}
	elapsed := time.Since(start)
	totalFacts := episodes * factsPerEpisode
	result := benchIngestResult{
		DatabasePath:      dbPath,
		Episodes:          episodes,
		FactsPerEpisode:   factsPerEpisode,
		ElapsedSeconds:    elapsed.Seconds(),
		EpisodesPerSecond: float64(episodes) / elapsed.Seconds(),
		FactsPerSecond:    float64(totalFacts) / elapsed.Seconds(),
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(
		c.stdout,
		"episodes=%d facts=%d elapsed=%.3fs eps=%.2f fps=%.2f\n",
		episodes, totalFacts, result.ElapsedSeconds, result.EpisodesPerSecond, result.FactsPerSecond,
	)
	return err
}

func (c cli) runBenchQuery(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul bench query --db PATH --query TEXT [--entity ID] [--fact ID] [--iterations N] [--json]
`)
	fs := newFlagSet("bench query")
	var dbPath string
	var query string
	var entityID string
	var factID string
	var iterations int
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.StringVar(&query, "query", "", "query text")
	fs.StringVar(&entityID, "entity", "", "entity anchor ID")
	fs.StringVar(&factID, "fact", "", "fact ID for provenance")
	fs.IntVar(&iterations, "iterations", 10, "number of iterations")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || query == "" || iterations <= 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}
	eng, err := openReadEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = closeEngine(ctx, eng) }()

	if entityID == "" || factID == "" {
		payload, err := exportDatabaseFromEngine(ctx, eng)
		if err != nil {
			return err
		}
		if factID == "" && len(payload.Facts) > 0 {
			factID = payload.Facts[0].ID
		}
	}

	metrics := map[string][]time.Duration{
		"search":       {},
		"neighborhood": {},
		"timeline":     {},
		"provenance":   {},
	}
	var searchHits int
	var searchRecordIDs []string
	for i := 0; i < iterations; i++ {
		start := time.Now()
		searchReq := yeoul.SearchRequest{
			QueryText: query,
			AnchorIDs: compactStrings(entityID),
			Page:      yeoul.Page{Limit: 10},
		}
		// Run the same planner the search command uses so the benchmark measures
		// production work rather than a truncated or differently filtered query.
		searchResp, err := eng.Search(ctx, searchReq)
		if err != nil {
			return err
		}
		searchHits = len(searchResp.Hits)
		searchRecordIDs = searchRecordIDs[:0]
		for _, hit := range searchResp.Hits {
			searchRecordIDs = append(searchRecordIDs, hit.RecordID)
		}
		metrics["search"] = append(metrics["search"], time.Since(start))

		if entityID != "" {
			start = time.Now()
			if _, err := eng.Neighborhood(ctx, yeoul.NeighborhoodRequest{
				AnchorIDs: []string{entityID},
				MaxHops:   2,
				MaxNodes:  100,
			}); err != nil {
				return err
			}
			metrics["neighborhood"] = append(metrics["neighborhood"], time.Since(start))

			start = time.Now()
			if _, err := eng.Timeline(ctx, yeoul.TimelineRequest{
				AnchorIDs: []string{entityID},
				Page:      yeoul.Page{Limit: 25},
			}); err != nil {
				return err
			}
			metrics["timeline"] = append(metrics["timeline"], time.Since(start))
		}

		if factID != "" {
			start = time.Now()
			if _, err := eng.Provenance(ctx, yeoul.ProvenanceRequest{
				Kind:     "fact",
				ID:       factID,
				MaxDepth: 2,
			}); err != nil {
				return err
			}
			metrics["provenance"] = append(metrics["provenance"], time.Since(start))
		}
	}

	result := benchQueryResult{
		DatabasePath: dbPath,
		Query:        query,
		Iterations:   iterations,
		SearchHits:   searchHits,
		RecordIDs:    searchRecordIDs,
		Metrics:      make(map[string]latency),
	}
	for key, samples := range metrics {
		if len(samples) == 0 {
			continue
		}
		result.Metrics[key] = summarizeLatencies(samples)
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	for _, key := range []string{"search", "neighborhood", "timeline", "provenance"} {
		metric, ok := result.Metrics[key]
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(c.stdout, "%s p50=%.3fms p95=%.3fms p99=%.3fms\n", key, metric.P50Millis, metric.P95Millis, metric.P99Millis); err != nil {
			return err
		}
	}
	return nil
}

func (c cli) runBenchLifecycle(ctx context.Context, args []string) error {
	usage := strings.TrimSpace(`
Usage:
  yeoul bench lifecycle --db PATH --iterations N [--json]
`)
	fs := newFlagSet("bench lifecycle")
	var dbPath string
	var iterations int
	var jsonOut bool
	fs.StringVar(&dbPath, "db", "", "database path")
	fs.IntVar(&iterations, "iterations", 10, "number of lifecycle iterations")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	handled, err := parseFlagSet(fs, usage, args, c.stdout)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	if fs.NArg() != 0 || iterations <= 0 {
		return &usageError{message: usage}
	}
	if err := requireDB(dbPath, usage); err != nil {
		return err
	}

	eng, err := openWriteEngine(ctx, dbPath)
	if err != nil {
		return err
	}
	episode, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{
		ID:      "bench-lifecycle-episode",
		SpaceID: "default",
		Kind:    "bench",
		Content: "lifecycle bench seed",
		Source:  yeoul.SourceInput{Kind: "bench", ExternalRef: "lifecycle"},
	})
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}
	entity, err := eng.UpsertEntity(ctx, yeoul.EntityInput{
		ID:            "bench:lifecycle:entity",
		SpaceID:       "default",
		Type:          "BenchEntity",
		CanonicalName: "Lifecycle",
	})
	if err != nil {
		_ = closeEngine(ctx, eng)
		return err
	}

	start := time.Now()
	supersedeCount := 0
	retractCount := 0
	for i := 0; i < iterations; i++ {
		fact, err := eng.AssertFact(ctx, yeoul.FactInput{
			ID:                   fmt.Sprintf("bench-lifecycle-fact-%06d", i),
			SpaceID:              "default",
			Predicate:            "HAS_LIFECYCLE_VALUE",
			SubjectID:            entity.ID,
			ValueText:            fmt.Sprintf("v-%d", i),
			SupportingEpisodeIDs: []string{episode.EpisodeID},
		})
		if err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		superseded, err := eng.SupersedeFact(ctx, fact.ID, yeoul.FactInput{
			ID:                   fmt.Sprintf("bench-lifecycle-fact-%06d-next", i),
			SpaceID:              "default",
			Predicate:            "HAS_LIFECYCLE_VALUE",
			SubjectID:            entity.ID,
			ValueText:            fmt.Sprintf("v-%d-next", i),
			SupportingEpisodeIDs: []string{episode.EpisodeID},
		}, "bench")
		if err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		supersedeCount++
		if _, err := eng.RetractFact(ctx, superseded.NewFactID, "bench"); err != nil {
			_ = closeEngine(ctx, eng)
			return err
		}
		retractCount++
	}
	if err := closeEngine(ctx, eng); err != nil {
		return err
	}
	elapsed := time.Since(start)
	result := benchLifecycleResult{
		DatabasePath:    dbPath,
		Iterations:      iterations,
		ElapsedSeconds:  elapsed.Seconds(),
		OpsPerSecond:    float64(supersedeCount+retractCount) / elapsed.Seconds(),
		SupersedeCount:  supersedeCount,
		RetractionCount: retractCount,
	}
	if jsonOut {
		return writeJSON(c.stdout, result)
	}
	_, err = fmt.Fprintf(c.stdout, "iterations=%d elapsed=%.3fs ops/sec=%.2f supersedes=%d retracts=%d\n", iterations, result.ElapsedSeconds, result.OpsPerSecond, supersedeCount, retractCount)
	return err
}

func summarizeLatencies(samples []time.Duration) latency {
	if len(samples) == 0 {
		return latency{}
	}
	values := append([]time.Duration(nil), samples...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return latency{
		P50Millis: durationAtPercentile(values, 0.50).Seconds() * 1000,
		P95Millis: durationAtPercentile(values, 0.95).Seconds() * 1000,
		P99Millis: durationAtPercentile(values, 0.99).Seconds() * 1000,
	}
}

func durationAtPercentile(values []time.Duration, percentile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(percentile * float64(len(values)-1))
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}
