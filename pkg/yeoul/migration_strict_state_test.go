package yeoul

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigrateDatabaseStrictReaderRejectsMalformedState covers persisted values
// that the lenient reader coerces instead of rejecting: the singleton meta
// sequence and the nullable SUPERSEDES reason. Both are live state, so the
// strict reader must fail the migration rather than silently rewrite them
// before the target database is verified.
func TestMigrateDatabaseStrictReaderRejectsMalformedState(t *testing.T) {
	useInProcessMigration(t)
	ctx := context.Background()
	cases := []struct {
		name       string
		statements []string
	}{
		{
			name:       "negative meta sequence",
			statements: []string{"MATCH (m:YeoulMeta {id:'singleton'}) SET m.sequence = -5"},
		},
		{
			name:       "null meta sequence",
			statements: []string{"MATCH (m:YeoulMeta {id:'singleton'}) SET m.sequence = NULL"},
		},
		{
			name: "unexpectedly typed supersedes reason",
			statements: []string{
				"DROP TABLE SUPERSEDES",
				"CREATE REL TABLE SUPERSEDES(FROM Fact TO Fact, reason INT64, created_at TIMESTAMP)",
				"MATCH (a:Fact), (b:Fact) WHERE a.id <> b.id CREATE (a)-[:SUPERSEDES {reason: 3, created_at: timestamp('2024-01-01T00:00:00Z')}]->(b)",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "malformed-state.lbug")
			writeStrictLegacyFixture(t, dbPath)
			execLegacyStatements(t, dbPath, tc.statements...)
			before := legacyDatabaseFileSet(t, dbPath)

			_, err := MigrateDatabase(ctx, dbPath)
			if err == nil {
				t.Fatal("expected migration to reject malformed legacy state")
			}
			yeoulErr := unwrapYeoulError(err)
			if yeoulErr == nil || yeoulErr.Code != ErrStorageFailed {
				t.Fatalf("expected %s error, got %v", ErrStorageFailed, err)
			}
			assertNoMigrationArtifacts(t, dbPath)
			if after := legacyDatabaseFileSet(t, dbPath); !mapsEqual(before, after) {
				t.Fatalf("failed migration changed the source file set\nbefore: %v\nafter:  %v", before, after)
			}
		})
	}
}
