# CLI Specification

The Yeoul CLI is a local developer and operator tool. It is not the primary application integration surface, but it must make the system inspectable and testable.

## Command design principles
- no mandatory network dependency
- clear subcommands
- structured output support
- safe defaults
- destructive operations require confirmation

## Command groups

### `yeoul init`
Create a new database and initialize schema.

#### Example
```bash
yeoul init --db ./yeoul.lbug
```

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

### `yeoul ingest`
Insert data into the memory graph.

#### Subcommands
- `episode`
- `file`
- `json`
- `batch`

#### Examples
```bash
yeoul ingest episode --db ./yeoul.lbug --kind chat_message --content-file ./note.txt --source-id thread_1
yeoul ingest json --db ./yeoul.lbug --file ./episode.json
```

### `yeoul search`
Run retrieval queries.

#### Examples
```bash
yeoul search --db ./yeoul.lbug --query "recent decisions about ladybug"
yeoul search --db ./yeoul.lbug --entity project:yeoul --window 30d
```

### `yeoul neighborhood`
Expand around an entity or fact.

#### Example
```bash
yeoul neighborhood --db ./yeoul.lbug --entity entity_project_yeoul --hops 2
```

### `yeoul fact`
Manage facts.

#### Subcommands
- `get`
- `assert`
- `supersede`
- `retract`

#### Examples
```bash
yeoul fact get --db ./yeoul.lbug --id fact_123
yeoul fact retract --db ./yeoul.lbug --id fact_123 --reason "incorrect source"
```

### `yeoul entity`
Inspect or manage entities.

#### Subcommands
- `get`
- `merge-preview`
- `merge`

### `yeoul policy`
Validate and inspect policy packs.

#### Subcommands
- `validate`
- `show`
- `list-recipes`

#### Example
```bash
yeoul policy validate --path ./policies/default
```

### `yeoul bench`
Run benchmark suites.

#### Example
```bash
yeoul bench ingest --db ./bench.lbug --episodes 100000
```

### `yeoul admin`
Operational commands.

#### Subcommands
- `checkpoint`
- `compact`
- `export`
- `import`

## Global flags
- `--db`
- `--json`
- `--verbose`
- `--policy`
- `--profile`
- `--confirm`

## Output modes
The CLI should support:
- human-readable table/text
- JSON output for automation
- exit codes aligned with `error-model.md`
