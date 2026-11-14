// Package fuse is a zero-dependency port of Fuse.js v7.5.0, the lightweight
// fuzzy-search library, for Go. It is behaviorally pinned to the reference
// implementation: search results, scores, and match indices (in UTF-16 code
// units, exactly as fuse.js reports them) are verified against the npm
// package through a differential golden suite.
//
// Feature flags: this package always provides the full build surface,
// equivalent to the npm "fuse.js" build (extended search, logical queries,
// and token search enabled), not "fuse.js/basic".
package fuse

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
)

func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// Version of this package's reference-compatibility target.
const Version = "7.5.0-compat"

// ── options ───────────────────────────────────────────────────────

// Options mirrors IFuseOptions. Pointer fields distinguish "unset" from an
// explicit zero value, exactly like the JS defaults table. Use the Bool,
// Int, and Float64 helpers to build them.
type Options struct {
	IsCaseSensitive    *bool
	IgnoreDiacritics   *bool
	IncludeScore       *bool
	Keys               []any // string, []string, Key, or *Key
	ShouldSort         *bool
	SortFn             func(a, b SortArg) int
	IncludeMatches     *bool
	FindAllMatches     *bool
	MinMatchCharLength *int
	Location           *int
	Threshold          *float64
	Distance           *int
	UseExtendedSearch  *bool
	UseTokenSearch     *bool
	Tokenize           any // *regexp.Regexp or func(string) []string
	TokenMatch         string
	GetFn              func(obj any, path []string) any
	IgnoreLocation     *bool
	IgnoreFieldNorm    *bool
	FieldNormWeight    *float64
}

// Bool returns a *bool for Options fields.
func Bool(v bool) *bool { return &v }

// Int returns an *int for Options fields.
func Int(v int) *int { return &v }

// Float64 returns a *float64 for Options fields.
func Float64(v float64) *float64 { return &v }

type resolvedOptions struct {
	isCaseSensitive    bool
	ignoreDiacritics   bool
	includeScore       bool
	keys               []any
	shouldSort         bool
	sortFn             func(a, b SortArg) int
	includeMatches     bool
	findAllMatches     bool
	minMatchCharLength int
	location           int
	threshold          float64
	distance           int
	useExtendedSearch  bool
	useTokenSearch     bool
	Tokenize           any
	tokenMatch         string
	getFn              func(obj any, path []string) any
	ignoreLocation     bool
	ignoreFieldNorm    bool
	fieldNormWeight    float64
}

func resolveOptions(o *Options) resolvedOptions {
	r := resolvedOptions{
		keys:               nil,
		shouldSort:         true,
		sortFn:             DefaultSortFn,
		minMatchCharLength: 1,
		threshold:          0.6,
		distance:           100,
		tokenMatch:         "any",
		getFn:              getValue,
		fieldNormWeight:    1,
	}
	if o == nil {
		return r
	}
	if o.IsCaseSensitive != nil {
		r.isCaseSensitive = *o.IsCaseSensitive
	}
	if o.IgnoreDiacritics != nil {
		r.ignoreDiacritics = *o.IgnoreDiacritics
	}
	if o.IncludeScore != nil {
		r.includeScore = *o.IncludeScore
	}
	if o.Keys != nil {
		r.keys = o.Keys
	}
	if o.ShouldSort != nil {
		r.shouldSort = *o.ShouldSort
	}
	if o.SortFn != nil {
		r.sortFn = o.SortFn
	}
	if o.IncludeMatches != nil {
		r.includeMatches = *o.IncludeMatches
	}
	if o.FindAllMatches != nil {
		r.findAllMatches = *o.FindAllMatches
	}
	if o.MinMatchCharLength != nil {
		r.minMatchCharLength = *o.MinMatchCharLength
	}
	if o.Location != nil {
		r.location = *o.Location
	}
	if o.Threshold != nil {
		r.threshold = *o.Threshold
	}
	if o.Distance != nil {
		r.distance = *o.Distance
	}
	if o.UseExtendedSearch != nil {
		r.useExtendedSearch = *o.UseExtendedSearch
	}
	if o.UseTokenSearch != nil {
		r.useTokenSearch = *o.UseTokenSearch
	}
	if o.Tokenize != nil {
		r.Tokenize = o.Tokenize
	}
	if o.TokenMatch != "" {
		r.tokenMatch = o.TokenMatch
	}
	if o.GetFn != nil {
		r.getFn = o.GetFn
	}
	if o.IgnoreLocation != nil {
		r.ignoreLocation = *o.IgnoreLocation
	}
	if o.IgnoreFieldNorm != nil {
		r.ignoreFieldNorm = *o.IgnoreFieldNorm
	}
	if o.FieldNormWeight != nil {
		r.fieldNormWeight = *o.FieldNormWeight
	}
	return r
}

