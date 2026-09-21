# Yeoul Memory SkillOpt: Entity Promotion

## Scope

This report records aggregate evaluation evidence for the Yeoul memory instructions. It contains no raw session text, private session content, session identifiers, or user names. Intentional product and repository-relative references identify the evaluated artifact; no absolute local filesystem paths are included.

The optimized artifact is `SKILL.md` in the `yeoul-memory` skill, which now lives in the global skill repository the active host loads. The target model, harness, tools, and scoring rubric remained fixed during baseline, candidate selection, and slow update.

## Corpus evidence

- Total session records inspected: 4,314
- Records mentioning the global database: 214
- Records containing search activity: 202
- Search-only records: 196
- Episode writes: 6
- Fact writes: 1
- Object upsert was rare

The aggregate indicates that search-first behavior was common while durable structured writes, especially object-backed relationships, were uncommon.

## Database evidence

- All records: 131 episodes, 46 entities, 43 facts
- Evaluated group: 96 episodes, 27 facts
- Object-backed facts in the evaluated group: 0 of 27
- Exact duplicate entity clusters found: 1

These counts are diagnostic evidence, not a claim that every episode should have become a fact.

## Selection result

| Stage | Score | Status |
| --- | ---: | --- |
| Baseline | 41/100 | Recorded |
| Candidate | 94/100 | Selection improvement |
| Slow update | 100/100 | Accepted for repository implementation |
| Final held-out | 100/100 | No regressions or hard failures |

- Selection set: 12 cases
- Score history: 41 -> 94 -> 100 -> held-out 100
- Final held-out result: 100/100, with no regressions or hard failures
- Real target-tool smoke: passed after repository propagation on a disposable database

## Accepted changes

1. Run a mandatory end-of-cycle classification: no memory, episode-only, new fact, or lifecycle change.
2. Do not stop after episode ingest for an authorized confirmed durable claim merely because its entity is missing; select canonical identity and reuse or upsert subject/object entities.
3. Distinguish reusable object entities from scalar or opaque text, preserve lifecycle and temporal semantics, and require read-back closure.
4. Keep privacy and write authority explicit, especially for delegated agents and shared databases.

## Preservation constraints

- Yeoul Core remains a memory substrate, not an automatic extraction or agent runtime.
- Policy YAML remains behavioral guidance, not an automatic fact-promotion engine.
- Every fact requires supporting episode provenance.
- Transient, ambiguous, exploratory, or unsupported material remains episode-only or is not stored. Secrets, credentials, and private keys are always omitted; sensitive personal or customer data requires explicit authorization for a defined scope and must be minimized or redacted.
- Ladybug-backed records remain canonical; derived retrieval state is not a second source of truth.
- A `SUPERSEDES` assertion never replaces the lifecycle command.

## Verification boundary

The held-out evaluation and real disposable smoke completed successfully. The content-contract check verifies only that critical guidance remains present across deployment surfaces; it does not evaluate agent behavior or substitute for the executable smoke.
