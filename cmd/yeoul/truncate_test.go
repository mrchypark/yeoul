package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// shorten must not split a multibyte rune at either cutoff, so previews of
// Korean or emoji text stay valid UTF-8.
func TestShortenKeepsValidUTF8(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		limit int
	}{
		{"korean crosses ellipsis cutoff", strings.Repeat("\uac00", 40), 10},
		{"emoji crosses ellipsis cutoff", strings.Repeat("\U0001f600", 40), 10},
		{"mixed crosses ellipsis cutoff", "ab\uac00cd\U0001f600ef" + strings.Repeat("\uac00", 20), 12},
		{"korean crosses short cutoff", strings.Repeat("\uac00", 20), 3},
		{"emoji crosses short cutoff", strings.Repeat("\U0001f600", 20), 2},
		{"mixed crosses short cutoff", "a\uac00\U0001f600bcd", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shorten(tc.text, tc.limit)
			if !utf8.ValidString(got) {
				t.Fatalf("shorten returned invalid UTF-8: %q", got)
			}
			if len(got) > tc.limit {
				t.Fatalf("shorten exceeded byte budget %d: len=%d %q", tc.limit, len(got), got)
			}
		})
	}
}

// ASCII behavior must not regress: the exact cutoff still applies.
func TestShortenASCIIUnchanged(t *testing.T) {
	if got := shorten("abcdefghij", 10); got != "abcdefghij" {
		t.Fatalf("expected passthrough, got %q", got)
	}
	if got := shorten("abcdefghijklm", 10); got != "abcdefg..." {
		t.Fatalf("expected ascii truncation, got %q", got)
	}
}

func TestTruncateBytesKeepsValidUTF8(t *testing.T) {
	for _, s := range []string{"\uac00\ub098\ub2e4\ub77c\ub9c8", "\U0001f600\U0001f601\U0001f602", "a\uac00b\U0001f600c"} {
		for n := 0; n <= len(s)+1; n++ {
			got := truncateBytes(s, n)
			if !utf8.ValidString(got) {
				t.Fatalf("truncateBytes(%q, %d) = %q is invalid UTF-8", s, n, got)
			}
			if len(got) > n && n >= 0 {
				t.Fatalf("truncateBytes(%q, %d) exceeded budget: %q", s, n, got)
			}
		}
	}
}
