# Ladybug Cypher DDL

상태: Draft v0.1

## 목적

Yeoul MVP가 Ladybug에 생성할 graph schema의 초안을 정의한다. 실제 Ladybug 문법과 타입 지원은 Phase 0 harness에서 검증 후 조정한다.

## 설계 원칙

1. Episode, Entity, Fact, Source를 node table로 둔다.
2. Provenance와 fact structure는 relationship table로 표현한다.
3. Fact는 edge가 아니라 node다.
4. 시간 필드는 문자열 ISO-8601 또는 TIMESTAMP로 저장한다. 실제 타입은 Ladybug Go binding 검증 후 결정한다.
5. JSON은 MVP에서 STRING으로 저장한다.

## Node tables

### Source

```cypher
CREATE NODE TABLE Source(
  id STRING PRIMARY KEY,
  kind STRING,
  uri STRING,
  external_ref STRING,
  created_at STRING,
  metadata_json STRING
);
```

### Episode

```cypher
CREATE NODE TABLE Episode(
  id STRING PRIMARY KEY,
  kind STRING,
  content STRING,
  content_hash STRING,
  source_id STRING,
  group_id STRING,
  observed_at STRING,
  ingested_at STRING,
  metadata_json STRING
);
```

### Entity

```cypher
CREATE NODE TABLE Entity(
  id STRING PRIMARY KEY,
  namespace STRING,
  type STRING,
  canonical_name STRING,
  aliases_json STRING,
  fingerprint STRING,
  status STRING,
  created_at STRING,
  updated_at STRING,
  metadata_json STRING
);
```

### Fact

```cypher
CREATE NODE TABLE Fact(
  id STRING PRIMARY KEY,
  predicate STRING,
  value_text STRING,
  confidence DOUBLE,
  status STRING,
  valid_from STRING,
  valid_to STRING,
  observed_at STRING,
  created_at STRING,
  updated_at STRING,
  retracted_at STRING,
  retraction_reason STRING,
  metadata_json STRING
);
```

### SchemaVersion

```cypher
CREATE NODE TABLE SchemaVersion(
  id STRING PRIMARY KEY,
  version INT64,
  applied_at STRING,
  metadata_json STRING
);
```

## Relationship tables

### Episode from Source

```cypher
CREATE REL TABLE FromSource(
  FROM Episode TO Source
);
```

### Episode mentions Entity

```cypher
CREATE REL TABLE Mentions(
  FROM Episode TO Entity,
  confidence DOUBLE,
  observed_at STRING
);
```

### Episode asserts Fact

```cypher
CREATE REL TABLE Asserts(
  FROM Episode TO Fact,
  confidence DOUBLE,
  observed_at STRING
);
```

### Fact subject

```cypher
CREATE REL TABLE Subject(
  FROM Fact TO Entity
);
```

### Fact object

```cypher
CREATE REL TABLE Object(
  FROM Fact TO Entity
);
```

### Fact derived from Episode

```cypher
CREATE REL TABLE DerivedFrom(
  FROM Fact TO Episode,
  method STRING,
  confidence DOUBLE
);
```

### Fact supersedes Fact

```cypher
CREATE REL TABLE Supersedes(
  FROM Fact TO Fact,
  reason STRING,
  created_at STRING
);
```

### Fact contradicts Fact

```cypher
CREATE REL TABLE Contradicts(
  FROM Fact TO Fact,
  reason STRING,
  created_at STRING
);
```

### Entity related to Entity

```cypher
CREATE REL TABLE RelatedTo(
  FROM Entity TO Entity,
  predicate STRING,
  confidence DOUBLE,
  observed_at STRING,
  metadata_json STRING
);
```

### Entity merged into Entity

```cypher
CREATE REL TABLE MergedInto(
  FROM Entity TO Entity,
  reason STRING,
  merged_at STRING
);
```

## Inserts

### Source + Episode

```cypher
CREATE (s:Source {
  id: $source_id,
  kind: $source_kind,
  uri: $source_uri,
  external_ref: $external_ref,
  created_at: $created_at,
  metadata_json: $source_metadata_json
});

CREATE (e:Episode {
  id: $episode_id,
  kind: $kind,
  content: $content,
  content_hash: $content_hash,
  source_id: $source_id,
  group_id: $group_id,
  observed_at: $observed_at,
  ingested_at: $ingested_at,
  metadata_json: $metadata_json
});

MATCH (e:Episode {id: $episode_id}), (s:Source {id: $source_id})
CREATE (e)-[:FromSource]->(s);
```

### Entity upsert pattern

Ladybug upsert syntax support는 검증 필요. MVP adapter는 먼저 lookup 후 create/update를 분기한다.

```cypher
MATCH (e:Entity {fingerprint: $fingerprint})
RETURN e;
```

없으면:

```cypher
CREATE (e:Entity {
  id: $entity_id,
  namespace: $namespace,
  type: $type,
  canonical_name: $canonical_name,
  aliases_json: $aliases_json,
  fingerprint: $fingerprint,
  status: 'active',
  created_at: $now,
  updated_at: $now,
  metadata_json: $metadata_json
});
```

### Fact assertion

```cypher
CREATE (f:Fact {
  id: $fact_id,
  predicate: $predicate,
  value_text: $value_text,
  confidence: $confidence,
  status: 'active',
  valid_from: $valid_from,
  valid_to: $valid_to,
  observed_at: $observed_at,
  created_at: $now,
  updated_at: $now,
  metadata_json: $metadata_json
});

MATCH (f:Fact {id: $fact_id}), (s:Entity {id: $subject_id})
CREATE (f)-[:Subject]->(s);

MATCH (f:Fact {id: $fact_id}), (o:Entity {id: $object_id})
CREATE (f)-[:Object]->(o);

MATCH (e:Episode {id: $episode_id}), (f:Fact {id: $fact_id})
CREATE (e)-[:Asserts {confidence: $confidence, observed_at: $observed_at}]->(f);
```

## Query examples

### Active facts for entity

```cypher
MATCH (f:Fact)-[:Subject]->(e:Entity {id: $entity_id})
WHERE f.status = 'active'
RETURN f
ORDER BY f.observed_at DESC
LIMIT $limit;
```

### Fact provenance

```cypher
MATCH (src:Source)<-[:FromSource]-(ep:Episode)-[:Asserts]->(f:Fact {id: $fact_id})
RETURN f, ep, src;
```

### Recent decision facts

```cypher
MATCH (f:Fact)-[:Subject]->(e:Entity)
WHERE f.status = 'active'
  AND f.predicate IN $predicates
  AND f.observed_at >= $after
RETURN f, e
ORDER BY f.observed_at DESC
LIMIT $limit;
```

## Migration note

Ladybug의 실제 DDL syntax와 index syntax는 버전별 차이가 있을 수 있으므로, 이 파일은 implementation target이며 Phase 0에서 검증 후 수정한다.

## 결론

이 DDL은 Yeoul의 최소 temporal graph memory schema다. 복잡한 domain ontology는 schema table을 늘리는 대신 Entity.type, Fact.predicate, metadata, policy validation으로 처리한다.
