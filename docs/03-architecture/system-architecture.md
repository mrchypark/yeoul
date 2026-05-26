# System Architecture

Yeoul consists of four layers:

## 1. Storage Layer

Backed by Ladybug.
Responsible for database lifecycle, schema migration, query execution, and transactions.

## 2. Memory Core

Responsible for episodes, entities, facts, relationships, temporal metadata, provenance, and retrieval primitives.

## 3. Policy Layer

Loads external YAML and Markdown policy files.
It does not make AI calls.
It converts policy declarations into ingest and search behavior.

## 4. Integration Layer

Optional adapters:

- CLI
- HTTP and gRPC service
- MCP adapter
- AI agent instruction pack

Only the Storage and Memory Core layers are required.

## Governing Rule

Yeoul Core must remain usable without any policy files and without any AI-specific packages.

## v0.2 Direction

The current system architecture remains valid for the core system boundary.
However, the intended v0.2 product shape adds stronger retrieval, agent, and evaluation layers around the same core.

See [v0.2-product-architecture.md](./v0.2-product-architecture.md) for the recommended product-facing layering:

- core as canonical temporal truth
- retrieval as hybrid search, rerank, and constructor
- agent as CLI and integration ergonomics
- evaluation as a first-class quality gate
