package yeoul

import (
	"sort"
	"testing"
	"time"
)

// TestTimelineEntryBeforeIsATotalOrder pins the timeline's event comparator.
// Facts and episodes live in maps, so the comparator is what makes the page
// order deterministic when several events share one instant, which is what a
// coarse platform clock reports. A comparator that is not a total order leaves
// the order up to the sort's pivots, so the same records can page differently
// between runs.
func TestTimelineEntryBeforeIsATotalOrder(t *testing.T) {
	at := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	later := at.Add(time.Second)
	entries := []timelineEntry{
		{event: TimelineEvent{EventID: "evt:fact:b:created", RecordID: "fact:b", Timestamp: at}, order: 0},
		{event: TimelineEvent{EventID: "evt:fact:a:retracted", RecordID: "fact:a", Timestamp: at}, order: 1},
		{event: TimelineEvent{EventID: "evt:fact:a:superseded", RecordID: "fact:a", Timestamp: at}, order: 0},
		{event: TimelineEvent{EventID: "evt:fact:c:created", RecordID: "fact:c", Timestamp: later}, order: 0},
	}

	// A total order is irreflexive and antisymmetric on distinct entries.
	for i := range entries {
		if timelineEntryBefore(entries[i], entries[i], false) {
			t.Fatalf("comparator is not irreflexive at %d: %#v", i, entries[i])
		}
		for j := range entries {
			if i == j {
				continue
			}
			forward := timelineEntryBefore(entries[i], entries[j], false)
			backward := timelineEntryBefore(entries[j], entries[i], false)
			if forward == backward {
				t.Fatalf("comparator is not antisymmetric for %#v and %#v", entries[i], entries[j])
			}
		}
	}

	// The order is timestamp first, then record ID, then the record's causal
	// position. The two "...:fact:a" events share an instant and a record, so
	// only their causal position separates them: the supersession, then the
	// retraction that followed it. The suffixes are the ones production emits,
	// and "retracted" sorts before "superseded" as text, so a comparator that
	// fell through to the event ID would invert a superseded-then-retracted fact.
	ordered := append([]timelineEntry(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool { return timelineEntryBefore(ordered[i], ordered[j], false) })
	got := make([]string, 0, len(ordered))
	for _, entry := range ordered {
		got = append(got, entry.event.EventID)
	}
	want := []string{
		"evt:fact:a:superseded",
		"evt:fact:a:retracted",
		"evt:fact:b:created",
		"evt:fact:c:created",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %#v, got %#v", want, got)
		}
	}

	// Descending reverses the timestamp order but keeps the tie-break: the
	// record-local causal order is not a timestamp and must not flip.
	sort.Slice(ordered, func(i, j int) bool { return timelineEntryBefore(ordered[i], ordered[j], true) })
	got = got[:0]
	for _, entry := range ordered {
		got = append(got, entry.event.EventID)
	}
	want = []string{
		"evt:fact:c:created",
		"evt:fact:a:superseded",
		"evt:fact:a:retracted",
		"evt:fact:b:created",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %#v when descending, got %#v", want, got)
		}
	}
}
