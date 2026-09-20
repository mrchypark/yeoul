# Search Recipes Specification

Search recipes are a single source of truth maintained in the agent pack:

- Canonical file: [`agent-pack/search_recipes.yaml`](../../agent-pack/search_recipes.yaml)

They define the retrieval recipes (`recent_context`, `preflight_briefing`,
`project_memory`, `contradiction_check`) with their strategies and rankings.
The `docs/` tree intentionally does not carry a copy; keep recipe changes in
the agent pack and reference the file above.

## Validated fields

`yeoul policy validate` rejects recipe fields the query layer does not
consume, so a pack cannot report valid while changing nothing:

- `strategy`: one of `hybrid`, `neighborhood`, `predicate_subject_lookup`.
- `filters`: `fact_status` (string or list of strings), `predicate` (string or
  list of strings), and `window_days` (integer). These are the only filters
  applied to a search request.
- `expand`: `entity_types` (list of strings) is applied to the search scope.
  `hops` is accepted as advisory metadata and must be an integer.
- `extensions`: the only place arbitrary advisory content is allowed.

Unknown keys outside `extensions` fail validation.
