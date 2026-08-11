package yeoul

import (
	"context"
	"math"
	"slices"
	"sort"
	"strings"
)

func (e *engine) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	_ = ctx
	query := strings.TrimSpace(strings.ToLower(req.QueryText))
	if query == "" {
		return nil, errorf(ErrInputInvalid, "query_text is required", map[string]any{"field": "query_text"}, nil)
	}

	types := req.Types
	if len(types) == 0 {
		types = []string{"fact", "episode", "entity"}
	}
	mode := req.Mode
	if mode == "" {
		mode = SearchModeHybrid
	}
	offset, err := decodeCursor(req.Page.Cursor)
	if err != nil {
		return nil, err
	}

	e.mu.RLock()
	defer e.mu.RUnlock()
	spaceID := normalizeSpaceID(req.Meta.SpaceID)

	hits := make([]SearchHit, 0)
	included := IncludedRecords{}
	graphSeeds := map[string]float64{}
	seenHits := map[string]bool{}
	stats := e.searchCorpusStats(types, req, spaceID)

	if slices.Contains(types, "fact") {
		for _, fact := range e.facts {
			factRecord := e.factVersionAt(fact, req.Temporal)
			if factRecord == nil || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal) {
				continue
			}
			if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, factRecord.Predicate) {
				continue
			}
			anchorMatched := matchesAnchors(req.AnchorIDs, append([]string{factRecord.ID, factRecord.SubjectID, factRecord.ObjectID}, factRecord.SupportingEpisodeIDs...)...)
			if len(req.AnchorIDs) > 0 && !anchorMatched {
				continue
			}
			text := strings.ToLower(factRecord.ValueText + " " + factRecord.Predicate + " " + factRecord.SubjectID + " " + factRecord.ObjectID)
			matched, baseScore, reason := matchSearchWithStats(mode, query, text, stats)
			if matched {
				score := baseScore + 0.4
				reasons := []string{reason}
				if anchorMatched {
					score += 0.15
					reasons = append(reasons, "anchor_match")
				}
				if len(req.Predicates) > 0 {
					score += 0.1
					reasons = append(reasons, "predicate_filter")
				}
				if req.MinScore != nil && score < *req.MinScore {
					continue
				}
				hits = append(hits, SearchHit{
					HitID:       "hit_" + factRecord.ID,
					HitType:     "fact",
					RecordID:    factRecord.ID,
					Score:       score,
					MatchedText: factRecord.ValueText,
					Reasons:     reasons,
				})
				seenHits["fact:"+factRecord.ID] = true
				addGraphSeeds(graphSeeds, score, factRecord.ID, factRecord.SubjectID, factRecord.ObjectID)
				addGraphSeeds(graphSeeds, score, factRecord.SupportingEpisodeIDs...)
				included.Facts = append(included.Facts, *factRecord)
				e.addFactSupport(&included, *factRecord, req.Scope, req.Temporal)
			}
		}
	}
	if slices.Contains(types, "episode") {
		for _, episode := range e.episodes {
			if episode.SpaceID != spaceID || !e.episodeVisibleAt(episode, req.Temporal) || !matchesScopeForEpisode(episode, req.Scope, e.sources) {
				continue
			}
			if len(req.Predicates) > 0 {
				continue
			}
			anchorMatched := matchesAnchors(req.AnchorIDs, episode.ID, episode.SourceID)
			if len(req.AnchorIDs) > 0 && !anchorMatched {
				continue
			}
			matched, baseScore, reason := matchSearchWithStats(mode, query, strings.ToLower(episode.Content), stats)
			if matched {
				score := baseScore + 0.2
				reasons := []string{reason}
				if anchorMatched {
					score += 0.15
					reasons = append(reasons, "anchor_match")
				}
				if req.MinScore != nil && score < *req.MinScore {
					continue
				}
				hits = append(hits, SearchHit{
					HitID:       "hit_" + episode.ID,
					HitType:     "episode",
					RecordID:    episode.ID,
					Score:       score,
					MatchedText: episode.Content,
					Reasons:     reasons,
				})
				seenHits["episode:"+episode.ID] = true
				addGraphSeeds(graphSeeds, score, episode.ID, episode.SourceID)
				included.Episodes = append(included.Episodes, episode)
				if source, ok := e.sources[episode.SourceID]; ok && source.SpaceID == spaceID && e.sourceVisibleAt(source, req.Temporal) {
					included.Sources = append(included.Sources, source)
				}
			}
		}
	}
	if slices.Contains(types, "entity") {
		for _, entity := range e.entities {
			entityRecord := e.entityVersionAt(entity, req.Temporal)
			if entityRecord == nil || entityRecord.SpaceID != spaceID || !matchesEntityType(*entityRecord, req.Scope.EntityTypes) {
				continue
			}
			if entityMarkedDuplicate(*entityRecord) {
				continue
			}
			if len(req.Predicates) > 0 {
				continue
			}
			anchorMatched := matchesAnchors(req.AnchorIDs, entityRecord.ID)
			if len(req.AnchorIDs) > 0 && !anchorMatched {
				continue
			}
			text := strings.ToLower(entityRecord.CanonicalName + " " + strings.Join(entityRecord.Aliases, " "))
			matched, baseScore, reason := matchSearchWithStats(mode, query, text, stats)
			if matched {
				score := baseScore
				reasons := []string{reason}
				if anchorMatched {
					score += 0.15
					reasons = append(reasons, "anchor_match")
				}
				if req.MinScore != nil && score < *req.MinScore {
					continue
				}
				hits = append(hits, SearchHit{
					HitID:       "hit_" + entity.ID,
					HitType:     "entity",
					RecordID:    entityRecord.ID,
					Score:       score,
					MatchedText: entityRecord.CanonicalName,
					Reasons:     reasons,
				})
				seenHits["entity:"+entityRecord.ID] = true
				addGraphSeeds(graphSeeds, score, entityRecord.ID)
				included.Entities = append(included.Entities, *entityRecord)
			}
		}
	}
	if mode != SearchModeKeyword && slices.Contains(types, "fact") && len(graphSeeds) > 0 {
		expandedSeeds := map[string]float64{}
		for _, fact := range e.facts {
			factRecord := e.factVersionAt(fact, req.Temporal)
			if factRecord == nil || seenHits["fact:"+factRecord.ID] || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal) {
				continue
			}
			if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, factRecord.Predicate) {
				continue
			}
			score := graphExpansionScore(graphSeeds, *factRecord)
			if score <= 0 {
				continue
			}
			if req.MinScore != nil && score < *req.MinScore {
				continue
			}
			hits = append(hits, SearchHit{
				HitID:       "hit_" + factRecord.ID,
				HitType:     "fact",
				RecordID:    factRecord.ID,
				Score:       score,
				MatchedText: factRecord.ValueText,
				Reasons:     []string{"graph_expansion"},
			})
			seenHits["fact:"+factRecord.ID] = true
			addGraphSeeds(expandedSeeds, score, factRecord.ID, factRecord.SubjectID, factRecord.ObjectID)
			addGraphSeeds(expandedSeeds, score, factRecord.SupportingEpisodeIDs...)
			included.Facts = append(included.Facts, *factRecord)
			e.addFactSupport(&included, *factRecord, req.Scope, req.Temporal)
		}
		for _, fact := range e.facts {
			factRecord := e.factVersionAt(fact, req.Temporal)
			if factRecord == nil || seenHits["fact:"+factRecord.ID] || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal) {
				continue
			}
			if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, factRecord.Predicate) {
				continue
			}
			score := graphExpansionScore(expandedSeeds, *factRecord)
			if score <= 0 {
				continue
			}
			if req.MinScore != nil && score < *req.MinScore {
				continue
			}
			hits = append(hits, SearchHit{
				HitID:       "hit_" + factRecord.ID,
				HitType:     "fact",
				RecordID:    factRecord.ID,
				Score:       score,
				MatchedText: factRecord.ValueText,
				Reasons:     []string{"graph_expansion_bfs"},
			})
			seenHits["fact:"+factRecord.ID] = true
			included.Facts = append(included.Facts, *factRecord)
			e.addFactSupport(&included, *factRecord, req.Scope, req.Temporal)
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].RecordID < hits[j].RecordID
		}
		return hits[i].Score > hits[j].Score
	})

	hitsPage, nextCursor, err := paginate(hits, offset, req.Page.Limit)
	if err != nil {
		return nil, err
	}

	response := &SearchResponse{
		Meta: newQueryMeta(req.Meta.SpaceID, e.now()),
		Hits: hitsPage,
	}
	response.Meta.NextCursor = nextCursor
	if req.Include.Provenance || req.Include.SupportingEpisodes || req.Include.RelatedEntities || req.Include.Snippets {
		response.Included = dedupeIncluded(included)
	}
	return response, nil
}

