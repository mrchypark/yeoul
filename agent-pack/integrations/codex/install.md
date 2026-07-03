# Codex Install and Placement Guide

This guide explains how to place Yeoul so Codex can use it as durable local memory.
It assumes you want Yeoul available across repositories, not only inside this repository.

## Target Layout

Use this layout for normal Codex work:

```text
~/.local/bin/yeoul                         # Yeoul CLI wrapper
~/.local/share/yeoul/work-memory.lbug      # user-level Yeoul database
~/.codex/skills/yeoul-memory/              # reusable Codex skill
<repo>/AGENTS.md                           # project-specific instruction hook
<repo>/agent-pack/                         # optional Yeoul policy pack for that repo
```

`agent-pack/` and `skills/yeoul-memory/` are different things:

- `skills/yeoul-memory/` is the Codex skill. Install it under `~/.codex/skills/` when you want Codex to discover Yeoul behavior globally.
- `agent-pack/` is the Yeoul policy pack. Keep it in a repository or shared policy location when you want CLI policy validation, ontology, episode rules, or search recipes.

## 1. Install the Yeoul CLI

Install the latest release:

```sh
curl -fsSL https://github.com/mrchypark/yeoul/releases/latest/download/install.sh | bash
```

Verify the CLI wrapper:

```sh
export PATH="$HOME/.local/bin:$PATH"
yeoul --help
```

Add `~/.local/bin` to your shell startup file if it is not already on `PATH`.

## 2. Create the User-Level Database

For normal Codex work, use one user-level Yeoul database:

```sh
export YEOUL_DB="$HOME/.local/share/yeoul/work-memory.lbug"
mkdir -p "$(dirname "$YEOUL_DB")"
yeoul init --db "$YEOUL_DB"
```

Use project-local `./yeoul.lbug` only for quickstarts, isolated tests, or disposable debugging.
One global database keeps decisions and durable facts reusable across projects.
Use `--group-id` or stable subject namespaces to keep repository-specific records scoped.

## 3. Install the Codex Skill

Clone or unpack Yeoul, then copy the reusable skill into Codex's local skill directory:

```sh
CODEX_HOME="${CODEX_HOME:-$HOME/.codex}"
mkdir -p "$CODEX_HOME/skills/yeoul-memory"
cp -R /path/to/yeoul/skills/yeoul-memory/. "$CODEX_HOME/skills/yeoul-memory/"
```

Restart Codex after installing or updating the skill so it reloads local skill metadata.

To verify placement:

```sh
test -f "${CODEX_HOME:-$HOME/.codex}/skills/yeoul-memory/SKILL.md"
```

## 4. Add Repository Instructions

Add or merge an `AGENTS.md` file in each repository where Codex should use Yeoul proactively.
At minimum, include:

```md
# Yeoul Decision Support

Use the `yeoul-memory` skill when a task depends on prior decisions, constraints, ownership, status changes, tradeoffs, or provenance in this repository.

Use a single user-level Yeoul database for normal work:
`$HOME/.local/share/yeoul/work-memory.lbug`

Search Yeoul before recommendations, design choices, prioritization, status interpretation, or conflict resolution when prior context may matter.

Write durable outcomes to Yeoul when they become clear:
- confirmed decisions
- stable rules or constraints
- ownership or status changes
- corrections or retractions
- dependencies or relationships
- stable preferences
- definitions or terminology
- validated benchmark or evaluation conclusions

Preserve provenance. Store source context as episodes first, and assert facts only when the subject, claim, scope, and supporting episode are clear.
```

If the repository already has an `AGENTS.md`, merge this section instead of replacing existing project rules.

## 5. Optionally Place the Policy Pack

When a repository should carry Yeoul policy files, keep `agent-pack/` in that repository:

```sh
mkdir -p ./agent-pack
cp -R /path/to/yeoul/agent-pack/. ./agent-pack/
yeoul policy validate --path ./agent-pack
yeoul policy show --path ./agent-pack --json
```

Use policy packs from CLI commands when you want named search recipes or policy-driven behavior:

```sh
yeoul search --db "$YEOUL_DB" \
  --query "release decision" \
  --group-id "repo:example" \
  --policy-path ./agent-pack \
  --recipe recent_context \
  --include-related
```

If `policy show --json` does not include `episode_rules.fact_promotion`, the installed Yeoul binary is too old. Upgrade to Yeoul `v0.2.1` or newer.

## 6. Smoke Test Codex Memory Use

Run these commands from a repository with `AGENTS.md` installed:

```sh
export YEOUL_DB="$HOME/.local/share/yeoul/work-memory.lbug"
yeoul ingest episode --db "$YEOUL_DB" \
  --kind note \
  --content "Codex Yeoul smoke test: this repository uses the global Yeoul database for durable memory." \
  --source-kind note \
  --source-external-ref codex-yeoul-smoke \
  --group-id "repo:smoke-test"

yeoul search --db "$YEOUL_DB" \
  --query "Codex Yeoul smoke test global database" \
  --group-id "repo:smoke-test"
```

Then ask Codex:

```text
Use the yeoul-memory skill and search what this repository decided about the Yeoul database location.
```

Expected behavior:

- Codex uses the `yeoul-memory` skill when the request depends on memory.
- Codex searches before making or revisiting decisions.
- Codex records durable outcomes only when the fact candidate is clear enough.
- Codex asks a focused clarification when a fact-worthy claim lacks subject, scope, or supporting context.

## Updating

When Yeoul changes, update both surfaces:

```sh
curl -fsSL https://github.com/mrchypark/yeoul/releases/latest/download/install.sh | bash

CODEX_HOME="${CODEX_HOME:-$HOME/.codex}"
mkdir -p "$CODEX_HOME/skills/yeoul-memory"
cp -R /path/to/yeoul/skills/yeoul-memory/. "$CODEX_HOME/skills/yeoul-memory/"
```

Restart Codex after updating the skill.

## Troubleshooting

- If Codex does not mention `yeoul-memory`, confirm `~/.codex/skills/yeoul-memory/SKILL.md` exists and restart Codex.
- If `yeoul` is not found, run `~/.local/bin/yeoul --help` and add `~/.local/bin` to `PATH`.
- If records appear in `./yeoul.lbug`, switch commands back to `$HOME/.local/share/yeoul/work-memory.lbug` unless you are running an isolated test.
- Do not store secrets, credentials, private keys, or verbatim sensitive data in Yeoul. Redact or omit them.
