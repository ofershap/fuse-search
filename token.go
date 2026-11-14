package fuse

import (
	"math"
	"regexp"
)

// maxMaskTerms: tokenMatch "all" packs coverage into a 32-bit mask; JS bitwise
// ops are signed 32-bit, so only bits 0..30 are safe (31 terms).
const maxMaskTerms = 31

var defaultTokenRE = regexp.MustCompile(`[\p{L}\p{M}\p{N}_]+`)

// TokenizeFunc mirrors the fuse.js tokenize option: a function or a regexp.
type TokenizeFunc func(text string) []string

type analyzer struct {
	isCaseSensitive  bool
	ignoreDiacritics bool
	re               *regexp.Regexp
	fn               TokenizeFunc
}

func newAnalyzer(isCaseSensitive, ignoreDiacritics bool, tokenize any) *analyzer {
	a := &analyzer{isCaseSensitive: isCaseSensitive, ignoreDiacritics: ignoreDiacritics}
	switch t := tokenize.(type) {
	case nil:
		a.re = defaultTokenRE
	case *regexp.Regexp:
		a.re = t
	case TokenizeFunc:
		a.fn = t
	case func(string) []string:
		a.fn = t
	default:
		a.re = defaultTokenRE
	}
	return a
}

func (a *analyzer) tokenize(text string) []string {
	if !a.isCaseSensitive {
		text = jsLowercaseString(text)
	}
	if a.ignoreDiacritics {
		text = stripDiacriticsString(text)
	}
	if a.fn != nil {
		return a.fn(text)
	}
	return a.re.FindAllString(text, -1)
}

// invertedIndexData ports search/token/InvertedIndex.ts (stats-only index).
type invertedIndexData struct {
	fieldCount       int
	df               map[string]int
	docFieldCount    map[int]int
	docTermFieldHits map[int]map[string]int
}

func newInvertedIndexData() *invertedIndexData {
	return &invertedIndexData{
		df:               map[string]int{},
		docFieldCount:    map[int]int{},
		docTermFieldHits: map[int]map[string]int{},
	}
}

func (ix *invertedIndexData) addField(text string, docIdx int, a *analyzer) {
	tokens := a.tokenize(text)
	if len(tokens) == 0 {
		return
	}
	ix.fieldCount++
	ix.docFieldCount[docIdx]++

	distinct := map[string]struct{}{}
	for _, t := range tokens {
		distinct[t] = struct{}{}
	}
	perDoc := ix.docTermFieldHits[docIdx]
	if perDoc == nil {
		perDoc = map[string]int{}
		ix.docTermFieldHits[docIdx] = perDoc
	}
	for term := range distinct {
		perDoc[term]++
		ix.df[term]++
	}
}

func (ix *invertedIndexData) ingestRecord(rec indexRecord, keyCount int, a *analyzer) {
	if rec.hasValue {
		ix.addField(rec.v, rec.i, a)
		return
	}
	if rec.fields == nil {
		return
	}
	for keyIdx := 0; keyIdx < keyCount; keyIdx++ {
		value, ok := rec.fields[keyIdx]
		if !ok || value == nil {
			continue
		}
		switch v := value.(type) {
		case subRecord:
			if s, ok := v.V.(string); ok {
				ix.addField(s, rec.i, a)
			}
		case []subRecord:
			for _, sub := range v {
				if s, ok := sub.V.(string); ok {
					ix.addField(s, rec.i, a)
				}
			}
		}
	}
}

func (ix *invertedIndexData) removeFrom(docIdx int) {
	fieldCount, ok := ix.docFieldCount[docIdx]
	if !ok {
		return
	}
	ix.fieldCount -= fieldCount
	delete(ix.docFieldCount, docIdx)

	perDoc := ix.docTermFieldHits[docIdx]
	if perDoc == nil {
		return
	}
	for term, hits := range perDoc {
		next := ix.df[term] - hits
		if next <= 0 {
			delete(ix.df, term)
		} else {
			ix.df[term] = next
		}
	}
	delete(ix.docTermFieldHits, docIdx)
}

