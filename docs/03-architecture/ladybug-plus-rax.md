# Ladybug Plus Rax Architecture

This document defines the recommended Yeoul integration model when using both Ladybug and `rax`.

The decision is:

```text
Use both systems, but do not give them equal truth authority.
Ladybug remains canonical truth.
Rax remains a derived retrieval and runtime layer.
```

## 1. Summary

Yeoul should use:

- Ladybug for canonical temporal memory
- `rax` for derived retrieval and runtime acceleration

Yeoul should not:

- treat `rax` as a second canonical memory store
- move Yeoul fact lifecycle semantics into `rax` as a prerequisite for progress
- force `Ladybug` and `rax` to converge into one storage engine before retrieval work can advance

## 2. Why this architecture exists

Yeoul and `rax` are strong in different places.

Yeoul already has:

- episode, entity, fact, and source primitives
- provenance
- temporal validity
- fact lifecycle semantics such as supersede and retract
- a local-first embedded graph storage model

`rax` now has stronger building blocks for:

- packed runtime boundaries
- text and vector execution lanes
- hybrid search fusion
- product CLI and MCP surfaces
- raw ingest and runtime-style search flows

The integration goal is to combine these strengths without collapsing the memory model into the search runtime.

## 3. Core rule

The most important integration rule is:

```text
Dual store is acceptable.
Dual truth is not.
```

That means:

- Ladybug stores canonical memory state
- `rax` stores or materializes derived retrieval state
- every search hit from `rax` must still resolve back to canonical Yeoul records

## 4. Responsibility split

### 4.1 Ladybug responsibilities

Ladybug remains responsible for:

- durable storage
- graph structure
- temporal semantics
- provenance
- lifecycle state
- canonical identifiers for episodes, entities, facts, and sources

### 4.2 Rax responsibilities

`rax` is responsible for:

- text retrieval execution
- vector retrieval execution
- hybrid fusion
- optional search-side metadata filtering
- runtime-oriented search ergonomics
- optional product-facing retrieval runtime experiments

### 4.3 Yeoul integration responsibilities

Yeoul's integration layer is responsible for:

- converting canonical records into retrieval projections
- publishing or rebuilding those projections into `rax`
- turning `rax` hits back into canonical Yeoul records
- applying temporal and provenance-aware rerank rules after raw retrieval

## 5. Data flow

The intended flow is:

1. write canonical memory into Ladybug
2. derive retrieval projection records from canonical memory
3. publish projection records into `rax`
4. execute text, vector, or hybrid search in `rax`
5. map returned hits back to canonical Yeoul ids
6. hydrate those ids from Ladybug
7. construct final Yeoul search responses with provenance and temporal context

## 6. Projection model

The integration should use an explicit Yeoul-owned projection model.

This keeps `rax` reusable while protecting Yeoul's semantics.

### 6.1 Projection record types

Recommended projection record types:

- `episode_doc`
- `fact_doc`
- `entity_doc`
- `decision_doc`

### 6.2 Required fields

Each projection record should include:

- canonical Yeoul record id
- projection type
- searchable text
- facet metadata
- temporal metadata
- graph hints
- provenance hints

### 6.3 Example facets

Useful projection fields include:

- `space_id`
- `group_id`
- `source_kind`
- `entity_type`
- `predicate`
- `status`
- `subject_id`
- `object_id`
- `supporting_episode_ids`

### 6.4 Example temporal fields

Useful temporal fields include:

- `observed_at`
- `valid_from`
- `valid_to`
- `is_active`
- `is_retracted`
- `is_superseded`

## 7. Query flow

The Yeoul search path should become two-stage.

### Stage 1: candidate retrieval

Use `rax` to return ranked candidate ids.

This stage is optimized for:

- recall
- latency
- hybrid text and vector fusion

### Stage 2: canonical hydration and rerank

Use Ladybug and Yeoul core logic to:

- hydrate facts, episodes, entities, and sources
- enforce temporal visibility
- prefer active facts
- expose provenance
- apply Yeoul-specific rerank signals

This stage is optimized for:

- correctness
- explainability
- temporal fidelity

## 8. Integration commands

The recommended Yeoul CLI additions are:

- `yeoul index build`
- `yeoul index rebuild`
- `yeoul index verify`
- `yeoul index status`
- `yeoul index publish-rax`

These commands should manage the Yeoul-to-`rax` projection boundary.

### 8.1 Example flows

Normal search should let Yeoul manage the bundled rax FFI runtime and projection automatically:

```bash
yeoul search --db "$YEOUL_DB" --query "prior decisions about retrieval"
```

The explicit index commands are for inspection, verification, benchmarking, or operator-controlled publishing:

```bash
yeoul index build --db "$YEOUL_DB" --root "$YEOUL_INDEX_ROOT"
yeoul index verify --db "$YEOUL_DB" --root "$YEOUL_INDEX_ROOT"
yeoul index publish-rax --root "$YEOUL_INDEX_ROOT" --store "$YEOUL_RAX_STORE"
```

Normal users should not install or run `rax` separately. Yeoul release archives include
the rax 0.4.4 FFI runtime beside the `yeoul` binary. The first native integration keeps
Ladybug search as the correctness filter and uses rax candidate order as an automatic
retrieval signal before deeper candidate hydration is added.

## 9. Why not move Ladybug semantics into rax now

It is tempting to push Yeoul's lifecycle and provenance model into `rax`.
That is not the recommended first move.

Reasons:

- it duplicates hard memory semantics already present in Yeoul
- it turns `rax` into a second evolving memory core
- it increases migration cost if the search runtime changes later
- it mixes retrieval experimentation with canonical memory correctness

The first objective should be to integrate `rax` as a retrieval substrate, not to convert it into a second Yeoul.

## 10. When deeper convergence becomes reasonable

Deeper convergence should be reconsidered only if `rax` grows first-class support for:

- temporal validity windows
- lifecycle transitions such as retract and supersede
- provenance sets richer than a single source timestamp pair
- graph-aware retrieval over canonical memory objects
- query contracts that can express Yeoul-style memory visibility rules

Until then, the clean split remains preferable.

## 11. Risks

### 11.1 Drift between canonical and derived stores

If projection publish or rebuild is weak, `rax` results may drift from Ladybug truth.

Mitigation:

- explicit rebuild command
- explicit verification command
- canonical hydration before final response

### 11.2 Scope inflation

If Yeoul starts adopting broad `rax` product scope directly, the retrieval layer may begin dictating the memory model.

Mitigation:

- keep the projection Yeoul-owned
- keep `rax` behind an adapter
- keep Yeoul query semantics defined in Yeoul docs and APIs

### 11.3 Confusing write ownership

If both systems can accept canonical writes directly, the architecture becomes ambiguous.

Mitigation:

- canonical writes go only to Ladybug-backed Yeoul
- `rax` receives only derived or staged retrieval data

## 12. Recommended implementation order

1. define Yeoul projection schemas
2. add Yeoul index build and verify commands
3. add a `rax` adapter behind a Yeoul-owned interface
4. route Yeoul search through `rax` candidate retrieval plus Ladybug hydration
5. add temporal and provenance-aware rerank diagnostics

The first `rax` adapter targets the rax 0.4.4 FFI document-ingest and search surface.
Explicit index commands can inspect `projection.ndjson`; managed search cache rebuilds stream the same projection as JSONL bytes into the rax FFI adapter and keep only the manifest plus `.rax` store.

## 13. One-line summary

Keep Ladybug as Yeoul's memory truth, and use `rax` as a replaceable retrieval engine around that truth.
