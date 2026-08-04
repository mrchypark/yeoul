package yeoul

import (
	"context"
	"reflect"
	"strings"
	"time"
)

// EntityID returns the deterministic entity ID generated from entity identity fields.
func EntityID(namespace, entityType, canonical string) string {
	parts := []string{normalizeIDPart(namespace), normalizeIDPart(entityType), normalizeIDPart(canonical)}
	return strings.Trim(strings.Join(parts, ":"), ":")
}

type Engine interface {
	Close(ctx context.Context) error

	IngestEpisode(ctx context.Context, input EpisodeInput) (*EpisodeResult, error)
	IngestBatch(ctx context.Context, input BatchInput) (*BatchResult, error)
	UpsertEntity(ctx context.Context, input EntityInput) (*Entity, error)
	AssertFact(ctx context.Context, input FactInput) (*Fact, error)
	SupersedeFact(ctx context.Context, factID string, input FactInput, reason string) (*SupersedeFactResult, error)
	RetractFact(ctx context.Context, factID string, reason string) (*RetractFactResult, error)

	GetRecord(ctx context.Context, req GetRecordRequest) (*GetRecordResponse, error)
	Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)
	LookupFacts(ctx context.Context, req FactLookupRequest) (*FactLookupResponse, error)
	Neighborhood(ctx context.Context, req NeighborhoodRequest) (*NeighborhoodResponse, error)
	Timeline(ctx context.Context, req TimelineRequest) (*TimelineResponse, error)
	Provenance(ctx context.Context, req ProvenanceRequest) (*ProvenanceResponse, error)

	GetEpisode(ctx context.Context, id string) (*Episode, error)
	GetEntity(ctx context.Context, id string) (*Entity, error)
	GetFact(ctx context.Context, id string) (*Fact, error)
	GetSource(ctx context.Context, id string) (*Source, error)
}

type SourceInput struct {
	ID          string         `json:"id,omitempty"`
	SpaceID     string         `json:"space_id,omitempty"`
	Kind        string         `json:"kind,omitempty"`
	URI         string         `json:"uri,omitempty"`
	ExternalRef string         `json:"external_ref,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type EpisodeInput struct {
	ID         string         `json:"id,omitempty"`
	SpaceID    string         `json:"space_id,omitempty"`
	Kind       string         `json:"kind"`
	Content    string         `json:"content"`
	SourceID   string         `json:"source_id,omitempty"`
	Source     SourceInput    `json:"source,omitempty"`
	GroupID    string         `json:"group_id,omitempty"`
	ObservedAt time.Time      `json:"observed_at,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type EntityInput struct {
	ID            string         `json:"id,omitempty"`
	SpaceID       string         `json:"space_id,omitempty"`
	Namespace     string         `json:"namespace,omitempty"`
	Type          string         `json:"type"`
	CanonicalName string         `json:"canonical_name"`
	StableKey     string         `json:"stable_key,omitempty"`
	Aliases       []string       `json:"aliases,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type FactInput struct {
	ID                   string         `json:"id,omitempty"`
	SpaceID              string         `json:"space_id,omitempty"`
	Predicate            string         `json:"predicate"`
	SubjectID            string         `json:"subject_id"`
	ObjectID             string         `json:"object_id,omitempty"`
	ValueText            string         `json:"value_text,omitempty"`
	Confidence           float64        `json:"confidence,omitempty"`
	Status               string         `json:"status,omitempty"`
	ValidFrom            time.Time      `json:"valid_from,omitempty"`
	ValidTo              time.Time      `json:"valid_to,omitempty"`
	ObservedAt           time.Time      `json:"observed_at,omitempty"`
	SupportingEpisodeIDs []string       `json:"supporting_episode_ids,omitempty"`
	Cardinality          string         `json:"cardinality,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
}

