package yeoul

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// summarize must truncate on rune boundaries so search/timeline previews of
// Korean or emoji content stay valid UTF-8.
func TestSummarizeKeepsValidUTF8(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"korean crosses cutoff", strings.Repeat("\uac00", 60)},
		{"emoji crosses cutoff", strings.Repeat("\U0001f600", 60)},
		{"mixed crosses cutoff", strings.Repeat("ab\uac00\U0001f600", 20)},
		{"korean padded ascii", strings.Repeat("x", 70) + strings.Repeat("\uac00", 20)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summarize(tc.content)
			if !utf8.ValidString(got) {
				t.Fatalf("summarize returned invalid UTF-8: %q", got)
			}
			if !strings.HasSuffix(got, "...") {
				t.Fatalf("expected truncated suffix, got %q", got)
			}
		})
	}
}

// Short ASCII content is unchanged, and long ASCII still truncates at 77 bytes.
func TestSummarizeASCIIUnchanged(t *testing.T) {
	short := "hello world"
	if got := summarize(short); got != short {
		t.Fatalf("expected passthrough, got %q", got)
	}
	long := strings.Repeat("a", 100)
	if got := summarize(long); got != strings.Repeat("a", 77)+"..." {
		t.Fatalf("expected ascii truncation, got %q", got)
	}
}

func TestTruncateUTF8KeepsValidUTF8(t *testing.T) {
	for _, s := range []string{"\uac00\ub098\ub2e4\ub77c\ub9c8", "\U0001f600\U0001f601\U0001f602", "a\uac00b\U0001f600c"} {
		for n := 0; n <= len(s)+1; n++ {
			got := truncateUTF8(s, n)
			if !utf8.ValidString(got) {
				t.Fatalf("truncateUTF8(%q, %d) = %q is invalid UTF-8", s, n, got)
			}
			if n >= 0 && len(got) > n {
				t.Fatalf("truncateUTF8(%q, %d) exceeded budget: %q", s, n, got)
			}
		}
	}
}
