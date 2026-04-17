# Plugin and Extension Model

상태: Draft v0.1

## 목적

Yeoul Core를 작고 안정적으로 유지하면서도, ingestion source, extraction strategy, ranking, policy pack, service adapter를 확장할 수 있는 구조를 정의한다. 핵심 원칙은 **Core는 AI를 모른다**는 것이다.

## 확장 모델 개요

Yeoul의 extension은 runtime plugin system보다 **package-level extension**과 **policy file extension**을 우선한다. 초기에는 동적 플러그인 로딩을 도입하지 않는다. Go에서 동적 plugin은 운영체제/빌드 제약이 크고, Yeoul의 local-first 배포 경험을 해칠 수 있다.

## Extension 종류

### 1. Storage adapter

기본은 Ladybug다. 향후 SQLite fallback이나 다른 graph backend를 도입할 수 있도록 interface는 둔다.

```go
type Store interface {
    Open(ctx context.Context, cfg StoreConfig) error
    Close(ctx context.Context) error
    Migrate(ctx context.Context) error
    Execute(ctx context.Context, query Query) (Result, error)
    Tx(ctx context.Context, fn func(Tx) error) error
}
```

제약:

- MVP에서는 Ladybug만 구현한다.
- Core business logic이 raw Cypher에 직접 의존하지 않도록 한다.
- Storage adapter는 policy/agent package를 import하면 안 된다.

### 2. Source adapter

Source adapter는 외부 입력을 Yeoul EpisodeInput으로 변환한다. 예: file, GitHub issue, email, chat transcript, local note.

```go
type SourceAdapter interface {
    Name() string
    Read(ctx context.Context, ref SourceRef) ([]EpisodeInput, error)
}
```

제약:

- Source adapter는 Core 밖에 위치한다.
- Source adapter가 직접 DB에 쓰지 않고 Core API를 호출한다.
- Source adapter는 provenance metadata를 반드시 제공해야 한다.

### 3. Extraction adapter

Yeoul Core는 AI extraction을 내장하지 않는다. 그러나 외부 adapter는 text에서 entity/fact candidate를 만들 수 있다.

```go
type Extractor interface {
    Extract(ctx context.Context, ep EpisodeInput, ontology Ontology) (*ExtractionResult, error)
}
```

종류:

- rule-based extractor
- regex extractor
- external LLM extractor
- domain-specific parser

제약:

- Extractor는 optional이다.
- LLM extractor는 Core package 안에 들어가면 안 된다.
- Extraction result는 Core의 validated input으로 변환되어야 한다.

### 4. Ranking adapter

Retrieval result를 정렬하는 score function을 확장할 수 있다.

```go
type Ranker interface {
    Rank(ctx context.Context, candidates []Candidate, req SearchRequest) ([]ScoredResult, error)
}
```

MVP ranker:

- recency
- graph distance
- confidence
- provenance strength

확장 ranker:

- semantic similarity
- user feedback
- domain priority

### 5. Policy pack

Policy pack은 동적 코드가 아니라 파일 묶음이다.

```text
policies/{name}/
  SKILL.md
  agent_instructions.md
  ontology.yaml
  episode_rules.yaml
  search_recipes.yaml
```

제약:

- Policy pack은 Core correctness를 바꾸지 않는다.
- Policy pack은 ingest/search behavior를 조정한다.
- Policy schema validation이 필요하다.

### 6. Service adapter

Embedded API 외에 CLI, HTTP/gRPC daemon, MCP adapter를 제공할 수 있다.

제약:

- 모든 adapter는 같은 Core API를 사용한다.
- Daemon mode에서 DB owner는 daemon 하나다.
- MCP adapter는 Agent Pack 영역에 속하며 Core가 아니다.

## Extension loading strategy

### MVP

- Go package import 방식
- YAML/Markdown policy loading
- No dynamic binary plugin loading

### Post-MVP

- WASM plugin 검토
- external extractor process 검토
- gRPC-based plugin 검토

## Extension boundary rules

1. Core는 Agent Pack을 import하지 않는다.
2. Storage adapter는 policy를 import하지 않는다.
3. Policy loader는 storage 내부 query를 직접 만들지 않는다.
4. Integration adapter는 Core public API를 통해서만 저장한다.
5. Extension failure는 Core database corruption으로 이어지면 안 된다.

## Plugin registry

MVP에서는 단순 registry를 둔다.

```go
type Registry struct {
    Sources map[string]SourceAdapter
    Extractors map[string]Extractor
    Rankers map[string]Ranker
}
```

Registry는 optional이며, embedded user가 직접 dependency injection할 수 있어야 한다.

## Versioning

각 extension은 다음 metadata를 가져야 한다.

```yaml
name: github-source-adapter
version: 0.1.0
yeoul_core_min: 0.2.0
capabilities:
  - source.read
  - episode.create
```

## 보안 고려

- Dynamic plugin execution은 MVP에서 제외한다.
- External adapter가 가져온 데이터는 policy validation과 redaction hook을 거쳐야 한다.
- Agent-facing adapter는 raw Cypher 실행 권한을 갖지 않는다.

## 결론

Yeoul의 확장성은 “Core를 크게 만드는 것”이 아니라 “Core 밖에서 source/extraction/ranking/policy를 조립할 수 있게 하는 것”이다. MVP에서는 package-level extension과 policy files로 충분하다.