type Source struct {
	ID          string         `json:"id"`
	SpaceID     string         `json:"space_id,omitempty"`
	Kind        string         `json:"kind,omitempty"`
	URI         string         `json:"uri,omitempty"`
	ExternalRef string         `json:"external_ref,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

type Episode struct {
	ID         string         `json:"id"`
	SpaceID    string         `json:"space_id,omitempty"`
	Kind       string         `json:"kind"`
	Content    string         `json:"content"`
	SourceID   string         `json:"source_id,omitempty"`
	GroupID    string         `json:"group_id,omitempty"`
	ObservedAt time.Time      `json:"observed_at,omitempty"`
	IngestedAt time.Time      `json:"ingested_at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Entity struct {
	ID            string         `json:"id"`
	SpaceID       string         `json:"space_id,omitempty"`
	Namespace     string         `json:"namespace,omitempty"`
	Type          string         `json:"type"`
	CanonicalName string         `json:"canonical_name"`
	Aliases       []string       `json:"aliases,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type Fact struct {
	ID                   string         `json:"id"`
	SpaceID              string         `json:"space_id,omitempty"`
	Predicate            string         `json:"predicate"`
	SubjectID            string         `json:"subject_id"`
	ObjectID             string         `json:"object_id,omitempty"`
	ValueText            string         `json:"value_text,omitempty"`
	Confidence           float64        `json:"confidence,omitempty"`
	Status               string         `json:"status"`
	ValidFrom            time.Time      `json:"valid_from,omitempty"`
	ValidTo              time.Time      `json:"valid_to,omitempty"`
	ObservedAt           time.Time      `json:"observed_at,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	RetractedAt          time.Time      `json:"retracted_at,omitempty"`
	RetractionReason     string         `json:"retraction_reason,omitempty"`
	SupportingEpisodeIDs []string       `json:"supporting_episode_ids,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
}

type MigrationWatermark struct {
	ID        string         `json:"id"`
	AppliedAt time.Time      `json:"applied_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type FactRevision struct {
	ID                   string         `json:"id"`
	FactID               string         `json:"fact_id"`
	SpaceID              string         `json:"space_id,omitempty"`
	RevisionKind         string         `json:"revision_kind"`
	TxTime               time.Time      `json:"tx_time"`
	Predicate            string         `json:"predicate"`
	SubjectID            string         `json:"subject_id"`
	ObjectID             string         `json:"object_id,omitempty"`
	ValueText            string         `json:"value_text,omitempty"`
	Confidence           float64        `json:"confidence,omitempty"`
	Status               string         `json:"status"`
	ValidFrom            time.Time      `json:"valid_from,omitempty"`
	ValidTo              time.Time      `json:"valid_to,omitempty"`
	ObservedAt           time.Time      `json:"observed_at,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	RetractedAt          time.Time      `json:"retracted_at,omitempty"`
	RetractionReason     string         `json:"retraction_reason,omitempty"`
	SupportingEpisodeIDs []string       `json:"supporting_episode_ids,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
}

type EntityRevision struct {
	ID            string         `json:"id"`
	EntityID      string         `json:"entity_id"`
	SpaceID       string         `json:"space_id,omitempty"`
	RevisionKind  string         `json:"revision_kind"`
	TxTime        time.Time      `json:"tx_time"`
	Namespace     string         `json:"namespace,omitempty"`
	Type          string         `json:"type"`
	CanonicalName string         `json:"canonical_name"`
	Aliases       []string       `json:"aliases,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type EpisodeResult struct {
	EpisodeID string `json:"episode_id"`
	SourceID  string `json:"source_id,omitempty"`
	Created   bool   `json:"created"`
}

type BatchInput struct {
	Episodes        []EpisodeInput   `json:"episodes,omitempty"`
	Entities        []EntityInput    `json:"entities,omitempty"`
	Facts           []FactInput      `json:"facts,omitempty"`
	EntityRevisions []EntityRevision `json:"entity_revisions,omitempty"`
	FactRevisions   []FactRevision   `json:"fact_revisions,omitempty"`
}

type BatchResult struct {
	EpisodeIDs []string `json:"episode_ids,omitempty"`
	EntityIDs  []string `json:"entity_ids,omitempty"`
	FactIDs    []string `json:"fact_ids,omitempty"`
}

type SupersedeFactResult struct {
	OldFactID string `json:"old_fact_id"`
	NewFactID string `json:"new_fact_id"`
}

type RetractFactResult struct {
	FactID string `json:"fact_id"`
	Status string `json:"status"`
}

type QueryMeta struct {
	RequestID string            `json:"request_id,omitempty"`
	SpaceID   string            `json:"space_id,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
}

