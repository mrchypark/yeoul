package yeoul

// Config controls how the embedded engine is opened.
type Config struct {
	Driver          StorageDriver
	DatabasePath    string
	InMemory        bool
	ReadOnly        bool
	CreateIfMissing bool

	// AllowMigration lets a read-only open convert a legacy database with the
	// default driver. A read-only open is otherwise a no-mutation open: a legacy
	// database it cannot read is reported as requiring an explicit migration
	// instead of being replaced in place. Writable opens convert automatically
	// and do not consult this field.
	//
	// The gate covers starting a conversion, not finishing one. A read-only open
	// that finds the marker of a migration interrupted mid-protocol completes
	// that recovery whether or not AllowMigration is set, because the marker
	// proves the conversion was already authorized and the database may be
	// unreadable until the interrupted protocol finishes. A read-only open of a
	// database with no marker still never converts, so AllowMigration remains
	// the only way for a reader to turn a legacy database into a LatticeDB one.
	AllowMigration bool

	// legacyLadybugWrites enables the non-transactional legacy Ladybug write
	// path. It exists only for in-repo tests that build legacy fixtures; the
	// Ladybug driver is read-only for every other caller.
	legacyLadybugWrites bool

	// legacyStrictRead makes the legacy Ladybug reader reject malformed or
	// missing mandatory data instead of dropping it silently. It is set only
	// by the migration reader; the ordinary read path stays lenient so data
	// users already read successfully keeps loading.
	legacyStrictRead bool
}
