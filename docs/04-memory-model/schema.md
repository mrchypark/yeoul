# Memory Schema

Yeoul targets a schema-first property graph model.

## Node Tables

### Episode

- id STRING PRIMARY KEY
- kind STRING
- content STRING
- content_hash STRING
- source_id STRING
- group_id STRING
- observed_at TIMESTAMP
- ingested_at TIMESTAMP
- metadata_json STRING

### Entity

- id STRING PRIMARY KEY
- type STRING
- canonical_name STRING
- aliases_json STRING
- fingerprint STRING
- created_at TIMESTAMP
- updated_at TIMESTAMP
- metadata_json STRING

### Fact

- id STRING PRIMARY KEY
- predicate STRING
- value_text STRING
- confidence DOUBLE
- status STRING
- valid_from TIMESTAMP
- valid_to TIMESTAMP
- observed_at TIMESTAMP
- created_at TIMESTAMP
- updated_at TIMESTAMP
- metadata_json STRING

### Source

- id STRING PRIMARY KEY
- kind STRING
- uri STRING
- external_ref STRING
- created_at TIMESTAMP
- metadata_json STRING

## Relationship Tables

### Mentions

Episode -> Entity

### Asserts

Episode -> Fact

### Subject

Fact -> Entity

### Object

Fact -> Entity

### DerivedFrom

Fact -> Episode

### Supersedes

Fact -> Fact

### RelatedTo

Entity -> Entity

## Design Note

Because Ladybug uses a schema-first graph model, Yeoul treats schema definition as a first-class design artifact rather than an implementation afterthought.
