package dcbor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// A SyntaxError reports input that is not valid under Dialog's dCBOR profile,
// together with the byte offset at which the problem was detected.
type SyntaxError struct {
	// Offset is the byte offset of the item that was rejected.
	Offset int
	// Msg describes what is wrong with the input.
	Msg string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("dcbor: %s (at byte offset %d)", e.Msg, e.Offset)
}

// Decode parses b as a single dCBOR value.
//
// Decode is a validator as much as a parser: it rejects anything that is not
// the canonical encoding of the value it represents. That includes
// floating-point values and every other major type 7 value except null, every
// tag but tag 4, non-canonical tag 4 decimal fractions, indefinite-length
// items, non-shortest integer and length encodings, unsorted or duplicate map
// keys, non-text map keys, text that is not valid UTF-8, truncated input, and
// trailing bytes after the top-level value.
func Decode(b []byte) (Value, error) {
	d := &decoder{data: b}
	v, err := d.value(0)
	if err != nil {
		return nil, err
	}
	if d.pos != len(d.data) {
		return nil, d.errorAt(d.pos, fmt.Sprintf("%d trailing byte(s) after the top-level value", len(d.data)-d.pos))
	}
	return v, nil
}

type decoder struct {
	data []byte
	pos  int
}

func (d *decoder) errorAt(off int, msg string) error {
	return &SyntaxError{Offset: off, Msg: msg}
}

func (d *decoder) value(depth int) (Value, error) {
	if depth > MaxDepth {
		return nil, d.errorAt(d.pos, fmt.Sprintf("nesting deeper than %d levels", MaxDepth))
	}
	start := d.pos
	if d.pos >= len(d.data) {
		return nil, d.errorAt(start, "unexpected end of input")
	}
	ib := d.data[d.pos]
	d.pos++
	major, ai := ib>>5, ib&0x1f

	switch major {
	case majorUint:
		arg, err := d.argument(start, ai)
		if err != nil {
			return nil, err
		}
		return Uint(arg), nil

	case majorNeg:
		arg, err := d.argument(start, ai)
		if err != nil {
			return nil, err
		}
		return Neg(arg), nil

	case majorBytes:
		raw, err := d.stringPayload(start, ai, "byte string")
		if err != nil {
			return nil, err
		}
		return Bytes(bytes.Clone(raw)), nil

	case majorText:
		raw, err := d.stringPayload(start, ai, "text string")
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(raw) {
			return nil, d.errorAt(start, "text string is not valid UTF-8")
		}
		return Text(raw), nil

	case majorArray:
		n, err := d.count(start, ai, "array")
		if err != nil {
			return nil, err
		}
		arr := make(Array, 0, n)
		for i := uint64(0); i < n; i++ {
			item, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, item)
		}
		return arr, nil

	case majorMap:
		return d.mapValue(start, ai, depth)

	case majorTag:
		return d.tagValue(start, ai, depth)

	case majorOther:
		return d.simple(start, ai)
	}
	panic("unreachable")
}

// tagValue handles major type 6. Tag 4 (decimal fraction) is the sole tag
// inside Dialog's profile; every other tag is rejected
// (spec/03-encoding.md, "Deterministic CBOR" rule 6).
func (d *decoder) tagValue(start int, ai byte, depth int) (Value, error) {
	tag, err := d.argument(start, ai)
	if err != nil {
		return nil, err
	}
	if tag != tagDecimalFraction {
		return nil, d.errorAt(start, fmt.Sprintf("CBOR tags (major type 6) are not permitted, except tag 4 (decimal fraction); got tag %d", tag))
	}
	if depth+1 > MaxDepth {
		return nil, d.errorAt(start, fmt.Sprintf("nesting deeper than %d levels", MaxDepth))
	}

	// c4 82 <exponent> <mantissa> (spec/03-encoding.md, "Decimal fractions").
	arrStart := d.pos
	if d.pos >= len(d.data) {
		return nil, d.errorAt(arrStart, "unexpected end of input in tag 4 content")
	}
	ib := d.data[d.pos]
	d.pos++
	major, arrAI := ib>>5, ib&0x1f
	if major != majorArray {
		return nil, d.errorAt(arrStart, "tag 4 content must be an array of exactly two integers")
	}
	n, err := d.count(arrStart, arrAI, "array")
	if err != nil {
		return nil, err
	}
	if n != 2 {
		return nil, d.errorAt(arrStart, fmt.Sprintf("tag 4 content array has %d element(s), want exactly 2 ([exponent, mantissa])", n))
	}

	exponent, err := d.decimalInt("exponent")
	if err != nil {
		return nil, err
	}
	mantissa, err := d.decimalInt("mantissa")
	if err != nil {
		return nil, err
	}

	dec := Decimal{Exponent: exponent, Mantissa: mantissa}
	if err := dec.checkCanonical(); err != nil {
		// checkCanonical's messages are already specific; give them an offset.
		return nil, d.errorAt(start, strings.TrimPrefix(err.Error(), "dcbor: "))
	}
	return dec, nil
}

