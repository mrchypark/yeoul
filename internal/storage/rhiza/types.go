package rhiza

import (
	"errors"
	"fmt"
)

type Record struct {
	Key        string
	Kind       string
	ID         string
	Payload    string
	SearchText string
	Deleted    bool
}

type ObjectStoreConfig struct {
	Provider, Endpoint, Bucket, Prefix, Region  string
	Insecure                                    bool
	AccessKey, SecretKey, SessionToken          string
	ServiceAccount                              string
	AzureStorageAccount, AzureStorageAccountKey string
	AzureConnectionString                       string
}

type MutationTooLargeError struct{ EncodedBytes, Limit int }

func (e *MutationTooLargeError) Error() string {
	return fmt.Sprintf("Rhiza graph delta is %d bytes; limit is %d", e.EncodedBytes, e.Limit)
}

var ErrUnsupported = errors.New("Rhiza is unsupported on this platform")
var ErrCommitUnknown = errors.New("Rhiza graph mutation commit outcome is unknown")
