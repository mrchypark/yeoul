# Personas

This document defines the primary users of Yeoul and the product needs each persona creates.

## Persona 1: Local AI Tool Builder

### Description
Builds desktop or local-first applications that need durable memory but does not want to operate a separate graph database server.

### Goals
- Embed memory directly into a Go application
- Persist state locally
- Avoid remote infrastructure
- Control data residency
- Keep AI orchestration outside the storage engine

### Pain points
- Existing memory tools mix prompts, LLM calls, storage, and workflow orchestration
- Server-based graph databases are operationally heavy
- Vector-only memory tools are weak at time-aware factual state

### What Yeoul must provide
- Simple embedded startup
- Stable Go API
- Clear schema and retrieval primitives
- Local disk persistence
- No forced dependency on agent frameworks

## Persona 2: Agent Harness Developer

### Description
Builds a custom harness around one or more coding, research, or workflow agents and needs a memory substrate with policy-driven behavior.

### Goals
- Load skills and instruction files from the repository
- Keep agent policies version-controlled
- Keep the memory engine reusable across multiple agent types
- Avoid raw Cypher in prompts or tools

### Pain points
- Agents accumulate long-term context poorly
- Prompt-based “memory” is brittle and expensive
- Memory rules are hard to test if they are embedded in prompts

### What Yeoul must provide
- External policy packs
- Search recipes
- Explicit provenance
- Fact lifecycle APIs
- Clear separation between core engine and agent behavior

## Persona 3: Domain Knowledge System Builder

### Description
Builds a knowledge system for operations, incident management, research tracking, compliance, or engineering decision history.

### Goals
- Capture time-aware facts
- Inspect how knowledge changes over time
- Audit where facts came from
- Query historical and active states separately

### Pain points
- Traditional knowledge graphs tend to overwrite history
- Document stores make relationship queries difficult
- Many graph systems are optimized for general graph storage, not memory workflows

### What Yeoul must provide
- First-class facts
- Status transitions (active, superseded, retracted)
- Provenance graph
- Historical retrieval
- Export and audit tooling

## Persona 4: OSS Infrastructure Maintainer

### Description
Evaluates whether Yeoul can be adopted in an open-source toolchain or packaged for internal engineering use.

### Goals
- Predictable repository layout
- Strong docs
- Low operational burden
- Testability without proprietary services
- Clean dependency story

### Pain points
- Projects that depend too heavily on remote AI providers are hard to integrate
- Weak architecture boundaries make contribution difficult
- “Framework” products often resist composition

### What Yeoul must provide
- Core/adapter separation
- Non-goals clearly documented
- Deterministic local tests
- Optional, not mandatory, network service

## Persona priorities

### Must optimize for
1. Local AI Tool Builder
2. Agent Harness Developer
3. Domain Knowledge System Builder

### Must support, but not over-optimize for
4. OSS Infrastructure Maintainer

## Product implication
Yeoul should be designed first as a **reusable memory substrate**, not as a complete end-user “AI memory product”.
