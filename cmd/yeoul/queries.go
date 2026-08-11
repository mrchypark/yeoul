package main

import (
	"fmt"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	lstore "github.com/mrchypark/yeoul/internal/storage/ladybug"
)

func queryRowsAllowMissing(store *lstore.Store, query string) ([]map[string]any, error) {
	rows, err := queryRows(store, query)
	if err != nil && strings.Contains(err.Error(), "Binder exception: Table ") {
		return nil, nil
	}
	return rows, err
}

func rowString(row map[string]any, key string) string {
	value := row[key]
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func rowTime(row map[string]any, key string) time.Time {
	switch value := row[key].(type) {
	case time.Time:
		return value.UTC()
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func rowFloat64(row map[string]any, key string) float64 {
	switch value := row[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	}
	return 0
}

func rowStringSlice(row map[string]any, key string) []string {
	raw := strings.TrimSpace(rowString(row, key))
	if raw == "" || raw == "null" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	return nil
}

func rowMap(row map[string]any, key string) map[string]any {
	raw := strings.TrimSpace(rowString(row, key))
	if raw == "" || raw == "null" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	return nil
}

func queryRows(store *lstore.Store, query string) ([]map[string]any, error) {
	result, err := store.Query(query)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	rows := make([]map[string]any, 0)
	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			return nil, err
		}
		row, err := tuple.GetAsMap()
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func singleStringQuery(store *lstore.Store, query string) (string, error) {
	rows, err := queryRows(store, query)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	for _, value := range rows[0] {
		return fmt.Sprint(value), nil
	}
	return "", nil
}

func singleIntQuery(store *lstore.Store, query string) (int, error) {
	rows, err := queryRows(store, query)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	for _, value := range rows[0] {
		switch v := value.(type) {
		case int:
			return v, nil
		case int64:
			return int(v), nil
		case float64:
			return int(v), nil
		default:
			return 0, fmt.Errorf("unexpected count type %T", value)
		}
	}
	return 0, nil
}
