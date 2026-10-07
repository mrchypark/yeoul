# Ontology Specification

The Yeoul ontology is a single source of truth maintained in the agent pack:

- Canonical file: [`agent-pack/ontology.yaml`](../../agent-pack/ontology.yaml)

It provides starter entity types, predicates, and deduplication keys for
agent-facing workflows. Yeoul Core does not enforce ontology membership when
records are written; the CLI validates the policy file, and agents/callers
apply its guidance. Keep ontology changes in the agent pack and reference the
file above.
