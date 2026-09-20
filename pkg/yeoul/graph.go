package yeoul

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (e *engine) LookupFacts(ctx context.Context, req FactLookupRequest) (*FactLookupResponse, error) {
	_ = ctx
	offset, err := decodeCursor(req.Page.Cursor)
	if err != nil {
		return nil, err
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	spaceID := normalizeSpaceID(req.Meta.SpaceID)
	index := newTemporalIndex(e)

	facts := make([]Fact, 0)
	included := IncludedRecords{}
	// Support records explain a hit; they are not the hit. Collecting them costs
	// an entity version lookup per fact, and that lookup builds the entity
	// revision index, so a request that discards them must not pay for them.
	collectSupport := req.Include.Provenance || req.Include.RelatedEntities || req.Include.SupportingEpisodes
	for _, fact := range e.facts {
		factRecord := e.factVersionAt(fact, req.Temporal, index)
		if factRecord == nil || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal, index) {
			continue
		}
		if len(req.SubjectIDs) > 0 && !slices.Contains(req.SubjectIDs, factRecord.SubjectID) {
			continue
		}
		if len(req.Predicates) > 0 && !slices.Contains(req.Predicates, factRecord.Predicate) {
			continue
		}
		if len(req.ObjectIDs) > 0 && !slices.Contains(req.ObjectIDs, factRecord.ObjectID) {
			continue
		}
		if req.ObjectText != "" && !strings.Contains(strings.ToLower(factRecord.ValueText), strings.ToLower(req.ObjectText)) {
			continue
		}
		facts = append(facts, *factRecord)
		if collectSupport {
			included.Facts = append(included.Facts, *factRecord)
			e.addFactSupport(&included, *factRecord, req.Scope, req.Temporal, index)
		}
	}

	sort.SliceStable(facts, func(i, j int) bool {
		if facts[i].ObservedAt.Equal(facts[j].ObservedAt) {
			return facts[i].ID < facts[j].ID
		}
		return facts[i].ObservedAt.After(facts[j].ObservedAt)
	})
	factPage, nextCursor, err := paginate(facts, offset, req.Page.Limit)
	if err != nil {
		return nil, err
	}

	resp := &FactLookupResponse{
		Meta:  newQueryMeta(req.Meta.SpaceID, e.now()),
		Facts: factPage,
	}
	resp.Meta.NextCursor = nextCursor
	if collectSupport {
		resp.Included = dedupeIncluded(included)
	}
	return resp, nil
}

