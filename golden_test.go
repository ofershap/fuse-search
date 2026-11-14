package fuse

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

type goldenFile struct {
	FuseVersion string           `json:"fuseVersion"`
	Cases       []goldenCase     `json:"cases"`
	Mutations   []goldenMut      `json:"mutations"`
	Roundtrip   *goldenRoundtrip `json:"roundtrip"`
}

type goldenCase struct {
	Name          string           `json:"name"`
	Docs          []any            `json:"docs"`
	Options       map[string]any   `json:"options"`
	Query         any              `json:"query"`
	SearchOptions *goldenSearchOpt `json:"searchOptions"`
	Expected      []any            `json:"expected"`
}

type goldenSearchOpt struct {
	Limit int `json:"limit"`
}

type goldenMut struct {
	Name        string           `json:"name"`
	Docs        []any            `json:"docs"`
	Options     map[string]any   `json:"options"`
	Ops         []map[string]any `json:"ops"`
	ExpectedOps []any            `json:"expectedOps"`
}

type goldenRoundtrip struct {
	Name       string         `json:"name"`
	Serialized map[string]any `json:"serialized"`
	Query      string         `json:"query"`
	Options    map[string]any `json:"options"`
	Expected   []any          `json:"expected"`
}

func loadGoldens(t *testing.T) goldenFile {
	t.Helper()
	b, err := os.ReadFile("testdata/goldens.json")
	if err != nil {
		t.Fatalf("read goldens: %v", err)
	}
	var g goldenFile
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("parse goldens: %v", err)
	}
	return g
}

func boolP(m map[string]any, k string) *bool {
	if v, ok := m[k]; ok {
		if b, ok := v.(bool); ok {
			return &b
		}
	}
	return nil
}

func intP(m map[string]any, k string) *int {
	if v, ok := m[k]; ok {
		if f, ok := v.(float64); ok {
			i := int(f)
			return &i
		}
	}
	return nil
}

func floatP(m map[string]any, k string) *float64 {
	if v, ok := m[k]; ok {
		if f, ok := v.(float64); ok {
			return &f
		}
	}
	return nil
}

func buildKeys(v any) []any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []any
	for _, e := range arr {
		switch k := e.(type) {
		case string:
			out = append(out, k)
		case []any:
			parts := make([]string, 0, len(k))
			allStr := true
			for _, p := range k {
				s, ok := p.(string)
				if !ok {
					allStr = false
					break
				}
				parts = append(parts, s)
			}
			if allStr {
				out = append(out, parts)
			}
		case map[string]any:
			key := Key{}
			if n, ok := k["name"]; ok {
				switch nv := n.(type) {
				case string:
					key.Name = nv
				case []any:
					parts := make([]string, 0, len(nv))
					for _, p := range nv {
						if s, ok := p.(string); ok {
							parts = append(parts, s)
						}
					}
					key.Name = parts
				}
			}
			if w, ok := k["weight"].(float64); ok {
				key.Weight = w
			}
			out = append(out, key)
		}
	}
	return out
}

func buildOptions(m map[string]any) *Options {
	o := &Options{}
	o.IsCaseSensitive = boolP(m, "isCaseSensitive")
	o.IgnoreDiacritics = boolP(m, "ignoreDiacritics")
	o.IncludeScore = boolP(m, "includeScore")
	o.ShouldSort = boolP(m, "shouldSort")
	o.IncludeMatches = boolP(m, "includeMatches")
	o.FindAllMatches = boolP(m, "findAllMatches")
	o.MinMatchCharLength = intP(m, "minMatchCharLength")
	o.Location = intP(m, "location")
	o.Threshold = floatP(m, "threshold")
	o.Distance = intP(m, "distance")
	o.UseExtendedSearch = boolP(m, "useExtendedSearch")
	o.UseTokenSearch = boolP(m, "useTokenSearch")
	o.IgnoreLocation = boolP(m, "ignoreLocation")
	o.IgnoreFieldNorm = boolP(m, "ignoreFieldNorm")
	o.FieldNormWeight = floatP(m, "fieldNormWeight")
	if tm, ok := m["tokenMatch"].(string); ok {
		o.TokenMatch = tm
	}
	if ks, ok := m["keys"]; ok {
		o.Keys = buildKeys(ks)
	}
	return o
}

// resultsToGeneric converts Results into the exact JSON shape fuse.js emits,
// so DeepEqual against the golden (JSON-decoded) is a full structural check.
func resultsToGeneric(results []Result, includeMatches, includeScore bool) []any {
	out := make([]any, 0, len(results))
	for _, r := range results {
		m := map[string]any{"item": r.Item, "refIndex": float64(r.RefIndex)}
		if includeMatches {
			var matches []any
			for _, mt := range r.Matches {
				var idx []any
				for _, pair := range mt.Indices {
					idx = append(idx, []any{float64(pair[0]), float64(pair[1])})
				}
				mm := map[string]any{"indices": idx, "value": mt.Value}
				if idx == nil {
					mm["indices"] = []any{}
				}
				if mt.Key != "" {
					mm["key"] = mt.Key
				}
				if mt.RefIndex != nil {
					mm["refIndex"] = float64(*mt.RefIndex)
				}
				matches = append(matches, mm)
			}
			if matches == nil {
				matches = []any{}
			}
			m["matches"] = matches
		}
		if includeScore && r.Score != nil {
			m["score"] = *r.Score
		}
		out = append(out, m)
	}
	return out
}

