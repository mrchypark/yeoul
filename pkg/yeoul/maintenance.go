package yeoul

import (
	"context"
	"strings"
)

// OpenMaintenance acquires writable ownership of databasePath and opens it
// read-write inside that ownership window. Explicit maintenance holds the window
// for its whole operation and closes the returned engine to release it.
//
// Compaction is the operation this exists for (issue #42). It used to discover
// its candidates on one short-lived read open and then apply them through a
// second write open, so another process could change exactly those records in
// the gap and the stale candidate list was applied to the changed database. A
// maintenance window removes the gap: the database is opened only after the
// writable ownership is acquired, so the state the caller discovers is the state
// the caller changes, and every other process is refused for the whole window
// instead of being able to interleave a change.
//
// The ownership taken here is the exclusive lock, so it is only granted while no
// other store is open on the database, not even a read-only one. An operation
// that cannot take it reports the refusal instead of acting on state another
// owner may still change. The window should last no longer than the maintenance
// operation itself.
func OpenMaintenance(ctx context.Context, databasePath string) (Engine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(databasePath) == "" {
		return nil, errorf(ErrConfigInvalid, "database path is required for maintenance", nil, nil)
	}
	cfg := Config{DatabasePath: databasePath}
	store, err := openStateStoreWithOwnership(cfg, openStoreExclusiveOwnership)
	if err != nil {
		return nil, err
	}
	state, err := store.Load()
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	maintenance := newEngine(cfg, store)
	maintenance.applyState(*state)
	return maintenance, nil
}