func (e *engine) Neighborhood(ctx context.Context, req NeighborhoodRequest) (*NeighborhoodResponse, error) {
	_ = ctx
	if len(req.AnchorIDs) == 0 {
		return nil, errorf(ErrInputInvalid, "anchor_ids is required", map[string]any{"field": "anchor_ids"}, nil)
	}

	e.mu.RLock()
	defer e.mu.RUnlock()
	spaceID := normalizeSpaceID(req.Meta.SpaceID)
	index := newTemporalIndex(e)

	nodeMap := make(map[string]GraphNode)
	edgeMap := make(map[string]GraphEdge)
	adjacency := make(map[string][]GraphEdge)

	addNode := func(node GraphNode) {
		nodeMap[node.ID] = node
	}
	// Edges are only materialized between nodes that survived filtering. Support
	// and reference edges can point at excluded, invisible, or other-space
	// records, and keeping those edges would let traversal reach phantom nodes.
	addEdge := func(edge GraphEdge) {
		if _, ok := nodeMap[edge.FromID]; !ok {
			return
		}
		if _, ok := nodeMap[edge.ToID]; !ok {
			return
		}
		edgeMap[edge.ID] = edge
		adjacency[edge.FromID] = append(adjacency[edge.FromID], edge)
		adjacency[edge.ToID] = append(adjacency[edge.ToID], edge)
	}

	for _, entity := range e.entities {
		entityRecord := e.entityVersionAt(entity, req.Temporal, index)
		if entityRecord == nil || entityRecord.SpaceID != spaceID {
			continue
		}
		addNode(GraphNode{ID: entityRecord.ID, Type: "Entity", Label: entityRecord.CanonicalName})
	}
	for _, source := range e.sources {
		if source.SpaceID != spaceID || !e.sourceVisibleAt(source, req.Temporal) {
			continue
		}
		addNode(GraphNode{ID: source.ID, Type: "Source", Label: source.Kind})
	}
	for _, episode := range e.episodes {
		if episode.SpaceID != spaceID || !matchesScopeForEpisode(episode, req.Scope, e.sources) || !e.episodeVisibleAt(episode, req.Temporal) {
			continue
		}
		addNode(GraphNode{ID: episode.ID, Type: "Episode", Label: episode.Kind})
		if episode.SourceID != "" {
			addEdge(GraphEdge{ID: "edge:" + episode.ID + ":source", Type: "FROM_SOURCE", FromID: episode.ID, ToID: episode.SourceID})
		}
	}
	for _, fact := range e.facts {
		factRecord := e.factVersionAt(fact, req.Temporal, index)
		if factRecord == nil || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, req.Temporal, index) {
			continue
		}
		addNode(GraphNode{ID: factRecord.ID, Type: "Fact", Label: factRecord.Predicate})
		addEdge(GraphEdge{ID: "edge:" + factRecord.ID + ":subject", Type: "SUBJECT", FromID: factRecord.ID, ToID: factRecord.SubjectID})
		if factRecord.ObjectID != "" {
			addEdge(GraphEdge{ID: "edge:" + factRecord.ID + ":object", Type: "OBJECT", FromID: factRecord.ID, ToID: factRecord.ObjectID})
		}
		for _, episodeID := range factRecord.SupportingEpisodeIDs {
			addEdge(GraphEdge{ID: "edge:" + factRecord.ID + ":" + episodeID, Type: "ASSERTS", FromID: episodeID, ToID: factRecord.ID})
		}
	}

	maxHops := req.MaxHops
	if maxHops <= 0 {
		maxHops = 1
	}

	dist := make(map[string]int)
	queue := make([]string, 0, len(req.AnchorIDs))
	for _, anchorID := range req.AnchorIDs {
		if _, ok := nodeMap[anchorID]; !ok {
			continue
		}
		dist[anchorID] = 0
		queue = append(queue, anchorID)
	}
	reachableNodes := make(map[string]struct{})
	reachableEdges := make(map[string]struct{})
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		reachableNodes[current] = struct{}{}
		if dist[current] >= maxHops {
			continue
		}
		for _, edge := range adjacency[current] {
			if len(req.EdgeTypes) > 0 && !slices.Contains(req.EdgeTypes, edge.Type) {
				continue
			}
			reachableEdges[edge.ID] = struct{}{}
			nextID := edge.FromID
			if nextID == current {
				nextID = edge.ToID
			}
			if _, ok := nodeMap[nextID]; !ok {
				continue
			}
			if _, seen := dist[nextID]; seen {
				continue
			}
			dist[nextID] = dist[current] + 1
			queue = append(queue, nextID)
		}
	}

	type reachableCandidate struct {
		node GraphNode
		dist int
	}
	candidates := make([]reachableCandidate, 0, len(reachableNodes))
	for id := range reachableNodes {
		node, ok := nodeMap[id]
		if !ok {
			continue
		}
		if len(req.NodeTypes) > 0 && !slices.Contains(req.NodeTypes, node.Type) {
			continue
		}
		candidates = append(candidates, reachableCandidate{node: node, dist: dist[id]})
	}
	// Anchors and near nodes are selected first so that max_nodes cannot drop the
	// anchor a caller started from.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].dist == candidates[j].dist {
			return candidates[i].node.ID < candidates[j].node.ID
		}
		return candidates[i].dist < candidates[j].dist
	})
	truncated := false
	if req.MaxNodes > 0 && len(candidates) > req.MaxNodes {
		candidates = candidates[:req.MaxNodes]
		truncated = true
	}

	nodes := make([]GraphNode, 0, len(candidates))
	keptNodes := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		nodes = append(nodes, candidate.node)
		keptNodes[candidate.node.ID] = struct{}{}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	edges := make([]GraphEdge, 0, len(reachableEdges))
	for id := range reachableEdges {
		edge, ok := edgeMap[id]
		if !ok {
			continue
		}
		if _, ok := keptNodes[edge.FromID]; !ok {
			continue
		}
		if _, ok := keptNodes[edge.ToID]; !ok {
			continue
		}
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })

	return &NeighborhoodResponse{
		Meta:      newQueryMeta(req.Meta.SpaceID, e.now()),
		Nodes:     nodes,
		Edges:     edges,
		Truncated: truncated,
	}, nil
}

