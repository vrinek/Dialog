// Package dcbor implements the deterministic CBOR (dCBOR) profile that the
// Dialog protocol uses for all serialization, as specified in
// spec/03-encoding.md ("Deterministic CBOR").
//
// The profile is deliberately small. Dialog structures are built from
// unsigned and negative integers, decimal fractions (CBOR tag 4), text
// strings, byte strings, arrays, maps with text-string keys, and null.
// Everything else — floating-point values, booleans, undefined, other simple
// values, every tag but tag 4, and indefinite-length items — is outside the
// profile and is rejected by both the encoder and the decoder.
//
// Dialog's profile is self-contained: it is RFC 8949 §4.2.1 Core
// Deterministic Encoding plus the restrictions of spec/03-encoding.md. It is
// narrower than the general-purpose deterministic CBOR profiles in
// circulation, so conformance to one does not imply conformance to the other.
//
// Text strings are required to be well-formed UTF-8 and are otherwise passed
// through unchanged. Unicode normalization is deliberately not applied: per
// spec/03-encoding.md, "Text strings and Unicode", content addressing
// operates on raw UTF-8 bytes, and strings differing in code points name
// different entities. Callers that want NFC should normalize user input at
// capture time, before an entity is built.
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
// implementations are Uint, Neg, Decimal, Text, Bytes, Array, Map, and
// NullValue.
type Value interface {
	isValue()
}

// Uint is a non-negative CBOR integer (major type 0). It covers the whole
// range 0..2^64-1.
type Uint uint64

// Neg is a negative CBOR integer (major type 1). It represents the value
// -1 - Neg, and so covers the whole range -1..-2^64.
type Neg uint64

// Decimal is a CBOR decimal fraction (tag 4), denoting the exact value
// Mantissa × 10^Exponent. It is the only tag inside Dialog's profile, and it
// carries the non-integer scalar filler values of spec/01-data-model.md.
//
// Only the canonical form of spec/03-encoding.md ("Decimal fractions") is a
// valid Decimal, so that each value has exactly one encoding:
//
//   - Exponent MUST be negative. Whole numbers are Uint or Neg, never Decimal.
//   - Mantissa MUST NOT be zero and MUST NOT be divisible by 10.
//
// Encode rejects any other Decimal, and Decode rejects the corresponding
// bytes. Use NewDecimal to build one from an arbitrary exponent/mantissa
// pair: it performs the canonicalization and returns an integer Value when
// the result is a whole number.
//
// Both components are int64 because spec/03-encoding.md, "Decimal fractions",
// bounds the exponent and the mantissa to the signed 64-bit range
// [-2^63, 2^63-1] and requires decoders to reject anything outside it. CBOR's
// integer types can express larger magnitudes; such a decimal fraction is not
// a Dialog document, and Decode refuses it.
type Decimal struct {
	Exponent int64
	Mantissa int64
}

// NewDecimal returns the canonical Value for mantissa × 10^exponent: a
// Decimal when the value is not a whole number, and a Uint or Neg when it is.
// Trailing zeros in the mantissa are absorbed into the exponent.
//
// It returns an error when the value is a whole number too large for an
// int64, since Dialog's profile has no representation for it.
func NewDecimal(exponent, mantissa int64) (Value, error) {
	if mantissa == 0 {
		return Uint(0), nil
	}
	// Strip trailing decimal zeros: [-2, 3140] and [-1, 314] denote the same
	// value, and only the latter is canonical.
	for exponent < 0 && mantissa%10 == 0 {
		mantissa /= 10
		exponent++
	}
	if exponent < 0 {
		return Decimal{Exponent: exponent, Mantissa: mantissa}, nil
	}
	// A non-negative exponent means a whole number, which rule 1 requires to
	// be a plain integer.
	for ; exponent > 0; exponent-- {
		scaled := mantissa * 10
		if scaled/10 != mantissa {
			return nil, fmt.Errorf("dcbor: decimal fraction %d×10^%d overflows int64", mantissa, exponent)
		}
		mantissa = scaled
	}
	return Int(mantissa), nil
}

// checkCanonical reports an error if d is not in the canonical form required
// by spec/03-encoding.md, "Decimal fractions".
func (d Decimal) checkCanonical() error {
	switch {
	case d.Exponent >= 0:
		return fmt.Errorf("dcbor: decimal fraction exponent %d is not negative; whole numbers must be encoded as integers", d.Exponent)
	case d.Mantissa == 0:
		return fmt.Errorf("dcbor: decimal fraction mantissa is zero; zero must be encoded as the integer 0")
	case d.Mantissa%10 == 0:
		return fmt.Errorf("dcbor: decimal fraction mantissa %d is divisible by 10; trailing zeros must be stripped into the exponent", d.Mantissa)
	}
	return nil
}

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
func (Decimal) isValue()   {}
func (Text) isValue()      {}
func (Bytes) isValue()     {}
func (Array) isValue()     {}
func (Map) isValue()       {}
func (NullValue) isValue() {}

// Int returns the Value representing i: a Uint for non-negative values, a Neg
// for negative ones.
func Int(i int64) Value {
	if i < 0 {
		// -(i+1) is in 0..2^63-1 for every negative int64, including
		// math.MinInt64, whose +1 makes the negation representable. That is
		// exactly CBOR's negative-integer encoding: n stands for -1-n.
		//nolint:gosec // G115: -(i+1) is non-negative for all i < 0.
		return Neg(uint64(-(i + 1)))
	}
	//nolint:gosec // G115: i >= 0 here.
	return Uint(i)
}

// Int64 reports the value of u as an int64. ok is false if the value exceeds
// math.MaxInt64.
func (u Uint) Int64() (v int64, ok bool) {
	if uint64(u) > math.MaxInt64 {
		return 0, false
	}
	//nolint:gosec // G115: the guard above is the range check.
	return int64(u), true
}

// Int64 reports the value of n as an int64. ok is false if the value is below
// math.MinInt64.
func (n Neg) Int64() (v int64, ok bool) {
	if uint64(n) > math.MaxInt64 {
		return 0, false
	}
	//nolint:gosec // G115: the guard above is the range check.
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
	case Decimal:
		bv, ok := b.(Decimal)
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

// validText reports an error for text that is not valid UTF-8
// (spec/03-encoding.md, "Text strings and Unicode").
//
// Validity is the only check applied. Dialog forbids Unicode normalization
// inside the protocol, so text passes through byte for byte; see the package
// documentation.
func validText(s string, what string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("dcbor: %s is not valid UTF-8: %q", what, s)
	}
	return nil
}
