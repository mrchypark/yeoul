//go:build linux || darwin

package yeoul

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrchypark/rhiza"
	rhizastore "github.com/mrchypark/yeoul/internal/storage/rhiza"
)

func TestRhizaLocalGraphDeltaReopen(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "rhiza")
	open := func() *rhiza.DB {
		t.Helper()
		db, err := rhiza.Open(ctx, rhiza.Config{
			Local: true, NodeID: "yeoul-local", DataDir: dir,
			LocalGraphNodePropertyIndexes: []rhiza.GraphNodePropertyIndex{{Label: "YeoulRecord", Property: "key"}},
		})
		if err != nil {
			t.Fatalf("open Rhiza: %v", err)
		}
		return db
	}
	write := func(db *rhiza.DB, requestID, id, payload string) {
		t.Helper()
		response, err := db.GraphExecute(ctx, rhiza.GraphCommand{
			RequestID: requestID,
			Cypher: `UNWIND $rows AS row
MERGE (r:YeoulRecord {key: row.key})
SET r.kind = row.kind, r.id = row.id, r.payload = row.payload, r.search_text = row.search_text, r.deleted = false`,
			Events: []rhiza.GraphStreamEvent{{Stream: "yeoul.catalog.v1", Kind: "records.changed", Payload: map[string]any{"keys": []any{"Episode:" + id}}}},
			Args: map[string]any{"rows": []any{map[string]any{
				"key": "Episode:" + id, "kind": "Episode", "id": id,
				"payload": payload, "search_text": "durable scout", "deleted": false,
			}}},
		})
		if err != nil {
			t.Fatalf("GraphExecute: %v", err)
		}
		if response.Status != rhiza.MutationCommitted {
			t.Fatalf("GraphExecute status = %q, want committed", response.Status)
		}
	}
	read := func(db *rhiza.DB, id string) string {
		t.Helper()
		result, err := db.GraphQuery(ctx, rhiza.GraphQueryRequest{
			Cypher:      `MATCH (r:YeoulRecord {key: $key}) RETURN r.payload`,
			Args:        map[string]any{"key": "Episode:" + id},
			Consistency: rhiza.ConsistencyLocal,
		})
		if err != nil {
			t.Fatalf("GraphQuery: %v", err)
		}
		if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
			t.Fatalf("GraphQuery rows = %#v", result.Rows)
		}
		value, ok := result.Rows[0][0].(string)
		if !ok {
			t.Fatalf("payload type = %T", result.Rows[0][0])
		}
		return value
	}

	db := open()
	write(db, "delta-1", "episode-1", `{"content":"first"}`)
	if got := read(db, "episode-1"); got != `{"content":"first"}` {
		t.Fatalf("payload = %q", got)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close Rhiza: %v", err)
	}

	db = open()
	if got := read(db, "episode-1"); got != `{"content":"first"}` {
		t.Fatalf("reopened payload = %q", got)
	}
	write(db, "delta-2", "episode-1", `{"content":"updated"}`)
	if got := read(db, "episode-1"); got != `{"content":"updated"}` {
		t.Fatalf("updated payload = %q", got)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close Rhiza before bulk writes: %v", err)
	}
	db = open()
	for batch, bounds := range [][2]int{{0, 4999}, {5000, 10000}} {
		keys := make([]any, 0, bounds[1]-bounds[0]+1)
		for i := bounds[0]; i <= bounds[1]; i++ {
			keys = append(keys, fmt.Sprintf("%d", i))
		}
		response, err := db.GraphExecute(ctx, rhiza.GraphCommand{
			RequestID: fmt.Sprintf("bulk-pagination-%d", batch),
			Cypher: fmt.Sprintf(`UNWIND range(%d, %d) AS i
CREATE (:YeoulRecord {key: toString(i), kind: 'Probe', id: toString(i), payload: '{}', search_text: '', deleted: false})`, bounds[0], bounds[1]),
			Events: []rhiza.GraphStreamEvent{{Stream: "yeoul.catalog.v1", Kind: "records.changed", Payload: map[string]any{"keys": keys}}},
		})
		if err != nil {
			t.Fatalf("bulk GraphExecute batch %d: %v", batch, err)
		}
		if response.Status != rhiza.MutationCommitted {
			t.Fatalf("bulk GraphExecute batch %d status = %q", batch, response.Status)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close bulk Rhiza: %v", err)
	}
	adapter, err := rhizastore.Open(dir)
	if err != nil {
		t.Fatalf("open Yeoul Rhiza adapter: %v", err)
	}
	defer adapter.Close()
	rows, err := adapter.Records(ctx)
	if err != nil {
		t.Fatalf("read paginated records: %v", err)
	}
	if len(rows) != 10002 {
		t.Fatalf("paginated record count = %d, want 10002", len(rows))
	}
	id := "after-bulk"
	key := "Probe:" + base64.RawURLEncoding.EncodeToString([]byte(id))
	if err := adapter.Apply(ctx, "write-after-bulk", []rhizastore.Record{{
		Key: key, Kind: "Probe", ID: id, Payload: `{"value":"updated"}`,
	}}); err != nil {
		t.Fatalf("apply record after bulk corpus: %v", err)
	}
	rows, err = adapter.Records(ctx)
	if err != nil {
		t.Fatalf("read after bulk write: %v", err)
	}
	if len(rows) != 10003 {
		t.Fatalf("record count after bulk write = %d, want 10003", len(rows))
	}
	found := false
	for _, row := range rows {
		if row.Key == key && row.Payload == `{"value":"updated"}` {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("post-bulk record was not returned with its updated payload")
	}
}

func TestRhizaCatalogTrimFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name  string
		trim  int
		match string
	}{
		{name: "gap", trim: 1, match: "sequence gap"},
		{name: "fully trimmed", trim: 3, match: "catalog is empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			dir := filepath.Join(t.TempDir(), "rhiza")
			db, err := rhiza.Open(ctx, rhiza.Config{
				Local: true, NodeID: "yeoul-local", DataDir: dir,
				LocalGraphNodePropertyIndexes: []rhiza.GraphNodePropertyIndex{{Label: "YeoulRecord", Property: "key"}},
			})
			if err != nil {
				t.Fatalf("open Rhiza: %v", err)
			}
			for i := 1; i <= 2; i++ {
				key := fmt.Sprintf("Episode:%d", i)
				response, err := db.GraphExecute(ctx, rhiza.GraphCommand{
					RequestID: fmt.Sprintf("catalog-trim-%d", i),
					Cypher:    fmt.Sprintf("CREATE (:YeoulRecord {key: '%s', kind: 'Episode', id: '%d', payload: '{}', search_text: '', deleted: false})", key, i),
					Events:    []rhiza.GraphStreamEvent{{Stream: "yeoul.catalog.v1", Kind: "records.changed", Payload: map[string]any{"keys": []any{key}}}},
				})
				if err != nil || response.Status != rhiza.MutationCommitted {
					t.Fatalf("append catalog event %d: response=%+v err=%v", i, response, err)
				}
			}
			if _, err := db.GraphExecute(ctx, rhiza.GraphCommand{
				RequestID: "catalog-trim-meta",
				Cypher:    `CREATE (:YeoulRecord {key: 'YeoulMeta:c3RhdGU', kind: 'YeoulMeta', id: 'state', payload: '{"version":1,"sequence":0}', search_text: '', deleted: false})`,
				Events:    []rhiza.GraphStreamEvent{{Stream: "yeoul.catalog.v1", Kind: "records.changed", Payload: map[string]any{"keys": []any{"YeoulMeta:c3RhdGU"}}}},
			}); err != nil {
				t.Fatalf("write metadata record: %v", err)
			}
			stream, err := db.GraphStreamRead(ctx, rhiza.GraphStreamReadRequest{Stream: "yeoul.catalog.v1", Limit: 64})
			if err != nil || len(stream.Records) != 3 {
				t.Fatalf("read catalog before trim: records=%d err=%v", len(stream.Records), err)
			}
			if err := db.TrimGraphStream(ctx, rhiza.GraphStreamTrimRequest{
				RequestID: "catalog-trim", Stream: "yeoul.catalog.v1", ThroughSequence: uint64(test.trim),
			}); err != nil {
				t.Fatalf("trim catalog: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close Rhiza: %v", err)
			}
			adapter, err := rhizastore.Open(dir)
			if err != nil {
				t.Fatalf("open Yeoul adapter: %v", err)
			}
			defer adapter.Close()
			if _, err := adapter.Records(ctx); err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("Records error = %v, want containing %q", err, test.match)
			}
		})
	}
}

