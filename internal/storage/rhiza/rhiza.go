//go:build linux || darwin

package rhiza

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	engine "github.com/mrchypark/rhiza"
)

const pageSize = 64

type DB struct{ db *engine.DB }

func Supported() bool { return true }

func Open(dataDir string, backups ...*ObjectStoreConfig) (*DB, error) {
	if len(backups) > 1 {
		return nil, fmt.Errorf("Rhiza accepts at most one object-store configuration")
	}
	var backup *ObjectStoreConfig
	if len(backups) == 1 {
		backup = backups[0]
	}
	config := engine.Config{
		Local:                         true,
		NodeID:                        "yeoul-local",
		DataDir:                       dataDir,
		LocalGraphNodePropertyIndexes: []engine.GraphNodePropertyIndex{{Label: "YeoulRecord", Property: "key"}},
	}
	if backup != nil {
		config.ObjStoreProvider = backup.Provider
		config.ObjStoreEndpoint = backup.Endpoint
		config.ObjStoreBucket = backup.Bucket
		config.ObjStorePrefix = backup.Prefix
		config.ObjStoreRegion = backup.Region
		config.ObjStoreInsecure = backup.Insecure
		config.ObjStoreAccessKey = backup.AccessKey
		config.ObjStoreSecretKey = backup.SecretKey
		config.ObjStoreSessionToken = backup.SessionToken
		config.ObjStoreServiceAccount = backup.ServiceAccount
		config.ObjStoreAzureStorageAccount = backup.AzureStorageAccount
		config.ObjStoreAzureStorageAccountKey = backup.AzureStorageAccountKey
		config.ObjStoreAzureConnectionString = backup.AzureConnectionString
		config.ObjStoreDurability = engine.ObjectStoreDurabilityBeforeAck
	}
	db, err := engine.Open(context.Background(), config)
	if err != nil {
		return nil, err
	}
	return &DB{db: db}, nil
}

func (db *DB) Close() error { return db.db.Close() }

func (db *DB) Apply(ctx context.Context, requestID string, records []Record) error {
	rows := make([]any, len(records))
	keys := make([]any, len(records))
	for i, record := range records {
		keys[i] = record.Key
		rows[i] = map[string]any{
			"key": record.Key, "kind": record.Kind, "id": record.ID,
			"payload": record.Payload, "search_text": record.SearchText, "deleted": record.Deleted,
		}
	}
	command := engine.GraphCommand{
		RequestID: requestID,
		Cypher: `UNWIND $rows AS row
MERGE (r:YeoulRecord {key: row.key})
SET r.kind = row.kind, r.id = row.id, r.payload = row.payload, r.search_text = row.search_text, r.deleted = row.deleted`,
		Args:   map[string]any{"rows": rows},
		Events: []engine.GraphStreamEvent{{Stream: "yeoul.catalog.v1", Kind: "records.changed", Payload: map[string]any{"keys": keys}}},
	}
	encoded, err := json.Marshal(command)
	if err != nil {
		return err
	}
	if len(encoded) > 96<<10 {
		return &MutationTooLargeError{EncodedBytes: len(encoded), Limit: 96 << 10}
	}
	response, err := db.db.GraphExecute(ctx, command)
	if err == nil {
		if response.Status != engine.MutationCommitted {
			return fmt.Errorf("Rhiza graph mutation status %q", response.Status)
		}
		return nil
	}
	if !errors.Is(err, engine.ErrCommitUnknown) {
		return err
	}
	status, statusErr := db.db.RequestStatus(ctx, engine.RequestStatusRequest{Kind: engine.RequestKindGraph, RequestID: requestID})
	if statusErr == nil {
		switch status.State {
		case engine.RequestStateCommitted:
			return nil
		case engine.RequestStateRejected:
			return fmt.Errorf("Rhiza graph mutation was rejected: %s", status.Receipt.ErrorCode)
		}
	}
	response, retryErr := db.db.GraphExecute(ctx, command)
	if retryErr == nil {
		if response.Status == engine.MutationCommitted {
			return nil
		}
		return fmt.Errorf("Rhiza graph mutation retry status %q", response.Status)
	}
	return errors.Join(ErrCommitUnknown, engine.ErrCommitUnknown, err, statusErr, retryErr)
}

