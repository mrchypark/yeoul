# Yeoul

Yeoul is a local-first Temporal Graph Memory Engine written in Go, backed by Ladybug for all durable on-disk storage, and designed to keep AI agent behavior outside the core through external skills, instructions, ontology files, episode rules, and search recipes.

한국어 요약:

여울은 Go와 Ladybug로 구현하는 로컬 우선 Temporal Graph Memory Engine이다. durable on-disk 저장소는 Ladybug만 사용하며, Core는 AI agent 로직을 포함하지 않고 agent 전용 행동은 skill, instruction, ontology, episode rule, search recipe 파일로 외부화한다.

## Documentation

- Core and product documentation lives under [`docs/`](./docs).
- Agent usage guidance and starter policy pack live under [`agent-pack/`](./agent-pack).

## Separation Rule

```text
Core는 AI를 모른다.
Agent Pack은 Core를 사용하는 규칙만 제공한다.
```
