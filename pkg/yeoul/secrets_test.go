package yeoul

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Synthetic canaries only. None of these are real credentials; each matches a
// recognized vendor-issued prefix shape so the pre-ingest boundary can be
// exercised without touching live secrets.
const (
	canaryAWSAccessKey = "AKIAIOSFODNN7EXAMPLE"
	canaryGitHubToken  = "ghp_0123456789abcdefghijklmnopqrstuvwxyz"
	canaryOpenAIKey    = "sk-abcdefghijklmnopqrstuvwxyz012345"
	canaryPrivateKey   = "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----"
)

func assertSecretRejected(t *testing.T, err error, wantField string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected the secret-bearing input to be rejected")
	}
	var yeoulErr *Error
	if !errors.As(err, &yeoulErr) {
		t.Fatalf("expected a structured Yeoul error, got %v", err)
	}
	if yeoulErr.Code != ErrInputInvalid {
		t.Fatalf("expected %s, got %s", ErrInputInvalid, yeoulErr.Code)
	}
	if got, _ := yeoulErr.Details["field"].(string); got != wantField {
		t.Fatalf("expected rejection field %q, got %#v", wantField, yeoulErr.Details)
	}
	if _, ok := yeoulErr.Details["secret_class"]; !ok {
		t.Fatalf("expected a secret_class detail, got %#v", yeoulErr.Details)
	}
	// Diagnostics must never echo the rejected payload.
	for _, canary := range []string{canaryAWSAccessKey, canaryGitHubToken, canaryOpenAIKey, canaryPrivateKey} {
		if strings.Contains(yeoulErr.Error(), canary) {
			t.Fatalf("error message echoed the rejected secret: %q", yeoulErr.Error())
		}
		if strings.Contains(yeoulErr.Message, canary) {
			t.Fatalf("error message field echoed the rejected secret: %q", yeoulErr.Message)
		}
	}
}

func newSecretTestEngine(t *testing.T) (Engine, *EpisodeResult) {
	t.Helper()
	ctx := context.Background()
	eng, err := Open(ctx, Config{InMemory: true})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	episode, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "clean note", Source: SourceInput{Kind: "note"}})
	if err != nil {
		t.Fatalf("ingest clean episode: %v", err)
	}
	return eng, episode
}

func TestIngestEpisodeRejectsSecretContent(t *testing.T) {
	ctx := context.Background()
	eng, _ := newSecretTestEngine(t)
	_, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "token is " + canaryGitHubToken, Source: SourceInput{Kind: "note"}})
	assertSecretRejected(t, err, "episode.content")
}

func TestIngestEpisodeRejectsSecretInMetadataAndSource(t *testing.T) {
	ctx := context.Background()
	eng, _ := newSecretTestEngine(t)
	_, err := eng.IngestEpisode(ctx, EpisodeInput{
		Kind:    "note",
		Content: "clean",
		Source:  SourceInput{Kind: "note"},
		Metadata: map[string]any{
			"nested": map[string]any{"credential": canaryAWSAccessKey},
		},
	})
	assertSecretRejected(t, err, "episode.metadata.nested.credential")

	_, err = eng.IngestEpisode(ctx, EpisodeInput{
		Kind:    "note",
		Content: "clean",
		Source:  SourceInput{Kind: "note", URI: "https://example.test/" + canaryOpenAIKey},
	})
	assertSecretRejected(t, err, "episode.source.uri")
}

