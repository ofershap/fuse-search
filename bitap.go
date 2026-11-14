package fuse

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// maxBits is the machine word size of the reference implementation.
const maxBits = 32

// bitapOptions carries the resolved (non-pointer) options a BitapSearch needs.
type bitapOptions struct {
	location           int
	threshold          float64
	distance           int
	includeMatches     bool
	findAllMatches     bool
	minMatchCharLength int
	isCaseSensitive    bool
	ignoreDiacritics   bool
	ignoreLocation     bool
}

type bitapChunk struct {
	pattern    []uint16
	alphabet   map[uint16]uint32
	startIndex int
}

// BitapSearch is a direct port of fuse.js BitapSearch.
type BitapSearch struct {
	options bitapOptions
	pattern []uint16
	chunks  []bitapChunk
}

// SearchResult mirrors the fuse.js SearchResult shape. Indices are UTF-16
// code-unit ranges, identical to fuse.js includeMatches output.
type SearchResult struct {
	IsMatch      bool
	Score        float64
	Indices      [][2]int
	HasInverse   bool
	matchedMask  uint32
	matchedTerms map[int]struct{}
	termCount    int
	hasTermCount bool
}

func newBitapSearch(pattern string, opts bitapOptions) *BitapSearch {
	b := &BitapSearch{options: opts}
	p := pattern
	if !opts.isCaseSensitive {
		p = jsLowercaseString(p)
	}
	if opts.ignoreDiacritics {
		p = stripDiacriticsString(p)
	}
	b.pattern = toU16(p)

	if len(b.pattern) == 0 {
		return b
	}

	addChunk := func(pat []uint16, startIndex int) {
		b.chunks = append(b.chunks, bitapChunk{
			pattern:    pat,
			alphabet:   createPatternAlphabet(pat),
			startIndex: startIndex,
		})
	}

	length := len(b.pattern)
	if length > maxBits {
		i := 0
		remainder := length % maxBits
		end := length - remainder
		for i < end {
			addChunk(b.pattern[i:i+maxBits], i)
			i += maxBits
		}
		if remainder != 0 {
			startIndex := length - maxBits
			addChunk(b.pattern[startIndex:], startIndex)
		}
	} else {
		addChunk(b.pattern, 0)
	}
	return b
}

func createPatternAlphabet(pattern []uint16) map[uint16]uint32 {
	mask := make(map[uint16]uint32, len(pattern))
	for i, c := range pattern {
		mask[c] |= 1 << uint(len(pattern)-i-1)
	}
	return mask
}

func (b *BitapSearch) searchIn(text string) (SearchResult, error) {
	opts := b.options
	t := text
	if !opts.isCaseSensitive {
		t = jsLowercaseString(t)
	}
	if opts.ignoreDiacritics {
		t = stripDiacriticsString(t)
	}
	tu := toU16(t)

	// Exact match
	if equalU16(b.pattern, tu) {
		if len(tu) < opts.minMatchCharLength {
			return SearchResult{IsMatch: false, Score: 1}, nil
		}
		result := SearchResult{IsMatch: true, Score: 0}
		if opts.includeMatches {
			result.Indices = [][2]int{{0, len(tu) - 1}}
		}
		return result, nil
	}

	var allIndices [][2]int
	totalScore := 0.0
	hasMatches := false

	for _, chunk := range b.chunks {
		res, err := bitapSearch(tu, chunk.pattern, chunk.alphabet, bitapOptions{
			location:           opts.location + chunk.startIndex,
			distance:           opts.distance,
			threshold:          opts.threshold,
			findAllMatches:     opts.findAllMatches,
			minMatchCharLength: opts.minMatchCharLength,
			includeMatches:     opts.includeMatches,
			ignoreLocation:     opts.ignoreLocation,
		})
		if err != nil {
			return SearchResult{}, err
		}
		if res.IsMatch {
			hasMatches = true
		}
		totalScore += res.Score
		if res.IsMatch && res.Indices != nil {
			allIndices = append(allIndices, res.Indices...)
		}
	}

	result := SearchResult{IsMatch: hasMatches, Score: 1}
	if hasMatches {
		result.Score = totalScore / float64(len(b.chunks))
	}
	if hasMatches && opts.includeMatches {
		result.Indices = mergeIndices(allIndices)
	}
	return result, nil
}