// decimalInt decodes one element of a tag 4 content array, which must be a
// shortest-form major type 0 or 1 integer within the int64 range.
func (d *decoder) decimalInt(what string) (int64, error) {
	start := d.pos
	if d.pos >= len(d.data) {
		return 0, d.errorAt(start, fmt.Sprintf("unexpected end of input in decimal fraction %s", what))
	}
	ib := d.data[d.pos]
	d.pos++
	major, ai := ib>>5, ib&0x1f
	if major != majorUint && major != majorNeg {
		return 0, d.errorAt(start, fmt.Sprintf("decimal fraction %s must be an integer (major type 0 or 1)", what))
	}
	arg, err := d.argument(start, ai)
	if err != nil {
		return 0, err
	}
	if arg > math.MaxInt64 {
		return 0, d.errorAt(start, fmt.Sprintf("decimal fraction %s is outside the int64 range", what))
	}
	if major == majorNeg {
		return -1 - int64(arg), nil
	}
	return int64(arg), nil
}

// simple handles major type 7. Only null is inside Dialog's profile.
func (d *decoder) simple(start int, ai byte) (Value, error) {
	switch {
	case ai == aiNull:
		return Null, nil
	case ai == 20 || ai == 21:
		return nil, d.errorAt(start, "boolean values are not part of Dialog's dCBOR profile")
	case ai == 23:
		return nil, d.errorAt(start, "the undefined value is not part of Dialog's dCBOR profile")
	case ai < 20:
		return nil, d.errorAt(start, fmt.Sprintf("simple value %d is not part of Dialog's dCBOR profile", ai))
	case ai == 24:
		return nil, d.errorAt(start, "simple values encoded with a one-byte argument are not part of Dialog's dCBOR profile")
	case ai >= 25 && ai <= 27:
		return nil, d.errorAt(start, "floating-point values are not permitted")
	case ai == 31:
		return nil, d.errorAt(start, "unexpected indefinite-length break stop code")
	default: // 28, 29, 30
		return nil, d.errorAt(start, fmt.Sprintf("reserved additional information value %d", ai))
	}
}

func (d *decoder) mapValue(start int, ai byte, depth int) (Value, error) {
	n, err := d.count(start, ai, "map")
	if err != nil {
		return nil, err
	}
	m := make(Map, 0, n)
	var prevKey []byte
	for i := uint64(0); i < n; i++ {
		keyStart := d.pos
		key, encKey, err := d.textKey()
		if err != nil {
			return nil, err
		}
		if prevKey != nil {
			switch cmp := bytes.Compare(prevKey, encKey); {
			case cmp == 0:
				return nil, d.errorAt(keyStart, fmt.Sprintf("duplicate map key %q", key))
			case cmp > 0:
				return nil, d.errorAt(keyStart, fmt.Sprintf("map key %q is not in bytewise lexicographic order", key))
			}
		}
		prevKey = encKey
		val, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		m = append(m, MapEntry{Key: key, Value: val})
	}
	return m, nil
}

