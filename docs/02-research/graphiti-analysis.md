# Graphiti Analysis

This document explains what Yeoul should learn from Graphiti and what it should intentionally leave behind.

## Why Graphiti matters

Graphiti demonstrates a useful framing: memory is not just vector retrieval or a static knowledge graph.
It is a temporal context graph that preserves how facts change over time and where they came from.

That framing remains valid for Yeoul.

## What Yeoul should keep conceptually

### 1. Episode-centric ingest
Graphiti treats an episode as the input unit. This is useful because it gives Yeoul a durable source-layer primitive that is stable across ontology changes.

### 2. Temporal facts
Graphiti's strongest idea is that facts change over time and memory should preserve this rather than flattening it.

### 3. Provenance
Being able to explain why a fact exists is essential for debugging, trust, replay, and compaction.

### 4. Retrieval as graph-aware context reconstruction
Graphiti is useful not because it stores a graph, but because it uses the graph to retrieve meaningful context.

## What Yeoul should not copy

### 1. Agent-centric framing
Graphiti presents itself as a framework for AI agents. Yeoul should not make agents a core concept.

### 2. Python-first framework assumptions
Yeoul should not assume a Python runtime, Python dependency graph, or framework-style extension pattern.

### 3. LLM behavior inside the core
Graphiti is designed in a world where extraction and retrieval are closely associated with AI pipelines.
Yeoul should keep extraction policy and agent behavior outside the engine.

### 4. Framework sprawl
Yeoul should resist becoming a broad orchestration framework.

## Comparative design summary

| Area | Graphiti | Yeoul |
|---|---|---|
| Core identity | AI agent memory framework | Temporal graph memory engine |
| Runtime language | Python | Go |
| Storage backend | Pluggable / graph backends | Ladybug first |
| Agent behavior | Central | Externalized |
| LLM calls | Common in workflow | Explicitly outside core |
| Policy files | Useful but secondary | First-class integration layer |
| Embedding mode | Possible | Primary |

## Derived design decisions

### DD-1
Episodes must remain durable even if entity or fact derivation changes.

### DD-2
Facts must be first-class objects with temporal and provenance fields.

### DD-3
Agent behavior should be represented through files, not engine code.

### DD-4
Search should be graph-aware, but application-facing APIs should remain domain-oriented instead of exposing the graph database directly.

## Practical consequence for implementation
Yeoul is best viewed as a **memory kernel** that can support agent systems, not as an agent framework in itself.
