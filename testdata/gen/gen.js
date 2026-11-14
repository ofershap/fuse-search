// Golden fixture generator for fuse-search.
// Runs the pinned reference fuse.js over a fixed corpus + query matrix and
// writes goldens.json. Reproduce: npm ci && node gen.js
import Fuse from 'fuse.js'
import fs from 'fs'

// Deterministic PRNG (mulberry32) so the corpus is reproducible.
function mulberry32(a) {
  return function () {
    a |= 0; a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}
const rnd = mulberry32(123456789)
const pick = (arr) => arr[Math.floor(rnd() * arr.length)]

const words = ['old', "man's", 'war', 'lock', 'history', 'love', 'code', 'rust', 'go', 'search', 'fuzzy', 'engine', 'data', 'book', 'guide', 'résumé', 'café', 'naïve', 'שלום', 'עולם', '東京', '日本', 'emoji', '🦄', 'rocket', '🚀', 'sigma', 'Σίσυφος', 'İstanbul', 'æther', 'œuvre', 'ß', 'straße']
const first = ['John', 'Jane', 'Bob', 'Alice', 'Carl', 'Dana', 'Eli', 'Fay', 'Gus', 'Hana', 'Émile', 'Ólafur']
const last = ['Smith', 'Doe', 'Brown', 'Lee', 'Katz', 'Reyes', 'Novak', 'Adler', 'Cohen', 'Weiss', 'Müller', 'Sørensen']

// ── doc sets ─────────────────────────────────────────────────────
const stringDocs = []
for (let i = 0; i < 60; i++) {
  stringDocs.push(`${pick(words)} ${pick(words)} ${pick(words)} ${i}`)
}
stringDocs.push('', '   ', 'SingleWord', 'a', 'ab', '🦄🦄🦄', 'İSTANBUL', 'İstanbul Constantinople', 'The Öre of Æsop', 'Straße StraSSe', 'Σίσυφος ΣΊΣΥΦΟΣ', 'hello-world_test.file')

const objectDocs = []
for (let i = 0; i < 40; i++) {
  objectDocs.push({
    title: `${pick(words)} ${pick(words)} ${i}`,
    author: { firstName: pick(first), lastName: pick(last) },
    tags: [pick(words), pick(words)],
    year: 1950 + Math.floor(rnd() * 75),
    rating: Math.round(rnd() * 50) / 10,
    blurb: `${pick(words)} ${pick(words)} ${pick(words)} ${pick(words)}`
  })
}
objectDocs.push({ title: 'No author book', tags: [], year: 2001 })
objectDocs.push({ title: 'Emoji tale 🦄', author: { firstName: 'Émile', lastName: 'Müller' }, tags: ['🦄', 'fairy'], year: 2020, nested: { deep: { deeper: 'treasure' } } })
objectDocs.push({ title: 'Multi array', author: { aliases: ['al one', 'al two', 'ally'] }, tags: ['x'] })

const booksKeys = ['title', { name: 'author.lastName', weight: 2 }, 'tags']
const weightedKeys = [{ name: 'title', weight: 0.7 }, { name: 'author.lastName', weight: 0.3 }]
const deepKey = [['nested', 'deep', 'deeper']]

// ── cases ────────────────────────────────────────────────────────
const cases = []
const add = (name, docs, options, query, searchOptions) => {
  cases.push({ name, docs, options: options || {}, query, searchOptions: searchOptions || undefined })
}

// string-list fuzzy
for (const q of ['old man', 'war', 'zzzqqx', 'go', 'fuz', 'SEARCH', 'resume', 'résumé', 'שלם', 'שלום', '東', '🦄', 'unicorn', 'istanbul', 'İstanbul', 'strasse', 'straße', 'sigma', 'a', 'ab', '']) {
  add(`str-default-${JSON.stringify(q)}`, stringDocs, {}, q)
  add(`str-score-${JSON.stringify(q)}`, stringDocs, { includeScore: true }, q)
}
add('str-matches', stringDocs, { includeMatches: true, includeScore: true }, 'old man')
add('str-matches-emoji', stringDocs, { includeMatches: true, includeScore: true }, '🦄')
add('str-matches-min', stringDocs, { includeMatches: true, minMatchCharLength: 3, includeScore: true }, 'war')
add('str-case', stringDocs, { isCaseSensitive: true, includeScore: true }, 'SEARCH')
add('str-case2', stringDocs, { isCaseSensitive: true, includeScore: true }, 'search')
add('str-diac', stringDocs, { ignoreDiacritics: true, includeScore: true }, 'resume')
add('str-diac2', stringDocs, { ignoreDiacritics: true, includeScore: true }, 'cafe')
add('str-diac-case', stringDocs, { ignoreDiacritics: true, isCaseSensitive: true, includeScore: true }, 'Résumé')
add('str-thresh0', stringDocs, { threshold: 0, includeScore: true }, 'war')
add('str-thresh1', stringDocs, { threshold: 1, includeScore: true }, 'war')
add('str-loc', stringDocs, { location: 20, distance: 10, threshold: 0.8, includeScore: true }, 'old')
add('str-dist0', stringDocs, { distance: 0, includeScore: true }, 'old man')
add('str-ignloc', stringDocs, { ignoreLocation: true, includeScore: true }, 'old man')
add('str-findall', stringDocs, { findAllMatches: true, includeMatches: true, includeScore: true }, 'o')
add('str-limit', stringDocs, { includeScore: true }, 'o', { limit: 5 })
add('str-limit-heap-nosort', stringDocs, { includeScore: true, shouldSort: false }, 'o', { limit: 5 })
add('str-long-pattern', stringDocs, { includeScore: true }, 'old man war lock history love code rust go search fuzzy')
add('str-long-pattern-exact', ['aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'], { includeMatches: true, includeScore: true }, 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
add('str-33', ['aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'], { includeScore: true, includeMatches: true }, 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
add('str-fieldnorm0', stringDocs, { fieldNormWeight: 0, includeScore: true }, 'war')
add('str-fieldnorm2', stringDocs, { fieldNormWeight: 2, includeScore: true }, 'war')
add('str-ignfn', stringDocs, { ignoreFieldNorm: true, includeScore: true }, 'war')

// object-list fuzzy
add('obj-basic', objectDocs, { keys: booksKeys, includeScore: true }, 'old man')
add('obj-basic2', objectDocs, { keys: booksKeys, includeScore: true }, 'smith')
add('obj-matches', objectDocs, { keys: booksKeys, includeScore: true, includeMatches: true }, 'old man')
add('obj-weighted', objectDocs, { keys: weightedKeys, includeScore: true, includeMatches: true }, 'müller')
add('obj-weighted2', objectDocs, { keys: weightedKeys, includeScore: true }, 'old')
add('obj-deep', objectDocs, { keys: deepKey, includeScore: true, includeMatches: true }, 'treasure')
add('obj-tags', objectDocs, { keys: ['tags'], includeScore: true, includeMatches: true }, 'rust')
add('obj-alias', objectDocs, { keys: ['author.aliases'], includeScore: true, includeMatches: true }, 'ally')
add('obj-year', objectDocs, { keys: ['year'], includeScore: true }, '2001')
add('obj-nosort', objectDocs, { keys: booksKeys, includeScore: true, shouldSort: false }, 'old')
add('obj-limit', objectDocs, { keys: booksKeys, includeScore: true }, 'o', { limit: 3 })
add('obj-empty', objectDocs, { keys: booksKeys, includeScore: true, includeMatches: true }, '')
add('obj-diac', objectDocs, { keys: booksKeys, ignoreDiacritics: true, includeScore: true }, 'muller')
add('obj-emoji-key', objectDocs, { keys: booksKeys, includeScore: true, includeMatches: true }, '🦄')

// extended search
const extQueries = ['=war', "'old", '^old', '!old', 'old$', '!^old', '!war$', '^"old man"', '="emoji tale 🦄"', '^core go$', 'war | rust', '^old war$', 'war | ^rust | py$', "!syrup", 'old !war', 'jscope', '="said \\"test\\""', '!^"hello world"']
for (const q of extQueries) {
  add(`ext-${JSON.stringify(q)}`, stringDocs, { useExtendedSearch: true, includeScore: true, includeMatches: true }, q)
}
add('ext-obj', objectDocs, { useExtendedSearch: true, keys: booksKeys, includeScore: true, includeMatches: true }, '^old !war')
add('ext-obj2', objectDocs, { useExtendedSearch: true, keys: booksKeys, includeScore: true }, '!syrup')
add('ext-obj3', objectDocs, { useExtendedSearch: true, keys: booksKeys, includeScore: true }, '=old')
add('ext-escaped-pipe', ['a|b', 'ab', 'a b'], { useExtendedSearch: true, includeScore: true, includeMatches: true }, 'a\\|b')
add('ext-thresh', stringDocs, { useExtendedSearch: true, threshold: 0.3, includeScore: true, includeMatches: true }, 'wr')

// logical search
add('log-and', objectDocs, { keys: booksKeys, includeScore: true, includeMatches: true }, { $and: [{ title: 'old' }, { 'author.lastName': 'smith' }] })
add('log-or', objectDocs, { keys: booksKeys, includeScore: true }, { $or: [{ title: 'old' }, { 'author.lastName': 'smith' }] })
add('log-keyless', objectDocs, { keys: booksKeys, includeScore: true }, { $and: ['old', 'smith'] })
add('log-nested', objectDocs, { keys: booksKeys, includeScore: true }, { $and: [{ $or: [{ title: 'old' }, { title: 'war' }] }, { tags: 'rust' }] })
add('log-path', objectDocs, { keys: booksKeys, includeScore: true }, { $and: [{ $path: ['author', 'lastName'], $val: 'müller' }] })
add('log-multikey', objectDocs, { keys: booksKeys, includeScore: true }, { title: 'old', 'author.lastName': 'smith' })

// token search
add('tok-any', stringDocs, { useTokenSearch: true, includeScore: true }, 'old war')
add('tok-all', stringDocs, { useTokenSearch: true, tokenMatch: 'all', includeScore: true }, 'old war')
add('tok-obj', objectDocs, { useTokenSearch: true, keys: booksKeys, includeScore: true, includeMatches: true }, 'old smith')
add('tok-obj-all', objectDocs, { useTokenSearch: true, tokenMatch: 'all', keys: booksKeys, includeScore: true }, 'old smith')
add('tok-diac', stringDocs, { useTokenSearch: true, ignoreDiacritics: true, includeScore: true }, 'resume cafe')
add('tok-limit', stringDocs, { useTokenSearch: true, includeScore: true }, 'o', { limit: 4 })

// mutation sequences: ops applied in order, then search
const mutCases = []
mutCases.push({
  name: 'mut-add-remove',
  docs: ['alpha one', 'beta two', 'gamma three'],
  options: { includeScore: true },
  ops: [
    { op: 'add', doc: 'delta four' },
    { op: 'search', query: 'delta' },
    { op: 'add', doc: '' },
    { op: 'search', query: 'four' },
    { op: 'removeAt', idx: 1 },
    { op: 'search', query: 'beta' },
    { op: 'search', query: 'alpha' },
    { op: 'remove', predFirst: 'alpha one' },
    { op: 'search', query: 'alpha' }
  ]
})
mutCases.push({
  name: 'mut-add-remove-obj',
  docs: [{ t: 'red apple' }, { t: 'green pear' }, { t: 'blue berry' }],
  options: { keys: ['t'], includeScore: true },
  ops: [
    { op: 'add', doc: { t: 'yellow banana' } },
    { op: 'search', query: 'banana' },
    { op: 'removeAt', idx: 0 },
    { op: 'search', query: 'apple' },
    { op: 'search', query: 'pear' }
  ]
})
mutCases.push({
  name: 'mut-token',
  docs: ['alpha one', 'beta two', 'gamma three'],
  options: { useTokenSearch: true, includeScore: true },
  ops: [
    { op: 'add', doc: 'delta four' },
    { op: 'search', query: 'alpha delta' },
    { op: 'removeAt', idx: 0 },
    { op: 'search', query: 'alpha delta' }
  ]
})

// index roundtrip: createIndex -> serialize -> parseIndex -> search
const idx = Fuse.createIndex(booksKeys, objectDocs)
const roundtrip = { name: 'index-roundtrip', serialized: JSON.parse(JSON.stringify(idx)), query: 'old man', options: { keys: booksKeys, includeScore: true, includeMatches: true } }

// ── run ──────────────────────────────────────────────────────────
function serialize(results) {
  return JSON.parse(JSON.stringify(results))
}

const out = { fuseVersion: '7.5.0', cases: [], mutations: [], roundtrip: null }

for (const c of cases) {
  const fuse = new Fuse(c.docs, c.options)
  const res = fuse.search(c.query, c.searchOptions)
  out.cases.push({ name: c.name, docs: c.docs, options: c.options, query: c.query, searchOptions: c.searchOptions, expected: serialize(res) })
}

for (const m of mutCases) {
  const fuse = new Fuse(m.docs.map(d => (d && typeof d === 'object') ? { ...d } : d), m.options)
  const expectedOps = []
  for (const op of m.ops) {
    if (op.op === 'add') {
      fuse.add(op.doc)
      expectedOps.push(null)
    } else if (op.op === 'removeAt') {
      fuse.removeAt(op.idx)
      expectedOps.push(null)
    } else if (op.op === 'remove') {
      const first = op.predFirst
      const removed = fuse.remove((doc) => doc === first)
      expectedOps.push({ removed: serialize(removed) })
    } else if (op.op === 'search') {
      expectedOps.push({ results: serialize(fuse.search(op.query)) })
    }
  }
  out.mutations.push({ name: m.name, docs: m.docs, options: m.options, ops: m.ops, expectedOps })
}

// roundtrip expected
{
  const fuse = new Fuse(objectDocs, roundtrip.options, idx)
  roundtrip.expected = serialize(fuse.search(roundtrip.query))
  out.roundtrip = roundtrip
}

fs.writeFileSync(new URL('../goldens.json', import.meta.url), JSON.stringify(out, null, 1))
console.log('cases:', out.cases.length, 'mutations:', out.mutations.length)

// ── numeric format boundary probe ────────────────────────────────
// JS String(number) output for boundary magnitudes; the Go test compares
// its key-extraction formatting against these strings exactly.
const numericFormatProbe = [
  1e-8, 1.5e-8, 5e-7, 1e-7, 9.99999e-7, 1e-6, 0.000001, 0.1 + 0.2, 3.14,
  1e20, 123456789012345680000, 1e21, 2.5e21, 1e22, -1e-8, -5e-7, 0, 100,
]
fs.writeFileSync(
  new URL('./numeric_format.json', import.meta.url),
  JSON.stringify(numericFormatProbe.map((v) => String(v)), null, 2)
)

// ── index interchange fixture (JS -> Go direction) ───────────────
// An index built entirely by fuse.js, serialized with toJSON(), plus the
// search results fuse.js produces when searching THROUGH that parsed index.
// The Go interchange test parses the same JSON and must reproduce results.
const ixDocs = [
  { title: 'Old Man\'s War', author: { lastName: 'Scalzi' }, tags: ['sci-fi', 'war'] },
  { title: 'The Lock Artist', author: { lastName: 'Hamilton' }, tags: ['crime'] },
  { title: 'Résumé of a Unicorn 🦄', author: { lastName: 'Müller' }, tags: ['fairy', 'emoji'] },
  { title: 'İstanbul Tales', author: { lastName: 'Sørensen' }, tags: ['travel'] },
]
const ixKeys = ['title', { name: 'author.lastName', weight: 2 }, 'tags']
const ixOptions = { includeScore: true, includeMatches: true }
const jsIndex = Fuse.createIndex(ixKeys, ixDocs)
const jsIndexJSON = jsIndex.toJSON()
const parsedFuse = new Fuse(ixDocs, ixOptions, Fuse.parseIndex(JSON.parse(JSON.stringify(jsIndexJSON)), ixOptions))
const ixQueries = ['old man', 'unicorn', "'lock", 'scazli', 'emoji 🦄']
fs.writeFileSync(
  new URL('../index_from_js.json', import.meta.url),
  JSON.stringify({
    docs: ixDocs,
    keys: ixKeys,
    options: ixOptions,
    index: jsIndexJSON,
    results: ixQueries.map((q, i) => ({ id: `ix${i}`, query: q, results: serialize(parsedFuse.search(q)) })),
  }, null, 2)
)

// ── extended-search edge patterns (empty-capture tokens) ─────────
// Patterns like "'" hit fuse.js's `if (token)` truthiness: an empty regex
// capture is skipped and the token falls through to a later matcher (usually
// the fuzzy catch-all). These cases pin that behavior differentially.
const edgeDocs = ['old man', "it's here", 'say "hi"', 'a=b', 'caret^', 'nope!', '']
const edgePatterns = ["'", '=', '""', '^', '!', '!^', "'a", '=a=b', '\|', 'a\|b']
const edgeOptions = { useExtendedSearch: true, includeScore: true, includeMatches: true }
fs.writeFileSync(
  new URL('../extended_edge.json', import.meta.url),
  JSON.stringify({
    docs: edgeDocs,
    options: edgeOptions,
    cases: edgePatterns.map((q, i) => ({ id: `edge${i}`, query: q, expected: serialize(new Fuse(edgeDocs, edgeOptions).search(q)) })),
  }, null, 2)
)
console.log('edge + interchange + numeric fixtures written')
