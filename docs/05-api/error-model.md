# Error Model

Yeoul needs a predictable error model across embedded API, CLI, and optional service mode.

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

### Consistency
Examples:
- supersession loop
- invalid lifecycle transition
- entity merge conflict

### NotFound
Examples:
- entity ID not found
- fact ID not found
- recipe name not found

### NotSupported
Examples:
- daemon-only feature in embedded mode
- unsupported policy version
- unsupported query operator

## Suggested error codes
- `YEOUL_CONFIG_INVALID`
- `YEOUL_DB_OPEN_FAILED`
- `YEOUL_DB_LOCK_CONFLICT`
- `YEOUL_DB_MIGRATION_FAILED`
- `YEOUL_QUERY_FAILED`
- `YEOUL_INPUT_INVALID`
- `YEOUL_POLICY_INVALID`
- `YEOUL_ENTITY_NOT_FOUND`
- `YEOUL_FACT_NOT_FOUND`
- `YEOUL_LIFECYCLE_INVALID`
- `YEOUL_NOT_SUPPORTED`

## Retry guidance
- validation errors: not retryable until input changes
- lock conflicts: retryable only after ownership changes
- transient service transport errors: retryable
- lifecycle consistency errors: not retryable without logic change

## CLI mapping
- validation/config errors -> exit 2
- not found -> exit 3
- storage/query failures -> exit 4
- unsafe operation blocked -> exit 5

## Service API mapping
- validation -> HTTP 400
- not found -> HTTP 404
- conflict -> HTTP 409
- unsupported -> HTTP 501 or 400 depending on call shape
- storage lock or unavailable -> HTTP 503 when acting as daemon
