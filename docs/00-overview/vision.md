# Yeoul Vision

Yeoul is a local-first Temporal Graph Memory Engine written in Go and backed by Ladybug.

Yeoul Core does not implement agents, LLM orchestration, prompt chains, or AI decision logic.
Instead, it provides a durable temporal graph substrate for storing episodes, entities, facts,
relationships, provenance, and retrieval-oriented context.

AI-specific behavior is provided outside the core through skills, instruction files, ontology files,
episode rules, and search recipes.

The governing product rule is:

```text
Core는 AI를 모른다.
Agent Pack은 Core를 사용하는 규칙만 제공한다.
```

Ladybug is treated as an embedded property graph database that provides Cypher, pre-defined schema,
on-disk and in-memory modes, and a Go client built on top of the Ladybug C API. Yeoul therefore
assumes an embedded, schema-first, local-first architecture from the start.