type sparseCorpusStats struct {
	DocCount int
	DF       map[string]int
}

func (e *engine) searchCorpusStats(types []string, req SearchRequest, spaceID string) *sparseCorpusStats {
	stats := &sparseCorpusStats{DF: map[string]int{}}
	observe := func(text string) {
		tokens := tokenize(text)
		if len(tokens) == 0 {
			return
		}
		stats.DocCount++
		for _, token := range tokens {
			stats.DF[token]++
		}
	}
	if slices.Contains(types, "fact") {
		for _, fact := range e.facts {
			factRecord := e.factVersionAt(fact, req.Temporal)
			if factRecord == nil || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal) {
				continue
			}
			if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, factRecord.Predicate) {
				continue
			}
			if len(req.AnchorIDs) > 0 && !matchesAnchors(req.AnchorIDs, append([]string{factRecord.ID, factRecord.SubjectID, factRecord.ObjectID}, factRecord.SupportingEpisodeIDs...)...) {
				continue
			}
			observe(factRecord.ValueText + " " + factRecord.Predicate + " " + factRecord.SubjectID + " " + factRecord.ObjectID)
		}
	}
	if slices.Contains(types, "episode") && len(req.Predicates) == 0 {
		for _, episode := range e.episodes {
			if episode.SpaceID != spaceID || !e.episodeVisibleAt(episode, req.Temporal) || !matchesScopeForEpisode(episode, req.Scope, e.sources) {
				continue
			}
			if len(req.AnchorIDs) > 0 && !matchesAnchors(req.AnchorIDs, episode.ID, episode.SourceID) {
				continue
			}
			observe(episode.Content)
		}
	}
	if slices.Contains(types, "entity") && len(req.Predicates) == 0 {
		for _, entity := range e.entities {
			entityRecord := e.entityVersionAt(entity, req.Temporal)
			if entityRecord == nil || entityRecord.SpaceID != spaceID || !matchesEntityType(*entityRecord, req.Scope.EntityTypes) || entityMarkedDuplicate(*entityRecord) {
				continue
			}
			if len(req.AnchorIDs) > 0 && !matchesAnchors(req.AnchorIDs, entityRecord.ID) {
				continue
			}
			observe(entityRecord.CanonicalName + " " + strings.Join(entityRecord.Aliases, " "))
		}
	}
	if stats.DocCount == 0 {
		return nil
	}
	return stats
}

