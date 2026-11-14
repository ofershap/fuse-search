package fuse

import (
	"errors"
	"fmt"
)

// Logical operators (fuse.js Expression shape).
const (
	LogicalAND = "$and"
	LogicalOR  = "$or"
	KeyPath    = "$path"
	KeyPattern = "$val"
)

type parsedLeaf struct {
	keyID    string
	hasKeyID bool
	pattern  string
	searcher searcher
}

type parsedOperator struct {
	children []parsedNode
	operator string
}

type parsedNode interface{ isParsedNode() }

func (parsedLeaf) isParsedNode()     {}
func (parsedOperator) isParsedNode() {}

func isExpressionQuery(query map[string]any) bool {
	_, hasAnd := query[LogicalAND]
	_, hasOr := query[LogicalOR]
	return hasAnd || hasOr
}

func convertToExplicit(query map[string]any) map[string]any {
	// Object.keys order in JS is insertion order for string keys; Go maps are
	// unordered. For deterministic behavior matching JS, callers should build
	// expressions with $and/$or slices; a plain map converts with sorted keys.
	var keys []string
	for k := range query {
		keys = append(keys, k)
	}
	sortStrings(keys)
	children := make([]any, 0, len(keys))
	for _, k := range keys {
		children = append(children, map[string]any{k: query[k]})
	}
	return map[string]any{LogicalAND: children}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func parseQuery(query any, opts resolvedOptions, invertedIndex *invertedIndexData) (parsedNode, error) {
	var next func(query any) (parsedNode, error)
	next = func(query any) (parsedNode, error) {
		// Keyless string entry: search across all keys.
		if qs, ok := query.(string); ok {
			return parsedLeaf{keyID: "", hasKeyID: false, pattern: qs, searcher: createSearcher(qs, opts, invertedIndex)}, nil
		}

		qmap, ok := query.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Invalid logical query entry of type %T", query)
		}

		_, isQueryPath := qmap[KeyPath]
		numKeys := len(qmap)

		if !isQueryPath && numKeys > 1 && !isExpressionQuery(qmap) {
			return next(convertToExplicit(qmap))
		}

		if !isExpressionQuery(qmap) {
			// Leaf
			var key string
			var keyAny, patternAny any
			if isQueryPath {
				keyAny = qmap[KeyPath]
				patternAny = qmap[KeyPattern]
			} else {
				for k, v := range qmap {
					keyAny, patternAny = k, v
				}
			}
			switch k := keyAny.(type) {
			case string:
				key = k
			case []string:
				key = createKeyID(k)
			case []any:
				var parts []string
				for _, e := range k {
					if s, ok := e.(string); ok {
						parts = append(parts, s)
					}
				}
				key = createKeyID(parts)
			}
			ps, ok := patternAny.(string)
			if !ok {
				return nil, fmt.Errorf("Invalid value for key %s", key)
			}
			return parsedLeaf{keyID: key, hasKeyID: true, pattern: ps, searcher: createSearcher(ps, opts, invertedIndex)}, nil
		}

		node := parsedOperator{children: []parsedNode{}}
		for k := range qmap {
			node.operator = k
		}
		for k, value := range qmap {
			arr, ok := value.([]any)
			if !ok {
				continue
			}
			for _, item := range arr {
				child, err := next(item)
				if err != nil {
					return nil, err
				}
				node.children = append(node.children, child)
			}
			_ = k
		}
		return node, nil
	}

	qmap, ok := query.(map[string]any)
	if !ok {
		return nil, errors.New("Logical query must be a map[string]any")
	}
	if !isExpressionQuery(qmap) {
		qmap = convertToExplicit(qmap)
	}
	return next(qmap)
}

func (f *Fuse) searchLogical(query any) ([]internalResult, error) {
	expression, err := parseQuery(query, f.options, f.invertedIndex)
	if err != nil {
		return nil, err
	}

	keys := f.normalizedKeys()

	var evaluate func(node parsedNode, item map[int]any, idx int) []internalResult
	evaluate = func(node parsedNode, item map[int]any, idx int) []internalResult {
		switch n := node.(type) {
		case parsedLeaf:
			var matches []matchScore
			if !n.hasKeyID {
				for keyIndex := range keys {
					key := keys[keyIndex]
					matches = append(matches, f.findMatches(&key, item[keyIndex], n.searcher)...)
				}
			} else {
				ko, ok := f.keyStore.get(n.keyID)
				if !ok {
					return nil
				}
				matches = f.findMatches(&ko, item[f.myIndex.keysMap[n.keyID]], n.searcher)
			}
			if len(matches) > 0 {
				return []internalResult{{idx: idx, item: item, matches: matches}}
			}
			return nil
		case parsedOperator:
			var res []internalResult
			for _, child := range n.children {
				result := evaluate(child, item, idx)
				if len(result) > 0 {
					res = append(res, result...)
				} else if n.operator == LogicalAND {
					return nil
				}
			}
			return res
		}
		return nil
	}

	resultMap := map[int]*internalResult{}
	var results []internalResult

	for _, rec := range f.myIndex.Records {
		if rec.fields == nil {
			continue
		}
		item := rec.fields
		expResults := evaluate(expression, item, rec.i)
		if len(expResults) > 0 {
			entry, ok := resultMap[rec.i]
			if !ok {
				results = append(results, internalResult{idx: rec.i, item: item, matches: nil})
				entry = &results[len(results)-1]
				resultMap[rec.i] = entry
			}
			for _, er := range expResults {
				entry.matches = append(entry.matches, er.matches...)
			}
		}
	}

	return results, nil
}

// Match compares one pattern against one text (Fuse.match). Token search is
// unsupported here, exactly as in fuse.js: it needs corpus statistics that a
// one-off comparison does not have.
func Match(pattern, text string, options *Options) (SearchResult, error) {
	opts := resolveOptions(options)
	if opts.useTokenSearch {
		return SearchResult{}, errors.New("Fuse.match does not support useTokenSearch: token search requires corpus-level statistics (df, fieldCount) that a one-off string comparison does not have. Use New(...).Search(...) instead.")
	}
	searcher := createSearcher(pattern, opts, nil)
	return searcher.searchIn(text), nil
}
