# Codex Install and Placement Guide

This guide explains how to place Yeoul so Codex can use it as durable local memory.
It assumes you want Yeoul available across repositories, not only inside this repository.

## Target Layout

Use this layout for normal Codex work:

```text
~/.local/bin/yeoul                         # Yeoul CLI wrapper
~/.local/share/yeoul/work-memory.ltdb      # user-level Yeoul database
/absolute/path/to/loaded/yeoul-memory/     # agent host's loaded skill directory
<repo>/AGENTS.md                           # project-specific instruction hook
<repo>/agent-pack/                         # optional Yeoul policy pack for that repo
```

`agent-pack/` and the loaded `yeoul-memory` skill are different things:

- The loaded `yeoul-memory` skill directory is the reusable Agent Skill. Install it in the directory that the agent host actually loads for `yeoul-memory`.
- `agent-pack/` is the Yeoul policy pack. Keep it in a repository or shared policy location when you want CLI policy validation, ontology, episode rules, or search recipes.

The reusable skill is distributed from the skill repository that the active host loads. Treat installed copies as derived artifacts: update them one-way from that loaded source, then restart Codex. Do not edit the installed copy independently.
Its bundled `search_recipes.yaml` lets the skill run recipe-backed searches even when no repository `agent-pack/` is available.

## 1. Install the Yeoul CLI

Install the latest release:

```sh
curl -fsSL https://github.com/mrchypark/yeoul/releases/latest/download/install.sh | bash
```

Verify the CLI wrapper:

```sh
export PATH="$HOME/.local/bin:$PATH"
yeoul --help
sed -n '1,5p' "$HOME/.local/bin/yeoul"
```

Add `~/.local/bin` to your shell startup file if it is not already on `PATH`.

## 2. Create the User-Level Database

For normal Codex work, use one user-level Yeoul database.
Only initialize a new path. If the target already contains data, do not use
`yeoul init` or `--force` on it.
If the existing user database is native Ladybug, stop: current releases cannot
migrate it. Use a compatible older release to migrate before upgrading rather
than initializing a new empty database. A migrated LatticeDB database may keep
its `.lbug` path; the extension alone does not identify the format.

```sh
export YEOUL_DB="$HOME/.local/share/yeoul/work-memory.ltdb"
mkdir -p "$(dirname "$YEOUL_DB")"
yeoul init --db "$YEOUL_DB"
```

Use project-local `./yeoul.ltdb` only for quickstarts, isolated tests, or disposable debugging. Never initialize or force-create a database over an existing path containing user data.

Current Yeoul releases use LatticeDB only and do not migrate native Ladybug databases. If `work-memory.lbug` is a genuine, unmigrated Ladybug database, migrate it with a compatible older Yeoul release before upgrading. Keep the timestamped `.ladybug-backup-*` copy until counts, search, revisions, and lifecycle state are verified. A migrated LatticeDB directory may still have a `.lbug` suffix; the extension alone does not identify the format, so do not migrate or rename an already-migrated database based on its name.
One global database keeps decisions and durable facts reusable across projects.
Use `--group-id` or stable subject namespaces to keep repository-specific records scoped.

## 3. Install the Codex Skill

Obtain the reusable skill from the skill repository the active host loads, then set `YEOUL_LOADED_SKILL_DIR` to the directory that the agent host actually loads for `yeoul-memory` and copy the skill there:

```sh
export YEOUL_LOADED_SKILL_DIR="/absolute/path/to/loaded/yeoul-memory"
mkdir -p "$YEOUL_LOADED_SKILL_DIR"
cp -R /path/to/skill-repository/yeoul-memory/. "$YEOUL_LOADED_SKILL_DIR/"
```

Restart Codex after installing or updating the skill so it reloads local skill metadata.

To verify placement:

```sh
test -f "$YEOUL_LOADED_SKILL_DIR/SKILL.md"
```

## 4. Add Repository Instructions

Add or merge an `AGENTS.md` file in each repository where Codex should use Yeoul proactively.
At minimum, include:

```md
# Yeoul Decision Support

Use the `yeoul-memory` skill when a task depends on prior decisions, constraints, ownership, status changes, tradeoffs, or provenance in this repository.

Use a single user-level Yeoul database for normal work:
`$HOME/.local/share/yeoul/work-memory.ltdb`

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
For an authorized confirmed durable claim, do not stop after episode ingest because an entity is missing. Reuse matching entities or use `--upsert-subject` and `--upsert-object`, then verify with `fact lookup` and `provenance`.
```

If the repository already has an `AGENTS.md`, merge this section instead of replacing existing project rules.

## 5. Optionally Place the Policy Pack

When a repository should carry Yeoul policy files, keep `agent-pack/` in that repository:

