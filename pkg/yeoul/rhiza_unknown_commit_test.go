package yeoul

import (
	"context"
	"errors"
	"testing"
)

type unknownCommitStore struct{ saves int }

func (*unknownCommitStore) Load() (*persistedState, error) {
	state := emptyPersistedState()
	return &state, nil
}
func (s *unknownCommitStore) Save(persistedState) error { s.saves++; return errStorageOutcomeUnknown }
func (*unknownCommitStore) Close() error                { return nil }

func TestUnknownCommitRestoresMemoryPoisonsAndSkipsCloseSave(t *testing.T) {
	ctx := context.Background()
	store := &unknownCommitStore{}
	eng := newEngine(Config{DatabasePath: "unknown"}, store)
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "unconfirmed"}); !errors.Is(err, errStorageOutcomeUnknown) {
		t.Fatalf("IngestEpisode error = %v, want unknown commit", err)
	}
	if got := len(eng.episodes); got != 0 {
		t.Fatalf("episodes after unknown commit = %d, want rollback", got)
	}
	if _, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "later write"}); err == nil {
		t.Fatal("write after unknown commit succeeded")
	}
	if err := eng.Close(ctx); !errors.Is(err, errStorageOutcomeUnknown) {
		t.Fatalf("Close error = %v, want unknown commit", err)
	}
	if store.saves != 1 {
		t.Fatalf("Save calls = %d, want only original uncertain save", store.saves)
	}
}