func (r resolvedOptions) bitap() bitapOptions {
	return bitapOptions{
		location:           r.location,
		threshold:          r.threshold,
		distance:           r.distance,
		includeMatches:     r.includeMatches,
		findAllMatches:     r.findAllMatches,
		minMatchCharLength: r.minMatchCharLength,
		isCaseSensitive:    r.isCaseSensitive,
		ignoreDiacritics:   r.ignoreDiacritics,
		ignoreLocation:     r.ignoreLocation,
	}
}

// SortArg is the sortFn argument shape (FuseSortFunctionArg).
type SortArg struct {
	Score float64
	Idx   int
}

// DefaultSortFn is the fuse.js default: score ascending, refIndex tie-break.
func DefaultSortFn(a, b SortArg) int {
	if a.Score == b.Score {
		if a.Idx < b.Idx {
			return -1
		}
		return 1
	}
	if a.Score < b.Score {
		return -1
	}
	return 1
}

// ── results ───────────────────────────────────────────────────────

// ResultMatch mirrors FuseResultMatch.
type ResultMatch struct {
	Indices  [][2]int `json:"indices"`
	Value    string   `json:"value"`
	Key      string   `json:"key,omitempty"`
	RefIndex *int     `json:"refIndex,omitempty"`
}

// Result mirrors FuseResult.
type Result struct {
	Item     any           `json:"item"`
	RefIndex int           `json:"refIndex"`
	Score    *float64      `json:"score,omitempty"`
	Matches  []ResultMatch `json:"matches,omitempty"`
}

// SearchOptions mirrors FuseSearchOptions.
type SearchOptions struct {
	Limit int // default -1 (no limit)
}

// ── internal result plumbing ──────────────────────────────────────

type matchScore struct {
	score        float64
	key          *keyObject
	value        string
	idx          int
	hasIdx       bool
	norm         float64
	indices      [][2]int
	hasInverse   bool
	matchedMask  uint32
	matchedTerms map[int]struct{}
	termCount    int
	hasTermCount bool
}

type internalResult struct {
	item    any
	idx     int
	matches []matchScore
	score   float64
}

// ── searcher dispatch ─────────────────────────────────────────────

type searcher interface {
	searchIn(text string) SearchResult
}

type bitapSearcher struct{ b *BitapSearch }

func (s bitapSearcher) searchIn(text string) SearchResult {
	r, _ := s.b.searchIn(text)
	return r
}

// createSearcher mirrors core/register.ts dispatch order: ExtendedSearch is
// registered before TokenSearch, so extended wins when both flags are set.
func createSearcher(pattern string, opts resolvedOptions, invertedIndex *invertedIndexData) searcher {
	if opts.useExtendedSearch {
		e := newExtendedSearch(pattern, opts.bitap())
		return searcherFunc(e.searchIn)
	}
	if opts.useTokenSearch {
		t := newTokenSearch(pattern, opts, invertedIndex)
		return searcherFunc(t.searchIn)
	}
	b := newBitapSearch(pattern, opts.bitap())
	return bitapSearcher{b}
}

type searcherFunc func(text string) SearchResult

func (f searcherFunc) searchIn(text string) SearchResult { return f(text) }

// ── Fuse ──────────────────────────────────────────────────────────

// Fuse is the fuzzy-search collection. Construct it with New.
type Fuse struct {
	options       resolvedOptions
	keyStore      *keyStore
	docs          []any
	myIndex       *FuseIndex
	invertedIndex *invertedIndexData
	lastQuery     string
	lastSearcher  searcher
	hasLast       bool
}

