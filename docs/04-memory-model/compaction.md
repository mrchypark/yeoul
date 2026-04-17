# Compaction

Compaction reduces redundant graph state without sacrificing provenance or temporal meaning.

## Why compaction exists
Yeoul will naturally accumulate:
- repeated episodes
- duplicate facts derived from similar text
- duplicate entities
- stale intermediate derivation artifacts

Compaction is needed for storage health and retrieval quality.

## What compaction is allowed to do
- merge duplicate or near-duplicate entities when safe
- collapse redundant facts with identical semantics and compatible provenance
- archive low-value episodes according to retention policy
- rebuild or prune derived helper structures

## What compaction must not do
- delete the only provenance path for an active fact
- overwrite history
- remove lifecycle edges needed to explain change over time
- silently change semantic meaning

## Compaction classes

### Entity compaction
Merge entities that have been determined to represent the same thing.

### Fact compaction
Consolidate multiple equivalent active or historical facts into a canonical surviving fact when policy allows it.

### Episode compaction
Archive or compress low-value episodes while preserving references for active facts.

### Derived-state compaction
Rebuild helper structures, summaries, and indexes from durable source state.

## Safety rules
- compaction must be reversible where practical, or at least auditable
- every compaction job should record its input and output counts
- dry-run mode should exist
- compaction should prefer additive changes until trust is established

## Suggested implementation approach
1. scan for candidates
2. create a compaction plan
3. run validation checks
4. apply compaction transactionally
5. emit a compaction report
6. update metrics

## Interaction with retention
Retention may mark data eligible for archive or deletion.
Compaction is the mechanism that executes some of those policy decisions.

## Operator-facing outputs
Compaction should produce:
- candidate count
- merged count
- skipped count
- reason breakdown
- integrity warnings

## Non-goal for MVP
Aggressive semantic compaction is not required in MVP.
MVP may limit compaction to duplicate entities, duplicate exact facts, and archive transitions.