// textKey decodes one map key, which must be a text string
// (spec/03-encoding.md, "Deterministic CBOR" rule 9), and returns both the key
// and its full CBOR encoding (used for the ordering check).
func (d *decoder) textKey() (key string, encoded []byte, err error) {
	start := d.pos
	if d.pos >= len(d.data) {
		return "", nil, d.errorAt(start, "unexpected end of input in map key")
	}
	ib := d.data[d.pos]
	d.pos++
	major, ai := ib>>5, ib&0x1f
	if major != majorText {
		return "", nil, d.errorAt(start, "map keys must be text strings")
	}
	raw, err := d.stringPayload(start, ai, "text string")
	if err != nil {
		return "", nil, err
	}
	if !utf8.Valid(raw) {
		return "", nil, d.errorAt(start, "map key is not valid UTF-8")
	}
	return string(raw), d.data[start:d.pos], nil
}

// stringPayload reads the length of a byte or text string and returns its
// payload as a sub-slice of the input.
func (d *decoder) stringPayload(start int, ai byte, what string) ([]byte, error) {
	n, err := d.count(start, ai, what)
	if err != nil {
		return nil, err
	}
	// count has already established that n is no larger than the number of
	// bytes left in d.data, which is an int, so the conversion cannot overflow
	// on any platform.
	//nolint:gosec // G115: n <= len(d.data)-d.pos, checked by count.
	raw := d.data[d.pos : d.pos+int(n)]
	d.pos += int(n) //nolint:gosec // G115: same bound as the line above.
	return raw, nil
}

// count reads a length or element count, rejecting indefinite lengths and
// lengths that cannot possibly be satisfied by the remaining input. Every
// array element and map entry takes at least one byte, so the same bound
// applies to all four container kinds; it also keeps a hostile length from
// driving a huge allocation.
func (d *decoder) count(start int, ai byte, what string) (uint64, error) {
	if ai == 31 {
		return 0, d.errorAt(start, fmt.Sprintf("indefinite-length %s is not permitted", what))
	}
	n, err := d.argument(start, ai)
	if err != nil {
		return 0, err
	}
	// d.pos never passes len(d.data) — every read bounds-checks first — so the
	// subtraction is non-negative and the conversion is exact.
	//nolint:gosec // G115: 0 <= len(d.data)-d.pos by the decoder's invariant.
	if remaining := uint64(len(d.data) - d.pos); n > remaining {
		return 0, d.errorAt(start, fmt.Sprintf("%s of length %d exceeds the %d byte(s) of remaining input", what, n, remaining))
	}
	return n, nil
}

// argument reads the argument of a head, enforcing shortest-form encoding
// (spec/03-encoding.md, "Deterministic CBOR" rules 1 and 4).
func (d *decoder) argument(start int, ai byte) (uint64, error) {
	switch {
	case ai < 24:
		return uint64(ai), nil
	case ai == 24:
		b, err := d.take(start, 1)
		if err != nil {
			return 0, err
		}
		v := uint64(b[0])
		if v < 24 {
			return 0, d.errorAt(start, fmt.Sprintf("argument %d is not encoded in the shortest form", v))
		}
		return v, nil
	case ai == 25:
		b, err := d.take(start, 2)
		if err != nil {
			return 0, err
		}
		v := uint64(binary.BigEndian.Uint16(b))
		if v <= 0xff {
			return 0, d.errorAt(start, fmt.Sprintf("argument %d is not encoded in the shortest form", v))
		}
		return v, nil
	case ai == 26:
		b, err := d.take(start, 4)
		if err != nil {
			return 0, err
		}
		v := uint64(binary.BigEndian.Uint32(b))
		if v <= 0xffff {
			return 0, d.errorAt(start, fmt.Sprintf("argument %d is not encoded in the shortest form", v))
		}
		return v, nil
	case ai == 27:
		b, err := d.take(start, 8)
		if err != nil {
			return 0, err
		}
		v := binary.BigEndian.Uint64(b)
		if v <= 0xffffffff {
			return 0, d.errorAt(start, fmt.Sprintf("argument %d is not encoded in the shortest form", v))
		}
		return v, nil
	case ai == 31:
		return 0, d.errorAt(start, "additional information 31 (indefinite length) is not permitted here")
	default: // 28, 29, 30
		return 0, d.errorAt(start, fmt.Sprintf("reserved additional information value %d", ai))
	}
}

func (d *decoder) take(start, n int) ([]byte, error) {
	if len(d.data)-d.pos < n {
		return nil, d.errorAt(start, "unexpected end of input")
	}
	b := d.data[d.pos : d.pos+n]
	d.pos += n
	return b, nil
}
