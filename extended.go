package fuse

import (
	"regexp"
	"strings"
)

// matcher ports one fuse.js extended-search matcher.
type matcher struct {
	typ    string
	search func(text []uint16) SearchResult
}

var (
	reExactMulti      = regexp.MustCompile(`^="(.*)"$`)
	reExactSingle     = regexp.MustCompile(`^=(.*)$`)
	reIncludeMulti    = regexp.MustCompile(`^'"(.*)"$`)
	reIncludeSingle   = regexp.MustCompile(`^'(.*)$`)
	rePrefixMulti     = regexp.MustCompile(`^\^"(.*)"$`)
	rePrefixSingle    = regexp.MustCompile(`^\^(.*)$`)
	reInvPrefixMulti  = regexp.MustCompile(`^!\^"(.*)"$`)
	reInvPrefixSingle = regexp.MustCompile(`^!\^(.*)$`)
	reInvSuffixMulti  = regexp.MustCompile(`^!"(.*)"\$$`)
	reInvSuffixSingle = regexp.MustCompile(`^!(.*)\$$`)
	reSuffixMulti     = regexp.MustCompile(`^"(.*)"\$$`)
	reSuffixSingle    = regexp.MustCompile(`^(.*)\$$`)
	reInvExactMulti   = regexp.MustCompile(`^!"(.*)"$`)
	reInvExactSingle  = regexp.MustCompile(`^!(.*)$`)
	reFuzzyMulti      = regexp.MustCompile(`^"(.*)"$`)
	reFuzzySingle     = regexp.MustCompile(`^(.*)$`)
)

type matcherDef struct {
	typ         string
	multiRegex  *regexp.Regexp
	singleRegex *regexp.Regexp
	create      func(pattern string, opts bitapOptions) matcher
}

// matcherDefs order matters; fuzzy (catch-all) must stay last.
func matcherDefs() []matcherDef {
	simple := func(typ string, fn func(pat, text []uint16) SearchResult) func(string, bitapOptions) matcher {
		return func(pattern string, _ bitapOptions) matcher {
			pat := toU16(pattern)
			return matcher{typ: typ, search: func(text []uint16) SearchResult { return fn(pat, text) }}
		}
	}
	return []matcherDef{
		{"exact", reExactMulti, reExactSingle, simple("exact", func(pat, text []uint16) SearchResult {
			isMatch := equalU16(text, pat)
			r := SearchResult{IsMatch: isMatch, Score: 1, Indices: [][2]int{{0, len(pat) - 1}}}
			if isMatch {
				r.Score = 0
			}
			return r
		})},
		{"include", reIncludeMulti, reIncludeSingle, simple("include", func(pat, text []uint16) SearchResult {
			location := 0
			var indices [][2]int
			patternLen := len(pat)
			if patternLen == 0 {
				// Unreachable via parseQueryExt (empty captures are skipped),
				// but never loop on a non-advancing empty needle.
				return SearchResult{IsMatch: false, Score: 1}
			}
			for {
				index := indexOfU16(text, pat, location)
				if index <= -1 {
					break
				}
				location = index + patternLen
				indices = append(indices, [2]int{index, location - 1})
			}
			isMatch := len(indices) > 0
			r := SearchResult{IsMatch: isMatch, Score: 1, Indices: indices}
			if isMatch {
				r.Score = 0
			}
			return r
		})},
		{"prefix-exact", rePrefixMulti, rePrefixSingle, simple("prefix-exact", func(pat, text []uint16) SearchResult {
			isMatch := hasPrefixU16(text, pat)
			r := SearchResult{IsMatch: isMatch, Score: 1, Indices: [][2]int{{0, len(pat) - 1}}}
			if isMatch {
				r.Score = 0
			}
			return r
		})},
		{"inverse-prefix-exact", reInvPrefixMulti, reInvPrefixSingle, simple("inverse-prefix-exact", func(pat, text []uint16) SearchResult {
			isMatch := !hasPrefixU16(text, pat)
			r := SearchResult{IsMatch: isMatch, Score: 1, Indices: [][2]int{{0, len(text) - 1}}}
			if isMatch {
				r.Score = 0
			}
			return r
		})},
		{"inverse-suffix-exact", reInvSuffixMulti, reInvSuffixSingle, simple("inverse-suffix-exact", func(pat, text []uint16) SearchResult {
			isMatch := !hasSuffixU16(text, pat)
			r := SearchResult{IsMatch: isMatch, Score: 1, Indices: [][2]int{{0, len(text) - 1}}}
			if isMatch {
				r.Score = 0
			}
			return r
		})},
		{"suffix-exact", reSuffixMulti, reSuffixSingle, simple("suffix-exact", func(pat, text []uint16) SearchResult {
			isMatch := hasSuffixU16(text, pat)
			r := SearchResult{IsMatch: isMatch, Score: 1, Indices: [][2]int{{len(text) - len(pat), len(text) - 1}}}
			if isMatch {
				r.Score = 0
			}
			return r
		})},
		{"inverse-exact", reInvExactMulti, reInvExactSingle, simple("inverse-exact", func(pat, text []uint16) SearchResult {
			isMatch := indexOfU16(text, pat, 0) == -1
			r := SearchResult{IsMatch: isMatch, Score: 1, Indices: [][2]int{{0, len(text) - 1}}}
			if isMatch {
				r.Score = 0
			}
			return r
		})},
		{"fuzzy", reFuzzyMulti, reFuzzySingle, func(pattern string, opts bitapOptions) matcher {
			bitap := newBitapSearch(pattern, opts)
			return matcher{typ: "fuzzy", search: func(text []uint16) SearchResult {
				r, _ := bitap.searchIn(fromU16(text))
				return r
			}}
		}},
	}
}

