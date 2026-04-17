# Design Principles

## 1. Agent-free core

The core engine must not depend on agent frameworks, LLM SDKs, prompt formats, or tool-calling protocols.

## 2. Policy outside code

Memory capture rules, ontology choices, retrieval recipes, and agent instructions must live in external files.

## 3. Temporal by default

Every episode, fact, and relationship must preserve time metadata.

## 4. Provenance by default

Every derived fact must be traceable to at least one source episode.

## 5. Local-first

The default deployment model is embedded local execution.

## 6. Query API before raw Cypher

Application code should use Yeoul APIs rather than writing raw Cypher directly.

## 7. Durable but inspectable

The database should be durable on disk, but the graph model and policy files should remain human-inspectable.

## 8. Concurrency by explicit ownership

Embedded mode assumes one process owns one READ_WRITE Ladybug database object and fans out work through multiple connections.
