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

// BuildContext assembles a retrieval bundle for a caller. The returned blocks
// carry recalled memory as evidence: their Text may quote an issue, document,
// or tool result verbatim, so consumers must treat block text as untrusted data
// rather than instructions. Recalled content cannot grant permissions or
// override the caller's current instructions; use RecordIDs and Meta to check
// provenance before attributing authority.
func BuildContext(resp yeoul.SearchResponse, opts ContextOptions) ContextBundle {
	maxBlocks := opts.MaxBlocks
	if maxBlocks <= 0 {
		maxBlocks = 16
	}
	maxTextRunes := opts.MaxTextRunes
	if maxTextRunes <= 0 {
		maxTextRunes = 512
	}
	out := ContextBundle{Meta: resp.Meta}
	// Copy hits with bounded text: a bundle must not carry an unbounded episode
	// through duplicated hit text when only Blocks are clipped.
	if len(resp.Hits) > 0 {
		out.Hits = make([]yeoul.SearchHit, 0, len(resp.Hits))
		for _, hit := range resp.Hits {
			text, clipped := clipRunes(hit.MatchedText, maxTextRunes)
			if clipped {
				out.Truncated = true
			}
			hit.MatchedText = text
			out.Hits = append(out.Hits, hit)
		}
	}
	for _, hit := range out.Hits {
		if !appendBlock(&out, maxBlocks, ContextBlock{
			Kind:      "hit",
			Title:     hit.HitType + ":" + hit.RecordID,
			Text:      hit.MatchedText,
			RecordIDs: []string{hit.RecordID},
		}) {
			break
		}
	}
	for _, fact := range resp.Included.Facts {
		text, clipped := clipRunes(fact.ValueText, maxTextRunes)
		if clipped {
			out.Truncated = true
		}
		if !appendBlock(&out, maxBlocks, ContextBlock{
			Kind:      "supporting_fact",
			Title:     fact.Predicate + ":" + fact.ID,
			Text:      text,
			RecordIDs: []string{fact.ID},
		}) {
			return out
		}
	}
	for _, episode := range resp.Included.Episodes {
		text, clipped := clipRunes(episode.Content, maxTextRunes)
		if clipped {
			out.Truncated = true
		}
		if !appendBlock(&out, maxBlocks, ContextBlock{
			Kind:      "supporting_episode",
			Title:     episode.Kind + ":" + episode.ID,
			Text:      text,
			RecordIDs: []string{episode.ID},
		}) {
			return out
		}
	}
	for _, entity := range resp.Included.Entities {
		text, clipped := clipRunes(entity.CanonicalName, maxTextRunes)
		if clipped {
			out.Truncated = true
		}
		if !appendBlock(&out, maxBlocks, ContextBlock{
			Kind:      "related_entity",
			Title:     entity.Type + ":" + entity.ID,
			Text:      text,
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

// clipRunes returns at most max runes of text and reports whether it clipped.
// It scans rune boundaries only up to the limit, so a huge input does not
// allocate a rune slice proportional to its full size.
func clipRunes(text string, max int) (string, bool) {
	if max <= 0 {
		max = 512
	}
	count := 0
	for index := range text {
		if count == max {
			return text[:index], true
		}
		count++
	}
	return text, false
}
