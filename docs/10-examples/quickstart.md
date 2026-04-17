# Quickstart

This quickstart shows the intended first-run experience for Yeoul.

## Goal
Create a local Yeoul database, ingest a simple episode, and retrieve the resulting memory.

## 1. Initialize the database
```bash
yeoul init --db ./yeoul.lbug
```

## 2. Validate the example policy pack
```bash
yeoul policy validate --path ./policies/default
```

## 3. Ingest an episode
```bash
yeoul ingest episode   --db ./yeoul.lbug   --kind chat_message   --source-id thread_1   --observed-at 2026-04-16T12:00:00Z   --content "We decided to use Ladybug for Yeoul and keep AI behavior outside the core."
```

## 4. Inspect counts
```bash
yeoul inspect counts --db ./yeoul.lbug
```

## 5. Search recent context
```bash
yeoul search --db ./yeoul.lbug --query "what did we decide about the storage engine?"
```

## 6. Explain a fact
```bash
yeoul fact get --db ./yeoul.lbug --id fact_001
```

## 7. Expand a neighborhood
```bash
yeoul neighborhood --db ./yeoul.lbug --entity entity_project_yeoul --hops 2
```

## Expected outcome
You should see:
- an Episode
- one or more Entities
- one or more Facts
- provenance linking the Fact back to the Episode and Source

## Next steps
- try the example skill pack
- ingest a batch file
- test superseding a fact
- run benchmark mode on a larger dataset