func RecordPassesSearchFilters(ctx context.Context, eng Engine, record any, req SearchRequest) bool {
	switch value := record.(type) {
	case *Fact:
		current, err := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "fact", ID: value.ID, Temporal: req.Temporal})
		if err != nil {
			return false
		}
		value, _ = current.Record.(*Fact)
		if value == nil {
			return false
		}
		if !req.Temporal.IncludeInactive && value.Status != factStatusActive {
			return false
		}
		if !matchesAnchors(req.AnchorIDs, append([]string{value.ID, value.SubjectID, value.ObjectID}, value.SupportingEpisodeIDs...)...) {
			return false
		}
		if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, value.Predicate) {
			return false
		}
		if len(req.Scope.FactStatus) > 0 && !slices.Contains(req.Scope.FactStatus, value.Status) {
			return false
		}
		if len(req.Scope.EntityTypes) > 0 {
			subject, subjectErr := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "entity", ID: value.SubjectID, Temporal: req.Temporal})
			object, objectErr := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "entity", ID: value.ObjectID, Temporal: req.Temporal})
			var subjectEntity *Entity
			var objectEntity *Entity
			if subjectErr == nil {
				subjectEntity, _ = subject.Record.(*Entity)
			}
			if objectErr == nil {
				objectEntity, _ = object.Record.(*Entity)
			}
			if (subjectEntity == nil || !matchesEntityType(*subjectEntity, req.Scope.EntityTypes)) && (objectEntity == nil || !matchesEntityType(*objectEntity, req.Scope.EntityTypes)) {
				return false
			}
		}
		if len(req.Scope.GroupIDs) == 0 && len(req.Scope.SourceIDs) == 0 && len(req.Scope.SourceKinds) == 0 {
			return true
		}
		for _, episodeID := range value.SupportingEpisodeIDs {
			episode, err := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "episode", ID: episodeID, Temporal: req.Temporal})
			if err == nil && RecordPassesSearchFilters(ctx, eng, episode.Record, SearchRequest{Meta: req.Meta, Scope: req.Scope, Temporal: req.Temporal}) {
				return true
			}
		}
		return false
	case *Episode:
		current, err := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "episode", ID: value.ID, Temporal: req.Temporal})
		if err != nil {
			return false
		}
		value, _ = current.Record.(*Episode)
		if value == nil {
			return false
		}
		if !matchesAnchors(req.AnchorIDs, value.ID, value.SourceID) {
			return false
		}
		if len(req.Predicates) > 0 {
			return false
		}
		if len(req.Scope.GroupIDs) > 0 && !slices.Contains(req.Scope.GroupIDs, value.GroupID) {
			return false
		}
		if len(req.Scope.SourceIDs) > 0 && !slices.Contains(req.Scope.SourceIDs, value.SourceID) {
			return false
		}
		if len(req.Scope.SourceKinds) > 0 {
			source, err := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "source", ID: value.SourceID, Temporal: req.Temporal})
			if err != nil || source == nil {
				return false
			}
			sourceRecord, _ := source.Record.(*Source)
			if sourceRecord == nil || sourceRecord.SpaceID != value.SpaceID || !slices.Contains(req.Scope.SourceKinds, sourceRecord.Kind) {
				return false
			}
		}
		return true
	case *Entity:
		current, err := eng.GetRecord(ctx, GetRecordRequest{Meta: req.Meta, Kind: "entity", ID: value.ID, Temporal: req.Temporal})
		if err != nil {
			return false
		}
		value, _ = current.Record.(*Entity)
		if value == nil {
			return false
		}
		if entityMarkedDuplicate(*value) {
			return false
		}
		if !matchesAnchors(req.AnchorIDs, value.ID) {
			return false
		}
		return len(req.Predicates) == 0 && matchesEntityType(*value, req.Scope.EntityTypes)
	default:
		return false
	}
}

