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

## Identity tuple

An entity identity is the tuple (space, namespace, type, canonical name, stable key).
The derived entity ID is a hash of the exact tuple: `EntityID(namespace, type, key)` does
not include the space, so the same namespace/type/key in two spaces derives one ID.

### Drift

Drift is a difference that still refers to the same conceptual entity. Recognized forms:
- namespace or type differs only by case (e.g. `repo` vs `Repo`)
- namespace is empty on one side but populated on the other
- canonical name matches a display-name alias (e.g. `yeoul` vs `Yeoul`)
- canonical name differs only by case
- one side carries a stable key and the other does not (near-duplicate guard only)

A drifted namespace or type cannot be turned into the canonical ID by recomputation.
The derived ID is computed from the exact tuple provided, so a caller that needs the
canonical entity must use the ID returned by the resolver rather than recomputing one
from the drifted input.

### Resolution flow

1. **Resolve by identity before writing.** Run `yeoul entity resolve` (or the Go
   `ResolveEntity` API) to check whether an entity already matches the identity tuple.
   For example, resolve a keyed person with:
   `yeoul entity resolve --db memory.ltdb --namespace people --type Person --name Alex --stable-key alex-123`.
   Use the returned entity ID for the assertion; do not reconstruct an ID from a
   drifted namespace or type.
2. **Fail closed on a near duplicate.** When the derived entity ID is new but an existing
   entity already matches the same identity under a different ID, `fact assert` with
   `--upsert-subject` or `--upsert-object` fails with `YEOUL_ENTITY_NEAR_DUPLICATE`
   (exit 2) instead of creating a second entity. The caller reuses the existing entity ID.
   The guard is conservative: an exact same-scope match may reuse a legacy ID, but an
   ambiguous display-name/key or namespace/type drift requires an explicit ID and does not guess.
3. **Reconcile recorded drift explicitly.** `entity merge-preview` groups only
   canonical-name identity candidates and may report namespace/type-case or
   empty-vs-populated namespace drift for an operator to review. Alias overlap is
   checked by direct `entity merge`, not inferred by preview grouping. `entity merge`
   still refuses different populated namespaces, different types, conflicting stable
   keys, and already-marked sources; same-scope explicit name renames remain mergeable.
4. **Read merged duplicates through their canonical entity.** A fact attached to an entity
   marked `duplicate_of X` is returned when the caller filters by X. This is a read-time
   interpretation; stored facts are not rewritten.
