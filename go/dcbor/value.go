// Package dcbor implements the deterministic CBOR (dCBOR) profile that the
// Dialog protocol uses for all serialization, as specified in
// spec/03-encoding.md ("Deterministic CBOR").
//
// The profile is deliberately small. Dialog structures are built from
// unsigned and negative integers, text strings, byte strings, arrays, maps
// with text-string keys, and null. Everything else — floating-point values,
// booleans, undefined, other simple values, tags, and indefinite-length
// items — is outside the profile and is rejected by both the encoder and the
// decoder.
//
// Encoding is canonical: for any Value there is exactly one byte string, and
// for any byte string this package accepts there is exactly one Value.
// Concretely, for every v accepted by Encode:
//
//	Equal(Decode(Encode(v)), v)
//
// and for every b accepted by Decode:
//
//	Encode(Decode(b)) == b
//
// This package has no dependencies outside the standard library.
package dcbor

import (
	"bytes"
	"fmt"
	"math"
	"unicode/utf8"
)

// MaxDepth is the maximum nesting depth of arrays and maps that this package
// will encode or decode. Dialog structures nest only a handful of levels
// deep; the limit exists so that hostile input cannot exhaust the stack.
const MaxDepth = 64

// A Value is a CBOR value inside Dialog's dCBOR profile. The only
// implementations are Uint, Neg, Text, Bytes, Array, Map, and NullValue.
type Value interface {
	isValue()
}

// Uint is a non-negative CBOR integer (major type 0). It covers the whole
// range 0..2^64-1.
type Uint uint64

// Neg is a negative CBOR integer (major type 1). It represents the value
// -1 - Neg, and so covers the whole range -1..-2^64.
type Neg uint64

// Text is a CBOR text string (major type 3). Its contents MUST be valid
// UTF-8.
type Text string

// Bytes is a CBOR byte string (major type 2).
type Bytes []byte

// Array is a CBOR array (major type 4).
type Array []Value

// MapEntry is one key/value pair of a Map. Dialog's profile allows text
// string keys only.
type MapEntry struct {
	Key   string
	Value Value
}

// Map is a CBOR map (major type 5) with text-string keys.
//
// A Map is an unordered set of entries as far as callers are concerned: the
// encoder sorts the entries into the canonical order (bytewise lexicographic
// order of the CBOR encoding of each key) and rejects duplicate keys. Decode
// always returns entries in that canonical order.
type Map []MapEntry

// NullValue is the type of the CBOR null value.
type NullValue struct{}

// Null is the CBOR null value (0xf6). It is permitted wherever the
// specification allows null, for example the prev field of a genesis block.
var Null = NullValue{}

func (Uint) isValue()      {}
func (Neg) isValue()       {}
func (Text) isValue()      {}
func (Bytes) isValue()     {}
func (Array) isValue()     {}
func (Map) isValue()       {}
func (NullValue) isValue() {}

// Int returns the Value representing i: a Uint for non-negative values, a Neg
// for negative ones.
func Int(i int64) Value {
	if i < 0 {
		return Neg(uint64(-(i + 1)))
	}
	return Uint(i)
}

// Int64 reports the value of u as an int64. ok is false if the value exceeds
// math.MaxInt64.
func (u Uint) Int64() (v int64, ok bool) {
	if uint64(u) > math.MaxInt64 {
		return 0, false
	}
	return int64(u), true
}

// Int64 reports the value of n as an int64. ok is false if the value is below
// math.MinInt64.
func (n Neg) Int64() (v int64, ok bool) {
	if uint64(n) > math.MaxInt64 {
		return 0, false
	}
	return -1 - int64(n), true
}

// Get returns the value stored under key, and whether it was present.
func (m Map) Get(key string) (Value, bool) {
	for _, e := range m {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

// Equal reports whether a and b are the same CBOR value. Maps compare equal
// when they hold the same set of key/value pairs, regardless of the order
// their entries happen to be stored in.
func Equal(a, b Value) bool {
	switch av := a.(type) {
	case Uint:
		bv, ok := b.(Uint)
		return ok && av == bv
	case Neg:
		bv, ok := b.(Neg)
		return ok && av == bv
	case Text:
		bv, ok := b.(Text)
		return ok && av == bv
	case Bytes:
		bv, ok := b.(Bytes)
		return ok && bytes.Equal(av, bv)
	case Array:
		bv, ok := b.(Array)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !Equal(av[i], bv[i]) {
				return false
			}
		}
		return true
	case Map:
		bv, ok := b.(Map)
		if !ok || len(av) != len(bv) {
			return false
		}
		for _, e := range av {
			other, found := bv.Get(e.Key)
			if !found || !Equal(e.Value, other) {
				return false
			}
		}
		// len(av) == len(bv) and every key of av is in bv; if bv held a
		// duplicate key, some key of bv would be missing from av.
		for _, e := range bv {
			if _, found := av.Get(e.Key); !found {
				return false
			}
		}
		return true
	case NullValue:
		_, ok := b.(NullValue)
		return ok
	default:
		return false
	}
}

// validText reports an error for text that is not valid UTF-8. RFC 8949
// requires text strings to be well-formed UTF-8; Dialog inherits that
// requirement through the dCBOR profile.
func validText(s string, what string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("dcbor: %s is not valid UTF-8: %q", what, s)
	}
	return nil
}