type ScopeFilter struct {
	GroupIDs    []string `json:"group_ids,omitempty"`
	SourceKinds []string `json:"source_kinds,omitempty"`
	SourceIDs   []string `json:"source_ids,omitempty"`
	EntityTypes []string `json:"entity_types,omitempty"`
	FactStatus  []string `json:"fact_status,omitempty"`
}

type TemporalFilter struct {
	AsOf            *time.Time `json:"as_of,omitempty"`
	ValidAt         *time.Time `json:"valid_at,omitempty"`
	ObservedFrom    *time.Time `json:"observed_from,omitempty"`
	ObservedTo      *time.Time `json:"observed_to,omitempty"`
	ValidFrom       *time.Time `json:"valid_from,omitempty"`
	ValidTo         *time.Time `json:"valid_to,omitempty"`
	IncludeInactive bool       `json:"include_inactive,omitempty"`
}

type Page struct {
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type Include struct {
	Provenance         bool `json:"provenance,omitempty"`
	SupportingFacts    bool `json:"supporting_facts,omitempty"`
	SupportingEpisodes bool `json:"supporting_episodes,omitempty"`
	RelatedEntities    bool `json:"related_entities,omitempty"`
	Snippets           bool `json:"snippets,omitempty"`
}

type QueryResponseMeta struct {
	RequestID   string     `json:"request_id,omitempty"`
	SpaceID     string     `json:"space_id,omitempty"`
	SnapshotAt  *time.Time `json:"snapshot_at,omitempty"`
	NextCursor  string     `json:"next_cursor,omitempty"`
	TotalApprox *int64     `json:"total_approx,omitempty"`
}

type GetRecordRequest struct {
	Meta     QueryMeta      `json:"meta,omitempty"`
	Kind     string         `json:"kind"`
	ID       string         `json:"id"`
	Temporal TemporalFilter `json:"temporal,omitempty"`
	Include  Include        `json:"include,omitempty"`
}

type GetRecordResponse struct {
	Meta   QueryResponseMeta `json:"meta"`
	Kind   string            `json:"kind"`
	Record any               `json:"record"`
}

type SearchMode string

const (
	SearchModeHybrid   SearchMode = "hybrid"
	SearchModeKeyword  SearchMode = "keyword"
	SearchModeSemantic SearchMode = "semantic"
)

type SearchRequest struct {
	Meta       QueryMeta      `json:"meta,omitempty"`
	QueryText  string         `json:"query_text"`
	Mode       SearchMode     `json:"mode,omitempty"`
	Scope      ScopeFilter    `json:"scope,omitempty"`
	Temporal   TemporalFilter `json:"temporal,omitempty"`
	AnchorIDs  []string       `json:"anchor_ids,omitempty"`
	Predicates []string       `json:"predicates,omitempty"`
	Types      []string       `json:"types,omitempty"`
	MinScore   *float64       `json:"min_score,omitempty"`
	Include    Include        `json:"include,omitempty"`
	Page       Page           `json:"page,omitempty"`
}

type SearchHit struct {
	HitID       string   `json:"hit_id"`
	HitType     string   `json:"hit_type"`
	RecordID    string   `json:"record_id"`
	Score       float64  `json:"score"`
	MatchedText string   `json:"matched_text,omitempty"`
	Reasons     []string `json:"reasons,omitempty"`
}

type IncludedRecords struct {
	Episodes []Episode `json:"episodes,omitempty"`
	Entities []Entity  `json:"entities,omitempty"`
	Facts    []Fact    `json:"facts,omitempty"`
	Sources  []Source  `json:"sources,omitempty"`
}

type SearchResponse struct {
	Meta     QueryResponseMeta `json:"meta"`
	Hits     []SearchHit       `json:"hits"`
	Included IncludedRecords   `json:"included,omitempty"`
}

type FactLookupRequest struct {
	Meta       QueryMeta      `json:"meta,omitempty"`
	Scope      ScopeFilter    `json:"scope,omitempty"`
	Temporal   TemporalFilter `json:"temporal,omitempty"`
	SubjectIDs []string       `json:"subject_ids,omitempty"`
	Predicates []string       `json:"predicates,omitempty"`
	ObjectIDs  []string       `json:"object_ids,omitempty"`
	ObjectText string         `json:"object_text,omitempty"`
	Include    Include        `json:"include,omitempty"`
	Page       Page           `json:"page,omitempty"`
}

type FactLookupResponse struct {
	Meta     QueryResponseMeta `json:"meta"`
	Facts    []Fact            `json:"facts"`
	Included IncludedRecords   `json:"included,omitempty"`
}

type NeighborhoodRequest struct {
	Meta      QueryMeta      `json:"meta,omitempty"`
	Scope     ScopeFilter    `json:"scope,omitempty"`
	Temporal  TemporalFilter `json:"temporal,omitempty"`
	AnchorIDs []string       `json:"anchor_ids"`
	MaxHops   int            `json:"max_hops,omitempty"`
	EdgeTypes []string       `json:"edge_types,omitempty"`
	NodeTypes []string       `json:"node_types,omitempty"`
	MaxNodes  int            `json:"max_nodes,omitempty"`
	Include   Include        `json:"include,omitempty"`
}

type GraphNode struct {
	ID    string         `json:"id"`
	Type  string         `json:"type"`
	Label string         `json:"label,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

type GraphEdge struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	FromID string         `json:"from_id"`
	ToID   string         `json:"to_id"`
	Meta   map[string]any `json:"meta,omitempty"`
}

type NeighborhoodResponse struct {
	Meta  QueryResponseMeta `json:"meta"`
	Nodes []GraphNode       `json:"nodes"`
	Edges []GraphEdge       `json:"edges"`
}

type TimelineRequest struct {
	Meta       QueryMeta      `json:"meta,omitempty"`
	Scope      ScopeFilter    `json:"scope,omitempty"`
	AnchorIDs  []string       `json:"anchor_ids,omitempty"`
	EventTypes []string       `json:"event_types,omitempty"`
	Temporal   TemporalFilter `json:"temporal,omitempty"`
	Descending bool           `json:"descending,omitempty"`
	Page       Page           `json:"page,omitempty"`
}

type TimelineEvent struct {
	EventID    string         `json:"event_id"`
	EventType  string         `json:"event_type"`
	RecordType string         `json:"record_type"`
	RecordID   string         `json:"record_id"`
	Timestamp  time.Time      `json:"timestamp"`
	Summary    string         `json:"summary,omitempty"`
	Meta       map[string]any `json:"meta,omitempty"`
}

type TimelineResponse struct {
	Meta   QueryResponseMeta `json:"meta"`
	Events []TimelineEvent   `json:"events"`
}

type ProvenanceRequest struct {
	Meta     QueryMeta      `json:"meta,omitempty"`
	Kind     string         `json:"kind"`
	ID       string         `json:"id"`
	Temporal TemporalFilter `json:"temporal,omitempty"`
	MaxDepth int            `json:"max_depth,omitempty"`
}

type ProvenanceNode struct {
	ID    string         `json:"id"`
	Type  string         `json:"type"`
	Label string         `json:"label,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

type ProvenanceEdge struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
}

type ProvenanceResponse struct {
	Meta  QueryResponseMeta `json:"meta"`
	Root  ProvenanceNode    `json:"root"`
	Nodes []ProvenanceNode  `json:"nodes"`
	Edges []ProvenanceEdge  `json:"edges"`
}

// Equal는 두 Source가 동일한지 비교합니다.
func (s Source) Equal(other Source) bool {
	return s.ID == other.ID &&
		s.SpaceID == other.SpaceID &&
		s.Kind == other.Kind &&
		s.URI == other.URI &&
		s.ExternalRef == other.ExternalRef &&
		s.CreatedAt.Equal(other.CreatedAt) &&
		mapsEqual(s.Metadata, other.Metadata)
}

// Equal는 두 Episode가 동일한지 비교합니다.
func (e Episode) Equal(other Episode) bool {
	return e.ID == other.ID &&
		e.SpaceID == other.SpaceID &&
		e.Kind == other.Kind &&
		e.Content == other.Content &&
		e.SourceID == other.SourceID &&
		e.GroupID == other.GroupID &&
		e.ObservedAt.Equal(other.ObservedAt) &&
		e.IngestedAt.Equal(other.IngestedAt) &&
		mapsEqual(e.Metadata, other.Metadata)
}

// Equal는 두 Entity가 동일한지 비교합니다.
func (e Entity) Equal(other Entity) bool {
	return e.ID == other.ID &&
		e.SpaceID == other.SpaceID &&
		e.Namespace == other.Namespace &&
		e.Type == other.Type &&
		e.CanonicalName == other.CanonicalName &&
		slicesEqual(e.Aliases, other.Aliases) &&
		e.CreatedAt.Equal(other.CreatedAt) &&
		e.UpdatedAt.Equal(other.UpdatedAt) &&
		mapsEqual(e.Metadata, other.Metadata)
}

// Equal는 두 Fact가 동일한지 비교합니다.
func (f Fact) Equal(other Fact) bool {
	return f.ID == other.ID &&
		f.SpaceID == other.SpaceID &&
		f.Predicate == other.Predicate &&
		f.SubjectID == other.SubjectID &&
		f.ObjectID == other.ObjectID &&
		f.ValueText == other.ValueText &&
		f.Confidence == other.Confidence &&
		f.Status == other.Status &&
		f.ValidFrom.Equal(other.ValidFrom) &&
		f.ValidTo.Equal(other.ValidTo) &&
		f.ObservedAt.Equal(other.ObservedAt) &&
		f.CreatedAt.Equal(other.CreatedAt) &&
		f.UpdatedAt.Equal(other.UpdatedAt) &&
		f.RetractedAt.Equal(other.RetractedAt) &&
		f.RetractionReason == other.RetractionReason &&
		slicesEqual(f.SupportingEpisodeIDs, other.SupportingEpisodeIDs) &&
		mapsEqual(f.Metadata, other.Metadata)
}

// Equal는 두 FactRevision이 동일한지 비교합니다.
func (r FactRevision) Equal(other FactRevision) bool {
	return r.ID == other.ID &&
		r.FactID == other.FactID &&
		r.SpaceID == other.SpaceID &&
		r.RevisionKind == other.RevisionKind &&
		r.TxTime.Equal(other.TxTime) &&
		r.Predicate == other.Predicate &&
		r.SubjectID == other.SubjectID &&
		r.ObjectID == other.ObjectID &&
		r.ValueText == other.ValueText &&
		r.Confidence == other.Confidence &&
		r.Status == other.Status &&
		r.ValidFrom.Equal(other.ValidFrom) &&
		r.ValidTo.Equal(other.ValidTo) &&
		r.ObservedAt.Equal(other.ObservedAt) &&
		r.CreatedAt.Equal(other.CreatedAt) &&
		r.UpdatedAt.Equal(other.UpdatedAt) &&
		r.RetractedAt.Equal(other.RetractedAt) &&
		r.RetractionReason == other.RetractionReason &&
		slicesEqual(r.SupportingEpisodeIDs, other.SupportingEpisodeIDs) &&
		mapsEqual(r.Metadata, other.Metadata)
}

// Equal는 두 EntityRevision이 동일한지 비교합니다.
func (r EntityRevision) Equal(other EntityRevision) bool {
	return r.ID == other.ID &&
		r.EntityID == other.EntityID &&
		r.SpaceID == other.SpaceID &&
		r.RevisionKind == other.RevisionKind &&
		r.TxTime.Equal(other.TxTime) &&
		r.Namespace == other.Namespace &&
		r.Type == other.Type &&
		r.CanonicalName == other.CanonicalName &&
		slicesEqual(r.Aliases, other.Aliases) &&
		r.CreatedAt.Equal(other.CreatedAt) &&
		r.UpdatedAt.Equal(other.UpdatedAt) &&
		mapsEqual(r.Metadata, other.Metadata)
}

// Equal는 두 MigrationWatermark가 동일한지 비교합니다.
func (m MigrationWatermark) Equal(other MigrationWatermark) bool {
	return m.ID == other.ID &&
		m.AppliedAt.Equal(other.AppliedAt) &&
		mapsEqual(m.Metadata, other.Metadata)
}

// mapsEqual는 두 맵이 동일한지 비교합니다.
func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || !interfaceEqual(v, bv) {
			return false
		}
	}
	return true
}

// slicesEqual는 두 문자열 슬라이스가 동일한지 비교합니다.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// interfaceEqual는 두 interface{}가 동일한지 비교합니다.
func interfaceEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}
