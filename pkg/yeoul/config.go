package yeoul

// Config controls how the embedded engine is opened.
type Config struct {
	Driver          StorageDriver
	DatabasePath    string
	InMemory        bool
	ReadOnly        bool
	CreateIfMissing bool
}
