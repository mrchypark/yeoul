# Temporal Semantics

## Time Fields

### observed_at

When the information was observed in the source world.

### ingested_at

When Yeoul ingested the information.

### valid_from

When a fact becomes valid.

### valid_to

When a fact stops being valid.

### updated_at

When Yeoul last wrote the fact record.

### retracted_at

When Yeoul retracted a fact. Set only for retracted facts.

## Fact Status

Yeoul implements exactly three fact statuses:

- `active`
- `superseded`
- `retracted`

Other lifecycle states discussed elsewhere (`contradicted`, `uncertain`,
`archived`, and an `expired_at` field) are not implemented. They are future
semantics: retrieval only ever returns these three statuses, and rejecting
unknown statuses is deliberate.

## Rules

1. New information should not overwrite old facts by default.
2. If a fact changes, create a new fact and link it with `Supersedes`.
3. If a fact is wrong, retract it. Contradiction is expressed by superseding
   the fact with the corrected one.
4. Retrieval should prefer active and recent facts but preserve access to older facts.
5. Provenance must remain intact after supersession.
