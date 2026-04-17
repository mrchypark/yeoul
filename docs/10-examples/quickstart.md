# Quickstart

This quickstart shows the intended first-run experience for Yeoul.

## Goal
Create a local Yeoul database, ingest a simple episode, and retrieve the resulting memory.

## 1. Initialize the database
```bash
yeoul init --db ./yeoul.lbug
```

## 2. Validate the bundled agent pack
```bash
yeoul policy validate --path ./agent-pack
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

## 6. Inspect the stored episode
```bash
yeoul get --db ./yeoul.lbug --kind episode --id ep_000001
```

## 7. List bundled search recipes
```bash
yeoul policy list-recipes --path ./agent-pack
```

## Expected outcome
You should see:
- one Source
- one Episode
- zero Entities
- zero Facts

Plain-text episode ingest stores the source episode and makes it searchable, but it does not automatically materialize entities or facts from free text.
To populate entities or facts, ingest structured JSON or use explicit fact lifecycle commands such as `yeoul fact assert`.

## Next steps
- try the example skill pack
- ingest a batch file
- test superseding a fact
- run benchmark mode on a larger dataset
