package yeoul

// Config controls how the embedded engine is opened.
type Config struct {
	Driver          StorageDriver
	DatabasePath    string
	InMemory        bool
	ReadOnly        bool
	CreateIfMissing bool

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
