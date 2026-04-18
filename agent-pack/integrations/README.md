# Agent Integration Guides

Yeoul is agent-agnostic, but each coding product loads persistent instructions in a different way.

Use this guide after installing `yeoul` itself. If your repository already has agent instruction files, merge these snippets into the existing files instead of overwriting them.

## Quick map

| Product | Team-shared project file | Optional reusable command surface |
| --- | --- | --- |
| Codex | `AGENTS.md` | Codex skill in `~/.codex/skills/yeoul-memory` |
| Gemini CLI | `GEMINI.md` | `.gemini/commands/*.toml` |
| Claude Code | `CLAUDE.md` | `.claude/commands/*.md` |

Replace `<DB_PATH>` below with your actual Yeoul database path, or document your team-standard path directly in the project instruction file.

## Codex

Codex understands repository guidance through `AGENTS.md`. In the local Codex runtime, reusable skills are also loaded from `~/.codex/skills/<skill-name>`.

### 1. Install the reusable Yeoul skill for Codex

```bash
mkdir -p ~/.codex/skills/yeoul-memory
cp -R /path/to/yeoul/skills/yeoul-memory/. ~/.codex/skills/yeoul-memory/
```

This path reflects the current Codex local runtime convention.

### 2. Add or merge an `AGENTS.md` file in your repository root

```md
# Yeoul Memory

Use the `yeoul-memory` skill when a task depends on prior decisions, constraints, working guidance, ownership, status changes, or provenance in this repository.

Search Yeoul before answering when prior project memory may matter.
Search Yeoul before starting non-trivial work when prior context may change the plan, tools, or output.
Prefer the workflows documented in `skills/yeoul-memory/SKILL.md` and `skills/yeoul-memory/references/cli-workflows.md`.

When writing memory, preserve provenance and lifecycle semantics. Do not overwrite old facts when the state changes.
```

### 3. Use it from Codex

Ask directly for the skill when you want deterministic behavior, for example:

```text
Use the yeoul-memory skill and check what we previously decided about release automation.
```

If you prefer not to install a global Codex skill, put the same guidance directly into `AGENTS.md`.

## Gemini CLI

Gemini CLI does not use Codex-style `SKILL.md` packages. The closest equivalent is a project `GEMINI.md` plus optional custom commands in `.gemini/commands/`.

### 1. Add or merge `GEMINI.md` in your repository root

Gemini CLI supports `@path` imports, so the simplest setup is to reuse Yeoul's existing skill files:

```md
# Yeoul Memory

Use Yeoul as the durable temporal memory for this repository.

@skills/yeoul-memory/SKILL.md
@skills/yeoul-memory/references/cli-workflows.md
```

### 2. Optional: add a reusable Gemini command

Create `.gemini/commands/yeoul-memory.toml`:

```toml
description = "Search Yeoul project memory"
prompt = """
Use Yeoul as the durable project memory for this repository.
Search first when prior decisions, constraints, status, or provenance may matter.
Also search before non-trivial work when standing guidance or recent context may change the plan.

Start with:
- yeoul search --db <DB_PATH> --query "{{args}}" --policy-path ./agent-pack --recipe recent_context
- before non-trivial work: yeoul search --db <DB_PATH> --query "{{args}}" --policy-path ./agent-pack --recipe preflight_briefing

Then refine with yeoul fact lookup, yeoul timeline, yeoul provenance, or yeoul neighborhood when needed.
Summarize answers with time context and provenance.
"""
```

### 3. Reload the configuration

Inside Gemini CLI:

```text
/memory reload
/commands reload
```

### 4. Use it from Gemini CLI

```text
/yeoul-memory release automation
```

## Claude Code

Claude Code also does not use Codex-style `SKILL.md` packages directly. Its equivalents are project memory in `CLAUDE.md` and custom slash commands in `.claude/commands/`.

### 1. Add or merge `CLAUDE.md` in your repository root

Claude Code supports `@path` imports, so you can reuse the Yeoul skill text directly:

```md
# Yeoul Memory

Use Yeoul as the durable temporal memory for this repository.

@skills/yeoul-memory/SKILL.md
@skills/yeoul-memory/references/cli-workflows.md
```

### 2. Optional: add a reusable Claude command

Create `.claude/commands/yeoul-memory.md`:

```md
Use Yeoul as the durable project memory for this repository.

Search first when prior decisions, constraints, status, or provenance may matter.
Also search before non-trivial work when standing guidance or recent context may change the plan.
Start with:
- `yeoul search --db <DB_PATH> --query "$ARGUMENTS" --policy-path ./agent-pack --recipe recent_context`
- before non-trivial work: `yeoul search --db <DB_PATH> --query "$ARGUMENTS" --policy-path ./agent-pack --recipe preflight_briefing`

Then refine with `yeoul fact lookup`, `yeoul timeline`, `yeoul provenance`, or `yeoul neighborhood` when needed.
Summarize answers with time context and provenance.
```

### 3. Reload or inspect Claude memory

Inside Claude Code:

```text
/memory
```

Claude will pick up project memory automatically when it starts, and `/memory` lets you inspect what is currently loaded.

### 4. Use it from Claude Code

```text
/yeoul-memory release automation
```

## References

- OpenAI Codex and `AGENTS.md`: <https://openai.com/index/introducing-codex/>
- OpenAI Codex app and skills: <https://openai.com/index/introducing-the-codex-app/>
- Gemini CLI `GEMINI.md`: <https://geminicli.com/docs/cli/gemini-md/>
- Gemini CLI custom commands: <https://geminicli.com/docs/cli/custom-commands/>
- Claude Code memory: <https://docs.anthropic.com/en/docs/claude-code/memory>
- Claude Code slash commands: <https://docs.anthropic.com/en/docs/claude-code/slash-commands>
