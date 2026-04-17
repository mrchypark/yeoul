# Test Plan

상태: Draft v0.1

## 목적

Yeoul MVP의 품질 보증을 위한 구체적 테스트 계획을 정의한다. 이 문서는 `testing-strategy.md`를 실행 가능한 test case 수준으로 내린다.

## Test scope

포함:

- Ladybug storage adapter
- schema migration
- episode/entity/fact model
- provenance
- fact lifecycle
- policy loader
- query API
- CLI
- local storage behavior

제외:

- cloud deployment
- hosted auth
- built-in LLM extraction
- full Graphiti parity

## TC-01. DB initialization

### Steps

1. 임시 경로 생성
2. `yeoul init --db path` 실행
3. schema version 조회
4. `yeoul inspect schema` 실행

### Expected

- DB file 생성
- schema version 존재
- 기본 node/relationship table 존재

## TC-02. Reopen persistence

### Steps

1. DB 생성
2. episode/entity/fact 입력
3. engine close
4. engine reopen
5. 입력한 데이터 조회

### Expected

- 모든 record 조회 가능
- provenance edge 유지

## TC-03. Episode ingest atomicity

### Steps

1. invalid subject entity를 포함한 fact assertion 시도
2. transaction 실패 유도
3. episode/fact partial insert 여부 확인

### Expected

- partial insert 없음
- `YEOL_DB_TX_FAILED` 또는 validation error 반환

## TC-04. Entity upsert exact match

### Steps

1. Project entity 생성
2. 같은 fingerprint로 다시 upsert

### Expected

- 새 entity 생성 없음
- existing entity 반환
- matched_by = fingerprint

## TC-05. Fact assertion provenance

### Steps

1. Source 생성
2. Episode 생성
3. Fact 생성
4. Fact provenance query 실행

### Expected

- Fact -> Episode -> Source 경로 조회 가능

## TC-06. Fact supersession

### Steps

1. active fact 생성
2. 새 fact로 supersede
3. old fact 조회
4. active fact search 실행

### Expected

- old status = superseded
- new status = active
- Supersedes relationship 존재
- 기본 active search는 new fact 우선

## TC-07. Fact retraction

### Steps

1. active fact 생성
2. retract 실행
3. active search
4. audit search

### Expected

- active search에서 제외
- audit search에서 조회
- retraction_reason 저장

## TC-08. Policy validation success

### Steps

1. valid policy pack 로드
2. ontology/rules/recipes 검증

### Expected

- no error
- recipe list 반환

## TC-09. Policy validation failure

### Steps

1. unknown predicate가 있는 recipe 생성
2. validate 실행

### Expected

- `YEOL_POLICY_VALIDATION_FAILED`
- file path와 field path 포함

## TC-10. Search recent decisions

### Steps

1. decision fact 여러 개 생성
2. old fact와 recent fact 생성
3. recent_decisions recipe 실행

### Expected

- recent active fact 우선
- scoring breakdown 포함

## TC-11. Neighborhood query

### Steps

1. Entity-Fact-Episode graph 생성
2. entity 기준 2-hop query 실행

### Expected

- 관련 fact와 episode 반환
- limit 적용

## TC-12. CLI JSON output

### Steps

1. `yeoul inspect stats --format json`
2. JSON parse

### Expected

- valid JSON
- required fields 존재

## TC-13. Multi-process lock negative test

### Steps

1. Process A가 DB open
2. Process B가 같은 DB open 시도

### Expected

- B는 lock error 또는 expected error 반환
- A는 정상 유지

## TC-14. Backup and restore

### Steps

1. DB에 데이터 입력
2. backup create
3. restore to new path
4. search 실행

### Expected

- restored DB에서 데이터 조회 가능

## TC-15. Retention dry-run

### Steps

1. old low-priority episodes 생성
2. retention dry-run 실행

### Expected

- candidate report 생성
- 실제 삭제 없음

## Test environments

- macOS arm64
- Linux x86_64
- Linux arm64 optional

Windows는 Ladybug Go binding 상태를 확인한 뒤 지원 여부를 결정한다.

## Exit criteria for MVP

- TC-01~TC-12 통과
- TC-13 documented behavior 통과
- TC-14 smoke 통과
- policy validation coverage 80% 이상
- core package no AI dependency 확인

## 결론

Yeoul MVP의 품질 기준은 feature breadth가 아니라 memory correctness다. 특히 persistence, transaction, provenance, lifecycle, policy validation을 반드시 통과해야 한다.
