# Data Retention

Yeoul needs explicit retention rules because a memory engine naturally accumulates large volumes of historical state.

## Goals
- preserve important history
- avoid unbounded low-value growth
- keep provenance for active facts
- support privacy and deletion requirements

## Retention classes

### Episode retention
Episodes are the durable substrate and should generally be retained longer than derived helper state.

### Fact retention
Facts should be retained according to lifecycle state and business need.
Active facts should be retained.
Historical facts may be retained or archived depending on policy.

### Source retention
Source references should be retained as long as active or historical facts rely on them.

### Derived-state retention
Derived helper structures may be rebuilt and can have shorter retention.

## Retention policy dimensions
- time since ingest
- time since last active use
- lifecycle state
- provenance importance
- source class
- privacy or legal requirement
- project-specific policy

## Default stance
For MVP:
- keep Episodes
- keep active Facts
- keep superseded/retracted Facts unless explicit archive policy says otherwise
- never delete the only provenance support for an active fact

## Retention actions
- keep
- archive
- redact
- export then delete
- delete derived-state only

## Operator controls
Retention tooling should support:
- dry-run mode
- summary report
- scope filters
- policy version stamp
- rollback notes for destructive actions

## Relationship to compaction
Retention decides what is eligible.
Compaction performs the graph changes needed to realize that decision safely.

## Privacy note
Retention and privacy are related but not identical.
A privacy deletion request may override normal retention windows.
