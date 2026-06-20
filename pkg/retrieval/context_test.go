package retrieval

import (
	"testing"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func TestBuildContextBoundsAndClipsHits(t *testing.T) {
	resp := yeoul.SearchResponse{Hits: []yeoul.SearchHit{
		{HitType: "fact", RecordID: "fact-1", MatchedText: "abcdef"},
		{HitType: "episode", RecordID: "ep-1", MatchedText: "second"},
	}}
	bundle := BuildContext(resp, ContextOptions{MaxBlocks: 1, MaxTextRunes: 3})
	if len(bundle.Blocks) != 1 || !bundle.Truncated {
		t.Fatalf("expected one truncated block, got %#v", bundle)
	}
	if bundle.Blocks[0].Text != "abc" || bundle.Blocks[0].RecordIDs[0] != "fact-1" {
		t.Fatalf("unexpected block: %#v", bundle.Blocks[0])
	}
}

func TestBuildContextIncludesSupportingRecords(t *testing.T) {
	resp := yeoul.SearchResponse{
		Hits: []yeoul.SearchHit{{HitType: "fact", RecordID: "fact-1", MatchedText: "hit"}},
		Included: yeoul.IncludedRecords{
			Facts:    []yeoul.Fact{{ID: "fact-1", Predicate: "HAS_STATUS", ValueText: "active"}},
			Episodes: []yeoul.Episode{{ID: "ep-1", Kind: "note", Content: "source episode"}},
			Entities: []yeoul.Entity{{ID: "entity-1", Type: "Project", CanonicalName: "Yeoul"}},
		},
	}
	bundle := BuildContext(resp, ContextOptions{MaxBlocks: 4, MaxTextRunes: 100})
	if len(bundle.Blocks) != 4 {
		t.Fatalf("expected hit plus supporting blocks, got %#v", bundle.Blocks)
	}
	for i, want := range []string{"hit", "supporting_fact", "supporting_episode", "related_entity"} {
		if bundle.Blocks[i].Kind != want {
			t.Fatalf("block %d kind = %q, want %q", i, bundle.Blocks[i].Kind, want)
		}
	}
}
