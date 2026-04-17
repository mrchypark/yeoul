# Local Storage

상태: Draft v0.1

## 목적

Yeoul의 local storage 운영 모델을 정의한다. Yeoul은 local-first 제품이므로 DB 경로, 파일 소유권, 백업, in-memory mode, lock behavior를 명확히 문서화해야 한다.

## 기본 저장 경로

기본 예시:

```text
./yeoul.lbug
```

애플리케이션별 권장:

```text
~/.local/share/yeoul/default.lbug
~/Library/Application Support/Yeoul/default.lbug
%APPDATA%\Yeoul\default.lbug
```

Go config:

```go
type Config struct {
    DatabasePath string
    InMemory bool
    ReadOnly bool
    CreateIfMissing bool
}
```

## On-disk mode

Production default다.

특징:

- 프로세스 재시작 후 데이터 유지
- 백업 가능
- migration 필요
- DB lock behavior 존재

## In-memory mode

테스트와 임시 작업용이다.

특징:

- 프로세스 종료 시 데이터 손실
- 빠른 테스트에 유리
- production memory로 사용하지 않는다

## DB ownership

### Embedded mode

Host process가 DB owner다.

```text
app process
  -> Yeoul Core
    -> Ladybug Database object
```

이 경우 다른 process가 같은 DB 파일을 직접 열지 않아야 한다.

### Daemon mode

`yeould`가 DB owner다.

```text
agent process -> HTTP/gRPC -> yeould -> Ladybug DB
cli process   -> HTTP/gRPC -> yeould -> Ladybug DB
```

multi-process가 필요하면 daemon mode를 사용한다.

## Lock behavior

DB lock conflict는 expected operational event다. Yeoul은 이를 명확히 표시해야 한다.

예:

```text
YEOL_DB_LOCKED: Database is already opened by another process.
```

복구:

1. 다른 process를 종료한다.
2. daemon mode를 사용한다.
3. read-only snapshot 또는 backup copy를 사용한다.

## Backup

### 안전한 백업 원칙

- Writer process를 종료하거나 checkpoint/snapshot 절차를 따른다.
- DB file과 schema version을 함께 기록한다.
- backup 후 reopen 검증을 수행한다.

CLI:

```bash
yeoul backup create --db ./memory.lbug --out ./backup/memory-20260417.lbug
yeoul backup verify ./backup/memory-20260417.lbug
```

## Restore

절차:

1. Yeoul process 종료
2. 현재 DB 백업
3. backup DB를 target path로 복사
4. `yeoul inspect integrity` 실행
5. schema version 확인

## Storage layout

Ladybug 버전에 따라 파일 구성은 달라질 수 있다. Yeoul은 Ladybug 공식 storage layout을 그대로 따른다. Yeoul 자체 sidecar file은 최소화한다.

가능한 sidecar:

- `yeoul.lock`은 사용하지 않는다. Ladybug lock에 의존한다.
- `yeoul.audit.jsonl`은 v0.5 이후 검토한다.
- policy files는 DB 밖에 둔다.

## Data deletion

Local storage에서 삭제는 두 종류다.

- logical deletion: status 변경, archived/retracted
- physical deletion: retention/compaction에 따른 실제 삭제

MVP는 logical deletion을 우선한다.

## 결론

Yeoul storage 운영은 단순해야 한다. Embedded mode에서는 host process가 DB owner이고, multi-process 접근이 필요하면 daemon mode로 전환한다.
