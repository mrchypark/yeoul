package yeoul

// Config controls how the embedded engine is opened.
type Config struct {
	Driver           StorageDriver
	DatabasePath     string
	InMemory         bool
	ReadOnly         bool
	CreateIfMissing  bool
	RhizaBackup      *RhizaBackupConfig
	RestoreIfMissing bool
}

// RhizaBackupConfig opts a local Rhiza database into remote object-store
// backup. Credentials are passed to Rhiza's provider SDKs and are never stored
// in Yeoul database files.
type RhizaBackupConfig struct {
	Provider               string
	Endpoint               string
	Bucket                 string
	Prefix                 string
	Region                 string
	Insecure               bool
	AccessKey              string
	SecretKey              string
	SessionToken           string
	GCSServiceAccount      string
	AzureStorageAccount    string
	AzureStorageAccountKey string
	AzureConnectionString  string
}