func compareResults(t *testing.T, name string, expected []any, got []any) {
	t.Helper()
	if len(expected) != len(got) {
		eb, _ := json.Marshal(expected)
		gb, _ := json.Marshal(got)
		t.Fatalf("%s: result count: want %d got %d\nwant: %s\ngot: %s", name, len(expected), len(got), eb, gb)
	}
	for i := range expected {
		if !reflect.DeepEqual(expected[i], got[i]) {
			eb, _ := json.Marshal(expected[i])
			gb, _ := json.Marshal(got[i])
			t.Errorf("%s: result %d differs:\nwant: %s\ngot:  %s", name, i, eb, gb)
		}
	}
}

func TestGoldens(t *testing.T) {
	g := loadGoldens(t)
	if g.FuseVersion != "7.5.0" {
		t.Fatalf("goldens generated against fuse.js %s; this package targets 7.5.0", g.FuseVersion)
	}
	for _, c := range g.Cases {
		t.Run(c.Name, func(t *testing.T) {
			opts := buildOptions(c.Options)
			f, err := New(c.Docs, opts, nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			var so *SearchOptions
			if c.SearchOptions != nil {
				so = &SearchOptions{Limit: c.SearchOptions.Limit}
			}
			results, err := f.Search(c.Query, so)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			im := opts.IncludeMatches != nil && *opts.IncludeMatches
			is := opts.IncludeScore != nil && *opts.IncludeScore
			// fuse.js returns bare {item, refIndex} for empty-string queries,
			// bypassing format() entirely.
			if qs, ok := c.Query.(string); ok && trimJS(qs) == "" {
				im, is = false, false
			}
			got := resultsToGeneric(results, im, is)
			compareResults(t, c.Name, c.Expected, got)
		})
	}
}

func TestGoldenMutations(t *testing.T) {
	g := loadGoldens(t)
	for _, m := range g.Mutations {
		t.Run(m.Name, func(t *testing.T) {
			opts := buildOptions(m.Options)
			f, err := New(m.Docs, opts, nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			for i, op := range m.Ops {
				expected := m.ExpectedOps[i]
				switch op["op"] {
				case "add":
					if err := f.Add(op["doc"]); err != nil {
						t.Fatalf("add: %v", err)
					}
				case "removeAt":
					if _, err := f.RemoveAt(int(op["idx"].(float64))); err != nil {
						t.Fatalf("removeAt: %v", err)
					}
				case "remove":
					first := op["predFirst"].(string)
					removed := f.Remove(func(doc any, idx int) bool {
						s, _ := doc.(string)
						return s == first
					})
					exp := expected.(map[string]any)["removed"].([]any)
					if len(exp) != len(removed) {
						t.Fatalf("op %d remove: want %d removed got %d", i, len(exp), len(removed))
					}
				case "search":
					results, err := f.Search(op["query"], nil)
					if err != nil {
						t.Fatalf("search: %v", err)
					}
					exp := expected.(map[string]any)["results"].([]any)
					got := resultsToGeneric(results, opts.IncludeMatches != nil && *opts.IncludeMatches, opts.IncludeScore != nil && *opts.IncludeScore)
					compareResults(t, m.Name+" op", exp, got)
				}
			}
		})
	}
}

func TestGoldenIndexRoundtrip(t *testing.T) {
	g := loadGoldens(t)
	rt := g.Roundtrip
	if rt == nil {
		t.Skip("no roundtrip case")
	}
	serialized, err := json.Marshal(rt.Serialized)
	if err != nil {
		t.Fatalf("marshal serialized: %v", err)
	}
	idx, err := ParseIndex(serialized, nil)
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	// Docs for the roundtrip case: reuse the object docs from the obj-basic case.
	var docs []any
	for _, c := range g.Cases {
		if c.Name == "obj-basic" {
			docs = c.Docs
			break
		}
	}
	if docs == nil {
		t.Fatal("obj-basic docs not found")
	}
	opts := buildOptions(rt.Options)
	f, err := New(docs, opts, idx)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	results, err := f.Search(rt.Query, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := resultsToGeneric(results, true, true)
	compareResults(t, rt.Name, rt.Expected, got)
}

// Guard: goldens must exercise a minimum breadth so the suite cannot silently
// shrink to triviality.
func TestGoldenBreadth(t *testing.T) {
	g := loadGoldens(t)
	if len(g.Cases) < 100 {
		t.Fatalf("golden corpus shrank to %d cases", len(g.Cases))
	}
	var total float64
	for _, c := range g.Cases {
		for _, e := range c.Expected {
			if m, ok := e.(map[string]any); ok {
				if s, ok := m["score"].(float64); ok {
					total += s
				}
			}
		}
	}
	if total == 0 || math.IsNaN(total) {
		t.Fatal("no scored results across corpus")
	}
}
