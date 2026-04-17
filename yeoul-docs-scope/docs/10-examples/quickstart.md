# Quickstart

상태: Draft v0.1

## 목적

Yeoul을 로컬 Go 프로젝트에서 embedded memory engine으로 사용하는 기본 흐름을 보여준다.

## 설치

```bash
go mod init example.com/yeoul-demo
go get github.com/your-org/yeoul
```

Ladybug Go binding 설치와 native library 요구사항은 실제 release 문서에 맞춰 보강한다.

## DB 초기화

```bash
yeoul init --db ./memory.lbug
```

또는 Go 코드에서 초기화한다.

```go
package main

import (
    "context"
    "log"

    "github.com/your-org/yeoul/pkg/yeoul"
)

func main() {
    ctx := context.Background()

    eng, err := yeoul.Open(ctx, yeoul.Config{
        DatabasePath: "./memory.lbug",
        CreateIfMissing: true,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer eng.Close(ctx)
}
```

## Episode 입력

```go
result, err := eng.IngestEpisode(ctx, yeoul.EpisodeInput{
    Kind: "chat_message",
    Content: "Yeoul Core will use Ladybug and will not include AI agent logic.",
    Source: yeoul.SourceInput{
        Kind: "chat",
        ExternalRef: "thread-001",
    },
    GroupID: "project:yeoul",
})
if err != nil {
    log.Fatal(err)
}

log.Println(result.EpisodeID)
```

## Entity와 Fact 입력

```go
project, _ := eng.UpsertEntity(ctx, yeoul.EntityInput{
    Type: "Project",
    Namespace: "default",
    CanonicalName: "Yeoul",
    Aliases: []string{"여울"},
})

storage, _ := eng.UpsertEntity(ctx, yeoul.EntityInput{
    Type: "Database",
    Namespace: "default",
    CanonicalName: "Ladybug",
})

fact, err := eng.AssertFact(ctx, yeoul.FactInput{
    SubjectID: project.ID,
    Predicate: "USES_STORAGE",
    ObjectID: storage.ID,
    ValueText: "Yeoul uses Ladybug as its storage engine.",
    EpisodeID: result.EpisodeID,
    Confidence: 0.95,
})
if err != nil {
    log.Fatal(err)
}

log.Println(fact.ID)
```

## 검색

```go
search, err := eng.Search(ctx, yeoul.SearchRequest{
    Query: "What storage engine does Yeoul use?",
    Limit: 5,
    Include: yeoul.SearchInclude{
        Facts: true,
        Episodes: true,
        Sources: true,
        Scoring: true,
    },
})
if err != nil {
    log.Fatal(err)
}

for _, hit := range search.Results {
    log.Println(hit.Text, hit.Score)
}
```

## CLI 검색

```bash
yeoul search "What storage engine does Yeoul use?" --db ./memory.lbug --explain
```

## Policy pack 사용

```bash
yeoul policy validate ./policies/default
yeoul search "recent Yeoul decisions" --policy ./policies/default --recipe recent_decisions
```

## 주의

- Core는 AI extraction을 하지 않는다.
- Agent가 추출한 entity/fact를 Core API에 넣을 수 있다.
- 여러 프로세스가 같은 DB 파일에 직접 쓰지 않는다.
- multi-process가 필요하면 `yeould`를 사용한다.

## 다음 단계

- `example-skill.md`로 agent 사용 규칙을 본다.
- `example-ontology.md`로 domain ontology를 정의한다.
- `example-ingest-workflow.md`로 episode→entity→fact 흐름을 확인한다.
