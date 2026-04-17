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

### expired_at

When Yeoul marks a fact as no longer active.

## Fact Status

- active
- superseded
- contradicted
- retracted
- uncertain

## Rules

1. New information should not overwrite old facts by default.
2. If a fact changes, create a new fact and link it with `Supersedes`.
3. If a fact is wrong, mark it as `retracted` or `contradicted`.
4. Retrieval should prefer active and recent facts but preserve access to older facts.
5. Provenance must remain intact after supersession.
