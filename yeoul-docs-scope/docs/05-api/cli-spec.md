# CLI Specification

상태: Draft v0.1

## 목적

Yeoul CLI는 개발, 검증, inspection, benchmark, 운영 보조를 위한 도구다. CLI는 Core API의 사용자 친화적 facade이며, raw Cypher shell이 아니다.

## 명령 구조

```bash
yeoul <command> [subcommand] [flags]
```

공통 flags:

```bash
--db ./yeoul.lbug
--format table|json|yaml
--quiet
--verbose
--trace
```

## `yeoul init`

DB와 schema를 초기화한다.

```bash
yeoul init --db ./memory.lbug
```

동작:

1. DB 파일이 없으면 생성한다.
2. schema migration을 실행한다.
3. schema version을 기록한다.
4. health check를 수행한다.

옵션:

```bash
--force
--in-memory
--schema-version latest
```

## `yeoul migrate`

Schema migration을 실행한다.

```bash
yeoul migrate --db ./memory.lbug
yeoul migrate status --db ./memory.lbug
```

출력:

- current version
- available migrations
- pending migrations
- destructive 여부

## `yeoul ingest episode`

Episode를 입력한다.

```bash
yeoul ingest episode --kind note --file ./note.md --source local-file:note.md
yeoul ingest episode --kind chat --text "Yeoul Core will not include agent logic."
```

옵션:

```bash
--observed-at 2026-04-17T12:00:00+09:00
--group-id project:yeoul
--metadata ./metadata.json
--policy ./policies/default
```

## `yeoul entity`

Entity 조회와 관리.

```bash
yeoul entity get ent_01HX...
yeoul entity list --type Project
yeoul entity search "Yeoul"
yeoul entity candidates --type Person
yeoul entity merge ent_a ent_b --dry-run
```

## `yeoul fact`

Fact 조회와 lifecycle 관리.

```bash
yeoul fact get fact_01HX...
yeoul fact search --subject ent_01HX --predicate USES
yeoul fact retract fact_01HX --reason "incorrect extraction"
yeoul fact supersede fact_old --new ./fact.json
```

Destructive 또는 lifecycle-changing 명령은 기본적으로 confirmation을 요구한다.

```bash
--yes
--dry-run
```

## `yeoul search`

Memory 검색.

```bash
yeoul search "what did we decide about Ladybug?"
yeoul search "agent-free core" --recipe recent_context
yeoul search "Yeoul" --entity-type Project --window 30d
```

옵션:

```bash
--recipe recent_context
--limit 10
--window 30d
--include-superseded
--include-retracted
--explain
--policy ./policies/default
```

`--explain` 출력은 scoring breakdown을 포함한다.

## `yeoul neighborhood`

Entity 주변 그래프 조회.

```bash
yeoul neighborhood ent_01HX --hops 2
yeoul neighborhood --query "Ladybug" --hops 1
```

옵션:

```bash
--types Entity,Fact,Episode
--edge-types SUBJECT,OBJECT,ASSERTS
--limit 100
```

## `yeoul inspect`

DB 상태 점검.

```bash
yeoul inspect schema
yeoul inspect stats
yeoul inspect sources
yeoul inspect indexes
yeoul inspect integrity
```

## `yeoul policy`

Policy pack 검증.

```bash
yeoul policy validate ./policies/default
yeoul policy explain ./policies/default
yeoul policy recipes ./policies/default
```

## `yeoul compact`

Memory compaction.

```bash
yeoul compact --dry-run
yeoul compact episodes --older-than 180d --dry-run
yeoul compact entities --candidates-only
```

## `yeoul bench`

성능 검증.

```bash
yeoul bench ingest --episodes 100000
yeoul bench search --queries ./queries.jsonl
yeoul bench concurrency --readers 10 --writers 1
```

## `yeoul export`

데이터 내보내기.

```bash
yeoul export --format jsonl --out ./export.jsonl
yeoul export facts --status active --out ./facts.jsonl
```

## `yeoul backup`

DB 백업.

```bash
yeoul backup create --out ./backup/yeoul-20260417.lbug
yeoul backup verify ./backup/yeoul-20260417.lbug
```

## Output formats

### Table

사람이 보기 좋은 기본 출력.

### JSON

자동화용 출력. 모든 CLI 명령은 JSON output을 지원해야 한다.

### YAML

정책/디버깅 문서와 잘 맞는 출력.

## Error behavior

- CLI exit code는 error model을 따른다.
- JSON output에서는 `error.code`, `error.message`, `error.details`를 포함한다.
- DB lock conflict는 별도 code로 반환한다.

## 결론

CLI는 Yeoul의 제품 품질을 빠르게 검증하는 핵심 도구다. MVP부터 init, migrate, ingest, search, inspect, policy validate, bench를 제공한다.
