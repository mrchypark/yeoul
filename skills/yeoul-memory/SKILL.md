---
name: yeoul-memory
description: Use when working in the Yeoul repository or when an agent should store, search, or explain durable local temporal memory with Yeoul. Covers when to remember, when to search first, which CLI commands to use, and how to report provenance and lifecycle state safely.
---

# Yeoul Memory

Use this skill when the task depends on prior project memory, durable decisions, fact lifecycle, or provenance in the Yeoul repo.

Yeoul Core is a memory substrate, not an agent runtime. Keep agent behavior in skills, instructions, ontology files, episode rules, and search recipes. Use Yeoul public CLI/API workflows instead of raw Cypher or storage queries.

## Default database path

For normal work, prefer a single user-level Yeoul database instead of a project-local database file.

Default path:
- `$HOME/.local/share/yeoul/work-memory.ltdb`

Project-local `./yeoul.ltdb` is only for quickstarts, isolated tests, or temporary debugging.
LatticeDB is canonical storage and `.ltdb` is the standard extension for new databases. Existing `.lbug` paths remain valid migration inputs and may contain an in-place migrated LatticeDB database. If the default `.ltdb` path is absent but the legacy `.lbug` path exists, use the legacy path until migration and any rename are verified instead of creating a second empty database.

## Local install and upgrade checks

When asked to install or upgrade Yeoul on this computer:
- Install the requested release with the repository installer or `scripts/install.sh --version TAG`.
- Verify the wrapper points at the expected install directory; the current CLI does not provide a `--version` command.
- Verify the user-level database actually opens with the installed binary using `yeoul inspect counts --db "$YEOUL_DB"` and at least one `yeoul search`.
- For a legacy Ladybug database, require Yeoul v0.5.1 or later with its bundled version-pinned migration helper. Stop other Yeoul processes, then run `yeoul admin migrate-db --db "$YEOUL_DB" --json` or allow the first default open to migrate it automatically. Require a verified LatticeDB result and retain the timestamped `.ladybug-backup-*` copy. Never use v0.5.0 to migrate a pristine v0.2.2 database.
- Do not treat an upgrade as complete until `inspect counts`, a representative search, lifecycle state, and the migration backup have been verified against the real user-level database.
- Preserve inactive, superseded, or revision-backed facts during upgrades. If `admin export` refuses lifecycle or revision state because full-fidelity restore is not implemented, keep the old database backup and report the limitation instead of replacing `$YEOUL_DB`.

## Search first

Search Yeoul before answering when:
- the user asks what was decided before
- the task depends on prior project constraints or status
- you are about to start non-trivial work and prior memory may change the plan, tools, or output shape
- ownership, task assignment, dependency, issue, or recent change history matters
- a new fact may conflict with existing memory
- you need provenance or change history

For Yeoul repo work, and for any non-trivial task where prior context might matter, run the `preflight_briefing` search from `references/cli-workflows.md` before planning, bulk exploration, implementation, or a final answer. Injected skill text, remembered summaries in the prompt, and prior conversation context do not count as a Yeoul search.

Prefer:
- `yeoul search` with the `recent_context` recipe for broad recall
- `yeoul search` with the `preflight_briefing` recipe before non-trivial work
- `yeoul search` with the `project_memory` recipe for project-level context
- `yeoul fact lookup` for subject/predicate checks and contradiction checks
- `yeoul timeline` for change history
- `yeoul provenance` for explanation
- `yeoul neighborhood` for local graph context

Default behavior:
- proactively search before recommendations, design choices, prioritization, status interpretation, or conflict resolution
- proactively search when the user refers to earlier decisions, previous attempts, current status, or continuity across work
- proactively check for active conflicting facts before asserting a new fact
- if `preflight_briefing` is unavailable, validate/list recipes, use `recent_context` once as a fallback, and say the policy pack needs syncing
- skip lookup only when the task is clearly self-contained and prior memory is unlikely to matter

When a decision is required:
- search for similar past decisions first
- present current options and realistic alternatives
- include implementation examples and tradeoffs when useful
- restate the user's chosen direction before recording it
- expect to reuse the recorded decision later

## Remember deliberately

Store memory only when the content is likely to matter later:
- explicit decisions
- stable constraints
- ownership or status changes
- confirmed dependencies or relationships
- corrections or retractions
- repeated problems and resolutions
- stable preferences
- definitions or terminology
- validated evaluation or benchmark conclusions

Do not store:
- acknowledgements
- brainstorming that is still unsettled
- low-signal chatter
- unsupported guesses
- duplicate summaries of the same event
- destructive corrections without a reason
- secrets, credentials, or private keys

Always omit secrets, credentials, and private keys. Store sensitive personal or customer data only with explicit authorization for a defined write scope, and minimize or redact it before storage. Non-sensitive stable preferences and commitments may be stored when applicable write authority and supporting provenance are present. Read-only or no-write instructions always win.

Default behavior:
- when a durable outcome becomes clear, treat it as a memory-write candidate even if the user did not explicitly ask to save it
- prefer storing at the end of a decision, implementation, review, or correction cycle
- if the outcome is still ambiguous, defer writing until the state is clear instead of recording a weak summary

## Fact extraction loop

For every substantive exchange, look for fact candidates that should be retrievable later.
Fact candidates include confirmed decisions, durable rules or constraints, current status, owners, corrections or retractions, repeated problems and resolutions, dependencies or relationships, stable preferences, definitions or terminology, and validated evaluation or benchmark conclusions.
First decide whether the exchange contains a fact-worthy claim or only episode-worthy context.
If it is fact-worthy but missing the subject, claim, scope, time/status, or supporting context needed for a reliable fact, ask a focused clarification instead of asserting a weak fact.