func TestRhizaCatalogReadsBeyondOneStreamPage(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "rhiza")
	db, err := rhiza.Open(ctx, rhiza.Config{
		Local: true, NodeID: "yeoul-local", DataDir: dir,
		LocalGraphNodePropertyIndexes: []rhiza.GraphNodePropertyIndex{{Label: "YeoulRecord", Property: "key"}},
	})
	if err != nil {
		t.Fatalf("open Rhiza: %v", err)
	}
	events := make([]rhiza.GraphStreamEvent, 70)
	for i := range events {
		key := fmt.Sprintf("%d", i)
		events[i] = rhiza.GraphStreamEvent{
			Stream: "yeoul.catalog.v1", Kind: "records.changed", Payload: map[string]any{"keys": []any{key}},
		}
	}
	response, err := db.GraphExecute(ctx, rhiza.GraphCommand{
		RequestID: "catalog-page-70",
		Cypher: `UNWIND range(0, 69) AS i
CREATE (:YeoulRecord {key: toString(i), kind: 'Probe', id: toString(i), payload: '{}', search_text: '', deleted: false})`,
		Events: events,
	})
	if err != nil || response.Status != rhiza.MutationCommitted {
		t.Fatalf("write 70 catalog rows: response=%+v err=%v", response, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close Rhiza: %v", err)
	}
	adapter, err := rhizastore.Open(dir)
	if err != nil {
		t.Fatalf("open Yeoul adapter: %v", err)
	}
	defer adapter.Close()
	rows, err := adapter.Records(ctx)
	if err != nil {
		t.Fatalf("read catalog pages: %v", err)
	}
	if len(rows) != 70 {
		t.Fatalf("catalog record count = %d, want 70", len(rows))
	}
}