func equalU16(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// bitapSearch ports fuse.js search/bitap/search.ts. All offsets are UTF-16
// code units. Bit masks use uint32, matching JS 32-bit bitwise semantics.
func bitapSearch(text, pattern []uint16, patternAlphabet map[uint16]uint32, opts bitapOptions) (SearchResult, error) {
	if len(pattern) > maxBits {
		return SearchResult{}, errors.New(fmt.Sprintf("Pattern length exceeds max of %d.", maxBits))
	}

	patternLen := len(pattern)
	textLen := len(text)
	expectedLocation := maxInt(0, minInt(opts.location, textLen))
	currentThreshold := opts.threshold
	bestLocation := expectedLocation

	calcScore := func(errCount, currentLocation int) float64 {
		accuracy := float64(errCount) / float64(patternLen)
		if opts.ignoreLocation {
			return accuracy
		}
		proximity := math.Abs(float64(expectedLocation - currentLocation))
		if opts.distance == 0 {
			if proximity != 0 {
				return 1.0
			}
			return accuracy
		}
		return accuracy + proximity/float64(opts.distance)
	}

	computeMatches := opts.minMatchCharLength > 1 || opts.includeMatches
	var matchMask []uint8
	if computeMatches {
		matchMask = make([]uint8, textLen)
	}

	// Get all exact matches, here for speed up
	index := -1
	for {
		index = indexOfU16(text, pattern, bestLocation)
		if index <= -1 {
			break
		}
		score := calcScore(0, index)
		currentThreshold = math.Min(score, currentThreshold)
		bestLocation = index + patternLen
		if computeMatches {
			for i := 0; i < patternLen; i++ {
				matchMask[index+i] = 1
			}
		}
	}

	bestLocation = -1
	var lastBitArr []uint32
	finalScore := 1.0
	bestErrors := 0
	binMax := patternLen + textLen

	var mask uint32 = 1 << uint(patternLen-1)

	for i := 0; i < patternLen; i++ {
		binMin := 0
		binMid := binMax

		for binMin < binMid {
			score := calcScore(i, expectedLocation+binMid)
			if score <= currentThreshold {
				binMin = binMid
			} else {
				binMax = binMid
			}
			binMid = (binMax-binMin)/2 + binMin
		}

		binMax = binMid

		start := maxInt(1, expectedLocation-binMid+1)
		var finish int
		if opts.findAllMatches {
			finish = textLen
		} else {
			finish = minInt(expectedLocation+binMid, textLen) + patternLen
		}

		bitArr := make([]uint32, finish+2)
		bitArr[finish+1] = (1 << uint(i)) - 1

		for j := finish; j >= start; j-- {
			currentLocation := j - 1
			var charMatch uint32
			if currentLocation >= 0 && currentLocation < textLen {
				charMatch = patternAlphabet[text[currentLocation]]
			}

			bitArr[j] = ((bitArr[j+1] << 1) | 1) & charMatch

			if i != 0 {
				var lb1, lb0 uint32
				if j+1 < len(lastBitArr) {
					lb1 = lastBitArr[j+1]
				}
				if j < len(lastBitArr) {
					lb0 = lastBitArr[j]
				}
				bitArr[j] |= ((lb1 | lb0) << 1) | 1 | lb1
			}

			if bitArr[j]&mask != 0 {
				finalScore = calcScore(i, currentLocation)
				if finalScore <= currentThreshold {
					currentThreshold = finalScore
					bestLocation = currentLocation
					bestErrors = i

					if bestLocation <= expectedLocation {
						break
					}

					start = maxInt(1, 2*expectedLocation-bestLocation)
				}
			}
		}

		score := calcScore(i+1, expectedLocation)
		if score > currentThreshold {
			break
		}

		lastBitArr = bitArr
	}

	if computeMatches && bestLocation >= 0 {
		matchEnd := minInt(textLen-1, bestLocation+patternLen-1+bestErrors)
		for k := bestLocation; k <= matchEnd; k++ {
			if patternAlphabet[text[k]] != 0 {
				matchMask[k] = 1
			}
		}
	}

	result := SearchResult{
		IsMatch: bestLocation >= 0,
		Score:   math.Max(0.001, finalScore),
	}

	if computeMatches {
		indices := convertMaskToIndices(matchMask, opts.minMatchCharLength)
		if len(indices) == 0 {
			result.IsMatch = false
		} else if opts.includeMatches {
			result.Indices = indices
		}
	}

	return result, nil
}

func convertMaskToIndices(matchmask []uint8, minMatchCharLength int) [][2]int {
	var indices [][2]int
	start := -1
	end := -1
	i := 0
	for ; i < len(matchmask); i++ {
		match := matchmask[i] != 0
		if match && start == -1 {
			start = i
		} else if !match && start != -1 {
			end = i - 1
			if end-start+1 >= minMatchCharLength {
				indices = append(indices, [2]int{start, end})
			}
			start = -1
		}
	}
	if len(matchmask) > 0 && matchmask[len(matchmask)-1] != 0 && i-start >= minMatchCharLength {
		indices = append(indices, [2]int{start, i - 1})
	}
	return indices
}

func mergeIndices(indices [][2]int) [][2]int {
	if len(indices) <= 1 {
		return indices
	}
	sort.Slice(indices, func(i, j int) bool {
		if indices[i][0] != indices[j][0] {
			return indices[i][0] < indices[j][0]
		}
		return indices[i][1] < indices[j][1]
	})
	merged := [][2]int{indices[0]}
	for i := 1; i < len(indices); i++ {
		last := &merged[len(merged)-1]
		curr := indices[i]
		if curr[0] <= last[1]+1 {
			if curr[1] > last[1] {
				last[1] = curr[1]
			}
		} else {
			merged = append(merged, curr)
		}
	}
	return merged
}
