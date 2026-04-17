# Yeoul Agent Pack

Yeoul Agent Pack defines how AI agents should use Yeoul Core as a memory store.

It is intentionally separate from the core engine.

```text
Core는 AI를 모른다.
Agent Pack은 Core를 사용하는 규칙만 제공한다.
```

## Contents

- `SKILL.md`: when and how an agent should remember or search
- `agent_instructions.md`: default operating instructions for agent integrations
- `integrations/`: product-specific setup guides for Codex, Gemini CLI, and Claude Code
- `ontology.yaml`: starter entity and predicate vocabulary
- `episode_rules.yaml`: starter rules for promoting or dropping events
- `search_recipes.yaml`: starter retrieval strategies
- `examples/`: pack variants by agent role
