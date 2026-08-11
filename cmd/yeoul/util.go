package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

func parseTemporalFlags(asOfRaw, fromRaw, toRaw string, includeInactive bool) (yeoul.TemporalFilter, error) {
	return parseTemporalFlagsFull(asOfRaw, "", fromRaw, toRaw, "", "", includeInactive)
}

func parseTemporalFlagsFull(asOfRaw, validAtRaw, fromRaw, toRaw, validFromRaw, validToRaw string, includeInactive bool) (yeoul.TemporalFilter, error) {
	var filter yeoul.TemporalFilter
	parseOne := func(raw string) (*time.Time, error) {
		if strings.TrimSpace(raw) == "" {
			return nil, nil
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, err
		}
		return &parsed, nil
	}
	asOf, err := parseOne(asOfRaw)
	if err != nil {
		return filter, err
	}
	validAt, err := parseOne(validAtRaw)
	if err != nil {
		return filter, err
	}
	from, err := parseOne(fromRaw)
	if err != nil {
		return filter, err
	}
	to, err := parseOne(toRaw)
	if err != nil {
		return filter, err
	}
	validFrom, err := parseOne(validFromRaw)
	if err != nil {
		return filter, err
	}
	validTo, err := parseOne(validToRaw)
	if err != nil {
		return filter, err
	}
	if validAt != nil && (validFrom != nil || validTo != nil) {
		return filter, fmt.Errorf("--valid-at cannot be combined with --valid-from or --valid-to")
	}
	if from != nil && to != nil && !from.Before(*to) {
		return filter, fmt.Errorf("--from must be before --to")
	}
	if validFrom != nil && validTo != nil && !validFrom.Before(*validTo) {
		return filter, fmt.Errorf("--valid-from must be before --valid-to")
	}
	filter.AsOf = asOf
	filter.ValidAt = validAt
	filter.ObservedFrom = from
	filter.ObservedTo = to
	filter.ValidFrom = validFrom
	filter.ValidTo = validTo
	filter.IncludeInactive = includeInactive
	return filter, nil
}

func compactStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}
