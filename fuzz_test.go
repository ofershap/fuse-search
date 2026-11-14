package fuse

import (
	"testing"
)

// FuzzSearch feeds arbitrary patterns and texts through every search mode.
// The engine must never panic and must always return a score in [0, 1].
func FuzzSearch(f *testing.F) {
	seeds := [][2]string{
		{"old man", "old man's war"},
		{"🦄", "emoji 🦄 tale"},
		{"שלום", "שלום עולם"},
		{"resume", "résumé café"},
		{"^old !war", "old man"},
		{"", ""},
		{"a", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "a"},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, pattern, text string) {
		docs := []any{text, "alpha one", "résumé", "🦄🦄", ""}
		for _, opts := range []*Options{
			nil,
			{IncludeScore: Bool(true), IncludeMatches: Bool(true)},
			{UseExtendedSearch: Bool(true), IncludeScore: Bool(true), IncludeMatches: Bool(true)},
			{UseTokenSearch: Bool(true), IncludeScore: Bool(true)},
			{UseTokenSearch: Bool(true), TokenMatch: "all"},
			{IsCaseSensitive: Bool(true)},
			{IgnoreDiacritics: Bool(true)},
			{IgnoreLocation: Bool(true), Threshold: Float64(1)},
			{FindAllMatches: Bool(true), MinMatchCharLength: Int(2), IncludeMatches: Bool(true)},
			{Location: Int(3), Distance: Int(5), Threshold: Float64(0.9)},
		} {
			fz, err := New(docs, opts, nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			res, err := fz.Search(pattern, nil)
			if err != nil {
				t.Fatalf("Search(%q): %v", pattern, err)
			}
			for _, r := range res {
				if r.Score != nil && (*r.Score < 0 || *r.Score > 1) {
					t.Fatalf("score out of range: %v (pattern %q text %q)", *r.Score, pattern, text)
				}
			}
			// object docs with keys, same options
			odocs := []any{
				map[string]any{"title": text, "n": 3.5},
				map[string]any{"title": "other"},
			}
			o2 := &Options{Keys: []any{"title"}}
			f2, err := New(odocs, o2, nil)
			if err != nil {
				t.Fatalf("New obj: %v", err)
			}
			if _, err := f2.Search(pattern, nil); err != nil {
				t.Fatalf("Search obj (%q): %v", pattern, err)
			}
		}
		// logical queries over the text
		f3, _ := New([]any{map[string]any{"t": text}}, &Options{Keys: []any{"t"}}, nil)
		if _, err := f3.Search(map[string]any{"$and": []any{map[string]any{"t": pattern}}}, nil); err != nil {
			t.Fatalf("logical: %v", err)
		}
		if _, err := Match(pattern, text, nil); err != nil {
			t.Fatalf("Match: %v", err)
		}
	})
}