func TestIngestBatchRejectsSecretsInEverySection(t *testing.T) {
	ctx := context.Background()
	eng, episode := newSecretTestEngine(t)

	_, err := eng.IngestBatch(ctx, BatchInput{Episodes: []EpisodeInput{{Kind: "note", Content: canaryPrivateKey, Source: SourceInput{Kind: "note"}}}})
	assertSecretRejected(t, err, "episode.content")

	_, err = eng.IngestBatch(ctx, BatchInput{Entities: []EntityInput{{Type: "Thing", CanonicalName: "thing", Metadata: map[string]any{"token": canaryGitHubToken}}}})
	assertSecretRejected(t, err, "entity.metadata.token")

	_, err = eng.IngestBatch(ctx, BatchInput{Entities: []EntityInput{{Type: "Thing", CanonicalName: "thing"}}, Facts: []FactInput{{
		Predicate:            "HAS_STATE",
		SubjectID:            "thing:x",
		ValueText:            "value " + canaryAWSAccessKey,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	}}})
	assertSecretRejected(t, err, "fact.value_text")

	_, err = eng.IngestBatch(ctx, BatchInput{Facts: []FactInput{{
		Predicate:            "HAS_STATE",
		SubjectID:            "thing:x",
		ValueText:            "clean",
		SupportingEpisodeIDs: []string{episode.EpisodeID, canaryOpenAIKey},
	}}})
	assertSecretRejected(t, err, "fact.supporting_episode_ids[1]")
}

func TestUpsertEntityRejectsSecretAlias(t *testing.T) {
	ctx := context.Background()
	eng, _ := newSecretTestEngine(t)
	_, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "thing", Aliases: []string{"clean", canaryGitHubToken}})
	assertSecretRejected(t, err, "entity.aliases[1]")
}

func TestAssertFactRejectsSecretMetadata(t *testing.T) {
	ctx := context.Background()
	eng, episode := newSecretTestEngine(t)
	_, err := eng.AssertFact(ctx, FactInput{
		Predicate:            "HAS_STATE",
		SubjectID:            "thing:x",
		ValueText:            "clean",
		SupportingEpisodeIDs: []string{episode.EpisodeID},
		Metadata:             map[string]any{"notes": []any{"ok", canaryAWSAccessKey}},
	})
	assertSecretRejected(t, err, "fact.metadata.notes[1]")
}

func TestSupersedeAndRetractRejectSecretReason(t *testing.T) {
	ctx := context.Background()
	eng, episode := newSecretTestEngine(t)
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "secret reason subject"})
	if err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{ID: "fact:secret-old", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "old", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert fact: %v", err)
	}
	_, err = eng.SupersedeFact(ctx, fact.ID, FactInput{SubjectID: entity.ID, Predicate: "HAS_STATE", ValueText: "new", SupportingEpisodeIDs: []string{episode.EpisodeID}}, "leaked "+canaryGitHubToken)
	assertSecretRejected(t, err, "supersede.reason")

	_, err = eng.RetractFact(ctx, fact.ID, "leaked "+canaryAWSAccessKey)
	assertSecretRejected(t, err, "retract.reason")

	// A clean reason still works, and a rejected lifecycle write leaves state alone.
	stored, err := eng.GetFact(ctx, fact.ID)
	if err != nil {
		t.Fatalf("get fact: %v", err)
	}
	if stored.Status != factStatusActive {
		t.Fatalf("expected the rejected retract to leave the fact active, got %q", stored.Status)
	}
}

func TestCleanInputIsAcceptedByEveryWritePath(t *testing.T) {
	ctx := context.Background()
	eng, episode := newSecretTestEngine(t)
	entity, err := eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "clean thing", Metadata: map[string]any{"note": "no credentials here"}})
	if err != nil {
		t.Fatalf("upsert clean entity: %v", err)
	}
	fact, err := eng.AssertFact(ctx, FactInput{ID: "fact:clean", Predicate: "HAS_STATE", SubjectID: entity.ID, ValueText: "clean", SupportingEpisodeIDs: []string{episode.EpisodeID}})
	if err != nil {
		t.Fatalf("assert clean fact: %v", err)
	}
	if _, err := eng.SupersedeFact(ctx, fact.ID, FactInput{SubjectID: entity.ID, Predicate: "HAS_STATE", ValueText: "clean replacement", SupportingEpisodeIDs: []string{episode.EpisodeID}}, "ordinary correction"); err != nil {
		t.Fatalf("supersede clean fact: %v", err)
	}
	if _, err := eng.IngestBatch(ctx, BatchInput{Episodes: []EpisodeInput{{Kind: "note", Content: "clean batch", Source: SourceInput{Kind: "note"}}}}); err != nil {
		t.Fatalf("ingest clean batch: %v", err)
	}
}

