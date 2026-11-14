package fuse

import (
	"fmt"
	"testing"
)

// Scoped benchmark corpus: 2000 book-like docs (same shape as the README's
// published comparison table; regenerate numbers with `go test -bench .`).
func benchDocs(n int) []any {
	first := []string{"John", "Jane", "Bob", "Alice", "Carl", "Dana", "Eli", "Fay", "Gus", "Hana"}
	last := []string{"Smith", "Doe", "Brown", "Lee", "Katz", "Reyes", "Novak", "Adler", "Cohen", "Weiss"}
	words := []string{"old", "man", "war", "lock", "history", "love", "code", "rust", "go", "search", "fuzzy", "engine", "data", "book", "guide"}
	docs := make([]any, 0, n)
	seed := uint32(42)
	next := func(m uint32) uint32 { seed = seed*1664525 + 1013904223; return seed % m }
	for i := 0; i < n; i++ {
		docs = append(docs, map[string]any{
			"title":  fmt.Sprintf("%s %s %s %d", words[next(15)], words[next(15)], words[next(15)], i),
			"author": map[string]any{"firstName": first[next(10)], "lastName": last[next(10)]},
		})
	}
	return docs
}

var benchOptions = &Options{
	Keys:         []any{Key{Name: "title", Weight: 0.7}, Key{Name: "author.lastName", Weight: 0.3}},
	IncludeScore: Bool(true),
}

func BenchmarkIndexBuild2000(b *testing.B) {
	docs := benchDocs(2000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := New(docs, benchOptions, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearchHit2000(b *testing.B)   { benchmarkSearch(b, "old man") }
func BenchmarkSearchMiss2000(b *testing.B)  { benchmarkSearch(b, "zzzqqx") }
func BenchmarkSearchShort2000(b *testing.B) { benchmarkSearch(b, "go") }

func benchmarkSearch(b *testing.B, pattern string) {
	f, err := New(benchDocs(2000), benchOptions, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := f.Search(pattern, nil); err != nil {
			b.Fatal(err)
		}
	}
}
