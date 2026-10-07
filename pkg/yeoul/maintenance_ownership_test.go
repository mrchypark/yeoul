package yeoul

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func seedMaintenanceDuplicateEntity(t *testing.T, eng Engine, id string) *Entity {
	t.Helper()
	entity, err := eng.UpsertEntity(context.Background(), EntityInput{
		ID:            id,
		SpaceID:       "default",
		Namespace:     "default",
		Type:          "Person",
		CanonicalName: "Alex",
		Metadata:      map[string]any{"stable_key": "alex-1"},
	})
	if err != nil {
		t.Fatalf("upsert entity %s: %v", id, err)
	}
	return entity
}

func maintenanceDuplicateEntityIDs(t *testing.T, eng Engine, canonicalName string) []string {
	t.Helper()
	snapshot, err := Snapshot(context.Background(), eng)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	ids := make([]string, 0, len(snapshot.Entities))
	for _, record := range snapshot.Entities {
		if record.CanonicalName != canonicalName {
			continue
		}
		ids = append(ids, record.ID)
	}
	slices.Sort(ids)
	return ids
}

// TestOpenMaintenanceRefusesWhileAnotherOwnerHoldsTheDatabase covers the
// refusal a maintenance operation gets when the writable ownership it needs is
// already held: the operation must report that another owner has the database
// instead of reading state that owner may still change.
func TestOpenMaintenanceRefusesWhileAnotherOwnerHoldsTheDatabase(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "maintenance-busy.ltdb")

	eng, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if _, err := OpenMaintenance(ctx, dbPath); err == nil {
		t.Fatal("expected maintenance to be refused while another store holds the database")
	} else if !strings.Contains(err.Error(), "database is owned by another process") {
		t.Fatalf("expected an ownership refusal, got %v", err)
	}
	if err := eng.Close(ctx); err != nil {
		t.Fatalf("close engine: %v", err)
	}

	exclusive, err := acquireDatabaseOwnership(dbPath, true)
	if err != nil {
		t.Fatalf("acquire exclusive ownership: %v", err)
	}
	if _, err := OpenMaintenance(ctx, dbPath); err == nil {
		t.Fatal("expected maintenance to be refused while the exclusive lock is held")
	} else if !strings.Contains(err.Error(), "database is owned by another process") {
		t.Fatalf("expected an ownership refusal, got %v", err)
	}
	if err := exclusive.Release(); err != nil {
		t.Fatalf("release exclusive ownership: %v", err)
	}
}

// TestOpenMaintenanceOwnsTheDatabaseForItsLifetime verifies the ownership a
// maintenance window holds: every other open is refused while the window is
// open, including read-only opens, and ownership is free again once the window
// is closed.
func TestOpenMaintenanceOwnsTheDatabaseForItsLifetime(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "maintenance-window.ltdb")

	seed, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	seedMaintenanceDuplicateEntity(t, seed, "person:alpha")
	if err := seed.Close(ctx); err != nil {
		t.Fatalf("close seed engine: %v", err)
	}

	maintenance, err := OpenMaintenance(ctx, dbPath)
	if err != nil {
		t.Fatalf("open maintenance window: %v", err)
	}
	if _, err := acquireDatabaseOwnership(dbPath, false); !errors.Is(err, errDatabaseOwnershipBusy) {
		t.Fatalf("expected the shared lock to be refused during a maintenance window, got %v", err)
	}
	if _, err := acquireDatabaseOwnership(dbPath, true); !errors.Is(err, errDatabaseOwnershipBusy) {
		t.Fatalf("expected the exclusive lock to be refused during a maintenance window, got %v", err)
	}
	if _, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true}); err == nil {
		t.Fatal("expected a read-only open to be refused during a maintenance window")
	} else if !strings.Contains(err.Error(), "database migration is in progress") {
		t.Fatalf("expected a migration-in-progress refusal, got %v", err)
	}

	if err := maintenance.Close(ctx); err != nil {
		t.Fatalf("close maintenance window: %v", err)
	}
	shared, err := acquireDatabaseOwnership(dbPath, false)
	if err != nil {
		t.Fatalf("expected the shared lock to be free after the window closed: %v", err)
	}
	if err := shared.Release(); err != nil {
		t.Fatalf("release shared ownership: %v", err)
	}
	exclusive, err := acquireDatabaseOwnership(dbPath, true)
	if err != nil {
		t.Fatalf("expected the exclusive lock to be free after the window closed: %v", err)
	}
	if err := exclusive.Release(); err != nil {
		t.Fatalf("release exclusive ownership: %v", err)
	}
}

