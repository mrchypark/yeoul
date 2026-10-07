# ADR 0002: Implement Yeoul Core in Go

## Status

Accepted at the time. The Ladybug binding and cgo consequences below are historical; current releases use LatticeDB only.

## Context

Yeoul should be embeddable into local developer tools and agent harnesses.

## Decision

Implement Yeoul Core in Go.

## Consequences

- The project will use the Ladybug Go binding.
- cgo-related build and packaging risks must be documented.
- Public APIs should be stable and small.
