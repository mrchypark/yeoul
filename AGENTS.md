# Yeoul Decision Support

Use the `yeoul-memory` skill when a task depends on prior decisions, constraints, ownership, status changes, tradeoffs, or provenance in this repository.

When working with repository memory in normal use, use a single user-level Yeoul database instead of a project-local database file.
Default path: `$HOME/.local/share/yeoul/work-memory.lbug`
For this repository, scope global-memory searches and episode writes with group ID `project:yeoul`; scope fact writes with repo-qualified entity namespace `repo:mrchypark/yeoul`, stable keys, or canonical project subject ID `repo:mrchypark-yeoul:project:yeoul`.
This is an agent fail-closed convention, not a storage-layer security boundary. If a command cannot carry `--group-id`, encode the repository scope in the subject ID or namespace/stable-key and do not imply runtime enforcement.
If the user, harness, or repo instructions provide a specific Yeoul binary or database path, treat them as the memory target. Run that exact binary path in every command; do not run `command -v yeoul`, `yeoul --help`, `file $(command -v yeoul)`, or any bare `yeoul` fallback unless no binary path was provided.

Project-local `./yeoul.lbug` is only for quickstart examples, isolated tests, or temporary debugging.
Prefer the workflows documented in `skills/yeoul-memory/SKILL.md` and `skills/yeoul-memory/references/cli-workflows.md`.
Use `docs/10-examples/proactive-decision-support.md` as the default operating loop for proactive Yeoul usage in this repository.
Use `agent-pack/agent_instructions.md` as the compact agent instruction pack for Graphiti-style memory flow mapped onto explicit Yeoul CLI commands.

Use Yeoul proactively during normal work in this repository.
Do not wait for the user to explicitly ask for memory lookup or memory write when prior context is likely to improve the answer or preserve a durable outcome.

## GitHub account safety

Before writing to GitHub for this repository, confirm the active GitHub API account with both `gh auth status` and `gh api user --jq .login`; both must identify `mrchypark`.
For pushes, also verify the git transport identity or use a remote/credential path known to write as `mrchypark`; if it cannot be verified as the same account, do not push.
For PR comments, review replies, review requests, issue comments, branch pushes, releases, and other remote GitHub mutations, use the repository owner account `mrchypark` unless the user explicitly says otherwise.
If another account such as `cypark-conalog` is active, if `gh auth status`, `gh api user`, and git transport identity diverge, or if any identity cannot be verified, stop before every remote mutation and ask the user to switch accounts; do not mutate host-wide GitHub CLI state automatically.
Do not post GitHub comments, review replies, review requests, or push branches from an unverified account.

## Search first

Search Yeoul before answering when:
- the user is making or revisiting a decision
- prior decisions or constraints may affect the recommendation
- ownership or current status matters
- a new claim may conflict with existing memory
- change history or provenance matters

Prefer:
- `yeoul search` for broad recall
- `yeoul fact lookup` for subject or predicate checks
- `yeoul timeline` for change history
- `yeoul provenance` for explanation and supporting context

Default behavior:
- proactively search before recommendations, design choices, prioritization, status interpretation, or conflict resolution
- proactively search when the user refers to "before", "again", "still", "last time", "current status", or similar continuity cues
- skip lookup only when the task is clearly self-contained and prior memory is unlikely to matter
- when a user provides a specific Yeoul binary, database path, group, or replay harness, use that exact target for all memory actions

## Decision support behavior

When helping with a decision:
- search for similar past decisions before proposing a new one
- summarize the decision question briefly
- retrieve relevant prior memory before recommending a direction
- present concrete options, implementation examples, and tradeoffs when multiple paths are viable
- make a recommendation only when the basis is clear
- if memory conflicts, surface the conflict instead of silently choosing one
- after the user decides, restate the selected direction briefly before recording it

Treat a turn as a decision candidate when the user chooses between options, accepts or rejects a recommendation, changes a previous direction, states a stable rule or constraint, sets a default, or produces a durable conclusion that future work should reuse.
Do not record open brainstorming, low-confidence guesses, or temporary execution details as decisions.
If a decision is implicit but likely durable, restate it and record only when the direction is clear.

## What to remember

Write to Yeoul only when the conversation contains durable information such as:
- a confirmed decision
- a stable constraint or rule
- an owner assignment
- a status change
- a correction with a reason
- a repeated problem and its resolution

Do not store:
- brainstorming that is still unsettled
- acknowledgements or low-signal chat
- unsupported guesses
- duplicate summaries of the same event
- secrets, credentials, personal data, customer data, or verbatim private content

