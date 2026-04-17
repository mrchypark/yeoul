# Conceptual Model

Yeoul stores memory as a temporal graph.

## Episode

An observed unit of input.

Examples:

- chat message
- meeting note
- issue update
- document summary
- tool result
- user preference update

## Entity

A thing that can be referred to repeatedly.

Examples:

- person
- project
- file
- repository
- task
- system
- organization

## Fact

A claim derived from one or more episodes.
Facts are first-class nodes because they need time, confidence, status, and provenance.

## Source

The origin of an episode or fact.

Examples:

- chat thread
- document
- issue
- commit
- email
- local file

## Relationship

A typed edge connecting episodes, entities, facts, and sources.

## Temporal Metadata

Every record should support:

- observed_at
- ingested_at
- valid_from
- valid_to
- expired_at
- superseded_by
