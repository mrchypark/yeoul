# CLI Specification

The Yeoul CLI is a local developer and operator tool. It is not the primary application integration surface, but it must make the system inspectable and testable.

This specification describes the implemented command surface. The authoritative usage text is emitted by `yeoul` with no arguments; this document is the prose reference.

## Command design principles
- no mandatory network dependency
- clear subcommands
- structured output support
- safe defaults
- destructive operations require confirmation (`--confirm`)

## Command groups

### `yeoul init`
Create a new database and initialize schema.

#### Example
```bash
yeoul init --db ./yeoul.ltdb
```

#### Flags
- `--db PATH` required
- `--force` recreate
- `--json`

#### Behavior
- opens database
- applies schema migrations
- prints schema version

### `yeoul migrate`
Run pending migrations.

#### Example
```bash
yeoul migrate --db ./yeoul.ltdb
```

### `yeoul ingest`
Insert data into the memory graph.

#### Subcommands
- `episode` — single episode from `--content` or `--content-file`
- `file` — one episode from a file
- `json` — bulk episodes, entities, and facts from JSON
- `batch` — alias for bulk JSON ingest

#### Examples
```bash
yeoul ingest episode --db ./yeoul.ltdb --kind chat_message --content-file ./note.txt --source-id thread_1
yeoul ingest json --db ./yeoul.ltdb --file ./episode.json
```

### `yeoul get`
Fetch one record by kind and ID.

#### Example
```bash
yeoul get --db ./yeoul.ltdb --kind episode --id ep_000001
```

#### Flags
- `--kind episode|entity|fact|source` required
- `--id ID` required
- `--json`

### `yeoul search`
Run retrieval queries.

#### Examples
```bash
yeoul search --db ./yeoul.ltdb --query "recent decisions about LatticeDB"
yeoul search --db ./yeoul.ltdb --query "recent decisions about storage engine"
yeoul search --db ./yeoul.ltdb --query "recent decisions" --type fact,episode --group-id project:yeoul --limit 20
```

#### Flags
- `--query TEXT` required
- `--type fact,episode,entity`
- `--group-id IDS`
- `--as-of`, `--valid-at`, `--valid-from`, `--valid-to` RFC3339
- `--limit N`
- `--include-related`
- `--json`

#### Behavior
- keeps LatticeDB-backed Yeoul records as canonical truth
- runs entirely in-process with no external retrieval runtime

### `yeoul context`
Build a bounded, factual context bundle from one scoped search response.

#### Example
```bash
yeoul context --db ./yeoul.ltdb --query "recent decisions about storage engine" --json
```

#### Flags
- `--query TEXT` required
- `--type fact,episode,entity`
- `--limit N`
- `--json`

### `yeoul timeline`
Inspect time-ordered episode and fact events.

#### Example
```bash
yeoul timeline --db ./yeoul.ltdb --entity entity_project_yeoul --descending
```

#### Flags
- one of `--entity`, `--fact`, `--episode`, `--source`
- `--event-type TYPES`
- `--as-of`, `--from`, `--to` RFC3339
- `--descending`
- `--limit N`
- `--json`

### `yeoul provenance`
Explain supporting provenance for a record.

#### Example
```bash
yeoul provenance --db ./yeoul.ltdb --fact fact_123
```

#### Flags
- one of `--kind KIND --id ID`, `--entity`, `--fact`, `--episode`
- `--as-of` RFC3339
- `--max-depth N`
- `--json`

### `yeoul inspect`
Inspect storage, schema, and counts.

#### Subcommands
- `schema`
- `counts`
- `entity`
- `fact`
- `episode`
- `source`

#### Examples
```bash
yeoul inspect schema --db ./yeoul.ltdb
yeoul inspect counts --db ./yeoul.ltdb
yeoul inspect entity --db ./yeoul.ltdb --id entity_project_yeoul
yeoul admin migrate-db --db ./legacy-yeoul.lbug --json
```

### `yeoul neighborhood`
Expand around an entity or fact.

#### Example
```bash
yeoul neighborhood --db ./yeoul.ltdb --entity entity_project_yeoul --hops 2
```

#### Flags
- one of `--entity`, `--fact`, `--episode`
- `--hops N`
- `--max-nodes N`
- `--json`

### `yeoul fact`
Manage facts.

#### Subcommands
- `get` — fetch one fact by ID
- `lookup` — query facts by subject, predicate, object, and temporal filters
- `assert` — create a fact, upserting subject/object entities when requested
- `supersede` — replace a fact with a successor
- `retract` — retract a fact with a reason

#### Examples
```bash
yeoul fact get --db ./yeoul.ltdb --id fact_123
yeoul fact lookup --db ./yeoul.ltdb --subject-id entity_project_yeoul --predicate OWNS
yeoul fact assert --db ./yeoul.ltdb --predicate OWNS --upsert-subject --subject-namespace repo:mrchypark/yeoul --subject-type Project --subject-name Yeoul --subject-stable-key yeoul --value-text "Yeoul uses one user-level database" --supporting-episodes ep_000001
yeoul fact retract --db ./yeoul.ltdb --id fact_123 --reason "incorrect source"
```