var multiMatchTypes = map[string]bool{"fuzzy": true, "include": true}

func isInverseType(typ string) bool { return strings.HasPrefix(typ, "inverse") }

const escapedPipe = "\x00"

// tokenizeExt ports parseQuery's tokenizer: splits on spaces while respecting
// quoted multi-match tokens (prefix + "content" with inner quotes/spaces).
func tokenizeExt(pattern string) []string {
	var tokens []string
	length := len(pattern)
	i := 0
	for i < length {
		for i < length && pattern[i] == ' ' {
			i++
		}
		if i >= length {
			break
		}
		j := i
		for j < length && pattern[j] != ' ' && pattern[j] != '"' {
			j++
		}
		if j < length && pattern[j] == '"' {
			j++
			for j < length {
				if pattern[j] == '"' {
					next := j + 1
					if next >= length || pattern[next] == ' ' {
						j++
						break
					}
					if pattern[next] == '$' && (next+1 >= length || pattern[next+1] == ' ') {
						j += 2
						break
					}
				}
				j++
			}
			tokens = append(tokens, pattern[i:j])
			i = j
		} else {
			for j < length && pattern[j] != ' ' {
				j++
			}
			tokens = append(tokens, pattern[i:j])
			i = j
		}
	}
	return tokens
}

// getMatchRE mirrors fuse.js's `getMatch` + `if (token)` truthiness check:
// an empty capture is falsy in JS, so the matcher is skipped and the token
// falls through to a later matcher def (e.g. pattern "'" becomes a fuzzy
// search for a literal apostrophe, not an include of the empty string -
// which would loop forever, since indexOf(”, loc) never advances).
func getMatchRE(pattern string, exp *regexp.Regexp) (string, bool) {
	m := exp.FindStringSubmatch(pattern)
	if m == nil || m[1] == "" {
		return "", false
	}
	return m[1], true
}

// parseQueryExt ports extended/parseQuery.ts.
func parseQueryExt(pattern string, opts bitapOptions) [][]matcher {
	defs := matcherDefs()
	escaped := strings.ReplaceAll(pattern, `\|`, escapedPipe)
	groups := strings.Split(escaped, "|")
	out := make([][]matcher, 0, len(groups))
	for _, item := range groups {
		restored := strings.ReplaceAll(item, escapedPipe, "|")
		var query []string
		for _, tok := range tokenizeExt(strings.TrimSpace(restored)) {
			if tok != "" && strings.TrimSpace(tok) != "" {
				query = append(query, tok)
			}
		}
		var results []matcher
		for _, queryItem := range query {
			found := false
			for _, def := range defs {
				if token, ok := getMatchRE(queryItem, def.multiRegex); ok {
					results = append(results, def.create(token, opts))
					found = true
					break
				}
			}
			if found {
				continue
			}
			for _, def := range defs {
				if token, ok := getMatchRE(queryItem, def.singleRegex); ok {
					results = append(results, def.create(token, opts))
					break
				}
			}
		}
		out = append(out, results)
	}
	return out
}

// ExtendedSearch ports fuse.js ExtendedSearch.
type ExtendedSearch struct {
	query   [][]matcher
	options bitapOptions
	pattern string
}

func newExtendedSearch(pattern string, opts bitapOptions) *ExtendedSearch {
	p := pattern
	if !opts.isCaseSensitive {
		p = jsLowercaseString(p)
	}
	if opts.ignoreDiacritics {
		p = stripDiacriticsString(p)
	}
	e := &ExtendedSearch{options: opts, pattern: p}
	e.query = parseQueryExt(p, opts)
	return e
}

func (e *ExtendedSearch) searchIn(text string) SearchResult {
	query := e.query
	if query == nil {
		return SearchResult{IsMatch: false, Score: 1}
	}
	opts := e.options
	t := text
	if !opts.isCaseSensitive {
		t = jsLowercaseString(t)
	}
	if opts.ignoreDiacritics {
		t = stripDiacriticsString(t)
	}
	tu := toU16(t)

	for _, searchers := range query {
		var allIndices [][2]int
		numMatches := 0
		totalScore := 0.0
		hasInverse := false

		for _, m := range searchers {
			res := m.search(tu)
			if res.IsMatch {
				numMatches++
				totalScore += res.Score
				if isInverseType(m.typ) {
					hasInverse = true
				}
				if opts.includeMatches {
					if multiMatchTypes[m.typ] {
						allIndices = append(allIndices, res.Indices...)
					} else if len(res.Indices) > 0 {
						allIndices = append(allIndices, res.Indices[0])
					}
				}
			} else {
				totalScore = 0
				numMatches = 0
				allIndices = nil
				hasInverse = false
				break
			}
		}

		if numMatches > 0 {
			result := SearchResult{IsMatch: true, Score: totalScore / float64(numMatches), HasInverse: hasInverse}
			if opts.includeMatches {
				result.Indices = mergeIndices(allIndices)
			}
			return result
		}
	}

	return SearchResult{IsMatch: false, Score: 1}
}
