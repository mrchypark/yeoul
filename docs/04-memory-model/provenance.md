# Provenance

Provenance is a first-class requirement in Yeoul. It explains where memory came from and why the engine believes it.

## Why provenance matters
Without provenance:
- facts cannot be debugged
- historical claims cannot be audited
- compaction becomes dangerous
- replay is hard
- trust collapses when a fact looks wrong

## Provenance goals
- every derived fact should be traceable to at least one episode
- every episode should be traceable to a source container
- derivation steps should remain inspectable
- provenance should survive supersession and compaction

## Provenance levels

### Level 1: Source provenance
Where the episode came from.
Examples:
- file path
- issue URL or issue ID
- chat thread ID
- ticket ID
- repository path

### Level 2: Episode provenance
The exact observed unit Yeoul ingested.

### Level 3: Derivation provenance
How a fact or entity linkage was produced.
Examples:
- direct assertion from a structured event
- rule-based extraction
- manual ingest
- replay from policy version X

### Level 4: Policy provenance
Which ontology, episode rules, and recipe or extraction policy version were in effect.

## Recommended graph representation

### Nodes
- `Source`
- `Episode`
- `Fact`
- optional `DerivationRun`

### Relationships
- `(:Episode)-[:FROM_SOURCE]->(:Source)`
- `(:Fact)-[:DERIVED_FROM]->(:Episode)`
- `(:Fact)-[:SUPPORTED_BY]->(:Episode)`
- `(:Fact)-[:EMITTED_BY]->(:DerivationRun)`

## Minimal provenance guarantee
For MVP, every Fact must have:
- one or more `DERIVED_FROM` or `SUPPORTED_BY` edges to Episode
- the Episode must have one `FROM_SOURCE` edge to Source unless the source is intentionally omitted

## Provenance fields
Recommended fields include:
- `policy_version`
- `extractor_kind`
- `confidence`
- `observed_at`
- `ingested_at`

## Query requirements
Yeoul must support:
- fetching the supporting episodes for a fact
- fetching the source of a fact
- fetching all facts derived from an episode
- explaining why a fact is still active or no longer active

## Provenance and lifecycle
Superseding or retracting a fact must not destroy provenance.
Instead:
- old Fact remains
- old Fact changes status
- new lifecycle edge is added
- provenance edges remain intact

## Provenance and compaction
Compaction may remove redundant derived nodes only if:
- surviving nodes preserve provenance coverage
- the compaction result is auditable
- fact explanation remains possible afterward
