package fuse

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// ── field norm ────────────────────────────────────────────────────

func isWordSeparator(code uint16) bool {
	return (code >= 9 && code <= 13) || code == 32 || code == 160
}

// fieldNorm mirrors fuse.js norm(): 1/sqrt(wordCount)^weight, rounded to
// `mantissa` decimals, cached per word count. Word counting follows the
// reference: tally word-starts over a narrow separator set (ASCII whitespace
// plus NBSP); empty/whitespace-only strings count as one token.
type fieldNorm struct {
	weight float64
	m      float64
	cache  map[int]float64
}

func newFieldNorm(weight float64, mantissa int) *fieldNorm {
	return &fieldNorm{weight: weight, m: math.Pow(10, float64(mantissa)), cache: map[int]float64{}}
}

func (n *fieldNorm) get(value string) float64 {
	u := toU16(value)
	numTokens := 0
	inWord := false
	for _, c := range u {
		if !isWordSeparator(c) {
			if !inWord {
				numTokens++
				inWord = true
			}
		} else {
			inWord = false
		}
	}
	if numTokens == 0 {
		numTokens = 1
	}
	if v, ok := n.cache[numTokens]; ok {
		return v
	}
	v := math.Round(n.m/jsPow(float64(numTokens), 0.5*n.weight)) / n.m
	n.cache[numTokens] = v
	return v
}

func (n *fieldNorm) clear() { n.cache = map[int]float64{} }

// ── keys ──────────────────────────────────────────────────────────

// Key is the object form of the fuse.js keys option.
type Key struct {
	// Name is a dotted path ("author.lastName") or a path slice.
	Name any
	// Weight must be > 0 when set. Zero means the default weight of 1.
	Weight float64
	// GetFn overrides value extraction for this key.
	GetFn func(obj any) any
}

type keyObject struct {
	path   []string
	id     string
	weight float64
	src    any
	getFn  func(obj any) any
}

func createKeyPath(key any) []string {
	switch k := key.(type) {
	case string:
		return strings.Split(k, ".")
	case []string:
		return append([]string(nil), k...)
	default:
		return nil
	}
}

func createKeyID(key any) string {
	switch k := key.(type) {
	case string:
		return k
	case []string:
		return strings.Join(k, ".")
	default:
		return ""
	}
}

func createKey(key any) (keyObject, error) {
	obj := keyObject{weight: 1}
	switch k := key.(type) {
	case string, []string:
		obj.src = key
		obj.path = createKeyPath(key)
		obj.id = createKeyID(key)
	case Key:
		if k.Name == nil {
			return obj, errors.New("Missing name property in key")
		}
		obj.src = k.Name
		weight := 1.0
		if k.Weight != 0 {
			weight = k.Weight
			if weight <= 0 {
				return obj, fmt.Errorf("Property 'weight' in key '%s' must be a positive integer", createKeyID(k.Name))
			}
		}
		obj.weight = weight
		obj.path = createKeyPath(k.Name)
		obj.id = createKeyID(k.Name)
		obj.getFn = k.GetFn
	case *Key:
		if k == nil {
			return obj, errors.New("Missing name property in key")
		}
		return createKey(*k)
	default:
		return obj, errors.New("Missing name property in key")
	}
	return obj, nil
}

type keyStore struct {
	keys   []keyObject
	keyMap map[string]keyObject
}

func newKeyStore(keys []any) (*keyStore, error) {
	ks := &keyStore{keyMap: map[string]keyObject{}}
	totalWeight := 0.0
	for _, key := range keys {
		obj, err := createKey(key)
		if err != nil {
			return nil, err
		}
		ks.keys = append(ks.keys, obj)
		ks.keyMap[obj.id] = obj
		totalWeight += obj.weight
	}
	for i := range ks.keys {
		ks.keys[i].weight /= totalWeight
		ks.keyMap[ks.keys[i].id] = ks.keys[i]
	}
	return ks, nil
}

func (ks *keyStore) get(keyID string) (keyObject, bool) {
	k, ok := ks.keyMap[keyID]
	return k, ok
}

// ── get() ─────────────────────────────────────────────────────────

