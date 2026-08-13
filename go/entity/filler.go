package entity

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// Keys of a filler and of a scalar value (spec/01-data-model.md,
// "Molecules").
const (
	keyType  = "type"
	keyValue = "value"
	keyUnit  = "unit"
	keyFrom  = "from"
	keyTo    = "to"
)

// A FillerType is the type tag of a filler (spec/01-data-model.md, "Filler
// types"). Tags 0 to 4 are the whole of the v1 vocabulary; any other value is
// rejected.
type FillerType uint64

// The five filler types.
const (
	// FillerAtom carries the 32-byte digest of an atom.
	FillerAtom FillerType = 0
	// FillerBond carries the 32-byte digest of a bond.
	FillerBond FillerType = 1
	// FillerMolecule carries the 32-byte digest of a molecule.
	FillerMolecule FillerType = 2
	// FillerIPFSURI carries an IPFS content identifier as a text string. It
	// is not an internal reference (spec/03-encoding.md, "Internal
	// references").
	FillerIPFSURI FillerType = 3
	// FillerScalar carries a number with an optional unit, or a datetime
	// range.
	FillerScalar FillerType = 4
)

// Valid reports whether t is one of the five types of spec/01-data-model.md.
func (t FillerType) Valid() bool { return t <= FillerScalar }

// IsRef reports whether t is one of the three internal-reference types (atom,
// bond, molecule), whose value is a raw 32-byte digest.
func (t FillerType) IsRef() bool { return t <= FillerMolecule }

// String names the filler type.
func (t FillerType) String() string {
	switch t {
	case FillerAtom:
		return "atom"
	case FillerBond:
		return "bond"
	case FillerMolecule:
		return "molecule"
	case FillerIPFSURI:
		return "ipfs-uri"
	case FillerScalar:
		return "scalar"
	default:
		return fmt.Sprintf("unknown(%d)", uint64(t))
	}
}

// A Filler fills one variable of a bond template. On the wire it is a
// two-key map — the type tag and the value it selects:
//
//	filler = { "type" => filler-type, "value" => filler-value }
//
// The value's shape is fixed by the type: a 32-byte digest for types 0, 1 and
// 2, a text string for type 3, and a scalar map for type 4
// (spec/01-data-model.md, "Filler types").
//
// A Filler holds exactly one of those three payloads, selected by Type. Build
// one with AtomFiller, BondFiller, MoleculeFiller, RefFiller, IPFSFiller or
// ScalarFiller; the zero Filler is a well-formed atom filler referencing the
// all-zero digest, which is a real filler and not a sentinel.
type Filler struct {
	typ    FillerType
	ref    cid.Digest // types 0, 1, 2
	uri    string     // type 3
	scalar Scalar     // type 4
}

// AtomFiller returns a filler referencing an atom by digest.
func AtomFiller(d cid.Digest) Filler { return Filler{typ: FillerAtom, ref: d} }

// BondFiller returns a filler referencing a bond by digest.
func BondFiller(d cid.Digest) Filler { return Filler{typ: FillerBond, ref: d} }

// MoleculeFiller returns a filler referencing a molecule by digest.
func MoleculeFiller(d cid.Digest) Filler { return Filler{typ: FillerMolecule, ref: d} }

// RefFiller returns a filler of one of the three reference types (atom, bond
// or molecule) carrying d. It is an error to pass any other type.
func RefFiller(t FillerType, d cid.Digest) (Filler, error) {
	if !t.IsRef() {
		return Filler{}, fmt.Errorf("entity: filler type %s does not carry a digest; use IPFSFiller or ScalarFiller", t)
	}
	return Filler{typ: t, ref: d}, nil
}

// IPFSFiller returns a type 3 filler carrying an IPFS content identifier.
//
// The specification defers the string's format to IPFS and puts it out of
// scope for Dialog (spec/03-encoding.md, "Internal references"), so the URI
// is treated as opaque: it must be non-empty, valid UTF-8 text, and nothing
// more is assumed about it. See todos/035 for the unstated question of
// whether an empty string is permitted, which this rejects.
func IPFSFiller(uri string) (Filler, error) {
	if uri == "" {
		return Filler{}, fmt.Errorf("entity: IPFS URI filler is empty")
	}
	if !utf8.ValidString(uri) {
		return Filler{}, fmt.Errorf("entity: IPFS URI filler is not valid UTF-8: %q", uri)
	}
	return Filler{typ: FillerIPFSURI, uri: uri}, nil
}

