// Copyright 2026, Jamf Software LLC

// Package pickone resolves a user-supplied identifier to exactly one record
// from an in-memory candidate list.
package pickone

import (
	"errors"
	"strings"
)

var (
	// ErrNone reports that no tier matched any candidate.
	ErrNone = errors.New("no match")
	// ErrAmbiguous reports that the deciding tier matched more than one candidate.
	ErrAmbiguous = errors.New("ambiguous")
)

// Tier reports whether a candidate is identified by arg under one identifier.
type Tier[T any] func(item T, arg string) bool

// Exact matches when key(item) equals arg byte for byte. An empty key never matches.
func Exact[T any](key func(T) string) Tier[T] {
	return func(item T, arg string) bool {
		k := key(item)
		return k != "" && k == arg
	}
}

// Fold matches when key(item) equals arg under Unicode case folding. An empty key never matches.
func Fold[T any](key func(T) string) Tier[T] {
	return func(item T, arg string) bool {
		k := key(item)
		return k != "" && strings.EqualFold(k, arg)
	}
}

// One tries each tier in order, and the first tier matching any candidate
// decides: one match is returned, more than one returns those candidates with
// ErrAmbiguous, and a later tier is never consulted to break the tie. ErrNone
// means no tier matched.
func One[T any](items []T, arg string, tiers ...Tier[T]) (T, []T, error) {
	var zero T
	for _, tier := range tiers {
		var hits []T
		for _, it := range items {
			if tier(it, arg) {
				hits = append(hits, it)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0], nil, nil
		default:
			return zero, hits, ErrAmbiguous
		}
	}
	return zero, nil, ErrNone
}
