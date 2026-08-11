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
yeoul init --db ./yeoul.lbug
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
yeoul migrate --db ./yeoul.lbug
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
yeoul ingest episode --db ./yeoul.lbug --kind chat_message --content-file ./note.txt --source-id thread_1
yeoul ingest json --db ./yeoul.lbug --file ./episode.json
```

### `yeoul get`
Fetch one record by kind and ID.

#### Example
```bash
yeoul get --db ./yeoul.lbug --kind episode --id ep_000001
```

#### Flags
- `--kind episode|entity|fact|source` required
- `--id ID` required
- `--json`

### `yeoul search`
Run retrieval queries.

#### Examples
```bash
yeoul search --db ./yeoul.lbug --query "recent decisions about ladybug"
yeoul search --db ./yeoul.lbug --query "recent decisions about rax" --backend rax
yeoul search --db ./yeoul.lbug --query "recent decisions" --type fact,episode --group-id project:yeoul --limit 20
```

#### Flags
- `--query TEXT` required
- `--backend auto|core|rax` (default `auto`)
- `--type fact,episode,entity`
- `--group-id IDS`
- `--as-of`, `--valid-at`, `--valid-from`, `--valid-to` RFC3339
- `--limit N`
- `--include-related`
- `--json`

#### Behavior
- defaults to `--backend auto`
- keeps Ladybug-backed Yeoul records as canonical truth
- may use the bundled rax FFI runtime as a derived retrieval signal
- falls back to core Yeoul search in `auto` mode when the rax runtime is unavailable
- fails on rax errors when `--backend rax` is explicitly requested

### `yeoul context`
Build a bounded, factual context bundle from one scoped search response.

#### Example
```bash
yeoul context --db ./yeoul.lbug --query "recent decisions about rax" --json
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
yeoul timeline --db ./yeoul.lbug --entity entity_project_yeoul --descending
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
yeoul provenance --db ./yeoul.lbug --fact fact_123
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
yeoul inspect schema --db ./yeoul.lbug
yeoul inspect counts --db ./yeoul.lbug
yeoul inspect entity --db ./yeoul.lbug --id entity_project_yeoul
```

### `yeoul neighborhood`
Expand around an entity or fact.

#### Example
```bash
yeoul neighborhood --db ./yeoul.lbug --entity entity_project_yeoul --hops 2
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
yeoul fact get --db ./yeoul.lbug --id fact_123
yeoul fact lookup --db ./yeoul.lbug --subject-id entity_project_yeoul --predicate OWNS
yeoul fact assert --db ./yeoul.lbug --predicate OWNS --upsert-subject --subject-namespace repo:mrchypark/yeoul --subject-type Project --subject-name Yeoul --subject-stable-key yeoul --value-text "Yeoul uses one user-level database" --supporting-episodes ep_000001
yeoul fact retract --db ./yeoul.lbug --id fact_123 --reason "incorrect source"
```

#### `assert` flags
- `--predicate PRED` required
- subject: `--subject-id ID` or `--upsert-subject` with `--subject-namespace NS --subject-type TYPE --subject-name NAME [--subject-stable-key KEY]`
- object: `--object-id ID` or `--upsert-object` with the same namespace/type/name/key flags
- `--value-text TEXT`
- `--observed-at`, `--valid-from`, `--valid-to` RFC3339
- `--cardinality one|many`
- `--supporting-episodes IDS` required

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

### `yeoul entity`
Inspect or manage entities.

#### Subcommands
- `get`
- `merge-preview` — list merge candidates from export payloads
- `merge` — apply entity merge markers (`--target`, `--source IDS`, `--reason`)

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
- `publish-rax`

#### Examples
```bash
yeoul index build --db ./yeoul.lbug --root ~/.local/share/yeoul/index
yeoul index verify --db ./yeoul.lbug --root ~/.local/share/yeoul/index
yeoul index publish-rax --root ~/.local/share/yeoul/index --store ~/.local/share/yeoul/rax/projection.rax
```

#### Flags
- `--root DIR`
- `--store FILE` for `publish-rax`
- `--rax-lib PATH`, `--rax-bin PATH` to override the rax runtime

#### Behavior
- treats the index as a derived artifact, not canonical truth
- rebuilds or validates projection state against the Ladybug-backed Yeoul database
- can publish Yeoul-owned projections into a rax FFI-backed `.rax` retrieval index

### `yeoul bench`
Run benchmark suites.

#### Subcommands
- `ingest` — `--episodes N [--facts-per-episode N]`
- `query` — `--query TEXT [--backend auto|core|rax] [--entity ID] [--fact ID] [--iterations N]`
- `lifecycle` — `--iterations N`

#### Example
```bash
yeoul bench ingest --db ./bench.lbug --episodes 100000
```

### `yeoul admin`
Operational commands.

#### Subcommands
- `checkpoint`
- `compact` — `--apply` performs compaction; dry-run by default
- `export` — `--out FILE`
- `import` — `--in FILE`

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