// ScalarFiller returns a type 4 filler carrying s.
func ScalarFiller(s Scalar) (Filler, error) {
	if err := s.validate(); err != nil {
		return Filler{}, err
	}
	return Filler{typ: FillerScalar, scalar: s}, nil
}

// Type returns the filler's type tag.
func (f Filler) Type() FillerType { return f.typ }

// Ref returns the digest carried by a filler of type 0, 1 or 2. ok is false
// for the other types.
func (f Filler) Ref() (d cid.Digest, ok bool) {
	if !f.typ.IsRef() {
		return cid.Digest{}, false
	}
	return f.ref, true
}

// URI returns the IPFS content identifier carried by a type 3 filler. ok is
// false for the other types.
func (f Filler) URI() (uri string, ok bool) {
	if f.typ != FillerIPFSURI {
		return "", false
	}
	return f.uri, true
}

// Scalar returns the scalar carried by a type 4 filler. ok is false for the
// other types.
func (f Filler) Scalar() (s Scalar, ok bool) {
	if f.typ != FillerScalar {
		return Scalar{}, false
	}
	return f.scalar, true
}

// Value returns the filler as a dCBOR value.
func (f Filler) Value() dcbor.Value {
	return dcbor.Map{
		{Key: keyType, Value: dcbor.Uint(f.typ)},
		{Key: keyValue, Value: f.valueField()},
	}
}

// Bytes returns the filler's canonical dCBOR encoding. A filler is not
// content-addressed on its own — it is encoded as part of a molecule — but
// its bytes are useful for tests and conformance vectors.
func (f Filler) Bytes() []byte { return dcbor.MustEncode(f.Value()) }

// String renders the filler for logs and test failures.
func (f Filler) String() string {
	switch f.typ {
	case FillerIPFSURI:
		return fmt.Sprintf("filler(%s, %q)", f.typ, f.uri)
	case FillerScalar:
		return fmt.Sprintf("filler(%s, %s)", f.typ, f.scalar)
	default:
		return fmt.Sprintf("filler(%s, %s)", f.typ, f.ref)
	}
}

func (f Filler) valueField() dcbor.Value {
	switch f.typ {
	case FillerIPFSURI:
		return dcbor.Text(f.uri)
	case FillerScalar:
		return f.scalar.Value()
	default:
		return dcbor.Bytes(f.ref.Bytes())
	}
}

// validate reports whether f is one of the five permitted types and its
// payload is well-formed. Constructors guarantee this; it guards values built
// by decoding.
func (f Filler) validate() error {
	switch {
	case !f.typ.Valid():
		return fmt.Errorf("entity: filler type %d is not one of the five types 0-4 of spec/01-data-model.md", uint64(f.typ))
	case f.typ == FillerIPFSURI:
		if f.uri == "" {
			return fmt.Errorf("entity: IPFS URI filler is empty")
		}
		if !utf8.ValidString(f.uri) {
			return fmt.Errorf("entity: IPFS URI filler is not valid UTF-8: %q", f.uri)
		}
		return nil
	case f.typ == FillerScalar:
		return f.scalar.validate()
	default:
		return nil
	}
}

// DecodeFiller parses and validates the canonical dCBOR encoding of a filler.
func DecodeFiller(b []byte) (Filler, error) {
	v, err := dcbor.Decode(b)
	if err != nil {
		return Filler{}, fmt.Errorf("entity: filler is not valid dCBOR: %w", err)
	}
	return fillerFromValue(v)
}

