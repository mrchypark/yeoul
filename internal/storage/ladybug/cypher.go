package ladybug

import (
	"fmt"
	"sort"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

// This file is the single owner of raw Cypher statements for the Ladybug
// backend. Public packages and the CLI must not construct Cypher text;
// they use the statement and query builders below.

// NodeRecord describes one node for statement generation. Props maps a
// property name to an already-rendered Cypher literal.
type NodeRecord struct {
	Label string
	ID    string
	Props map[string]string
}

// RelationshipSpec describes one relationship for statement generation.
// FromID or ToID may be empty to match any node of that label.
type RelationshipSpec struct {
	FromLabel string
	FromID    string
	Type      string
	ToLabel   string
	ToID      string
	Props     map[string]string
}

// DDLStatements returns the Yeoul graph schema DDL.
func DDLStatements() []string {
	return []string{
		"CREATE NODE TABLE IF NOT EXISTS YeoulMeta(id STRING, sequence INT64, PRIMARY KEY(id))",
		"CREATE NODE TABLE IF NOT EXISTS Source(id STRING, space_id STRING, kind STRING, uri STRING, external_ref STRING, created_at TIMESTAMP, metadata_json STRING, PRIMARY KEY(id))",
		"CREATE NODE TABLE IF NOT EXISTS Episode(id STRING, space_id STRING, kind STRING, content STRING, content_hash STRING, source_id STRING, group_id STRING, observed_at TIMESTAMP, ingested_at TIMESTAMP, metadata_json STRING, PRIMARY KEY(id))",
		"CREATE NODE TABLE IF NOT EXISTS Entity(id STRING, space_id STRING, namespace STRING, type STRING, canonical_name STRING, aliases_json STRING, fingerprint STRING, created_at TIMESTAMP, updated_at TIMESTAMP, metadata_json STRING, PRIMARY KEY(id))",
		"CREATE NODE TABLE IF NOT EXISTS Fact(id STRING, space_id STRING, predicate STRING, value_text STRING, confidence DOUBLE, status STRING, valid_from TIMESTAMP, valid_to TIMESTAMP, observed_at TIMESTAMP, created_at TIMESTAMP, updated_at TIMESTAMP, retracted_at TIMESTAMP, retraction_reason STRING, metadata_json STRING, PRIMARY KEY(id))",
		"CREATE NODE TABLE IF NOT EXISTS YeoulMigration(id STRING, applied_at TIMESTAMP, metadata_json STRING, PRIMARY KEY(id))",
		"CREATE NODE TABLE IF NOT EXISTS EntityRevision(id STRING, entity_id STRING, space_id STRING, revision_kind STRING, tx_time TIMESTAMP, namespace STRING, type STRING, canonical_name STRING, aliases_json STRING, created_at TIMESTAMP, updated_at TIMESTAMP, metadata_json STRING, PRIMARY KEY(id))",
		"CREATE NODE TABLE IF NOT EXISTS FactRevision(id STRING, fact_id STRING, space_id STRING, revision_kind STRING, tx_time TIMESTAMP, predicate STRING, subject_id STRING, object_id STRING, value_text STRING, confidence DOUBLE, status STRING, valid_from TIMESTAMP, valid_to TIMESTAMP, observed_at TIMESTAMP, created_at TIMESTAMP, updated_at TIMESTAMP, retracted_at TIMESTAMP, retraction_reason STRING, supporting_episode_ids_json STRING, metadata_json STRING, PRIMARY KEY(id))",
		"CREATE REL TABLE IF NOT EXISTS FROM_SOURCE(FROM Episode TO Source, created_at TIMESTAMP)",
		"CREATE REL TABLE IF NOT EXISTS ASSERTS(FROM Episode TO Fact, created_at TIMESTAMP)",
		"CREATE REL TABLE IF NOT EXISTS SUBJECT(FROM Fact TO Entity, created_at TIMESTAMP)",
		"CREATE REL TABLE IF NOT EXISTS OBJECT_ENTITY(FROM Fact TO Entity, created_at TIMESTAMP)",
		"CREATE REL TABLE IF NOT EXISTS SUPPORTED_BY(FROM Fact TO Episode, support_kind STRING, created_at TIMESTAMP)",
		"CREATE REL TABLE IF NOT EXISTS SUPERSEDES(FROM Fact TO Fact, reason STRING, created_at TIMESTAMP)",
	}
}

// CreateNode renders a CREATE statement for one node record.
func CreateNode(record NodeRecord) string {
	parts := nodeClauses(record)
	return fmt.Sprintf("CREATE (:%s {%s})", record.Label, strings.Join(parts, ", "))
}

// UpdateNode renders a MATCH+SET statement for one node record.
func UpdateNode(record NodeRecord) string {
	names := make([]string, 0, len(record.Props))
	for name := range record.Props {
		if name == "id" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	setParts := make([]string, 0, len(names))
	for _, name := range names {
		setParts = append(setParts, fmt.Sprintf("n.%s=%s", name, record.Props[name]))
	}
	if len(setParts) == 0 {
		return fmt.Sprintf("MATCH (n:%s {id:%s})", record.Label, StringLiteral(record.ID))
	}
	return fmt.Sprintf("MATCH (n:%s {id:%s}) SET %s", record.Label, StringLiteral(record.ID), strings.Join(setParts, ", "))
}

// DeleteNode renders a MATCH+DELETE statement for one node record.
func DeleteNode(label, id string) string {
	return fmt.Sprintf("MATCH (n:%s {id:%s}) DELETE n", label, StringLiteral(id))
}

// DeleteMetaSequence renders the deletion of the singleton meta node.
func DeleteMetaSequence() string {
	return "MATCH (m:YeoulMeta {id:'singleton'}) DELETE m"
}

// CreateMetaSequence renders the creation of the singleton meta node.
func CreateMetaSequence(sequence uint64) string {
	return fmt.Sprintf("CREATE (:YeoulMeta {id:'singleton', sequence:%s})", Uint64Literal(sequence))
}

// CreateRelationship renders a MATCH-two-nodes + CREATE relationship
// statement. Both FromID and ToID must be non-empty.
func CreateRelationship(spec RelationshipSpec) string {
	props := propClauses(spec.Props)
	if props == "" {
		return fmt.Sprintf(
			"MATCH (a:%s {id:%s}), (b:%s {id:%s}) CREATE (a)-[:%s]->(b)",
			spec.FromLabel, StringLiteral(spec.FromID),
			spec.ToLabel, StringLiteral(spec.ToID),
			spec.Type,
		)
	}
	return fmt.Sprintf(
		"MATCH (a:%s {id:%s}), (b:%s {id:%s}) CREATE (a)-[:%s {%s}]->(b)",
		spec.FromLabel, StringLiteral(spec.FromID),
		spec.ToLabel, StringLiteral(spec.ToID),
		spec.Type,
		props,
	)
}

// DeleteRelationship renders a MATCH+DELETE relationship statement. An
// empty FromID or ToID matches any node of that label.
func DeleteRelationship(spec RelationshipSpec) string {
	return fmt.Sprintf(
		"MATCH (%s)-[r:%s]->(%s) DELETE r",
		nodePattern(spec.FromLabel, spec.FromID),
		spec.Type,
		nodePattern(spec.ToLabel, spec.ToID),
	)
}

// QueryNodes returns a query that returns all nodes of a label.
func QueryNodes(label string) string {
	return fmt.Sprintf("MATCH (n:%s) RETURN n", label)
}

// QueryMetaSequence returns a query for the singleton meta sequence value.
func QueryMetaSequence() string {
	return "MATCH (m:YeoulMeta {id: 'singleton'}) RETURN m.sequence"
}

// QuerySubjectEdges returns the SUBJECT edges of all facts.
func QuerySubjectEdges() string {
	return "MATCH (f:Fact)-[:SUBJECT]->(e:Entity) RETURN f.id, e.id"
}

// QueryObjectEdges returns the OBJECT_ENTITY edges of all facts.
func QueryObjectEdges() string {
	return "MATCH (f:Fact)-[:OBJECT_ENTITY]->(e:Entity) RETURN f.id, e.id"
}

// QuerySupportedByEdges returns the SUPPORTED_BY edges of all facts.
func QuerySupportedByEdges() string {
	return "MATCH (f:Fact)-[:SUPPORTED_BY]->(e:Episode) RETURN f.id, e.id"
}

// QuerySupersedesEdges returns the SUPERSEDES edges between facts.
func QuerySupersedesEdges() string {
	return "MATCH (newFact:Fact)-[r:SUPERSEDES]->(oldFact:Fact) RETURN newFact.id, oldFact.id, r.reason"
}

// QueryVersion returns the Ladybug engine version.
func QueryVersion() string {
	return "CALL db_version() RETURN *"
}

// QueryTables returns the catalog tables of the database.
func QueryTables() string {
	return "CALL show_tables() RETURN *"
}

// QueryCount returns a query that counts all nodes of a label.
func QueryCount(label string) string {
	return fmt.Sprintf("MATCH (n:%s) RETURN count(n)", label)
}

// QueryAllIDs returns a query that returns the id column of all nodes of a
// label, aliased to "id".
func QueryAllIDs(label string) string {
	return fmt.Sprintf("MATCH (n:%s) RETURN n.id AS id", label)
}

// QuerySourceRefs returns compact source rows, aliased for CLI export.
func QuerySourceRefs() string {
	return "MATCH (s:Source) RETURN s.id AS id, s.kind AS kind, s.uri AS uri, s.external_ref AS external_ref"
}

// QueryEntityRevisionRows returns all entity revision rows, aliased for
// CLI export.
func QueryEntityRevisionRows() string {
	return "MATCH (r:EntityRevision) RETURN r.id AS id, r.entity_id AS entity_id, r.space_id AS space_id, r.revision_kind AS revision_kind, r.tx_time AS tx_time, r.namespace AS namespace, r.type AS type, r.canonical_name AS canonical_name, r.aliases_json AS aliases_json, r.created_at AS created_at, r.updated_at AS updated_at, r.metadata_json AS metadata_json"
}

// QueryFactRevisionRows returns all fact revision rows, aliased for CLI
// export.
func QueryFactRevisionRows() string {
	return "MATCH (r:FactRevision) RETURN r.id AS id, r.fact_id AS fact_id, r.space_id AS space_id, r.revision_kind AS revision_kind, r.tx_time AS tx_time, r.predicate AS predicate, r.subject_id AS subject_id, r.object_id AS object_id, r.value_text AS value_text, r.confidence AS confidence, r.status AS status, r.valid_from AS valid_from, r.valid_to AS valid_to, r.observed_at AS observed_at, r.created_at AS created_at, r.updated_at AS updated_at, r.retracted_at AS retracted_at, r.retraction_reason AS retraction_reason, r.supporting_episode_ids_json AS supporting_episode_ids_json, r.metadata_json AS metadata_json"
}

// IsMissingTableError reports whether err is a missing-table binder error
// from Ladybug.
func IsMissingTableError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Binder exception: Table ")
}

// StringLiteral renders a Cypher string literal.
func StringLiteral(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

// JSONLiteral renders a value as a Cypher string literal of its JSON
// encoding.
func JSONLiteral(value any) string {
	data, _ := json.Marshal(value)
	return StringLiteral(string(data))
}

// TimeLiteral renders a Cypher timestamp literal, or NULL for a zero time.
func TimeLiteral(value time.Time) string {
	if value.IsZero() {
		return "NULL"
	}
	return fmt.Sprintf("timestamp(%s)", StringLiteral(value.UTC().Format(time.RFC3339Nano)))
}

// FloatLiteral renders a Cypher float literal.
func FloatLiteral(value float64) string {
	return fmt.Sprintf("%g", value)
}

// Uint64Literal renders a Cypher integer literal.
func Uint64Literal(value uint64) string {
	return fmt.Sprintf("%d", value)
}

func nodeClauses(record NodeRecord) []string {
	parts := make([]string, 0, len(record.Props)+1)
	parts = append(parts, "id:"+StringLiteral(record.ID))
	names := make([]string, 0, len(record.Props))
	for name := range record.Props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s:%s", name, record.Props[name]))
	}
	return parts
}

func propClauses(props map[string]string) string {
	parts := make([]string, 0, len(props))
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s:%s", name, props[name]))
	}
	return strings.Join(parts, ", ")
}

func nodePattern(label, id string) string {
	if id == "" {
		return ":" + label
	}
	return fmt.Sprintf(":%s {id:%s}", label, StringLiteral(id))
}
