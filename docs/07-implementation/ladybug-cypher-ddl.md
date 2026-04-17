# Ladybug Cypher DDL

This document records the initial schema DDL for Yeoul on Ladybug.
It is intentionally conservative and can be evolved through migrations.

## Principles
- stable primary keys for every main node table
- explicit node tables and relationship tables
- properties stored on Facts rather than forcing every semantic relation into a direct edge
- keep Source, Episode, Entity, and Fact separate

## Initial DDL

```cypher
CREATE NODE TABLE Source(
  id STRING PRIMARY KEY,
  kind STRING,
  uri STRING,
  external_ref STRING,
  created_at TIMESTAMP,
  metadata_json STRING
);

CREATE NODE TABLE Episode(
  id STRING PRIMARY KEY,
  kind STRING,
  content STRING,
  content_hash STRING,
  source_id STRING,
  group_id STRING,
  observed_at TIMESTAMP,
  ingested_at TIMESTAMP,
  metadata_json STRING
);

CREATE NODE TABLE Entity(
  id STRING PRIMARY KEY,
  type STRING,
  canonical_name STRING,
  aliases_json STRING,
  fingerprint STRING,
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  metadata_json STRING
);

CREATE NODE TABLE Fact(
  id STRING PRIMARY KEY,
  predicate STRING,
  value_text STRING,
  confidence DOUBLE,
  status STRING,
  valid_from TIMESTAMP,
  valid_to TIMESTAMP,
  observed_at TIMESTAMP,
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  metadata_json STRING
);
```

## Relationship tables

```cypher
CREATE REL TABLE FROM_SOURCE(
  FROM Episode TO Source,
  created_at TIMESTAMP
);

CREATE REL TABLE MENTIONS(
  FROM Episode TO Entity,
  role STRING,
  created_at TIMESTAMP
);

CREATE REL TABLE ASSERTS(
  FROM Episode TO Fact,
  created_at TIMESTAMP
);

CREATE REL TABLE SUBJECT(
  FROM Fact TO Entity,
  created_at TIMESTAMP
);

CREATE REL TABLE OBJECT_ENTITY(
  FROM Fact TO Entity,
  created_at TIMESTAMP
);

CREATE REL TABLE DERIVED_FROM(
  FROM Fact TO Episode,
  extractor_kind STRING,
  policy_version STRING,
  created_at TIMESTAMP
);

CREATE REL TABLE SUPPORTED_BY(
  FROM Fact TO Episode,
  support_kind STRING,
  created_at TIMESTAMP
);

CREATE REL TABLE SUPERSEDES(
  FROM Fact TO Fact,
  reason STRING,
  created_at TIMESTAMP
);

CREATE REL TABLE CONTRADICTS(
  FROM Fact TO Fact,
  created_at TIMESTAMP
);

CREATE REL TABLE POSSIBLY_SAME_AS(
  FROM Entity TO Entity,
  confidence DOUBLE,
  created_at TIMESTAMP
);

CREATE REL TABLE RELATED_TO(
  FROM Entity TO Entity,
  relation STRING,
  created_at TIMESTAMP
);
```

## Optional later additions
- vector-bearing helper tables
- derived summary nodes
- derivation run nodes
- retention or archive marker nodes

## Migration note
This schema should be installed through migration files, not only through ad hoc runtime execution.