func matchesEntityType(entity Entity, types []string) bool {
	return len(types) == 0 || slices.Contains(types, entity.Type)
}

func validFactStatus(status string) bool {
	switch status {
	case factStatusActive, factStatusSuperseded, factStatusRetracted:
		return true
	default:
		return false
	}
}

func matchesAnchors(anchors []string, ids ...string) bool {
	if len(anchors) == 0 {
		return true
	}
	for _, id := range ids {
		if id != "" && slices.Contains(anchors, id) {
			return true
		}
	}
	return false
}

func addGraphSeeds(seeds map[string]float64, score float64, ids ...string) {
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || seeds[id] >= score {
			continue
		}
		seeds[id] = score
	}
}

func graphExpansionScore(seeds map[string]float64, fact Fact) float64 {
	best := 0.0
	for _, id := range append([]string{fact.SubjectID, fact.ObjectID}, fact.SupportingEpisodeIDs...) {
		if score := seeds[id] * 0.45; score > best {
			best = score
		}
	}
	return best
}

func matchSearch(mode SearchMode, query, text string) (bool, float64, string) {
	return matchSearchWithStats(mode, query, text, nil)
}

func matchSearchWithStats(mode SearchMode, query, text string, stats *sparseCorpusStats) (bool, float64, string) {
	query = strings.TrimSpace(strings.ToLower(query))
	text = strings.TrimSpace(strings.ToLower(text))
	if query == "" || text == "" {
		return false, 0, ""
	}
	if strings.Contains(text, query) {
		switch mode {
		case SearchModeKeyword:
			return true, 1.0, "keyword_match"
		case SearchModeSemantic:
			return true, 0.9, "semantic_fallback"
		default:
			return true, 1.0, "hybrid_keyword_match"
		}
	}

	score := sparseTermScore(query, text, stats)
	switch mode {
	case SearchModeKeyword:
		return false, 0, ""
	case SearchModeSemantic:
		if score <= 0 {
			vectorScore := charNgramCosine(query, text)
			if vectorScore < 0.25 {
				return false, 0, ""
			}
			return true, 0.35 + vectorScore*0.4, "semantic_vector_similarity"
		}
		return true, 0.5 + score*0.5, "semantic_token_overlap"
	default:
		if score <= 0 {
			vectorScore := charNgramCosine(query, text)
			if vectorScore < 0.25 {
				return false, 0, ""
			}
			return true, 0.25 + vectorScore*0.35, "hybrid_vector_similarity"
		}
		return true, 0.35 + score*0.45, "hybrid_token_overlap"
	}
}

