// Direction B live check: load an index marshaled by the Go package
// (testdata/index_from_go.json) into fuse.js via Fuse.parseIndex and search
// through it. Run: node verify_index_from_go.mjs (from testdata/gen).
import Fuse from 'fuse.js'
import fs from 'fs'

const docs = [
  { title: "Old Man's War", author: { lastName: 'Scalzi' }, tags: ['sci-fi', 'war'] },
  { title: 'The Lock Artist', author: { lastName: 'Hamilton' }, tags: ['crime'] },
  { title: 'Résumé of a Unicorn 🦄', author: { lastName: 'Müller' }, tags: ['fairy', 'emoji'] },
  { title: 'İstanbul Tales', author: { lastName: 'Sørensen' }, tags: ['travel'] },
]
const options = { includeScore: true, includeMatches: true }
const goIndex = JSON.parse(fs.readFileSync(new URL('../index_from_go.json', import.meta.url), 'utf8'))
const fuse = new Fuse(docs, options, Fuse.parseIndex(goIndex))
for (const q of ['old man', "'lock", 'scazli', 'emoji 🦄']) {
  console.log(q, '=>', JSON.stringify(fuse.search(q).map((r) => [r.refIndex, r.score])))
}
console.log('OK: fuse.js consumed the Go-marshaled index and searched through it')