// New constructs a Fuse over docs (strings, maps, or structs). An optional
// pre-built index (from CreateIndex/ParseIndex) may be supplied.
func New(docs []any, options *Options, index *FuseIndex) (*Fuse, error) {
	f := &Fuse{options: resolveOptions(options)}
	ks, err := newKeyStore(f.options.keys)
	if err != nil {
		return nil, err
	}
	f.keyStore = ks
	if err := f.SetCollection(docs, index); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *Fuse) getSearcher(query string) searcher {
	if f.hasLast && f.lastQuery == query {
		return f.lastSearcher
	}
	f.lastSearcher = createSearcher(query, f.options, f.invertedIndex)
	f.lastQuery = query
	f.hasLast = true
	return f.lastSearcher
}

// SetCollection replaces the docs and index (Fuse.setCollection).
func (f *Fuse) SetCollection(docs []any, index *FuseIndex) error {
	f.docs = docs
	if index == nil {
		w := f.options.fieldNormWeight
		idx, err := CreateIndex(f.options.keys, docs, &IndexOptions{
			GetFn:           f.options.getFn,
			FieldNormWeight: &w,
		})
		if err != nil {
			return err
		}
		index = idx
	}
	f.myIndex = index

	if f.options.useTokenSearch {
		a := newAnalyzer(f.options.isCaseSensitive, f.options.ignoreDiacritics, f.options.Tokenize)
		ix := newInvertedIndexData()
		for _, rec := range f.myIndex.Records {
			ix.ingestRecord(rec, len(f.myIndex.Keys), a)
		}
		f.invertedIndex = ix
	} else {
		f.invertedIndex = nil
	}
	f.invalidateSearcherCache()
	return nil
}

// Add appends a doc (Fuse.add). Blank strings are indexed as no-ops.
func (f *Fuse) Add(doc any) error {
	if doc == nil {
		return nil
	}
	f.docs = append(f.docs, doc)
	rec, err := f.myIndex.add(doc, len(f.docs)-1)
	if err != nil {
		return err
	}
	if f.invertedIndex != nil && rec != nil {
		a := newAnalyzer(f.options.isCaseSensitive, f.options.ignoreDiacritics, f.options.Tokenize)
		f.invertedIndex.ingestRecord(*rec, len(f.myIndex.Keys), a)
	}
	f.invalidateSearcherCache()
	return nil
}

// Remove removes all docs for which predicate returns true (Fuse.remove).
func (f *Fuse) Remove(predicate func(doc any, idx int) bool) []any {
	var results []any
	var indicesToRemove []int
	for i, doc := range f.docs {
		if predicate(doc, i) {
			results = append(results, doc)
			indicesToRemove = append(indicesToRemove, i)
		}
	}
	if len(indicesToRemove) > 0 {
		if f.invertedIndex != nil {
			f.invertedIndex.removeAndShift(indicesToRemove)
		}
		toRemove := map[int]struct{}{}
		for _, i := range indicesToRemove {
			toRemove[i] = struct{}{}
		}
		kept := f.docs[:0]
		for i, doc := range f.docs {
			if _, rm := toRemove[i]; !rm {
				kept = append(kept, doc)
			}
		}
		f.docs = kept
		f.myIndex.removeAll(indicesToRemove)
		f.invalidateSearcherCache()
	}
	return results
}

// RemoveAt removes the doc at idx (Fuse.removeAt).
func (f *Fuse) RemoveAt(idx int) (any, error) {
	if idx < 0 || idx >= len(f.docs) {
		return nil, errors.New("Invalid doc index: must be a non-negative integer within the bounds of the docs array")
	}
	if f.invertedIndex != nil {
		f.invertedIndex.removeAndShift([]int{idx})
	}
	doc := f.docs[idx]
	f.docs = append(f.docs[:idx], f.docs[idx+1:]...)
	if err := f.myIndex.removeAt(idx); err != nil {
		return nil, err
	}
	f.invalidateSearcherCache()
	return doc, nil
}

func (f *Fuse) invalidateSearcherCache() {
	f.hasLast = false
	f.lastSearcher = nil
	f.lastQuery = ""
}

// GetIndex returns the underlying index (Fuse.getIndex).
func (f *Fuse) GetIndex() *FuseIndex { return f.myIndex }

func (f *Fuse) normalizedKeys() []keyObject {
	out := make([]keyObject, 0, len(f.myIndex.Keys))
	for _, key := range f.myIndex.Keys {
		if k, ok := f.keyStore.get(key.id); ok {
			out = append(out, k)
		} else {
			out = append(out, key)
		}
	}
	return out
}

