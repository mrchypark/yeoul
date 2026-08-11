# Search Recipes Specification

Search recipes are a single source of truth maintained in the agent pack:

- Canonical file: [`agent-pack/search_recipes.yaml`](../../agent-pack/search_recipes.yaml)

They define the retrieval recipes (`recent_context`, `preflight_briefing`,
`project_memory`, `contradiction_check`) with their strategies and rankings.
The `docs/` tree intentionally does not carry a copy; keep recipe changes in
the agent pack and reference the file above.