// jsToString mirrors the fuse.js toString(): numbers render JS-style.
func jsToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(v)
	case int8, int16, int32, int64:
		return strconv.FormatInt(reflect.ValueOf(v).Int(), 10)
	case uint, uint8, uint16, uint32, uint64:
		return strconv.FormatUint(reflect.ValueOf(v).Uint(), 10)
	case float32:
		return jsFormatFloat(float64(v))
	case float64:
		return jsFormatFloat(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// jsFormatFloat renders a float the way JS String(number) does for the
// magnitudes typical in indexed data (no exponent notation in [1e-7, 1e21)).
func jsFormatFloat(f float64) string {
	if f == 0 {
		if math.Signbit(f) {
			return "-0"
		}
		return "0"
	}
	abs := math.Abs(f)
	if abs >= 1e-7 && abs < 1e21 {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		return s
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	// JS exponent style: 1e+21, 1.5e-7 (Go prints e+21 / e-07)
	parts := strings.SplitN(s, "e", 2)
	exp := parts[1]
	sign := ""
	if strings.HasPrefix(exp, "+") || strings.HasPrefix(exp, "-") {
		sign = exp[:1]
		exp = exp[1:]
	}
	exp = strings.TrimLeft(exp, "0")
	if exp == "" {
		exp = "0"
	}
	if sign == "" {
		sign = "+"
	}
	return parts[0] + "e" + sign + exp
}

func isScalarValue(v any) bool {
	switch v.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	}
	return false
}

type getEntry struct {
	v any
	i int
}

// getValue ports helpers/get.ts: dotted-path lookup over maps and structs.
// When a path segment traverses an array, every element is visited and the
// result is a []getEntry list of {v, i} with original array indices.
func getValue(obj any, path []string) any {
	var list []any
	arr := false

	var deepGet func(obj any, index int, arrayIndex int, hasArrayIndex bool)
	deepGet = func(obj any, index int, arrayIndex int, hasArrayIndex bool) {
		if obj == nil {
			return
		}
		if index >= len(path) || path[index] == "" {
			if hasArrayIndex {
				list = append(list, getEntry{v: obj, i: arrayIndex})
			} else {
				list = append(list, obj)
			}
			return
		}
		key := path[index]
		value := lookupMember(obj, key)
		if value == nil {
			return
		}
		rv := reflect.ValueOf(value)
		if index == len(path)-1 && isScalarValue(value) {
			if hasArrayIndex {
				list = append(list, getEntry{v: jsToString(value), i: arrayIndex})
			} else {
				list = append(list, jsToString(value))
			}
		} else if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			arr = true
			for i := 0; i < rv.Len(); i++ {
				deepGet(rv.Index(i).Interface(), index+1, i, true)
			}
		} else if len(path) > 0 {
			deepGet(value, index+1, arrayIndex, hasArrayIndex)
		}
	}
	deepGet(obj, 0, 0, false)

	if arr {
		return list
	}
	if len(list) == 0 {
		return nil
	}
	return list[0]
}

// lookupMember resolves one path segment against a Go value: map keys, struct
// fields (name or json tag), or pointer indirection.
func lookupMember(obj any, key string) any {
	for {
		if obj == nil {
			return nil
		}
		rv := reflect.ValueOf(obj)
		if rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
			if rv.IsNil() {
				return nil
			}
			obj = rv.Elem().Interface()
			continue
		}
		switch rv.Kind() {
		case reflect.Map:
			kv := reflect.ValueOf(key)
			if kv.Type().AssignableTo(rv.Type().Key()) {
				mv := rv.MapIndex(kv)
				if mv.IsValid() {
					return mv.Interface()
				}
			}
			return nil
		case reflect.Struct:
			t := rv.Type()
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				if f.PkgPath != "" {
					continue
				}
				if f.Name == key || jsonTagName(f.Tag.Get("json")) == key {
					return rv.Field(i).Interface()
				}
			}
			return nil
		}
		return nil
	}
}

func jsonTagName(tag string) string {
	if idx := strings.IndexByte(tag, ','); idx >= 0 {
		tag = tag[:idx]
	}
	return tag
}