// Search runs a query: a string, or a logical expression map
// (map[string]any with $and/$or or key:pattern entries).
func (f *Fuse) Search(query any, options *SearchOptions) ([]Result, error) {
	limit := -1
	if options != nil && options.Limit != 0 {
		limit = options.Limit
	}
	opts := f.options

	// Empty string query returns all docs.
	if qs, ok := query.(string); ok && trimJS(qs) == "" {
		var docs []Result
		for idx, item := range f.docs {
			docs = append(docs, Result{Item: item, RefIndex: idx})
		}
		if limit > -1 {
			docs = docs[:minInt(limit, len(docs))]
		}
		return docs, nil
	}

	comparator := opts.sortFn
	stable := func(a, b internalResult) int {
		if c := comparator(SortArg{Score: a.score, Idx: a.idx}, SortArg{Score: b.score, Idx: b.idx}); c != 0 {
			return c
		}
		if a.idx < b.idx {
			return -1
		}
		if a.idx > b.idx {
			return 1
		}
		return 0
	}

	_, isStringQuery := query.(string)
	useHeap := opts.shouldSort && limit > 0 && isStringQuery

	var results []internalResult

	if useHeap {
		heap := newMaxHeap(limit, stable)
		if f.isStringList() {
			f.searchStringList(query.(string), heap)
		} else {
			if _, err := f.searchObjectList(query.(string), heap); err != nil {
				return nil, err
			}
		}
		results = heap.extractSorted()
	} else {
		if isStringQuery {
			if f.isStringList() {
				results = f.searchStringList(query.(string), nil)
			} else {
				var err error
				results, err = f.searchObjectList(query.(string), nil)
				if err != nil {
					return nil, err
				}
			}
		} else {
			var err error
			results, err = f.searchLogical(query)
			if err != nil {
				return nil, err
			}
		}

		computeScores(results, opts.ignoreFieldNorm)

		if opts.shouldSort {
			if isStringQuery {
				sort.SliceStable(results, func(i, j int) bool { return stable(results[i], results[j]) < 0 })
			} else {
				sort.SliceStable(results, func(i, j int) bool {
					return comparator(SortArg{Score: results[i].score, Idx: results[i].idx}, SortArg{Score: results[j].score, Idx: results[j].idx}) < 0
				})
			}
		}

		if limit > -1 {
			results = results[:minInt(limit, len(results))]
		}
	}

	return formatResults(results, f.docs, opts.includeMatches, opts.includeScore), nil
}

func trimJS(s string) string {
	// JS String.prototype.trim: Unicode whitespace + line terminators.
	return stringsTrimFuncJS(s)
}

func (f *Fuse) isStringList() bool {
	if len(f.docs) == 0 {
		return false
	}
	_, ok := f.docs[0].(string)
	return ok
}

func computeScoreSingle(matches []matchScore, ignoreFieldNorm bool) float64 {
	totalScore := 1.0
	for _, m := range matches {
		var weight float64
		if m.key != nil {
			weight = m.key.weight
		}
		score := m.score
		if score == 0 && weight != 0 {
			score = math.Nextafter(1, 2) - 1 // Number.EPSILON
		}
		w := weight
		if w == 0 {
			w = 1
		}
		norm := m.norm
		if ignoreFieldNorm {
			norm = 1
		}
		totalScore *= jsPow(score, w*norm)
	}
	return totalScore
}

func computeScores(results []internalResult, ignoreFieldNorm bool) {
	for i := range results {
		results[i].score = computeScoreSingle(results[i].matches, ignoreFieldNorm)
	}
}

func formatResults(results []internalResult, docs []any, includeMatches, includeScore bool) []Result {
	out := make([]Result, 0, len(results))
	for _, result := range results {
		data := Result{Item: docs[result.idx], RefIndex: result.idx}
		if includeMatches {
			data.Matches = formatMatches(result)
		}
		if includeScore {
			s := result.score
			data.Score = &s
		}
		out = append(out, data)
	}
	return out
}

func formatMatches(result internalResult) []ResultMatch {
	var matches []ResultMatch
	for _, match := range result.matches {
		if len(match.indices) == 0 {
			continue
		}
		obj := ResultMatch{Indices: match.indices, Value: match.value}
		if match.key != nil {
			obj.Key = match.key.id
		}
		if match.hasIdx && match.idx > -1 {
			idx := match.idx
			obj.RefIndex = &idx
		}
		matches = append(matches, obj)
	}
	return matches
}