func (e *engine) Timeline(ctx context.Context, req TimelineRequest) (*TimelineResponse, error) {
	_ = ctx
	offset, err := decodeCursor(req.Page.Cursor)
	if err != nil {
		return nil, err
	}
	temporal := req.Temporal
	temporal.IncludeInactive = true
	e.mu.RLock()
	defer e.mu.RUnlock()
	spaceID := normalizeSpaceID(req.Meta.SpaceID)
	index := newTemporalIndex(e)

	events := make([]TimelineEvent, 0)
	allowedEvents := req.EventTypes

	addIfAllowed := func(event TimelineEvent) {
		if len(allowedEvents) > 0 && !slices.Contains(allowedEvents, event.EventType) {
			return
		}
		events = append(events, event)
	}

	for _, episode := range e.episodes {
		if episode.SpaceID != spaceID || !e.episodeVisibleAt(episode, temporal) || !matchesScopeForEpisode(episode, req.Scope, e.sources) || !matchesAnchors(req.AnchorIDs, episode.ID, episode.SourceID) {
			continue
		}
		event := TimelineEvent{
			EventID:    "evt:" + episode.ID,
			EventType:  "episode",
			RecordType: "episode",
			RecordID:   episode.ID,
			Timestamp:  chooseTime(episode.ObservedAt, episode.IngestedAt),
			Summary:    summarize(episode.Content),
		}
		if !timelineEventVisible(event, temporal) {
			continue
		}
		addIfAllowed(event)
	}
	// Index the append-only revision log once instead of rescanning it for every
	// fact, which would make timeline construction O(F x R).
	revisionsByFact := e.factRevisionsByFact()
	for _, fact := range e.facts {
		factRecord := e.factVersionAt(fact, temporal, index)
		if factRecord == nil || factRecord.SpaceID != spaceID || !e.matchesScopeForFact(*factRecord, req.Scope, temporal, index) || !matchesAnchors(req.AnchorIDs, append([]string{factRecord.SubjectID, factRecord.ObjectID, factRecord.ID}, factRecord.SupportingEpisodeIDs...)...) {
			continue
		}
		createdEvent := TimelineEvent{
			EventID:    "evt:" + factRecord.ID + ":created",
			EventType:  "fact_created",
			RecordType: "fact",
			RecordID:   factRecord.ID,
			Timestamp:  chooseTime(factRecord.ObservedAt, factRecord.CreatedAt),
			Summary:    factRecord.Predicate,
		}
		if timelineEventVisible(createdEvent, temporal) {
			addIfAllowed(createdEvent)
		}
		for _, event := range e.factLifecycleEvents(*factRecord, revisionsByFact[factRecord.ID], temporal) {
			if !timelineEventVisible(event, temporal) {
				continue
			}
			addIfAllowed(event)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp.Equal(events[j].Timestamp) {
			return events[i].EventID < events[j].EventID
		}
		if req.Descending {
			return events[i].Timestamp.After(events[j].Timestamp)
		}
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	eventPage, nextCursor, err := paginate(events, offset, req.Page.Limit)
	if err != nil {
		return nil, err
	}
	return &TimelineResponse{
		Meta: QueryResponseMeta{
			SpaceID:    firstNonEmpty(req.Meta.SpaceID, "default"),
			SnapshotAt: ptrTime(e.now()),
			NextCursor: nextCursor,
		},
		Events: eventPage,
	}, nil
}

// factRevisionsByFact groups the append-only revision log by fact ID in a
// single pass. Timeline builds it once per invocation so lifecycle derivation
// stays O(F + R) instead of rescanning every revision once per fact.
func (e *engine) factRevisionsByFact() map[string][]FactRevision {
	index := make(map[string][]FactRevision, len(e.facts))
	for _, revision := range e.factRevisions {
		index[revision.FactID] = append(index[revision.FactID], revision)
	}
	return index
}

// factLifecycleEvents derives a fact's supersede and retract events from its
// append-only revisions. Deriving them from revisions instead of the fact's
// current status keeps earlier transitions visible: retracting a superseded
// fact must not erase the supersession from the timeline.
//
// revisions is the fact's slice from the per-invocation index built by
// factRevisionsByFact. It is Timeline-local scratch and is sorted in place.
func (e *engine) factLifecycleEvents(fact Fact, revisions []FactRevision, filter TemporalFilter) []TimelineEvent {
	if len(revisions) == 0 {
		// Facts loaded from state without revisions fall back to their current
		// status so legacy stores keep reporting lifecycle events.
		revisions = []FactRevision{newFactRevision("", fact, "", fact.UpdatedAt)}
	}
	sort.Slice(revisions, func(i, j int) bool {
		if revisions[i].TxTime.Equal(revisions[j].TxTime) {
			return revisions[i].ID < revisions[j].ID
		}
		return revisions[i].TxTime.Before(revisions[j].TxTime)
	})

	events := make([]TimelineEvent, 0, 2)
	seen := make(map[string]struct{}, 2)
	emit := func(suffix, eventType, summary string, at time.Time) {
		if at.IsZero() {
			return
		}
		if _, ok := seen[suffix]; ok {
			return
		}
		seen[suffix] = struct{}{}
		events = append(events, TimelineEvent{
			EventID:    "evt:" + fact.ID + ":" + suffix,
			EventType:  eventType,
			RecordType: "fact",
			RecordID:   fact.ID,
			Timestamp:  at,
			Summary:    summary,
		})
	}

	for _, revision := range revisions {
		if !e.factVisibleAt(revision.toFact(), filter) {
			continue
		}
		switch revision.Status {
		case factStatusSuperseded:
			emit("superseded", "fact_superseded", revision.Predicate, revision.UpdatedAt)
		case factStatusRetracted:
			emit("retracted", "fact_retracted", revision.RetractionReason, chooseTime(revision.RetractedAt, revision.UpdatedAt))
		}
	}
	return events
}

func (e *engine) Provenance(ctx context.Context, req ProvenanceRequest) (*ProvenanceResponse, error) {
	_ = ctx
	e.mu.RLock()
	defer e.mu.RUnlock()
	spaceID := normalizeSpaceID(req.Meta.SpaceID)
	index := newTemporalIndex(e)
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 8
	}

	root := ProvenanceNode{ID: req.ID, Type: strings.Title(req.Kind), Label: req.ID}
	nodes := make([]ProvenanceNode, 0)
	edges := make([]ProvenanceEdge, 0)
	addNode := func(node ProvenanceNode, depth int) bool {
		if depth > maxDepth {
			return false
		}
		nodes = append(nodes, node)
		return true
	}
	addEdge := func(edge ProvenanceEdge, depth int) bool {
		if depth > maxDepth {
			return false
		}
		edges = append(edges, edge)
		return true
	}

	switch req.Kind {
	case "fact":
		fact, ok := e.facts[req.ID]
		factRecord := e.factVersionAt(fact, req.Temporal, index)
		if !ok || factRecord == nil || factRecord.SpaceID != spaceID {
			return nil, errorf(ErrFactNotFound, "fact not found", map[string]any{"fact_id": req.ID}, nil)
		}
		root.Label = factRecord.Predicate
		root.Meta = factProvenanceMeta(*factRecord)
		nodes = append(nodes, root)
		for _, episodeID := range factRecord.SupportingEpisodeIDs {
			episode, ok := e.episodes[episodeID]
			if !ok || episode.SpaceID != spaceID || !e.episodeVisibleAt(episode, req.Temporal) {
				continue
			}
			if !addNode(ProvenanceNode{ID: episode.ID, Type: "Episode", Label: episode.Kind}, 1) {
				continue
			}
			addEdge(ProvenanceEdge{ID: "prov:" + episode.ID + ":" + fact.ID, Type: "ASSERTS", FromID: episode.ID, ToID: fact.ID}, 1)
			if maxDepth < 2 {
				continue
			}
			if source, ok := e.sources[episode.SourceID]; ok {
				if source.SpaceID != spaceID || !e.sourceVisibleAt(source, req.Temporal) {
					continue
				}
				if addNode(ProvenanceNode{ID: source.ID, Type: "Source", Label: source.Kind}, 2) {
					addEdge(ProvenanceEdge{ID: "prov:" + source.ID + ":" + episode.ID, Type: "FROM_SOURCE", FromID: episode.ID, ToID: source.ID}, 2)
				}
			}
		}
		if nextID, _ := factRecord.Metadata["superseded_by"].(string); nextID != "" && maxDepth >= 1 {
			if nextFact, ok := e.facts[nextID]; ok && nextFact.SpaceID == spaceID {
				nextRecord := e.factVersionAt(nextFact, req.Temporal, index)
				if nextRecord != nil && addNode(ProvenanceNode{ID: nextRecord.ID, Type: "Fact", Label: nextRecord.Predicate, Meta: factProvenanceMeta(*nextRecord)}, 1) {
					addEdge(ProvenanceEdge{ID: "prov:" + nextFact.ID + ":" + fact.ID + ":supersedes", Type: "SUPERSEDES", FromID: nextFact.ID, ToID: fact.ID}, 1)
				}
			}
		}
		previousIDs := map[string]bool{}
		for _, previousID := range metadataStringIDs(factRecord.Metadata["supersedes"]) {
			previousIDs[previousID] = true
		}
		for _, candidate := range e.facts {
			if previousID, _ := candidate.Metadata["superseded_by"].(string); previousID == factRecord.ID {
				previousIDs[candidate.ID] = true
			}
		}
		if len(previousIDs) > 0 && maxDepth >= 1 {
			for previousID := range previousIDs {
				if previousFact, ok := e.facts[previousID]; ok && previousFact.SpaceID == spaceID {
					previousRecord := e.factVersionAt(previousFact, req.Temporal, index)
					if previousRecord != nil && addNode(ProvenanceNode{ID: previousRecord.ID, Type: "Fact", Label: previousRecord.Predicate, Meta: factProvenanceMeta(*previousRecord)}, 1) {
						addEdge(ProvenanceEdge{ID: "prov:" + fact.ID + ":" + previousFact.ID + ":supersedes", Type: "SUPERSEDES", FromID: fact.ID, ToID: previousFact.ID}, 1)
					}
				}
			}
		}
	case "entity":
		entity, ok := e.entities[req.ID]
		entityRecord := e.entityVersionAt(entity, req.Temporal, index)
		if !ok || entityRecord == nil || entityRecord.SpaceID != spaceID {
			return nil, errorf(ErrEntityNotFound, "entity not found", map[string]any{"entity_id": req.ID}, nil)
		}
		root.Label = entityRecord.CanonicalName
		nodes = append(nodes, root)
		for _, fact := range e.facts {
			factRecord := e.factVersionAt(fact, req.Temporal, index)
			if factRecord == nil || factRecord.SpaceID != spaceID || (factRecord.SubjectID != entityRecord.ID && factRecord.ObjectID != entityRecord.ID) {
				continue
			}
			if !addNode(ProvenanceNode{ID: factRecord.ID, Type: "Fact", Label: factRecord.Predicate}, 1) {
				continue
			}
			edgeType := "SUBJECT"
			if factRecord.ObjectID == entityRecord.ID {
				edgeType = "OBJECT"
			}
			addEdge(ProvenanceEdge{ID: "prov:" + factRecord.ID + ":" + entityRecord.ID, Type: edgeType, FromID: factRecord.ID, ToID: entityRecord.ID}, 1)
			if maxDepth < 2 {
				continue
			}
			for _, episodeID := range factRecord.SupportingEpisodeIDs {
				episode, ok := e.episodes[episodeID]
				if !ok || episode.SpaceID != spaceID || !e.episodeVisibleAt(episode, req.Temporal) {
					continue
				}
				if addNode(ProvenanceNode{ID: episode.ID, Type: "Episode", Label: episode.Kind}, 2) {
					addEdge(ProvenanceEdge{ID: "prov:" + episode.ID + ":" + factRecord.ID, Type: "ASSERTS", FromID: episode.ID, ToID: factRecord.ID}, 2)
				}
			}
			if nextID, _ := factRecord.Metadata["superseded_by"].(string); nextID != "" && maxDepth >= 2 {
				nextFact, ok := e.facts[nextID]
				nextRecord := e.factVersionAt(nextFact, req.Temporal, index)
				if ok && nextRecord != nil && nextRecord.SpaceID == spaceID && addNode(ProvenanceNode{ID: nextRecord.ID, Type: "Fact", Label: nextRecord.Predicate, Meta: factProvenanceMeta(*nextRecord)}, 2) {
					addEdge(ProvenanceEdge{ID: "prov:" + nextFact.ID + ":" + fact.ID + ":supersedes", Type: "SUPERSEDES", FromID: nextFact.ID, ToID: fact.ID}, 2)
				}
			}
		}
	case "episode":
		episode, ok := e.episodes[req.ID]
		if !ok || episode.SpaceID != spaceID || !e.episodeVisibleAt(episode, req.Temporal) {
			return nil, errorf(ErrQueryFailed, "episode not found", map[string]any{"episode_id": req.ID}, nil)
		}
		root.Label = episode.Kind
		nodes = append(nodes, root)
		if source, ok := e.sources[episode.SourceID]; ok {
			if source.SpaceID == spaceID && addNode(ProvenanceNode{ID: source.ID, Type: "Source", Label: source.Kind}, 1) {
				addEdge(ProvenanceEdge{ID: "prov:" + episode.ID + ":" + source.ID, Type: "FROM_SOURCE", FromID: episode.ID, ToID: source.ID}, 1)
			}
		}
		if maxDepth >= 1 {
			for _, fact := range e.facts {
				factRecord := e.factVersionAt(fact, req.Temporal, index)
				if factRecord == nil || factRecord.SpaceID != spaceID || !slices.Contains(factRecord.SupportingEpisodeIDs, episode.ID) {
					continue
				}
				if addNode(ProvenanceNode{ID: factRecord.ID, Type: "Fact", Label: factRecord.Predicate}, 1) {
					addEdge(ProvenanceEdge{ID: "prov:" + episode.ID + ":" + factRecord.ID, Type: "ASSERTS", FromID: episode.ID, ToID: factRecord.ID}, 1)
				}
			}
		}
	default:
		return nil, errorf(ErrQueryFailed, "unsupported provenance kind", map[string]any{"kind": req.Kind}, nil)
	}

	return &ProvenanceResponse{
		Meta:  newQueryMeta(req.Meta.SpaceID, e.now()),
		Root:  root,
		Nodes: dedupeProvNodes(nodes),
		Edges: dedupeProvEdges(edges),
	}, nil
}

func (e *engine) addFactSupport(included *IncludedRecords, fact Fact, scope ScopeFilter, temporal TemporalFilter, index *temporalIndex) {
	if entity, ok := e.entities[fact.SubjectID]; ok {
		if version := e.entityVersionAt(entity, temporal, index); version != nil && matchesEntityType(*version, scope.EntityTypes) {
			included.Entities = append(included.Entities, *version)
		}
	}
	if fact.ObjectID != "" {
		if entity, ok := e.entities[fact.ObjectID]; ok {
			if version := e.entityVersionAt(entity, temporal, index); version != nil && matchesEntityType(*version, scope.EntityTypes) {
				included.Entities = append(included.Entities, *version)
			}
		}
	}
	for _, episodeID := range fact.SupportingEpisodeIDs {
		if episode, ok := e.episodes[episodeID]; ok && episode.SpaceID == fact.SpaceID && e.episodeVisibleAt(episode, temporal) && matchesScopeForEpisode(episode, scope, e.sources) {
			included.Episodes = append(included.Episodes, episode)
			if source, ok := e.sources[episode.SourceID]; ok && source.SpaceID == fact.SpaceID && source.SpaceID == episode.SpaceID && e.sourceVisibleAt(source, temporal) {
				included.Sources = append(included.Sources, source)
			}
		}
	}
}

func dedupeIncluded(in IncludedRecords) IncludedRecords {
	return IncludedRecords{
		Episodes: dedupeEpisodes(in.Episodes),
		Entities: dedupeEntities(in.Entities),
		Facts:    dedupeFacts(in.Facts),
		Sources:  dedupeSources(in.Sources),
	}
}

func dedupeEpisodes(items []Episode) []Episode {
	seen := make(map[string]Episode)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]Episode, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dedupeEntities(items []Entity) []Entity {
	seen := make(map[string]Entity)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]Entity, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dedupeFacts(items []Fact) []Fact {
	seen := make(map[string]Fact)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]Fact, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dedupeSources(items []Source) []Source {
	seen := make(map[string]Source)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]Source, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dedupeProvNodes(items []ProvenanceNode) []ProvenanceNode {
	seen := make(map[string]ProvenanceNode)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]ProvenanceNode, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dedupeProvEdges(items []ProvenanceEdge) []ProvenanceEdge {
	seen := make(map[string]ProvenanceEdge)
	for _, item := range items {
		seen[item.ID] = item
	}
	out := make([]ProvenanceEdge, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func newQueryMeta(spaceID string, now time.Time) QueryResponseMeta {
	return QueryResponseMeta{
		SpaceID:    firstNonEmpty(spaceID, "default"),
		SnapshotAt: &now,
	}
}

func pageLimit(limit, total int) int {
	if limit <= 0 || limit > total {
		return total
	}
	return limit
}

func paginate[T any](items []T, offset, limit int) ([]T, string, error) {
	if offset < 0 {
		return nil, "", errorf(ErrQueryFailed, "cursor offset is invalid", nil, nil)
	}
	if offset > len(items) {
		return []T{}, "", nil
	}
	pageSize := pageLimit(limit, len(items)-offset)
	if pageSize < 0 {
		pageSize = 0
	}
	end := offset + pageSize
	page := items[offset:end]
	if end >= len(items) {
		return page, "", nil
	}
	return page, encodeCursor(end), nil
}

func decodeCursor(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	if !strings.HasPrefix(cursor, "offset:") {
		return 0, errorf(ErrQueryFailed, "cursor_invalid", map[string]any{"cursor": cursor}, nil)
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(cursor, "offset:"))
	if err != nil || offset < 0 {
		return 0, errorf(ErrQueryFailed, "cursor_invalid", map[string]any{"cursor": cursor}, nil)
	}
	return offset, nil
}

func encodeCursor(offset int) string {
	return fmt.Sprintf("offset:%d", offset)
}
