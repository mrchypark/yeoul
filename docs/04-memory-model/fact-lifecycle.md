# Fact Lifecycle

Facts in Yeoul are not static rows. They move through a lifecycle.

## Why a lifecycle is needed
A temporal memory engine must preserve:
- the current believed state
- the earlier believed state
- the transition between them

Overwriting a fact in place destroys the history Yeoul is meant to preserve.

## Lifecycle states
Recommended states:
- `active`
- `superseded`
- `contradicted`
- `retracted`
- `uncertain`
- `archived`

## Meaning of states

### active
The fact is currently considered valid.

### superseded
A newer fact replaced this fact for the same subject/predicate/object slot or semantic role.

### contradicted
A conflicting fact exists and the conflict is not automatically resolved.

### retracted
The fact was previously asserted but later marked invalid.

### uncertain
The fact exists but confidence or provenance is insufficient for active use.

### archived
The fact is retained for history but normally omitted from standard retrieval.

## Lifecycle transitions

### Assert new fact
Creates an `active` fact.

### Supersede fact
Old fact becomes `superseded`.
New fact becomes `active`.
A `SUPERSEDES` edge links new to old.

### Retract fact
Target fact becomes `retracted`.
A reason and actor/policy context should be stored if available.

### Contradict fact
Both facts may remain visible.
At least one conflict relation should be recorded.

### Archive fact
Administrative transition for storage or retrieval policy, not truth semantics.

## Transition rules
- transitions should be transactional
- provenance must remain intact
- state changes should be auditable
- active-state retrieval must be cheap

## Retrieval behavior
Default retrieval should:
- prefer `active`
- optionally include `uncertain`
- exclude `archived` by default
- include `superseded` or `retracted` only on explicit historical queries or explanation flows

## Lifecycle validation
The engine should reject or warn on:
- multiple active facts for an exclusive predicate when policy says only one should exist
- retraction of a non-existent fact
- supersession loops

## Predicate policy interaction
Some predicates are multi-valued by nature.
Example:
- `WORKS_WITH` may allow many active facts

Some are exclusive by nature.
Example:
- `CURRENT_OWNER` may allow only one active fact per subject

The ontology or predicate rules should define this.
