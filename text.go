package fuse

import (
	"strings"
	"unicode"
	"unicode/utf16"
)

// fuse.js operates on JavaScript strings: sequences of UTF-16 code units.
// Match indices, pattern lengths, MAX_BITS chunking, and location arithmetic
// are all expressed in code units. To stay byte-exact with the reference
// engine, fuse-search performs every search on UTF-16 code units as well.
// Conversion happens once at the public boundary.

func toU16(s string) []uint16 {
	return utf16.Encode([]rune(s))
}

func fromU16(u []uint16) string {
	return string(utf16.Decode(u))
}

// jsLowercaseString mirrors JS String.prototype.toLowerCase: full Unicode
// case mapping, which differs from Go's simple per-rune mapping for a small
// set of runes (see jsLowerDelta, generated from V8).
func jsLowercaseString(s string) string {
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range rs {
		if mapped, ok := jsLowerDelta[r]; ok {
			b.WriteString(mapped)
		} else {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// stripDiacriticsString mirrors fuse.js stripDiacritics: NFD normalization,
// combining-mark removal, and an explicit non-decomposable map, folded into a
// single per-rune table generated from the reference implementation.
func stripDiacriticsString(s string) string {
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range rs {
		if mapped, ok := jsStrip[r]; ok {
			b.WriteString(mapped)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// indexOfU16 mirrors String.prototype.indexOf(pattern, fromIndex): first
// occurrence at or after fromIndex, or -1. An empty pattern matches at
// fromIndex (clamped to the text length).
func indexOfU16(text, pattern []uint16, from int) int {
	if from < 0 {
		from = 0
	}
	if len(pattern) == 0 {
		if from > len(text) {
			return len(text)
		}
		return from
	}
	if len(pattern) > len(text)-from {
		return -1
	}
	for i := from; i+len(pattern) <= len(text); i++ {
		j := 0
		for ; j < len(pattern); j++ {
			if text[i+j] != pattern[j] {
				break
			}
		}
		if j == len(pattern) {
			return i
		}
	}
	return -1
}

func hasPrefixU16(text, prefix []uint16) bool {
	if len(prefix) > len(text) {
		return false
	}
	for i := range prefix {
		if text[i] != prefix[i] {
			return false
		}
	}
	return true
}

func hasSuffixU16(text, suffix []uint16) bool {
	if len(suffix) > len(text) {
		return false
	}
	off := len(text) - len(suffix)
	for i := range suffix {
		if text[off+i] != suffix[i] {
			return false
		}
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