// TestMaintenanceWindowStateCannotChangeUnderneathTheOperation covers the gap
// issue #42 reports. Compaction discovers its candidates from one snapshot and
// then changes exactly the records that snapshot named. That plan is only safe
// while the records in it cannot change, so the operation has to hold writable
// ownership from before the discovery: no other owner may open the database for
// the whole window, and the state the operation reads is the state it applies.
//
// Without the window the competing write below succeeds, which is exactly how a
// candidate changed between discovery and the writable open.
func TestMaintenanceWindowStateCannotChangeUnderneathTheOperation(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "maintenance-state.ltdb")

	seed, err := Open(ctx, Config{DatabasePath: dbPath, CreateIfMissing: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	seedMaintenanceDuplicateEntity(t, seed, "person:alpha")
	seedMaintenanceDuplicateEntity(t, seed, "person:beta")
	seedMaintenanceDuplicateEntity(t, seed, "person:gamma")
	if err := seed.Close(ctx); err != nil {
		t.Fatalf("close seed engine: %v", err)
	}

	// The snapshot a discovery taken outside the window would produce: the
	// three entities share one identity, so it names one duplicate group.
	discovery, err := Open(ctx, Config{DatabasePath: dbPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("open discovery engine: %v", err)
	}
	discovered := maintenanceDuplicateEntityIDs(t, discovery, "Alex")
	if err := discovery.Close(ctx); err != nil {
		t.Fatalf("close discovery engine: %v", err)
	}
	if len(discovered) != 3 {
		t.Fatalf("expected three duplicate candidates before the change, got %v", discovered)
	}

	maintenance, err := OpenMaintenance(ctx, dbPath)
	if err != nil {
		t.Fatalf("open maintenance window: %v", err)
	}
	// The change that used to land in the gap between discovery and the
	// writable open that applied the candidates. The lock every other open
	// takes is refused for the whole window, so nothing can change the records
	// the plan names while the plan is being applied.
	if _, err := acquireDatabaseOwnership(dbPath, false); !errors.Is(err, errDatabaseOwnershipBusy) {
		t.Fatalf("expected the lock every ordinary open takes to be refused for the whole window, got %v", err)
	}
	owned := maintenanceDuplicateEntityIDs(t, maintenance, "Alex")
	if !slices.Equal(discovered, owned) {
		t.Fatalf("expected the discovered records to still be the records in the database, got %v then %v", discovered, owned)
	}

	// The window is writable, so the plan it discovered can be applied to it.
	if _, err := maintenance.UpsertEntity(ctx, EntityInput{
		ID:            "person:beta",
		SpaceID:       "default",
		Namespace:     "default",
		Type:          "Person",
		CanonicalName: "Alexandra",
		Metadata:      map[string]any{"stable_key": "alex-1"},
	}); err != nil {
		t.Fatalf("rename entity inside the window: %v", err)
	}
	renamed, err := maintenance.GetEntity(ctx, "person:beta")
	if err != nil {
		t.Fatalf("get renamed entity: %v", err)
	}
	if renamed.CanonicalName != "Alexandra" {
		t.Fatalf("expected the window to apply its own change, got %q", renamed.CanonicalName)
	}
	if err := maintenance.Close(ctx); err != nil {
		t.Fatalf("close maintenance window: %v", err)
	}

	reopened, err := Open(ctx, Config{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	persisted, err := reopened.GetEntity(ctx, "person:beta")
	if err != nil {
		_ = reopened.Close(ctx)
		t.Fatalf("get persisted entity: %v", err)
	}
	if err := reopened.Close(ctx); err != nil {
		t.Fatalf("close reopened engine: %v", err)
	}
	if persisted.CanonicalName != "Alexandra" {
		t.Fatalf("expected the change made in the window to persist, got %q", persisted.CanonicalName)
	}
}
