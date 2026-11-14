package fuse

import "strings"

func stringsTrimFuncJS(s string) string {
	return strings.TrimSpace(s)
}

// maxHeap keeps the worst result at the root for top-N selection.
type maxHeap struct {
	limit      int
	heap       []internalResult
	comparator func(a, b internalResult) int
}

func newMaxHeap(limit int, comparator func(a, b internalResult) int) *maxHeap {
	return &maxHeap{limit: limit, comparator: comparator}
}

func (h *maxHeap) insert(item internalResult) {
	if len(h.heap) < h.limit {
		h.heap = append(h.heap, item)
		h.bubbleUp(len(h.heap) - 1)
	} else if h.comparator(item, h.heap[0]) < 0 {
		h.heap[0] = item
		h.sinkDown(0)
	}
}

func (h *maxHeap) extractSorted() []internalResult {
	out := append([]internalResult(nil), h.heap...)
	sortInternal(out, h.comparator)
	return out
}

func sortInternal(items []internalResult, cmp func(a, b internalResult) int) {
	// insertion sort over small heaps; heaps are bounded by limit
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && cmp(items[j], items[j-1]) < 0; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func (h *maxHeap) bubbleUp(i int) {
	for i > 0 {
		parent := (i - 1) >> 1
		if h.comparator(h.heap[i], h.heap[parent]) <= 0 {
			break
		}
		h.heap[i], h.heap[parent] = h.heap[parent], h.heap[i]
		i = parent
	}
}

func (h *maxHeap) sinkDown(i int) {
	length := len(h.heap)
	largest := i
	for {
		i = largest
		left := 2*i + 1
		right := 2*i + 2
		if left < length && h.comparator(h.heap[left], h.heap[largest]) > 0 {
			largest = left
		}
		if right < length && h.comparator(h.heap[right], h.heap[largest]) > 0 {
			largest = right
		}
		if largest == i {
			break
		}
		h.heap[i], h.heap[largest] = h.heap[largest], h.heap[i]
	}
}

func (f *Fuse) searchStringList(query string, heap *maxHeap) []internalResult {
	searcher := f.getSearcher(query)
	requireAllTokens := f.options.useTokenSearch && f.options.tokenMatch == "all"
	var results []internalResult
	if heap == nil {
		results = []internalResult{}
	}

	for _, rec := range f.myIndex.Records {
		if !rec.hasValue {
			continue
		}
		text := rec.v
		res := searcher.searchIn(text)
		if res.IsMatch {
			match := matchScore{
				score:   res.Score,
				value:   text,
				norm:    rec.n,
				indices: res.Indices,
			}
			if requireAllTokens {
				match.matchedMask = res.matchedMask
				match.matchedTerms = res.matchedTerms
				match.termCount = res.termCount
				match.hasTermCount = res.hasTermCount
			}
			matches := []matchScore{match}
			if !requireAllTokens || coversAllTokens(matches) {
				result := internalResult{item: text, idx: rec.i, matches: matches}
				if heap != nil {
					result.score = computeScoreSingle(result.matches, f.options.ignoreFieldNorm)
					heap.insert(result)
				} else {
					results = append(results, result)
				}
			}
		}
	}
	return results
}

func (f *Fuse) searchObjectList(query string, heap *maxHeap) ([]internalResult, error) {
	searcher := f.getSearcher(query)
	requireAllTokens := f.options.useTokenSearch && f.options.tokenMatch == "all"
	keys := f.normalizedKeys()
	var results []internalResult
	if heap == nil {
		results = []internalResult{}
	}

	for _, rec := range f.myIndex.Records {
		if rec.fields == nil {
			continue
		}
		item := rec.fields
		var matches []matchScore
		anyKeyFailed := false
		hasInverse := false

		for keyIndex := range keys {
			key := keys[keyIndex]
			keyMatches := f.findMatches(&key, item[keyIndex], searcher)
			if len(keyMatches) > 0 {
				matches = append(matches, keyMatches...)
				if keyMatches[0].hasInverse {
					hasInverse = true
				}
			} else {
				anyKeyFailed = true
			}
		}

		// If the search involves inverse patterns, ALL keys must match.
		if hasInverse && anyKeyFailed {
			continue
		}

		if len(matches) > 0 && (!requireAllTokens || coversAllTokens(matches)) {
			result := internalResult{idx: rec.i, item: item, matches: matches}
			if heap != nil {
				result.score = computeScoreSingle(result.matches, f.options.ignoreFieldNorm)
				heap.insert(result)
			} else {
				results = append(results, result)
			}
		}
	}
	return results, nil
}

func (f *Fuse) findMatches(key *keyObject, value any, searcher searcher) []matchScore {
	if value == nil {
		return nil
	}
	var matches []matchScore

	switch v := value.(type) {
	case []subRecord:
		for _, sub := range v {
			text, _ := sub.V.(string)
			res := searcher.searchIn(text)
			if res.IsMatch {
				match := matchScore{
					score:      res.Score,
					key:        key,
					value:      text,
					idx:        sub.I,
					hasIdx:     sub.hasI,
					norm:       sub.N,
					indices:    res.Indices,
					hasInverse: res.HasInverse,
				}
				if res.hasTermCount {
					match.matchedMask = res.matchedMask
					match.matchedTerms = res.matchedTerms
					match.termCount = res.termCount
					match.hasTermCount = true
				}
				matches = append(matches, match)
			}
		}
	case subRecord:
		text, _ := v.V.(string)
		res := searcher.searchIn(text)
		if res.IsMatch {
			match := matchScore{
				score:      res.Score,
				key:        key,
				value:      text,
				norm:       v.N,
				indices:    res.Indices,
				hasInverse: res.HasInverse,
			}
			if res.hasTermCount {
				match.matchedMask = res.matchedMask
				match.matchedTerms = res.matchedTerms
				match.termCount = res.termCount
				match.hasTermCount = true
			}
			matches = append(matches, match)
		}
	}
	return matches
}

func coversAllTokens(matches []matchScore) bool {
	if len(matches) == 0 || !matches[0].hasTermCount {
		return true
	}
	termCount := matches[0].termCount
	if termCount <= maxMaskTerms {
		var coverage uint32
		for _, m := range matches {
			coverage |= m.matchedMask
		}
		return coverage == (1<<uint(termCount))-1
	}
	coverage := map[int]struct{}{}
	for _, m := range matches {
		for t := range m.matchedTerms {
			coverage[t] = struct{}{}
		}
	}
	return len(coverage) == termCount
}