func (db *DB) Records(ctx context.Context) ([]Record, error) {
	// The catalog is canonical recovery metadata and must not be trimmed.
	// shortcut: startup replays all history; add verified snapshots if this becomes costly.
	keys := make(map[string]struct{})
	var after uint64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := db.db.GraphStreamRead(ctx, engine.GraphStreamReadRequest{
			Stream: "yeoul.catalog.v1", AfterSequence: after, Limit: pageSize, Consistency: engine.ConsistencyLocal,
		})
		if err != nil {
			return nil, err
		}
		for _, event := range result.Records {
			if event.Sequence != after+1 || event.Kind != "records.changed" {
				return nil, fmt.Errorf("Rhiza Yeoul catalog has a sequence gap or unknown event at %d", event.Sequence)
			}
			payload, ok := event.Payload.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("Rhiza Yeoul catalog event %d has invalid payload", event.Sequence)
			}
			eventKeys, ok := payload["keys"].([]any)
			if !ok {
				return nil, fmt.Errorf("Rhiza Yeoul catalog event %d has invalid keys", event.Sequence)
			}
			for _, value := range eventKeys {
				key, ok := value.(string)
				if !ok || key == "" {
					return nil, fmt.Errorf("Rhiza Yeoul catalog event %d has invalid key", event.Sequence)
				}
				keys[key] = struct{}{}
			}
			after = event.Sequence
		}
		// GraphStreamRead has no HasMore field. A short page is not a terminal
		// signal, so only an empty read ends replay.
		if len(result.Records) == 0 {
			break
		}
	}
	if after == 0 {
		result, err := db.db.GraphQuery(ctx, engine.GraphQueryRequest{
			Cypher: `MATCH (r:YeoulRecord {key: $key}) RETURN r.key LIMIT 1`,
			Args:   map[string]any{"key": "YeoulMeta:c3RhdGU"}, Consistency: engine.ConsistencyLocal,
		})
		if err != nil {
			return nil, err
		}
		if len(result.Rows) != 0 {
			return nil, fmt.Errorf("Rhiza Yeoul catalog is empty but application metadata exists")
		}
	}
	var records []Record
	for key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := db.db.GraphQuery(ctx, engine.GraphQueryRequest{
			Cypher: `MATCH (r:YeoulRecord {key: $key}) RETURN r.key, r.kind, r.id, r.payload, r.search_text, r.deleted`,
			Args:   map[string]any{"key": key}, Consistency: engine.ConsistencyLocal,
		})
		if err != nil {
			return nil, err
		}
		if len(result.Rows) != 1 {
			return nil, fmt.Errorf("Rhiza Yeoul catalog references missing or duplicate record %q", key)
		}
		row := result.Rows[0]
		if len(row) != 6 {
			return nil, fmt.Errorf("Rhiza YeoulRecord row has %d columns", len(row))
		}
		gotKey, keyOK := row[0].(string)
		kind, kindOK := row[1].(string)
		id, idOK := row[2].(string)
		payload, payloadOK := row[3].(string)
		searchText, searchOK := row[4].(string)
		deleted, deletedOK := row[5].(bool)
		if !keyOK || !kindOK || !idOK || !payloadOK || !searchOK || !deletedOK || gotKey != key {
			return nil, fmt.Errorf("Rhiza YeoulRecord row has invalid properties for %q", key)
		}
		records = append(records, Record{Key: gotKey, Kind: kind, ID: id, Payload: payload, SearchText: searchText, Deleted: deleted})
	}
	return records, nil
}

func (db *DB) Search(ctx context.Context, query string) ([]Record, error) {
	result, err := db.db.GraphQuery(ctx, engine.GraphQueryRequest{
		Cypher: `MATCH (r:YeoulRecord) WHERE r.deleted = false AND r.kind IN $kinds AND r.search_text @@ $query RETURN r.kind, r.id`,
		Args:   map[string]any{"kinds": []any{"Fact", "Episode", "Entity"}, "query": query}, Consistency: engine.ConsistencyLocal,
	})
	if err != nil {
		return nil, err
	}
	rows := make([]Record, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) != 2 {
			return nil, fmt.Errorf("Rhiza search row has %d columns", len(row))
		}
		kind, kindOK := row[0].(string)
		id, idOK := row[1].(string)
		if !kindOK || !idOK {
			return nil, fmt.Errorf("Rhiza search row has invalid properties")
		}
		rows = append(rows, Record{Kind: kind, ID: id})
	}
	return rows, nil
}