## Episode and fact boundary

Episodes are source records. Use them to preserve background, evidence, context, and provenance.
Facts are promoted claims. Promote confirmed durable claims that need fact lookup.
Do not promote every episode to a fact.
Keep raw progress, benchmark results, implementation logs, review notes, and exploratory context as episodes unless they establish durable state, a reusable conclusion, a rule, a relationship, a stable preference, or a definition.
Every fact must have at least one supporting episode.
Episode content should fit the fact type: decisions need context/options/why/tradeoffs; status needs previous/new state and as-of time; corrections need wrong/right/reason; benchmarks need setup/metric/result/decision impact; ownership needs owner/scope; dependencies/relationships need subject/object/relation/evidence; preferences need holder/scope/default; definitions need term/scope/meaning; repeated problems need symptom/root cause/resolution; rules need scope/exceptions.

## Structured-memory checkpoint

At the end of a substantive decision, implementation, review, or correction cycle, and before the final answer, classify the outcome as no memory, episode-only context, a new durable fact, or a lifecycle change.

1. Keep transient, ambiguous, exploratory, or unsupported material episode-only or do not store it.
2. For a confirmed durable claim, do not stop after episode ingest merely because an entity is missing. Preserve the supporting episode, then select the subject namespace, type, canonical name, and stable key; the predicate; and either a reusable object entity or a text value.
3. Check for an existing entity, active subject/predicate facts, and possible conflicts before creating or changing structured memory.
4. If the write is authorized, complete the fact or lifecycle operation and read it back. If it is not authorized, give the owning agent the exact proposed memory action instead.

This checkpoint does not require every episode to become a fact.

When recording a decision, prefer storing more than the conclusion alone.
Include, when available:
- `Topic`: the decision topic or question
- `Context`: the background or context
- `Similar past decisions`: relevant previous decisions or constraints
- `Options`: the main options considered
- `Decision`: the final decision and brief summary
- `Why`: the reason for choosing it
- `Tradeoffs`: important tradeoffs or rejected paths
- `Current application`: how the decision applies in the present project or task
- `Revisit when`: conditions that would justify revisiting the decision

Prefer the most reusable abstraction that is still true.
If the current project choice is one application of a broader pattern, store the broader pattern as the main decision and treat the project-specific detail as the current application.
For decisions shaped like "use X for Y", do not make `X` the whole fact unless the user gave the reusable selection criterion or explicitly said the exact tool choice is the durable rule. Store the selection criterion as the fact and keep `X for Y` as `Current application`; if the criterion is missing, ask or keep it episode-only.
Do not let a one-off tool name, environment name, or implementation detail become the main decision unless that specificity is exactly what future work will need.

## Write rules

- Use `ingest episode` or `ingest file` for source episodes.
- Use `fact assert` for a confirmed durable claim when its subject, predicate, scope, and supporting episodes are clear.
- A missing entity is not a reason to stop at episode-only. Reuse an existing entity when identity matches; otherwise use `fact assert --upsert-subject` and, for a relationship target, `--upsert-object`.
- Model a named referent as an object entity when future work should reuse, filter, or traverse it. Use `value_text` for a scalar, status, short conclusion, or opaque text. Do not create an entity for every noun.
- Use one canonical identity scheme inside a scope. For a Repository entity, use namespace `repo:<owner>/<repo>` and stable key `<owner>/<repo>` without repeating the namespace. Prefer real external IDs for other durable entities; examples include file=`<owner>/<repo>:<path>` and issue=`<owner>/<repo>#<number>`.
- Prefer ontology patterns when they fit: `Project DECIDED Decision`, `Person OWNS Repository` or `Task`, component `DEPENDS_ON` component, and record `MENTIONED_IN Document`. A domain pack may use `Role` as the owner type when it declares that type.
- Use `fact supersede --confirm` for state changes rather than overwriting.
- Use `fact retract --confirm` only with an explicit reason.
- A `SUPERSEDES` assertion never substitutes for `fact supersede`.
- Use `--cardinality one` only when overlapping active facts in the same subject/predicate slot should be replaced.
- Use `--as-of` for what Yeoul knew then and `--valid-at` for what was true then.
- After writing, verify with `fact lookup` and `provenance`; use `neighborhood` for relationships and `timeline` for lifecycle changes. Use `context` when a bounded agent-ready retrieval bundle is needed.
- Use `admin compact` as dry-run first; treat apply as maintenance, not normal editing.

## Authority and delegation

- Read-only search, lookup, timeline, provenance, neighborhood, and planning may be delegated.
- A delegated agent may propose a memory action, but must not write a shared or user-level database unless the user, repository policy, or owning agent explicitly delegated that exact write scope.
- The owning agent validates the exact scope, subject, predicate, object or value, supporting episode, and lifecycle operation before a shared write. Proactive non-sensitive repo-scoped writes are allowed only when current instructions authorize them and all required fields are clear; otherwise ask.
- Read-only or no-write instructions always win. Neither Yeoul Core nor policy YAML automatically extracts entities or promotes facts; these remain agent decisions.

## Response rules

- Prefer active facts for the default answer.
- If facts conflict, surface the conflict.
- Mention time context when it matters.
- Mention provenance or supporting episodes when explaining why something is believed.
- Treat duplicate-marked entities as historical aliases, not canonical current answers.
- When memory use materially changes the answer, say briefly that prior context was checked and summarize the relevant decision, constraint, or conflict.

## Repo-specific workflow

In this repo, prefer the local CLI over inventing raw storage queries.

Read [references/cli-workflows.md](references/cli-workflows.md) when you need concrete command patterns for search, timeline, provenance, lifecycle changes, policy recipes, or maintenance flows.
