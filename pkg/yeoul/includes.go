package yeoul

// IncludedSupportResolver resolves a record kind and id to the payload that may
// appear in IncludedRecords. Implementations must apply the request's space,
// scope, and temporal visibility rules and return false for any record the
// request must not see.
type IncludedSupportResolver func(kind, id string) (any, bool)

// AssembleIncludedRecords shapes the page-scoped IncludedRecords for a search or
// lookup response. It is shared by the core engine and the Rax backend so both
// backends honor the same flag implications and dedupe shared support.
//
// Flag implications:
//   - SupportingFacts returns the facts behind the returned hits.
//   - SupportingEpisodes returns the episodes supporting those facts.
//   - Provenance returns the same episodes plus their source descriptors.
//   - RelatedEntities returns the subject/object entities of those facts and any
//     returned entity hits.
//   - Snippets selects no included family; it only asks the backend to attach
//     matched excerpts to the returned hits.
//
// An episode's source descriptor is part of the episode's provenance, so
// including an episode always includes its visible source.
func AssembleIncludedRecords(include Include, page []SearchHit, resolve IncludedSupportResolver) IncludedRecords {
	if !include.Provenance && !include.SupportingFacts && !include.SupportingEpisodes && !include.RelatedEntities && !include.Snippets {
		return IncludedRecords{}
	}
	includeEpisodes := include.SupportingEpisodes || include.Provenance
	included := IncludedRecords{}
	for _, hit := range page {
		switch hit.HitType {
		case "fact":
			if !include.SupportingFacts && !include.RelatedEntities && !includeEpisodes {
				continue
			}
			fact := resolveFact(resolve, hit.RecordID)
			if fact == nil {
				continue
			}
			if include.SupportingFacts {
				included.Facts = append(included.Facts, *fact)
			}
			if include.RelatedEntities {
				appendIncludedEntity(&included, resolve, fact.SubjectID)
				appendIncludedEntity(&included, resolve, fact.ObjectID)
			}
			if includeEpisodes {
				for _, episodeID := range fact.SupportingEpisodeIDs {
					appendIncludedEpisode(&included, resolve, episodeID)
				}
			}
		case "episode":
			if !includeEpisodes {
				continue
			}
			appendIncludedEpisode(&included, resolve, hit.RecordID)
		case "entity":
			if !include.RelatedEntities {
				continue
			}
			appendIncludedEntity(&included, resolve, hit.RecordID)
		}
	}
	return dedupeIncluded(included)
}

func resolveFact(resolve IncludedSupportResolver, id string) *Fact {
	if id == "" {
		return nil
	}
	value, ok := resolve("fact", id)
	if !ok {
		return nil
	}
	fact, _ := value.(*Fact)
	return fact
}

func appendIncludedEntity(included *IncludedRecords, resolve IncludedSupportResolver, id string) {
	if id == "" {
		return
	}
	value, ok := resolve("entity", id)
	if !ok {
		return
	}
	if entity, ok := value.(*Entity); ok && entity != nil {
		included.Entities = append(included.Entities, *entity)
	}
}

func appendIncludedEpisode(included *IncludedRecords, resolve IncludedSupportResolver, id string) {
	if id == "" {
		return
	}
	value, ok := resolve("episode", id)
	if !ok {
		return
	}
	episode, ok := value.(*Episode)
	if !ok || episode == nil {
		return
	}
	included.Episodes = append(included.Episodes, *episode)
	if source, ok := resolve("source", episode.SourceID); ok {
		if sourceRecord, ok := source.(*Source); ok && sourceRecord != nil {
			included.Sources = append(included.Sources, *sourceRecord)
		}
	}
}
