// Package jsjson reads and writes JSON the way JSON.parse and JSON.stringify do,
// so a graph written by the TypeScript weave re-encodes byte for byte.
//
// Objects keep their key order, including JavaScript's rule that integer-like
// keys ("0", "12") come first in ascending order. Numbers keep their lexeme, so
// a number read from a file is written back unchanged; JS-written files only
// contain canonical lexemes, which is where this equals JSON.stringify(JSON.parse(x)).
// Non-canonical input ("1.0", "1E2", integers past 2^53) is preserved, not
// normalized as JS would. Use Num for numbers computed in Go.
package jsjson

import "strconv"

// Kind is the JSON type of a Value.
type Kind uint8

const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

// Value is one JSON value. The zero Value is null.
type Value struct {
	kind Kind
	b    bool
	s    string // string content, or the number's lexeme
	a    []Value
	o    *Object
}

// Null is the JSON null.
func Null() Value { return Value{} }

// Bool wraps a boolean.
func Bool(b bool) Value { return Value{kind: KindBool, b: b} }

// Str wraps a string.
func Str(s string) Value { return Value{kind: KindString, s: s} }

// Num wraps a number computed in Go; it is written as JS would write it.
// NaN and ±Infinity become null, as in JSON.stringify.
func Num(f float64) Value {
	lex := FormatNumber(f)
	if lex == "NaN" || lex == "Infinity" || lex == "-Infinity" {
		return Null()
	}
	return Value{kind: KindNumber, s: lex}
}

// Int wraps an integer.
func Int(n int) Value { return Value{kind: KindNumber, s: strconv.Itoa(n)} }

// Arr wraps an array.
func Arr(vs ...Value) Value {
	if vs == nil {
		vs = []Value{}
	}
	return Value{kind: KindArray, a: vs}
}

// Obj wraps an object; a nil object is written as {}.
func Obj(o *Object) Value {
	if o == nil {
		o = &Object{}
	}
	return Value{kind: KindObject, o: o}
}

// Kind returns the JSON type.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.kind == KindNull }

// Bool returns the boolean, false for any other kind.
func (v Value) Bool() bool { return v.b }

// Str returns the string content, "" for any other kind.
func (v Value) Str() string {
	if v.kind != KindString {
		return ""
	}
	return v.s
}

// Lexeme returns a number's text as read or written.
func (v Value) Lexeme() string {
	if v.kind != KindNumber {
		return ""
	}
	return v.s
}

// Float returns the number's value.
func (v Value) Float() float64 {
	if v.kind != KindNumber {
		return 0
	}
	f, _ := strconv.ParseFloat(v.s, 64)
	return f
}

// Array returns the elements, nil for any other kind.
func (v Value) Array() []Value { return v.a }

// Object returns the object, nil for any other kind.
func (v Value) Object() *Object { return v.o }

// Equal reports deep equality, keys compared in order.
func (v Value) Equal(w Value) bool {
	if v.kind != w.kind || v.b != w.b || v.s != w.s || len(v.a) != len(w.a) {
		return false
	}
	for i := range v.a {
		if !v.a[i].Equal(w.a[i]) {
			return false
		}
	}
	if v.kind == KindObject {
		if v.o.Len() != w.o.Len() {
			return false
		}
		for i := range v.o.keys {
			if v.o.keys[i] != w.o.keys[i] || !v.o.vals[i].Equal(w.o.vals[i]) {
				return false
			}
		}
	}
	return true
}

// Object is an ordered JSON object with JavaScript property order.
type Object struct {
	keys []string
	vals []Value
}

// Len is the number of keys.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Keys returns the keys in order. The slice must not be modified.
func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	return o.keys
}

// At returns the i-th key and value.
func (o *Object) At(i int) (string, Value) { return o.keys[i], o.vals[i] }

func (o *Object) index(k string) int {
	if o == nil {
		return -1
	}
	for i, x := range o.keys {
		if x == k {
			return i
		}
	}
	return -1
}

// Get returns the value under k.
func (o *Object) Get(k string) (Value, bool) {
	if i := o.index(k); i >= 0 {
		return o.vals[i], true
	}
	return Value{}, false
}

// Has reports whether k is present.
func (o *Object) Has(k string) bool { return o.index(k) >= 0 }

// Set assigns k like `obj[k] = v`: an existing key keeps its place, a new one is
// appended, except that integer-like keys sort numerically ahead of the rest.
func (o *Object) Set(k string, v Value) {
	if i := o.index(k); i >= 0 {
		o.vals[i] = v
		return
	}
	at := len(o.keys)
	if n, ok := arrayIndex(k); ok {
		at = 0
		for at < len(o.keys) {
			m, ok := arrayIndex(o.keys[at])
			if !ok || m > n {
				break
			}
			at++
		}
	}
	o.keys = append(o.keys, "")
	o.vals = append(o.vals, Value{})
	copy(o.keys[at+1:], o.keys[at:])
	copy(o.vals[at+1:], o.vals[at:])
	o.keys[at], o.vals[at] = k, v
}

// Delete removes k, keeping the order of the rest.
func (o *Object) Delete(k string) {
	if i := o.index(k); i >= 0 {
		o.keys = append(o.keys[:i], o.keys[i+1:]...)
		o.vals = append(o.vals[:i], o.vals[i+1:]...)
	}
}

// Clone copies the object shallowly, as `{ ...obj }` does.
func (o *Object) Clone() *Object {
	if o == nil {
		return &Object{}
	}
	return &Object{keys: append([]string(nil), o.keys...), vals: append([]Value(nil), o.vals...)}
}

// arrayIndex reports whether k is a canonical array index (0 ≤ n < 2^32-1),
// the keys JavaScript orders before all others.
func arrayIndex(k string) (uint32, bool) {
	if k == "" || len(k) > 10 || (k[0] == '0' && len(k) > 1) {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return 0, false
		}
		n = n*10 + uint64(k[i]-'0')
	}
	if n >= 1<<32-1 {
		return 0, false
	}
	return uint32(n), true
}