func (ix *invertedIndexData) removeAndShift(removedIndices []int) {
	if len(removedIndices) == 0 {
		return
	}
	sorted := dedupSortInts(removedIndices)
	for _, idx := range sorted {
		ix.removeFrom(idx)
	}
	shift := func(oldIdx int) int {
		lo, hi := 0, len(sorted)
		for lo < hi {
			mid := (lo + hi) / 2
			if sorted[mid] < oldIdx {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		return oldIdx - lo
	}
	firstRemoved := sorted[0]

	shifted := map[int]int{}
	for oldKey, count := range ix.docFieldCount {
		k := oldKey
		if oldKey > firstRemoved {
			k = shift(oldKey)
		}
		shifted[k] = count
	}
	ix.docFieldCount = shifted

	shiftedHits := map[int]map[string]int{}
	for oldKey, terms := range ix.docTermFieldHits {
		k := oldKey
		if oldKey > firstRemoved {
			k = shift(oldKey)
		}
		shiftedHits[k] = terms
	}
	ix.docTermFieldHits = shiftedHits
}

func dedupSortInts(in []int) []int {
	set := map[int]struct{}{}
	var out []int
	for _, v := range in {
		if _, ok := set[v]; !ok {
			set[v] = struct{}{}
			out = append(out, v)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// TokenSearch ports fuse.js TokenSearch (TF-IDF weighted per-term bitap).
type TokenSearch struct {
	termSearchers []*BitapSearch
	idfWeights    []float64
	includeMatch  bool
	analyzer      *analyzer
	combineAll    bool
	numTerms      int
	useMask       bool
}

func newTokenSearch(pattern string, opts resolvedOptions, invertedIndex *invertedIndexData) *TokenSearch {
	t := &TokenSearch{includeMatch: opts.includeMatches}
	t.analyzer = newAnalyzer(opts.isCaseSensitive, opts.ignoreDiacritics, opts.Tokenize)
	queryTerms := t.analyzer.tokenize(pattern)
	for _, term := range queryTerms {
		t.termSearchers = append(t.termSearchers, newBitapSearch(term, bitapOptions{
			location:           opts.location,
			threshold:          opts.threshold,
			distance:           opts.distance,
			includeMatches:     opts.includeMatches,
			findAllMatches:     opts.findAllMatches,
			minMatchCharLength: opts.minMatchCharLength,
			isCaseSensitive:    opts.isCaseSensitive,
			ignoreDiacritics:   opts.ignoreDiacritics,
			ignoreLocation:     true,
		}))
		docFreq := invertedIndex.df[term]
		idf := jsLog(1 + (float64(invertedIndex.fieldCount)-float64(docFreq)+0.5)/(float64(docFreq)+0.5))
		t.idfWeights = append(t.idfWeights, idf)
	}
	t.combineAll = opts.tokenMatch == "all"
	t.numTerms = len(t.termSearchers)
	t.useMask = t.numTerms <= maxMaskTerms
	return t
}

func (t *TokenSearch) searchIn(text string) SearchResult {
	if len(t.termSearchers) == 0 {
		return SearchResult{IsMatch: false, Score: 1}
	}
	var allIndices [][2]int
	weightedScore := 0.0
	maxPossibleScore := 0.0
	matchedCount := 0

	var matchedMask uint32
	var matchedTerms map[int]struct{}
	if t.combineAll && !t.useMask {
		matchedTerms = map[int]struct{}{}
	}

	for i, searcher := range t.termSearchers {
		result, _ := searcher.searchIn(text)
		idf := t.idfWeights[i]
		maxPossibleScore += idf
		if result.IsMatch {
			matchedCount++
			weightedScore += idf * (1 - result.Score)
			if result.Indices != nil {
				allIndices = append(allIndices, result.Indices...)
			}
			if t.combineAll {
				if t.useMask {
					matchedMask |= 1 << uint(i)
				} else {
					matchedTerms[i] = struct{}{}
				}
			}
		}
	}

	if matchedCount == 0 {
		return SearchResult{IsMatch: false, Score: 1}
	}

	normalized := 0.0
	if maxPossibleScore > 0 {
		normalized = 1 - weightedScore/maxPossibleScore
	}
	searchResult := SearchResult{IsMatch: true, Score: math.Max(0.001, normalized)}
	if t.includeMatch && len(allIndices) > 0 {
		searchResult.Indices = mergeIndices(allIndices)
	}
	if t.combineAll {
		searchResult.matchedMask = matchedMask
		searchResult.matchedTerms = matchedTerms
		searchResult.termCount = t.numTerms
		searchResult.hasTermCount = true
	}
	return searchResult
}