// fillerFromValue validates one decoded filler map.
func fillerFromValue(v dcbor.Value) (Filler, error) {
	m, err := asMap(v, "filler")
	if err != nil {
		return Filler{}, err
	}
	if err := requireKeys(m, "filler", keyType, keyValue); err != nil {
		return Filler{}, err
	}

	tv, _ := m.Get(keyType)
	tu, ok := tv.(dcbor.Uint)
	if !ok {
		return Filler{}, fmt.Errorf("entity: filler %q must be an unsigned integer in 0-4, got %s", keyType, kindOf(tv))
	}
	t := FillerType(tu)
	if !t.Valid() {
		return Filler{}, fmt.Errorf("entity: filler type %d is not one of the five types 0-4 of spec/01-data-model.md", uint64(tu))
	}

	value, _ := m.Get(keyValue)
	switch t {
	case FillerIPFSURI:
		s, ok := value.(dcbor.Text)
		if !ok {
			return Filler{}, fmt.Errorf("entity: filler of type %s must carry a text string, got %s", t, kindOf(value))
		}
		return IPFSFiller(string(s))
	case FillerScalar:
		s, err := scalarFromValue(value)
		if err != nil {
			return Filler{}, err
		}
		return ScalarFiller(s)
	default:
		d, err := asDigest(value, fmt.Sprintf("filler of type %s", t))
		if err != nil {
			return Filler{}, err
		}
		return Filler{typ: t, ref: d}, nil
	}
}

// A ScalarKind selects which of the two shapes of spec/01-data-model.md's
// scalar-value a Scalar holds.
type ScalarKind uint8

const (
	// ScalarNumber is an integer or decimal fraction, optionally with a unit.
	ScalarNumber ScalarKind = iota + 1
	// ScalarDatetimeRange is a pair of Dialog timestamps (see
	// ValidateTimestamp) in chronological order.
	ScalarDatetimeRange
)

// String names the scalar kind.
func (k ScalarKind) String() string {
	switch k {
	case ScalarNumber:
		return "number"
	case ScalarDatetimeRange:
		return "datetime-range"
	default:
		return "invalid"
	}
}

// A Scalar is the value of a type 4 filler. It is one of two shapes
// (spec/01-data-model.md, "Scalars"):
//
//	scalar-value = {
//	  ? "unit" => bstr .size 32,
//	  "value" => int / #6.4([int, int])
//	}
//	/ datetime-range
//
//	datetime-range = { "from" => timestamp, "to" => timestamp }
//
// A number is a plain CBOR integer, or a decimal fraction in the canonical
// tag 4 form of spec/03-encoding.md, "Decimal fractions" — whole numbers are
// always plain integers, and both components are bounded to the signed 64-bit
// range. The optional unit is the digest of an atom naming the unit. A
// datetime range never carries a unit; the two shapes are exclusive. Its
// endpoints are Dialog timestamps and from must not be later than to
// (spec/01-data-model.md, "Datetime ranges").
//
// The zero Scalar is not a scalar. Build one with IntScalar, DecimalScalar,
// NumberScalar or NewDatetimeRange.
type Scalar struct {
	kind     ScalarKind
	number   dcbor.Value // ScalarNumber: dcbor.Uint, dcbor.Neg or dcbor.Decimal
	unit     cid.Digest
	hasUnit  bool
	from, to string // ScalarDatetimeRange
}

// IntScalar returns a unitless integer scalar.
func IntScalar(v int64) Scalar {
	return Scalar{kind: ScalarNumber, number: dcbor.Int(v)}
}

// DecimalScalar returns a unitless scalar for mantissa × 10^exponent,
// canonicalized by dcbor.NewDecimal: trailing zeros are stripped into the
// exponent, and a whole number becomes a plain integer rather than a tag 4
// decimal fraction.
func DecimalScalar(exponent, mantissa int64) (Scalar, error) {
	v, err := dcbor.NewDecimal(exponent, mantissa)
	if err != nil {
		return Scalar{}, fmt.Errorf("entity: scalar: %w", err)
	}
	return Scalar{kind: ScalarNumber, number: v}, nil
}

// NumberScalar returns a scalar for an already-built dCBOR number, which must
// be a dcbor.Uint, dcbor.Neg or a canonical dcbor.Decimal.
func NumberScalar(v dcbor.Value) (Scalar, error) {
	s := Scalar{kind: ScalarNumber, number: v}
	if err := s.validate(); err != nil {
		return Scalar{}, err
	}
	return s, nil
}

// WithUnit returns a copy of a number scalar carrying the digest of the atom
// that names its unit. It is an error to give a datetime range a unit — the
// CDDL's two shapes are exclusive.
func (s Scalar) WithUnit(unit cid.Digest) (Scalar, error) {
	if s.kind != ScalarNumber {
		return Scalar{}, fmt.Errorf("entity: only a number scalar can carry a unit, not a %s", s.kind)
	}
	s.unit, s.hasUnit = unit, true
	return s, nil
}

