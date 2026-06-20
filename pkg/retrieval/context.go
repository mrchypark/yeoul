package retrieval

import "github.com/mrchypark/yeoul/pkg/yeoul"

type ContextOptions struct {
	MaxBlocks    int
	MaxTextRunes int
}

type ContextBlock struct {
	Kind      string   `json:"kind"`
	Title     string   `json:"title,omitempty"`
	Text      string   `json:"text,omitempty"`
	RecordIDs []string `json:"record_ids,omitempty"`
}

type ContextBundle struct {
	Meta      yeoul.QueryResponseMeta `json:"meta"`
	Hits      []yeoul.SearchHit       `json:"hits"`
	Blocks    []ContextBlock          `json:"blocks"`
	Truncated bool                    `json:"truncated,omitempty"`
}

func BuildContext(resp yeoul.SearchResponse, opts ContextOptions) ContextBundle {
	maxBlocks := opts.MaxBlocks
	if maxBlocks <= 0 {
		maxBlocks = 16
	}
	out := ContextBundle{
		Meta: resp.Meta,
		Hits: append([]yeoul.SearchHit(nil), resp.Hits...),
	}
	for _, hit := range resp.Hits {
		if !appendBlock(&out, maxBlocks, ContextBlock{
			Kind:      "hit",
			Title:     hit.HitType + ":" + hit.RecordID,
			Text:      clipRunes(hit.MatchedText, opts.MaxTextRunes),
			RecordIDs: []string{hit.RecordID},
		}) {
			break
		}
	}
	for _, fact := range resp.Included.Facts {
		if !appendBlock(&out, maxBlocks, ContextBlock{
			Kind:      "supporting_fact",
			Title:     fact.Predicate + ":" + fact.ID,
			Text:      clipRunes(fact.ValueText, opts.MaxTextRunes),
			RecordIDs: []string{fact.ID},
		}) {
			return out
		}
	}
	for _, episode := range resp.Included.Episodes {
		if !appendBlock(&out, maxBlocks, ContextBlock{
			Kind:      "supporting_episode",
			Title:     episode.Kind + ":" + episode.ID,
			Text:      clipRunes(episode.Content, opts.MaxTextRunes),
			RecordIDs: []string{episode.ID},
		}) {
			return out
		}
	}
	for _, entity := range resp.Included.Entities {
		if !appendBlock(&out, maxBlocks, ContextBlock{
			Kind:      "related_entity",
			Title:     entity.Type + ":" + entity.ID,
			Text:      clipRunes(entity.CanonicalName, opts.MaxTextRunes),
			RecordIDs: []string{entity.ID},
		}) {
			return out
		}
	}
	return out
}

func appendBlock(bundle *ContextBundle, max int, block ContextBlock) bool {
	if len(bundle.Blocks) >= max {
		bundle.Truncated = true
		return false
	}
	bundle.Blocks = append(bundle.Blocks, block)
	return true
}

func clipRunes(text string, max int) string {
	if max <= 0 {
		max = 512
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}
