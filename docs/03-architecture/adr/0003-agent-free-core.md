# ADR 0003: Keep Yeoul Core agent-free

## Status

Accepted

## Decision

Yeoul Core must not include agent orchestration, LLM calls, prompt management, or autonomous planning.

## Consequences

- AI-specific behavior moves to Agent Pack.
- Skills and instructions are documentation and configuration artifacts.
- Core can be used by non-AI applications.