func charNgramCosine(query, text string) float64 {
	queryVector := charNgramVector(query, 3)
	textVector := charNgramVector(text, 3)
	if len(queryVector) == 0 || len(textVector) == 0 {
		return 0
	}
	dot := 0.0
	queryNorm := 0.0
	textNorm := 0.0
	for gram, count := range queryVector {
		queryNorm += count * count
		dot += count * textVector[gram]
	}
	for _, count := range textVector {
		textNorm += count * count
	}
	if queryNorm == 0 || textNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(queryNorm) * math.Sqrt(textNorm))
}

func charNgramVector(value string, n int) map[string]float64 {
	value = strings.Join(strings.Fields(strings.ToLower(value)), " ")
	runes := []rune(value)
	if len(runes) < n {
		return nil
	}
	out := map[string]float64{}
	for i := 0; i <= len(runes)-n; i++ {
		out[string(runes[i:i+n])]++
	}
	return out
}

func sparseTermScore(query, text string, stats *sparseCorpusStats) float64 {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return 0
	}
	textTerms := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '-' || r == '_' || r == ':' || r == ',' || r == '.'
	})
	if len(textTerms) == 0 {
		return 0
	}
	tf := make(map[string]int)
	for _, token := range textTerms {
		token = strings.TrimSpace(token)
		if token != "" {
			tf[token]++
		}
	}
	score := 0.0
	for _, token := range queryTokens {
		count := tf[token]
		if count == 0 {
			continue
		}
		idf := 1.0
		if stats != nil && stats.DocCount > 0 {
			df := stats.DF[token]
			idf = math.Log(1 + (float64(stats.DocCount)-float64(df)+0.5)/(float64(df)+0.5))
		}
		score += idf * float64(count) / (float64(count) + 1.2 + 0.25*float64(len(textTerms)))
	}
	score = score / float64(len(queryTokens))
	if score > 1 {
		return 1
	}
	return score
}

func tokenize(value string) []string {
	parts := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '-' || r == '_' || r == ':' || r == ',' || r == '.'
	})
	return dedupeStrings(parts)
}

func summarize(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= 80 {
		return content
	}
	return content[:77] + "..."
}