// NewDatetimeRange returns a datetime-range scalar. Both endpoints must be
// Dialog timestamps — UTC RFC 3339 date-times at second precision, see
// ValidateTimestamp — and from must not be later than to
// (spec/01-data-model.md, "Datetime ranges").
func NewDatetimeRange(from, to string) (Scalar, error) {
	s := Scalar{kind: ScalarDatetimeRange, from: from, to: to}
	if err := s.validate(); err != nil {
		return Scalar{}, err
	}
	return s, nil
}

// Kind reports which shape s holds.
func (s Scalar) Kind() ScalarKind { return s.kind }

// Number returns the integer or decimal fraction of a number scalar. ok is
// false for a datetime range.
func (s Scalar) Number() (v dcbor.Value, ok bool) {
	if s.kind != ScalarNumber {
		return nil, false
	}
	return s.number, true
}

// Unit returns the digest of the unit atom, if the scalar has one.
func (s Scalar) Unit() (d cid.Digest, ok bool) {
	if !s.hasUnit {
		return cid.Digest{}, false
	}
	return s.unit, true
}

// Range returns the endpoints of a datetime-range scalar. ok is false for a
// number.
func (s Scalar) Range() (from, to string, ok bool) {
	if s.kind != ScalarDatetimeRange {
		return "", "", false
	}
	return s.from, s.to, true
}

// Value returns the scalar as a dCBOR value.
func (s Scalar) Value() dcbor.Value {
	if s.kind == ScalarDatetimeRange {
		return dcbor.Map{
			{Key: keyFrom, Value: dcbor.Text(s.from)},
			{Key: keyTo, Value: dcbor.Text(s.to)},
		}
	}
	m := dcbor.Map{{Key: keyValue, Value: s.number}}
	if s.hasUnit {
		m = append(m, dcbor.MapEntry{Key: keyUnit, Value: dcbor.Bytes(s.unit.Bytes())})
	}
	return m
}

// String renders the scalar for logs and test failures.
func (s Scalar) String() string {
	switch s.kind {
	case ScalarDatetimeRange:
		return fmt.Sprintf("%s..%s", s.from, s.to)
	case ScalarNumber:
		if s.hasUnit {
			return fmt.Sprintf("%v %s", s.number, s.unit)
		}
		return fmt.Sprintf("%v", s.number)
	default:
		return "invalid"
	}
}

func (s Scalar) validate() error {
	switch s.kind {
	case ScalarNumber:
		switch n := s.number.(type) {
		case dcbor.Uint, dcbor.Neg:
			return nil
		case dcbor.Decimal:
			// Encode enforces the canonicalization rules of
			// spec/03-encoding.md, "Decimal fractions".
			if _, err := dcbor.Encode(n); err != nil {
				return fmt.Errorf("entity: scalar value: %w", err)
			}
			return nil
		default:
			return fmt.Errorf("entity: scalar value must be an integer or a tag 4 decimal fraction, got %s", kindOf(s.number))
		}
	case ScalarDatetimeRange:
		if err := ValidateTimestamp(s.from); err != nil {
			return fmt.Errorf("entity: datetime range %q endpoint: %w", keyFrom, err)
		}
		if err := ValidateTimestamp(s.to); err != nil {
			return fmt.Errorf("entity: datetime range %q endpoint: %w", keyTo, err)
		}
		// Every timestamp is fixed-width, zero-padded, UTC and
		// most-significant-first, so string order is chronological order —
		// the comparison spec/01-data-model.md, "Datetime ranges", requires.
		if s.from > s.to {
			return fmt.Errorf("entity: datetime range starts after it ends: %q is later than %q", s.from, s.to)
		}
		return nil
	default:
		return fmt.Errorf("entity: scalar has no value; build one with IntScalar, DecimalScalar, NumberScalar or NewDatetimeRange")
	}
}

