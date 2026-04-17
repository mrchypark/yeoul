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
