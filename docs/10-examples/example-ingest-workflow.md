# Example Ingest Workflow

This example shows a policy-aware ingest path for a coding-agent integration.

## Input event
A chat message arrives:

> We decided to keep Yeoul agent-free in the core and move behavior into policy packs.

## Step 1: Normalize event
Create a normalized envelope:
- event type: `chat_message`
- source: `thread_42`
- observed_at: timestamp
- raw content: message text

## Step 2: Apply episode rules
The rules identify this as a decision-bearing event.
Result: promote to Episode.

## Step 3: Create Episode
Write an Episode node and link it to its Source.

## Step 4: Extract entities
Candidate entities:
- `Yeoul` (Project)
- `policy packs` (Concept or ProjectComponent)
- `core` (ProjectComponent)

## Step 5: Assert facts
Potential facts:
- `Yeoul` `DECIDED` `agent-free core`
- `Yeoul` `CHANGED_TO` `policy-pack behavior externalization`

## Step 6: Attach provenance
Link each Fact to the Episode with `DERIVED_FROM` or `SUPPORTED_BY`.

## Step 7: Search later
A later query like “Why is Yeoul agent-free in the core?” should return:
- the active fact
- supporting episode
- related decision context
- timeline if the rule later changes