// ── index records ─────────────────────────────────────────────────

type subRecord struct {
	V    any     `json:"v"`
	I    int     `json:"i,omitempty"`
	N    float64 `json:"n"`
	hasI bool
}

// indexRecord: {v,i,n} for string docs, {i,$} for object docs.
type indexRecord struct {
	hasValue bool
	v        string
	i        int
	n        float64
	fields   map[int]any // keyIndex -> subRecord or []subRecord
}

func isBlankString(s string) bool {
	return strings.TrimSpace(s) == ""
}

// FuseIndex ports tools/FuseIndex.ts.
type FuseIndex struct {
	norm      *fieldNorm
	getFn     func(obj any, path []string) any
	isCreated bool
	docs      []any
	Records   []indexRecord
	Keys      []keyObject
	keysMap   map[string]int
}

// IndexOptions customizes CreateIndex / ParseIndex.
type IndexOptions struct {
	GetFn           func(obj any, path []string) any
	FieldNormWeight *float64
}

func newFuseIndex(opts *IndexOptions) *FuseIndex {
	fnw := 1.0
	getFn := getValue
	if opts != nil {
		if opts.FieldNormWeight != nil {
			fnw = *opts.FieldNormWeight
		}
		if opts.GetFn != nil {
			getFn = opts.GetFn
		}
	}
	return &FuseIndex{
		norm:    newFieldNorm(fnw, 3),
		getFn:   getFn,
		keysMap: map[string]int{},
	}
}

func (ix *FuseIndex) setKeys(keys []keyObject) {
	ix.Keys = keys
	ix.keysMap = map[string]int{}
	for idx, key := range keys {
		ix.keysMap[key.id] = idx
	}
}

func (ix *FuseIndex) create() {
	if ix.isCreated || len(ix.docs) == 0 {
		return
	}
	ix.isCreated = true
	records := make([]indexRecord, 0, len(ix.docs))
	if _, ok := ix.docs[0].(string); ok {
		for i, doc := range ix.docs {
			s, _ := doc.(string)
			if rec, ok := ix.createStringRecord(s, i); ok {
				records = append(records, rec)
			}
		}
	} else {
		for i, doc := range ix.docs {
			records = append(records, ix.createObjectRecord(doc, i))
		}
	}
	ix.Records = records
	ix.norm.clear()
}

func (ix *FuseIndex) add(doc any, docIndex int) (*indexRecord, error) {
	if docIndex < 0 {
		return nil, errors.New("Invalid doc index: must be a non-negative integer within the bounds of the docs array")
	}
	if s, ok := doc.(string); ok {
		rec, ok := ix.createStringRecord(s, docIndex)
		if !ok {
			return nil, nil
		}
		ix.Records = append(ix.Records, rec)
		return &ix.Records[len(ix.Records)-1], nil
	}
	rec := ix.createObjectRecord(doc, docIndex)
	ix.Records = append(ix.Records, rec)
	return &ix.Records[len(ix.Records)-1], nil
}

func (ix *FuseIndex) removeAt(idx int) error {
	if idx < 0 {
		return errors.New("Invalid doc index: must be a non-negative integer within the bounds of the docs array")
	}
	for i := 0; i < len(ix.Records); i++ {
		if ix.Records[i].i == idx {
			ix.Records = append(ix.Records[:i], ix.Records[i+1:]...)
			break
		}
	}
	for i := range ix.Records {
		if ix.Records[i].i > idx {
			ix.Records[i].i--
		}
	}
	return nil
}