// scalarFromValue validates one decoded scalar map. The key set alone decides
// which of the two shapes it is: {"from","to"} is a datetime range, anything
// else must be {"value"} or {"unit","value"}.
func scalarFromValue(v dcbor.Value) (Scalar, error) {
	m, err := asMap(v, "scalar filler value")
	if err != nil {
		return Scalar{}, err
	}
	_, hasFrom := m.Get(keyFrom)
	_, hasTo := m.Get(keyTo)
	if hasFrom || hasTo {
		if err := requireKeys(m, "datetime range", keyFrom, keyTo); err != nil {
			return Scalar{}, err
		}
		from, err := textField(m, keyFrom, "datetime range")
		if err != nil {
			return Scalar{}, err
		}
		to, err := textField(m, keyTo, "datetime range")
		if err != nil {
			return Scalar{}, err
		}
		return NewDatetimeRange(from, to)
	}

	number, ok := m.Get(keyValue)
	if !ok {
		return Scalar{}, fmt.Errorf("entity: scalar filler value must hold %q, or %q and %q for a datetime range", keyValue, keyFrom, keyTo)
	}
	s, err := NumberScalar(number)
	if err != nil {
		return Scalar{}, err
	}
	switch len(m) {
	case 1:
		return s, nil
	case 2:
		unit, err := digestField(m, keyUnit, "scalar")
		if err != nil {
			return Scalar{}, err
		}
		return s.WithUnit(unit)
	default:
		return Scalar{}, fmt.Errorf("entity: scalar filler value has %d key(s); it must hold %q and optionally %q", len(m), keyValue, keyUnit)
	}
}

// TimestampLayout is the one form a Dialog timestamp may take, as a Go
// reference time: YYYY-MM-DDTHH:MM:SSZ. The trailing Z is a literal in this
// layout, not Go's numeric-offset marker, so time.Parse accepts nothing but
// UTC spelled with the designator.
const TimestampLayout = "2006-01-02T15:04:05Z"

// timestampLen is the length of every valid timestamp: 19 characters of
// date-time plus the Z designator.
const timestampLen = 20

// ValidateTimestamp reports whether s is a Dialog timestamp — an RFC 3339
// date-time restricted to the canonical profile of spec/01-data-model.md,
// "Datetime ranges":
//
//   - the form is exactly YYYY-MM-DDTHH:MM:SSZ, 20 characters;
//   - the offset is the designator Z, never a numeric offset, not even
//     +00:00 or -00:00;
//   - the T separator and the Z designator are uppercase;
//   - there is no fractional-second part, not even a zero one;
//   - the seconds value is 00-59, so the leap second 60 is rejected;
//   - the components denote a real instant — a day that exists in that month
//     of that year, an hour below 24.
//
// The profile exists for content addressing: entities are hashed over their
// raw bytes, so admitting a second spelling of an instant would admit a second
// CID for the same statement. Times recorded in another zone or at another
// precision are converted before the entity is created.
func ValidateTimestamp(s string) error {
	if s == "" {
		return fmt.Errorf("timestamp is empty; it must have the form YYYY-MM-DDTHH:MM:SSZ")
	}
	if len(s) != timestampLen {
		return fmt.Errorf("timestamp %q is %d bytes; it must have the form YYYY-MM-DDTHH:MM:SSZ (%d bytes)", s, len(s), timestampLen)
	}
	// Fixed punctuation, uppercase T and Z, digits everywhere else. This
	// rejects numeric offsets, fractional seconds and the lowercase
	// separators RFC 3339 would otherwise permit, before any parsing.
	for i, want := range [...]byte{4: '-', 7: '-', 10: 'T', 13: ':', 16: ':', 19: 'Z'} {
		if want == 0 {
			if c := s[i]; c < '0' || c > '9' {
				return fmt.Errorf("timestamp %q has %q where a digit is required; it must have the form YYYY-MM-DDTHH:MM:SSZ", s, s[i:i+1])
			}
			continue
		}
		if s[i] != want {
			return fmt.Errorf("timestamp %q has %q at position %d, want %q; it must have the form YYYY-MM-DDTHH:MM:SSZ in UTC", s, s[i:i+1], i, string(want))
		}
	}
	// The leap second RFC 3339 permits is not a Dialog timestamp.
	if s[17:19] > "59" {
		return fmt.Errorf("timestamp %q uses the leap second %q; the seconds value must be 00-59", s, s[17:19])
	}
	// What remains is calendar validity: month, day-of-month for that year,
	// and hour. time.Parse checks each and nothing else is left for it to
	// disagree about, the lexical form being already fixed.
	if _, err := time.Parse(TimestampLayout, s); err != nil {
		return fmt.Errorf("timestamp %q does not denote a real instant", s)
	}
	return nil
}