// TestIngestEpisodeRejectsSecretMetadataKeyWithoutLeakingKey covers the case
// where the credential is the metadata key rather than the value. The key must
// still be scanned and rejected, but it must not appear in the diagnostic path.
func TestIngestEpisodeRejectsSecretMetadataKeyWithoutLeakingKey(t *testing.T) {
	ctx := context.Background()
	eng, _ := newSecretTestEngine(t)
	_, err := eng.IngestEpisode(ctx, EpisodeInput{
		Kind:     "note",
		Content:  "clean",
		Source:   SourceInput{Kind: "note"},
		Metadata: map[string]any{canaryAWSAccessKey: "clean-value"},
	})
	if err == nil {
		t.Fatal("expected the credential-shaped metadata key to be rejected")
	}
	var yeoulErr *Error
	if !errors.As(err, &yeoulErr) {
		t.Fatalf("expected a structured Yeoul error, got %v", err)
	}
	if yeoulErr.Code != ErrInputInvalid {
		t.Fatalf("expected %s, got %s", ErrInputInvalid, yeoulErr.Code)
	}
	got, _ := yeoulErr.Details["field"].(string)
	if got != "episode.metadata.<redacted-key>" {
		t.Fatalf("expected the diagnostic path to use the placeholder segment, got %#v", yeoulErr.Details)
	}
	if strings.Contains(got, canaryAWSAccessKey) {
		t.Fatalf("rejection path leaked the credential-shaped key: %q", got)
	}
	if _, ok := yeoulErr.Details["secret_class"]; !ok {
		t.Fatalf("expected a secret_class detail, got %#v", yeoulErr.Details)
	}
	if strings.Contains(yeoulErr.Error(), canaryAWSAccessKey) {
		t.Fatalf("error message echoed the credential-shaped key: %q", yeoulErr.Error())
	}
	if strings.Contains(yeoulErr.Message, canaryAWSAccessKey) {
		t.Fatalf("error message field echoed the credential-shaped key: %q", yeoulErr.Message)
	}
}

// TestNewlyScannedSpaceAndSourceFieldsRejectSecrets covers the input fields the
// engine persists but the scanner previously skipped: SpaceID on episodes,
// entities, and facts, plus the promoted source id and source space id.
func TestNewlyScannedSpaceAndSourceFieldsRejectSecrets(t *testing.T) {
	ctx := context.Background()
	eng, episode := newSecretTestEngine(t)

	_, err := eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "clean", SpaceID: canaryAWSAccessKey, Source: SourceInput{Kind: "note"}})
	assertSecretRejected(t, err, "episode.space_id")

	_, err = eng.IngestEpisode(ctx, EpisodeInput{Kind: "note", Content: "clean", Source: SourceInput{Kind: "note", ID: canaryGitHubToken}})
	assertSecretRejected(t, err, "episode.source.id")

	_, err = eng.UpsertEntity(ctx, EntityInput{Type: "Thing", CanonicalName: "clean space thing", SpaceID: canaryAWSAccessKey})
	assertSecretRejected(t, err, "entity.space_id")

	_, err = eng.AssertFact(ctx, FactInput{
		Predicate:            "HAS_STATE",
		SubjectID:            "thing:x",
		ValueText:            "clean",
		SpaceID:              canaryAWSAccessKey,
		SupportingEpisodeIDs: []string{episode.EpisodeID},
	})
	assertSecretRejected(t, err, "fact.space_id")
}
