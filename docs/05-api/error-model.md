# Error Model

Yeoul needs a predictable error model across the embedded API and the CLI. Service mode is planned
and unavailable in current releases, so the service mapping below is a design target, not a
supported surface.

## Goals
- stable machine-readable errors
- human-readable diagnostics
- clear retry guidance
- consistent mapping across interfaces

## Error shape
Every surfaced error should provide:
- `code`
- `message`
- `category`
- `retryable`
- `details` (optional)
- `cause` (optional internal chain)

## Error categories

### Configuration
Examples:
- invalid database path
- missing policy directory
- unsupported config combination

### Storage
Examples:
- cannot open database
- lock conflict
- migration failure
- query execution failure

### Validation
Examples:
- malformed episode input
- missing required field
- invalid ontology file
- unsupported recipe parameter
- input that matches a recognized credential shape (`YEOUL_INPUT_INVALID`); the
  rejection reports only the input path and the credential class, never the
  rejected value

### Consistency
Examples:
- supersession loop
- invalid lifecycle transition
- entity merge conflict
- near-duplicate entity guard on fact assert (`YEOUL_ENTITY_NEAR_DUPLICATE`)
- single-value slot conflict on fact assert (`YEOUL_FACT_CONFLICT`)

### NotFound
Examples:
- entity ID not found
- fact ID not found
- recipe name not found

### NotSupported
Examples:
- planned service-only feature in embedded mode
- unsupported policy version
- unsupported query operator
- read-only open of a database that requires migration (`YEOUL_NOT_SUPPORTED`)

## Suggested error codes
- `YEOUL_CONFIG_INVALID`
- `YEOUL_DB_OPEN_FAILED`
- `YEOUL_DB_LOCK_CONFLICT`
- `YEOUL_DB_MIGRATION_FAILED`
- `YEOUL_QUERY_FAILED`
- `YEOUL_INPUT_INVALID`
- `YEOUL_POLICY_INVALID`
- `YEOUL_ENTITY_NOT_FOUND`
- `YEOUL_ENTITY_NEAR_DUPLICATE`
- `YEOUL_FACT_NOT_FOUND`
- `YEOUL_FACT_CONFLICT`
- `YEOUL_LIFECYCLE_INVALID`
- `YEOUL_NOT_SUPPORTED`

## Retry guidance
- validation errors: not retryable until input changes
- lock conflicts: retryable only after ownership changes
- transient service transport errors: retryable
- lifecycle consistency errors: not retryable without logic change

## CLI mapping

The CLI classifies the concrete error codes below, not every error that shares a
semantic label:

- success -> exit 0
- usage errors (including a destructive command that is missing `--confirm`) and the `YEOUL_CONFIG_INVALID`, `YEOUL_INPUT_INVALID`, `YEOUL_FACT_CONFLICT`, `YEOUL_LIFECYCLE_INVALID`, `YEOUL_ENTITY_NEAR_DUPLICATE`, and `YEOUL_NOT_SUPPORTED` codes -> exit 2
- the `YEOUL_ENTITY_NOT_FOUND`, `YEOUL_FACT_NOT_FOUND`, and `YEOUL_SOURCE_NOT_FOUND` codes -> exit 3
- the `YEOUL_QUERY_FAILED` code -> exit 4
- every other error -> exit 1, including `YEOUL_STORAGE_FAILED` and plain errors such as a missing search recipe or an unsupported recipe strategy

There is no separate exit code for blocked operations, and an error that merely
describes a missing record or an unsupported operation still exits 1 unless it
carries one of the codes listed above.

## Service API mapping (planned; no service is implemented in current releases)
- validation -> HTTP 400
- not found -> HTTP 404
- conflict -> HTTP 409
- unsupported -> HTTP 501 or 400 depending on call shape
- storage lock or unavailable -> HTTP 503 when acting as daemon
