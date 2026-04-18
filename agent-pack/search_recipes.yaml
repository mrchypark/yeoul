version: 1

recipes:
  recent_context:
    description: Retrieve recent facts and episodes related to a query.
    strategy: hybrid
    filters:
      window_days: 30
      fact_status: active
    ranking:
      recency: 0.4
      semantic_similarity: 0.3
      graph_distance: 0.2
      confidence: 0.1

  preflight_briefing:
    description: Retrieve the context an agent should check before starting non-trivial work.
    strategy: neighborhood
    filters:
      fact_status: active
    expand:
      hops: 2
      entity_types: [Project, Task, Repository, File, Issue, Document, Person, Decision]
    ranking:
      graph_distance: 0.3
      recency: 0.25
      semantic_similarity: 0.2
      provenance: 0.15
      confidence: 0.1

  project_memory:
    description: Retrieve project-level context and durable guidance.
    strategy: neighborhood
    expand:
      hops: 2
      entity_types: [Project, Task, Decision, Issue, Document, Person]
    ranking:
      graph_distance: 0.4
      recency: 0.3
      provenance: 0.3

  contradiction_check:
    description: Find facts that may conflict with a new fact.
    strategy: predicate_subject_lookup
    filters:
      fact_status: active