func (ix *FuseIndex) removeAll(indices []int) {
	toRemove := map[int]struct{}{}
	for _, v := range indices {
		if v >= 0 {
			toRemove[v] = struct{}{}
		}
	}
	if len(toRemove) == 0 {
		return
	}
	kept := ix.Records[:0]
	for _, r := range ix.Records {
		if _, rm := toRemove[r.i]; !rm {
			kept = append(kept, r)
		}
	}
	ix.Records = kept
	sorted := dedupSortInts(indices)
	for i := range ix.Records {
		lo, hi := 0, len(sorted)
		for lo < hi {
			mid := (lo + hi) / 2
			if sorted[mid] < ix.Records[i].i {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		ix.Records[i].i -= lo
	}
}

func (ix *FuseIndex) getValueForItemAtKeyID(item any, keyID string) any {
	fields, ok := item.(map[int]any)
	if !ok {
		return nil
	}
	return fields[ix.keysMap[keyID]]
}

func (ix *FuseIndex) createStringRecord(doc string, docIndex int) (indexRecord, bool) {
	if isBlankString(doc) {
		return indexRecord{}, false
	}
	return indexRecord{hasValue: true, v: doc, i: docIndex, n: ix.norm.get(doc)}, true
}

func (ix *FuseIndex) createObjectRecord(doc any, docIndex int) indexRecord {
	record := indexRecord{i: docIndex, fields: map[int]any{}}
	for keyIndex, key := range ix.Keys {
		var value any
		if key.getFn != nil {
			value = key.getFn(doc)
		} else {
			value = ix.getFn(doc, key.path)
		}
		if value == nil {
			continue
		}
		switch v := value.(type) {
		case []any:
			subRecords := []subRecord{}
			for i, item := range v {
				if item == nil {
					continue
				}
				switch it := item.(type) {
				case string:
					if !isBlankString(it) {
						subRecords = append(subRecords, subRecord{V: it, I: i, N: ix.norm.get(it), hasI: true})
					}
				case getEntry:
					text := jsToString(it.v)
					if !isBlankString(text) {
						subRecords = append(subRecords, subRecord{V: text, I: it.i, N: ix.norm.get(text), hasI: true})
					}
				default:
					if isScalarValue(it) {
						text := jsToString(it)
						if !isBlankString(text) {
							subRecords = append(subRecords, subRecord{V: text, I: i, N: ix.norm.get(text), hasI: true})
						}
					}
				}
			}
			record.fields[keyIndex] = subRecords
		case string:
			if !isBlankString(v) {
				record.fields[keyIndex] = subRecord{V: v, N: ix.norm.get(v)}
			}
		default:
			if isScalarValue(v) {
				text := jsToString(v)
				if !isBlankString(text) {
					record.fields[keyIndex] = subRecord{V: text, N: ix.norm.get(text)}
				}
			}
		}
	}
	return record
}

// CreateIndex builds a searchable index for docs (Fuse.createIndex).
func CreateIndex(keys []any, docs []any, opts *IndexOptions) (*FuseIndex, error) {
	myIndex := newFuseIndex(opts)
	created := make([]keyObject, 0, len(keys))
	for _, k := range keys {
		ko, err := createKey(k)
		if err != nil {
			return nil, err
		}
		created = append(created, ko)
	}
	myIndex.setKeys(created)
	myIndex.docs = docs
	myIndex.create()
	return myIndex, nil
}

// serializedIndex is the JSON shape of a Fuse index (FuseIndex.toJSON()).
type serializedIndex struct {
	Keys    []map[string]any `json:"keys"`
	Records []any            `json:"records"`
}

// MarshalIndex produces the JSON shape of FuseIndex.toJSON().
func (ix *FuseIndex) MarshalIndex() ([]byte, error) {
	keys := make([]map[string]any, 0, len(ix.Keys))
	for _, k := range ix.Keys {
		keys = append(keys, map[string]any{
			"path": k.path, "id": k.id, "weight": k.weight, "src": k.src,
		})
	}
	records := make([]any, 0, len(ix.Records))
	for _, r := range ix.Records {
		records = append(records, r.toJSONValue())
	}
	return jsonMarshal(serializedIndex{Keys: keys, Records: records})
}

func (r indexRecord) toJSONValue() any {
	if r.hasValue {
		return map[string]any{"v": r.v, "i": r.i, "n": r.n}
	}
	fields := map[string]any{}
	for k, v := range r.fields {
		fields[strconv.Itoa(k)] = subRecordJSON(v)
	}
	return map[string]any{"i": r.i, "$": fields}
}

func subRecordJSON(v any) any {
	switch s := v.(type) {
	case subRecord:
		m := map[string]any{"v": s.V, "n": s.N}
		if s.hasI {
			m["i"] = s.I
		}
		return m
	case []subRecord:
		out := make([]any, 0, len(s))
		for _, e := range s {
			out = append(out, subRecordJSON(e))
		}
		return out
	}
	return nil
}

// ParseIndex restores a previously serialized index (Fuse.parseIndex).
// `data` must decode from the JSON shape produced by MarshalIndex.
func ParseIndex(data []byte, opts *IndexOptions) (*FuseIndex, error) {
	var raw struct {
		Keys    []map[string]any  `json:"keys"`
		Records []json.RawMessage `json:"records"`
	}
	if err := jsonUnmarshal(data, &raw); err != nil {
		return nil, err
	}
	myIndex := newFuseIndex(opts)
	keys := make([]keyObject, 0, len(raw.Keys))
	for _, k := range raw.Keys {
		ko := keyObject{weight: 1}
		if p, ok := k["path"].([]any); ok {
			for _, e := range p {
				if s, ok := e.(string); ok {
					ko.path = append(ko.path, s)
				}
			}
		}
		if id, ok := k["id"].(string); ok {
			ko.id = id
		}
		if w, ok := k["weight"].(float64); ok {
			ko.weight = w
		}
		ko.src = k["src"]
		keys = append(keys, ko)
	}
	myIndex.setKeys(keys)
	for _, rr := range raw.Records {
		rec, err := parseRecordJSON(rr)
		if err != nil {
			return nil, err
		}
		myIndex.Records = append(myIndex.Records, rec)
	}
	myIndex.isCreated = true
	return myIndex, nil
}

func parseRecordJSON(rr json.RawMessage) (indexRecord, error) {
	var probe map[string]json.RawMessage
	if err := jsonUnmarshal(rr, &probe); err != nil {
		return indexRecord{}, err
	}
	var rec indexRecord
	if vraw, ok := probe["v"]; ok {
		var v string
		if err := jsonUnmarshal(vraw, &v); err != nil {
			return indexRecord{}, err
		}
		rec.hasValue = true
		rec.v = v
		rec.i = jsonInt(probe["i"])
		rec.n = jsonFloat(probe["n"])
		return rec, nil
	}
	rec.i = jsonInt(probe["i"])
	rec.fields = map[int]any{}
	if fraw, ok := probe["$"]; ok {
		var fmap map[string]json.RawMessage
		if err := jsonUnmarshal(fraw, &fmap); err != nil {
			return indexRecord{}, err
		}
		for ks, vr := range fmap {
			ki, _ := strconv.Atoi(ks)
			var arrProbe []json.RawMessage
			if err := jsonUnmarshal(vr, &arrProbe); err == nil {
				var subs []subRecord
				for _, er := range arrProbe {
					sub, err := parseSubRecordJSON(er)
					if err != nil {
						return indexRecord{}, err
					}
					subs = append(subs, sub)
				}
				rec.fields[ki] = subs
			} else {
				sub, err := parseSubRecordJSON(vr)
				if err != nil {
					return indexRecord{}, err
				}
				rec.fields[ki] = sub
			}
		}
	}
	return rec, nil
}

func parseSubRecordJSON(rr json.RawMessage) (subRecord, error) {
	var m map[string]json.RawMessage
	if err := jsonUnmarshal(rr, &m); err != nil {
		return subRecord{}, err
	}
	var sub subRecord
	if v, ok := m["v"]; ok {
		var s string
		if err := jsonUnmarshal(v, &s); err == nil {
			sub.V = s
		}
	}
	sub.N = jsonFloat(m["n"])
	if iraw, ok := m["i"]; ok {
		sub.I = jsonInt(iraw)
		sub.hasI = true
	}
	return sub, nil
}

func jsonInt(rr json.RawMessage) int {
	var f float64
	_ = jsonUnmarshal(rr, &f)
	return int(f)
}

func jsonFloat(rr json.RawMessage) float64 {
	var f float64
	_ = jsonUnmarshal(rr, &f)
	return f
}

// keep sort imported (used by removeAll shift via dedupSortInts)
var _ = sort.Ints
