# Product Requirements Document: Yeoul

## Problem

Applications need a local-first way to store evolving contextual memory as temporal graph data.
Existing AI memory systems often couple storage, retrieval, agents, prompts, and LLM calls too tightly.

## Target Users

- Developers building local AI tools
- Developers building agent harnesses
- Developers building long-running assistants
- Developers building domain-specific memory systems
- Developers who want graph memory without operating a graph DB server

## Product

Yeoul is a Go library and optional local service that stores temporal graph memory using Ladybug.

## Core Capabilities

- Ingest episodes
- Store entities
- Store facts
- Store temporal relationships
- Track provenance
- Retrieve relevant context
- Load policy files
- Execute search recipes
- Export and inspect memory

## Explicit Exclusions

- No built-in LLM calls
- No built-in agent runtime
- No prompt execution
- No autonomous planning
- No mandatory network service

## Success Criteria

- Can run embedded in a Go process
- Can persist memory to a local Ladybug database
- Can reopen and query the database after restart
- Can ingest at least 100k episodes in benchmark mode
- Can retrieve context by entity, fact, time, and provenance
- Can use external skill and policy files without recompiling
