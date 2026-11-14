// Mirror of bench_test.go for fuse.js 7.5.0 (Node). Same corpus, same options,
// same harness: median of 25 timed runs with 3 warmups per search pattern.
// Run: node bench_mirror.mjs  (from testdata/gen, after `npm ci`)
import Fuse from 'fuse.js'

const first = ['John', 'Jane', 'Bob', 'Alice', 'Carl', 'Dana', 'Eli', 'Fay', 'Gus', 'Hana']
const last = ['Smith', 'Doe', 'Brown', 'Lee', 'Katz', 'Reyes', 'Novak', 'Adler', 'Cohen', 'Weiss']
const words = ['old', 'man', 'war', 'lock', 'history', 'love', 'code', 'rust', 'go', 'search', 'fuzzy', 'engine', 'data', 'book', 'guide']

function benchDocs(n) {
  const docs = []
  let seed = 42
  const next = (m) => { seed = (seed * 1664525 + 1013904223) >>> 0; return seed % m }
  for (let i = 0; i < n; i++) {
    docs.push({
      title: `${words[next(15)]} ${words[next(15)]} ${words[next(15)]} ${i}`,
      author: { firstName: first[next(10)], lastName: last[next(10)] },
    })
  }
  return docs
}

const options = {
  keys: [
    { name: 'title', weight: 0.7 },
    { name: 'author.lastName', weight: 0.3 },
  ],
  includeScore: true,
}

function median(times) {
  const s = [...times].sort((a, b) => a - b)
  return s[Math.floor(s.length / 2)]
}

function run(label, fn) {
  const times = []
  for (let i = 0; i < 25; i++) {
    const t0 = performance.now()
    fn()
    times.push(performance.now() - t0)
  }
  console.log(`${label}: ${median(times).toFixed(2)} ms (median of 25)`)
}

const docs = benchDocs(2000)
run('index build', () => new Fuse(docs, options))
const fuse = new Fuse(docs, options)
for (const [label, pat] of [['search hit (old man)', 'old man'], ['search miss (zzzqqx)', 'zzzqqx'], ['search short (go)', 'go']]) {
  for (let i = 0; i < 3; i++) fuse.search(pat)
  run(label, () => fuse.search(pat))
}