#### `assert` flags
- `--predicate PRED` required
- subject: `--subject-id ID` or `--upsert-subject` with `--subject-namespace NS --subject-type TYPE --subject-name NAME [--subject-stable-key KEY]`
- object: `--object-id ID` or `--upsert-object` with the same namespace/type/name/key flags
- `--value-text TEXT`
- `--observed-at`, `--valid-from`, `--valid-to` RFC3339
- `--cardinality one|many`
- `--supporting-episodes IDS` required

`--cardinality one` is a single-value slot guard: the assert fails with
`YEOUL_FACT_CONFLICT` when the same space, subject, and predicate slot already has an
overlapping active fact. Nothing is written and nothing is retired. Use
`fact supersede --id ID` to replace a specific existing fact explicitly. Interval
splitting is not implemented.

#### `lookup` flags
- `--subject-id IDS`, `--predicate PREDS`, `--object-id IDS`, `--object-text TEXT`
- `--group-id IDS`
- `--as-of`, `--valid-at`, `--valid-from`, `--valid-to` RFC3339
- `--include-inactive`
- `--limit N`, `--cursor CURSOR`
- `--json`

#### `supersede` flags
- `--id ID` required
- `--predicate PRED`, `--subject-id ID` required, `--object-id ID`
- `--value-text TEXT`
- `--valid-from`, `--valid-to` RFC3339
- `--supporting-episodes IDS` required
- `--reason TEXT` required

`supersede` is target-only: it retires exactly the fact named by `--id` and creates one
successor, even when other active facts occupy the same space, subject, and predicate
slot. The successor records the named fact in its `supersedes` lineage.

### `yeoul entity`
Inspect or manage entities.

#### Subcommands
- `get`
- `resolve` — look up an entity by identity tuple (namespace, type, canonical name, stable key); exits `3` when no entity matches
- `merge-preview` — list merge candidates from export payloads; each candidate reports `drift_namespace`, `drift_type`, and `drift_name` when its sources differ from the target in namespace, type, or canonical name
- `merge` — apply entity merge markers (`--target`, `--source IDS`, `--reason`)

#### `resolve` flags
- `--db PATH` required
- `--type TYPE` required
- `--namespace NS`
- `--name NAME`
- `--stable-key KEY`
- `--space ID`
- `--json`

At least one of `--name` or `--stable-key` must be provided. Exit code `3` when no
entity matches the identity tuple.

### `yeoul policy`
Validate and inspect policy packs.

#### Subcommands
- `validate`
- `show`
- `list-recipes`

#### Example
```bash
yeoul policy validate --path ./agent-pack
```

#### Flags
- `--path PATH` required
- `--json`

### `yeoul index`
Manage derived retrieval projections.

#### Subcommands
- `build`
- `rebuild`
- `verify`
- `status`

#### Examples
```bash
yeoul index build --db ./yeoul.ltdb --root ~/.local/share/yeoul/index
yeoul index verify --db ./yeoul.ltdb --root ~/.local/share/yeoul/index
```

#### Flags
- `--root DIR`

#### Behavior
- treats the index as a derived artifact, not canonical truth
- rebuilds or validates projection state against the LatticeDB-backed Yeoul database

### `yeoul bench`
Run benchmark suites.

#### Subcommands
- `ingest` — `--episodes N [--facts-per-episode N]`
- `query` — `--query TEXT [--entity ID] [--fact ID] [--iterations N]`
- `lifecycle` — `--iterations N`

#### Example
```bash
yeoul bench ingest --db ./bench.ltdb --episodes 100000
```

### `yeoul admin`
Operational commands.

#### Subcommands
- `checkpoint`
- `compact` — `--apply` performs compaction; dry-run by default
- `export` — `--out FILE`
- `import` — `--in FILE`

#### `export`/`import` scope
`admin export` and `admin import` move logical records, not temporal state. Export
deliberately refuses to emit an importable snapshot when the database contains
revision history, inactive facts, or fact lifecycle metadata, because full
fidelity restore is not implemented and a lossy snapshot would silently drop
history. Import re-ingests episodes, entities, and facts as new records, so the
original ingestion times and revisions are not restored. For a full-fidelity
backup that preserves records, revisions, and historical query results, stop all
Yeoul processes and copy the native database directory as described in
[local-storage.md](../08-operations/local-storage.md); `admin export` is not a
temporal backup/restore mechanism.

## Global flags
- `--confirm` — required for destructive operations (stripped before dispatch)
- `--db PATH` — database path for database commands
- `--json` — structured output where supported

## Output modes
The CLI should support:
- human-readable table/text
- JSON output for automation
- exit codes aligned with `error-model.md`

Exit codes: `0` success, `2` usage/input/lifecycle/unsupported errors, `3` not-found errors, `4` query failures, `1` other failures.

The `YEOUL_ENTITY_NEAR_DUPLICATE` code maps to exit `2`; it is a fail-closed guard
that prevents creating a second entity when an existing entity already matches the
identity under a different ID.