Do not treat every message as durable memory. Prefer quality over quantity.

Default behavior:
- when a durable outcome becomes clear, treat it as a memory-write candidate even if the user did not explicitly ask to save it
- never store prohibited sensitive data even with confirmation; redact or omit it
- before implicit writes to the global database, confirm exact scope/fact text unless the user explicitly requested the write in the current turn and the content is non-sensitive and repo-scoped
- prefer storing at the end of a decision, implementation, review, or correction cycle
- store self-contained decision context and evidence as an episode, then record the decision statement, status, ownership, or dependency as facts when the subject can be named
- if the outcome is still ambiguous, defer writing until the state is clear instead of recording a weak summary

When recording decision context, prefer storing more than the conclusion alone.
Before asking the user, fill every field that can be inferred from the conversation, retrieved memory, code, tests, docs, or command output. Ask only for missing information that is required for a complete and truthful record, such as unclear scope, final choice, reason, owner/status, or revisit condition. Do not use missing optional details as a reason to save a thin record.
If the current turn only proposes a durable choice or asks for advice, do not silently postpone the write and call that confirmation. Say the exact missing question first, then record only after the user confirms or the final choice is otherwise explicit.
For multi-turn transcripts or replay evaluations, process each user turn as a live turn: search or answer first, ask if confirmation is missing, write immediately after a later turn confirms, then verify the record can be retrieved before using it.
Use a field-labeled `decision_context` episode for decisions. If a field below is unknown but required to make the record truthful, ask before asserting the fact; if it is optional and unavailable, write `unknown` or `none found` rather than inventing it.
Include, when available:
- `Topic`: the reusable decision topic or question, not just the current product or file name
- `Context`: the background or context
- `Similar past decisions`: relevant previous decisions or constraints, or `none found`
- `Options`: the main options considered
- `Decision`: the final decision and brief summary
- `Why`: the reason for choosing it
- `Tradeoffs`: important tradeoffs or rejected paths
- `Current application`: the immediate project or task where this applies
- `Revisit when`: conditions that would justify revisiting the decision
- `Owner/status`: who owns follow-up and whether the decision is proposed, active, validated, superseded, or retracted
- `Evidence`: supporting conversation, document, PR, test, benchmark, or source episode
- `Observed at`: when the decision became known
- `Valid from`: when the decision starts applying, if different from observation time

Prefer the most reusable abstraction that is still true.
If a project-specific choice is only one application of a broader rule, store the broader rule as the main decision and treat the project-specific detail as an example or current application.
Do not let a product name, environment name, or one-off implementation detail become the main decision unless that specificity is itself what will matter later.

When adjusting a decision later, do not rewrite the old record.
Use `timeline` and `provenance` first, then use `fact supersede` for replacement or `fact retract` only when the old record should no longer be trusted.

## Write rules

Use `ingest episode` or `ingest file` for context and evidence records.
Use `fact assert` for the decision statement or other durable state when the subject and supporting episode are clear.
Prefer `fact assert --upsert-subject --subject-namespace REPO --subject-type TYPE --subject-name NAME --subject-stable-key KEY` when writing to the global database and the subject has not been created yet.
Use `fact assert --cardinality one` only when all overlapping active facts for the same subject/predicate should be replaced. Use explicit `fact supersede --confirm --reason` when one known fact should be linked to one replacement or the replacement needs an audit reason.
Use stable keys when names can change but entity identity should remain stable.
Pass `fact assert --observed-at RFC3339` when the fact observation time differs from the supporting episode time; otherwise the CLI inherits the first non-empty supporting episode `observed_at`, then falls back to system time with `observed_at_basis=system_time_default`.
Use `--as-of` for knowledge/lifecycle time and `--valid-at` for domain validity; do not conflate what was known then with what was true then.
Do not overwrite old facts when the state changes.
Prefer lifecycle operations such as `fact supersede` and `fact retract` with an explicit reason.
Preserve provenance when storing or summarizing memory.
Treat `--backend auto` as the normal search default. rax is a derived retrieval index; Ladybug/Yeoul records remain canonical.

## Response rules

When answering from memory:
- prefer active facts by default
- mention time context when it matters
- mention provenance when the basis matters
- distinguish active state from superseded or retracted history

When memory use materially changes the answer:
- say that prior context was checked
- summarize the relevant prior decision, constraint, or conflict briefly
- keep the explanation concise unless the user asks for detail
