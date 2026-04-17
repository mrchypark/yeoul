# Entity Resolution

Entity resolution determines when multiple observations should point to the same conceptual entity.

## Why it matters
Memory systems degrade quickly when the same entity appears under slightly different names or identifiers.

Examples:
- "yeoul"
- "Yeoul"
- "Project Yeoul"
- repository `org/yeoul`
- internal codename

These may or may not be the same entity, depending on context.

## Design goals
- support deterministic baseline deduplication
- avoid aggressive merging that destroys meaning
- allow policy-specific dedup hints
- make uncertain merges inspectable

## Resolution strategy

### Step 1: Candidate generation
Generate candidates from:
- exact identifier match
- canonical name match
- alias match
- source-specific keys
- ontology-specific dedup keys

### Step 2: Scoring
Score candidates using:
- strong identifiers
- type compatibility
- source namespace
- normalized name similarity
- context overlap

### Step 3: Decision
Possible outcomes:
- exact reuse of existing entity
- create a new entity
- link as possible duplicate
- queue for manual resolution in future tooling

## Resolution modes

### Conservative mode
Only merge on strong identifiers or clear canonical matches.
Default for MVP.

### Balanced mode
Uses both strong identifiers and high-confidence fuzzy matching.

### Aggressive mode
Useful only in controlled domains where false merges are acceptable.

## Entity keys
Per-type entity resolution should rely on type-aware keys, for example:
- Person: email, account ID, canonical name
- Repository: URL, org/name
- File: repository + path
- Task: tracker key or stable local ID
- Project: canonical slug, known aliases

## Model guidance
Entity identity should not be derived from display text alone when a more stable source key exists.

## Handling ambiguity
When confidence is insufficient:
- create a distinct entity
- optionally add `POSSIBLY_SAME_AS` relation
- preserve source-specific identifiers

This is safer than destructive over-merge.

## Resolution metadata
Each merge or match should retain:
- resolution mode
- matched keys
- confidence
- policy version
- observed source

## API implications
Yeoul should eventually expose:
- explicit entity resolution result
- merge preview
- duplicate candidate search
- controlled merge operation

## Non-goal for MVP
Fully autonomous entity mastering is out of scope.
MVP should provide deterministic and explainable entity reuse.