```sh
# Run from the repository root.
mkdir -p ./agent-pack
cp -R /path/to/yeoul/agent-pack/. ./agent-pack/
yeoul policy validate --path "$PWD/agent-pack"
yeoul policy show --path "$PWD/agent-pack" --json
```

Use policy packs from CLI commands when you want named search recipes or policy-driven behavior. Select an absolute path before the command: prefer the repository pack when it exists, otherwise use the installed skill directory.

```sh
# Run this from the repository root when its agent-pack/ is present.
export YEOUL_POLICY_PATH="$(CDPATH= cd agent-pack && pwd -P)"

# Otherwise set this to the actual loaded skill directory for the active host.
# export YEOUL_LOADED_SKILL_DIR="/absolute/path/to/loaded/yeoul-memory"
# export YEOUL_POLICY_PATH="$(CDPATH= cd "$YEOUL_LOADED_SKILL_DIR" && pwd -P)"
```

Relative `--policy-path` values are resolved from the current working directory, so use the selected absolute path. Yeoul does not discover a default policy path and does not read `YEOUL_POLICY_PATH`; the variable is an explicit shell convention used in the command below.

```sh
yeoul search --db "$YEOUL_DB" \
  --query "release decision" \
  --group-id "repo:example" \
  --policy-path "$YEOUL_POLICY_PATH" \
  --recipe recent_context \
  --include-related
```

If `policy show --json` does not include `episode_rules.fact_promotion`, the installed Yeoul binary is too old. Upgrade to Yeoul `v0.2.1` or newer.

## 6. Smoke Test Codex Memory Use

Run the repository smoke script from a checkout with `AGENTS.md` installed. It uses a disposable database, preserves it on failure, and removes it only after every assertion passes:

```sh
YEOUL_BIN="$HOME/.local/bin/yeoul" \
YEOUL_INSTALLED_SKILL_PATH="/absolute/path/to/loaded/yeoul-memory" \
  scripts/ci/smoke-yeoul-memory-guidance.sh
```

`YEOUL_INSTALLED_SKILL_PATH` is explicit so CI can validate the installed skill that an active host actually loads. In source-tree CI, omit it to use the repository `agent-pack/` policy source.

Then ask Codex:

```text
Use the yeoul-memory skill and search what this repository decided about the Yeoul database location.
```

Expected behavior:

- Codex uses the `yeoul-memory` skill when the request depends on memory.
- Codex searches before making or revisiting decisions.
- Codex records durable outcomes only when the fact candidate is clear enough.
- Codex asks a focused clarification when a fact-worthy claim lacks subject, scope, or supporting context.
- Codex does not stop after episode ingest when an authorized confirmed durable claim has a clear subject.
- Codex creates or reuses canonical subject and object entities.
- Codex verifies the fact and provenance before reporting memory completion.

## Updating

When Yeoul changes, update the installed skill one-way from the skill repository the active host loads:

```sh
curl -fsSL https://github.com/mrchypark/yeoul/releases/latest/download/install.sh | bash

export YEOUL_LOADED_SKILL_DIR="/absolute/path/to/loaded/yeoul-memory"
mkdir -p "$YEOUL_LOADED_SKILL_DIR"
cp -R /path/to/skill-repository/yeoul-memory/. "$YEOUL_LOADED_SKILL_DIR/"
```

Then verify the installed binary against the real user-level database, not just `--help`:

```sh
export YEOUL_DB="$HOME/.local/share/yeoul/work-memory.ltdb"
yeoul inspect counts --db "$YEOUL_DB" --json
yeoul search --db "$YEOUL_DB" \
  --query "recent Yeoul memory" \
  --limit 3
```

Do not run the current release against a native Ladybug database. Migrate it with a compatible older Yeoul release before upgrading, and retain the reported `.ladybug-backup-*` copy until the migrated records and lifecycle state are verified. A migrated LatticeDB directory can remain at its `.lbug` path.

Restart Codex after updating the skill.

## Troubleshooting

- If Codex does not mention `yeoul-memory`, confirm the agent host's actual loaded skill directory contains `yeoul-memory/SKILL.md`, then restart Codex.
- If `yeoul` is not found, run `~/.local/bin/yeoul --help` and add `~/.local/bin` to `PATH`.
- If records appear in `./yeoul.ltdb`, switch commands back to `$HOME/.local/share/yeoul/work-memory.ltdb` unless you are running an isolated test.
- If a native Ladybug database remains, stop before initializing a replacement; use a compatible older Yeoul release to migrate it before upgrading.
- Always omit secrets, credentials, and private keys. Store sensitive personal or customer data only with explicit authorization for a defined write scope, and minimize or redact it before storage. Non-sensitive stable preferences and commitments may be stored when applicable write authority and supporting provenance are present. Read-only or no-write instructions always win.
