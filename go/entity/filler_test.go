package entity

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// testDigest returns a distinct, deterministic digest for test fixtures.
func testDigest(n byte) cid.Digest {
	var d cid.Digest
	for i := range d {
		d[i] = n
	}
	return d
}

// scalarValue is the map a scalar filler carries, built directly so that
// malformed shapes can be expressed.
func fillerMap(t FillerType, value dcbor.Value) dcbor.Value {
	return dcbor.Map{
		{Key: keyType, Value: dcbor.Uint(t)},
		{Key: keyValue, Value: value},
	}
}

// datetimeFiller builds a type 4 filler carrying a datetime range directly,
// bypassing the constructors so that endpoints the profile forbids can be put
// on the wire and offered to the decoder.
func datetimeFiller(from, to string) dcbor.Value {
	return fillerMap(FillerScalar, dcbor.Map{
		{Key: keyFrom, Value: dcbor.Text(from)},
		{Key: keyTo, Value: dcbor.Text(to)},
	})
}

// TestFillerEncoding pins the wire shape of each filler kind against the CDDL
// of spec/01-data-model.md, "Molecules".
func TestFillerEncoding(t *testing.T) {
	d := testDigest(0xab)
	digestHex := strings.Repeat("ab", 32)

	withUnit, err := IntScalar(72).WithUnit(d)
	if err != nil {
		t.Fatalf("WithUnit: %v", err)
	}
	dec, err := DecimalScalar(-2, -314)
	if err != nil {
		t.Fatalf("DecimalScalar: %v", err)
	}
	rng, err := NewDatetimeRange("2026-02-20T00:00:00Z", "2026-02-20T23:59:59Z")
	if err != nil {
		t.Fatalf("NewDatetimeRange: %v", err)
	}
	ipfs, err := IPFSFiller("bafyrei")
	if err != nil {
		t.Fatalf("IPFSFiller: %v", err)
	}

	mustScalar := func(s Scalar) Filler {
		t.Helper()
		f, err := ScalarFiller(s)
		if err != nil {
			t.Fatalf("ScalarFiller: %v", err)
		}
		return f
	}

	cases := []struct {
		name   string
		filler Filler
		hex    string
	}{
		{"atom reference", AtomFiller(d), "a2 647479706500 6576616c7565 5820" + digestHex},
		{"bond reference", BondFiller(d), "a2 647479706501 6576616c7565 5820" + digestHex},
		{"molecule reference", MoleculeFiller(d), "a2 647479706502 6576616c7565 5820" + digestHex},
		{"ipfs uri", ipfs, "a2 647479706503 6576616c7565 6762616679726569"},
		{"unitless integer", mustScalar(IntScalar(42)), "a2 647479706504 6576616c7565 a1 6576616c7565 182a"},
		{"negative integer", mustScalar(IntScalar(-1)), "a2 647479706504 6576616c7565 a1 6576616c7565 20"},
		// "unit" sorts before "value": both keys are five bytes long, and
		// 0x64 < 0x65 in the first byte of the encoded key.
		{"integer with unit", mustScalar(withUnit), "a2 647479706504 6576616c7565 a2 64756e69745820" + digestHex + " 6576616c7565 1848"},
		{"decimal fraction", mustScalar(dec), "a2 647479706504 6576616c7565 a1 6576616c7565 c4822139 0139"},
		// "to" sorts before "from": 0x62 < 0x64.
		{"datetime range", mustScalar(rng), "a2 647479706504 6576616c7565 a2" +
			" 62746f 74" + hex.EncodeToString([]byte("2026-02-20T23:59:59Z")) +
			" 6466726f6d 74" + hex.EncodeToString([]byte("2026-02-20T00:00:00Z"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.ReplaceAll(tc.hex, " ", "")
			checkBytes(t, tc.name, tc.filler.Bytes(), want)

			got, err := DecodeFiller(tc.filler.Bytes())
			if err != nil {
				t.Fatalf("DecodeFiller: %v", err)
			}
			if h := hex.EncodeToString(got.Bytes()); h != want {
				t.Errorf("re-encoded round trip = %s, want %s", h, want)
			}
		})
	}
}

// TestScalarAccessors checks the union's accessors report the right shape.
func TestScalarAccessors(t *testing.T) {
	unit := testDigest(0x01)
	s, err := IntScalar(5).WithUnit(unit)
	if err != nil {
		t.Fatalf("WithUnit: %v", err)
	}
	if s.Kind() != ScalarNumber {
		t.Errorf("Kind = %s, want number", s.Kind())
	}
	if got, ok := s.Unit(); !ok || got != unit {
		t.Errorf("Unit = (%s, %v), want (%s, true)", got, ok, unit)
	}
	if v, ok := s.Number(); !ok || !dcbor.Equal(v, dcbor.Uint(5)) {
		t.Errorf("Number = (%v, %v), want (5, true)", v, ok)
	}
	if _, _, ok := s.Range(); ok {
		t.Errorf("Range ok = true for a number scalar")
	}

	r, err := NewDatetimeRange("2026-02-20T00:00:00Z", "2026-02-20T23:59:59Z")
	if err != nil {
		t.Fatalf("NewDatetimeRange: %v", err)
	}
	from, to, ok := r.Range()
	if !ok || from != "2026-02-20T00:00:00Z" || to != "2026-02-20T23:59:59Z" {
		t.Errorf("Range = (%q, %q, %v)", from, to, ok)
	}
	if _, ok := r.Number(); ok {
		t.Errorf("Number ok = true for a datetime range")
	}
	if _, ok := r.Unit(); ok {
		t.Errorf("Unit ok = true for a datetime range")
	}
	if _, err := r.WithUnit(unit); err == nil {
		t.Errorf("WithUnit on a datetime range: want an error")
	}

	// DecimalScalar canonicalizes: a whole number becomes a plain integer,
	// and trailing zeros move into the exponent (spec/03-encoding.md,
	// "Decimal fractions").
	stripped, err := DecimalScalar(-2, 30)
	if err != nil {
		t.Fatalf("DecimalScalar: %v", err)
	}
	if v, _ := stripped.Number(); !dcbor.Equal(v, dcbor.Decimal{Exponent: -1, Mantissa: 3}) {
		t.Errorf("DecimalScalar(-2, 30) number = %v, want 3×10^-1", v)
	}
	whole, err := DecimalScalar(-2, 300)
	if err != nil {
		t.Fatalf("DecimalScalar: %v", err)
	}
	if v, _ := whole.Number(); !dcbor.Equal(v, dcbor.Uint(3)) {
		t.Errorf("DecimalScalar(-2, 300) number = %v, want the integer 3", v)
	}
	zero, err := DecimalScalar(-2, 0)
	if err != nil {
		t.Fatalf("DecimalScalar: %v", err)
	}
	if v, _ := zero.Number(); !dcbor.Equal(v, dcbor.Uint(0)) {
		t.Errorf("DecimalScalar(-2, 0) number = %v, want the integer 0", v)
	}
}

// TestFillerConstructorsReject covers the constructor-side validity rules.
func TestFillerConstructorsReject(t *testing.T) {
	if _, err := RefFiller(FillerIPFSURI, testDigest(1)); err == nil {
		t.Errorf("RefFiller(ipfs-uri): want an error")
	}
	if _, err := RefFiller(FillerScalar, testDigest(1)); err == nil {
		t.Errorf("RefFiller(scalar): want an error")
	}
	if _, err := RefFiller(FillerType(7), testDigest(1)); err == nil {
		t.Errorf("RefFiller(7): want an error")
	}
	if _, err := IPFSFiller(""); err == nil {
		t.Errorf("IPFSFiller(\"\"): want an error")
	}
	if _, err := IPFSFiller("bafy\xffrei"); err == nil {
		t.Errorf("IPFSFiller(invalid UTF-8): want an error")
	}
	if _, err := ScalarFiller(Scalar{}); err == nil {
		t.Errorf("ScalarFiller(zero Scalar): want an error")
	}
	if _, err := NumberScalar(dcbor.Text("42")); err == nil {
		t.Errorf("NumberScalar(text): want an error")
	}
	if _, err := NumberScalar(dcbor.Decimal{Exponent: 1, Mantissa: 5}); err == nil {
		t.Errorf("NumberScalar(non-canonical decimal): want an error")
	}
	if _, err := NewDatetimeRange("2026-02-20", "2026-02-21"); err == nil {
		t.Errorf("NewDatetimeRange(dates only): want an error")
	}
}

// TestValidateTimestamp is the accept/reject table for the canonical
// timestamp profile of spec/01-data-model.md, "Datetime ranges": UTC spelled
// with Z, uppercase separators, exactly second precision, no leap second, a
// real instant. One case per rule, in both directions.
func TestValidateTimestamp(t *testing.T) {
	valid := []struct {
		name string
		s    string
	}{
		{"midnight", "2026-02-20T00:00:00Z"},
		{"last second of the day", "2026-02-20T23:59:59Z"},
		{"leap day", "2024-02-29T12:00:00Z"},
		{"epoch", "1970-01-01T00:00:00Z"},
		{"year 0000", "0000-01-01T00:00:00Z"},
		{"year 9999", "9999-12-31T23:59:59Z"},
		// Proleptic Gregorian calendar: 1600 and 0000 are divisible by 400
		// and so are leap years, before the calendar existed as well as after.
		{"leap day of 1600", "1600-02-29T00:00:00Z"},
		{"leap day of year 0000", "0000-02-29T00:00:00Z"},
	}
	for _, tc := range valid {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			if err := ValidateTimestamp(tc.s); err != nil {
				t.Errorf("ValidateTimestamp(%q) = %v, want nil", tc.s, err)
			}
		})
	}

	invalid := []struct {
		name string
		s    string
	}{
		{"empty", ""},
		{"date only", "2026-02-20"},
		{"basic format", "20260220T000000Z"},
		{"space separator", "2026-02-20 00:00:00Z"},
		{"no offset", "2026-02-20T00:00:00"},
		// Rule: UTC only, spelled Z.
		{"positive offset", "2026-02-20T00:00:00+01:00"},
		{"negative offset", "2026-02-19T23:00:00-01:00"},
		{"zero offset", "2026-02-20T00:00:00+00:00"},
		{"negative zero offset", "2026-02-20T00:00:00-00:00"},
		{"offset without colon", "2026-02-20T00:00:00+0100"},
		// Rule: uppercase T and Z.
		{"lowercase t", "2026-02-20t00:00:00Z"},
		{"lowercase z", "2026-02-20T00:00:00z"},
		{"lowercase t and z", "2026-02-20t00:00:00z"},
		// Rule: exactly second precision.
		{"one fractional digit", "2026-02-20T00:00:00.5Z"},
		{"nine fractional digits", "2026-02-20T00:00:00.123456789Z"},
		{"zero fractional part", "2026-02-20T00:00:00.000Z"},
		{"minute precision", "2026-02-20T00:00Z"},
		// Rule: no leap second.
		{"leap second", "2026-12-31T23:59:60Z"},
		{"seconds 60 at midday", "2026-02-20T12:00:60Z"},
		// Rule: a real instant.
		{"no such day", "2026-02-30T00:00:00Z"},
		{"day 31 in April", "2026-04-31T00:00:00Z"},
		{"not a leap year", "2026-02-29T00:00:00Z"},
		// Proleptic Gregorian: 1500 and 1900 are divisible by 100 but not by
		// 400, so February had 28 days in both, whatever the calendar in civil
		// use at the time said (spec/01-data-model.md, "Datetime ranges").
		{"Julian leap day of 1500", "1500-02-29T00:00:00Z"},
		{"not a leap year in 1900", "1900-02-29T00:00:00Z"},
		{"month 13", "2026-13-01T00:00:00Z"},
		{"month 00", "2026-00-01T00:00:00Z"},
		{"day 00", "2026-01-00T00:00:00Z"},
		{"hour 24", "2026-02-20T24:00:00Z"},
		{"minute 60", "2026-02-20T00:60:00Z"},
		// Rule: form, 20 characters of the right shape.
		{"trailing space", "2026-02-20T00:00:00Z "},
		{"leading space", " 2026-02-20T00:00:00Z"},
		{"letters for digits", "20AB-02-20T00:00:00Z"},
		{"slashes for dashes", "2026/02/20T00:00:00Z"},
		{"word", "now"},
		{"twenty letters", "aaaaaaaaaaaaaaaaaaaa"},
	}
	for _, tc := range invalid {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			if err := ValidateTimestamp(tc.s); err == nil {
				t.Errorf("ValidateTimestamp(%q) = nil, want an error", tc.s)
			}
		})
	}
}

// TestDatetimeRangeOrdering covers the ordering rule: "from" MUST NOT be later
// than "to", compared as strings (spec/01-data-model.md, "Datetime ranges").
func TestDatetimeRangeOrdering(t *testing.T) {
	if _, err := NewDatetimeRange("2026-02-20T00:00:00Z", "2026-02-20T23:59:59Z"); err != nil {
		t.Errorf("NewDatetimeRange(ascending) = %v, want nil", err)
	}
	// An instant, not a span: the endpoints may be equal.
	if _, err := NewDatetimeRange("2026-02-20T12:00:00Z", "2026-02-20T12:00:00Z"); err != nil {
		t.Errorf("NewDatetimeRange(equal endpoints) = %v, want nil", err)
	}
	reversed := [][2]string{
		{"2026-02-20T23:59:59Z", "2026-02-20T00:00:00Z"},
		{"2026-02-21T00:00:00Z", "2026-02-20T00:00:00Z"},
		{"2026-02-20T00:00:01Z", "2026-02-20T00:00:00Z"},
	}
	for _, tc := range reversed {
		if _, err := NewDatetimeRange(tc[0], tc[1]); err == nil {
			t.Errorf("NewDatetimeRange(%q, %q) = nil, want an error", tc[0], tc[1])
		} else if !strings.Contains(err.Error(), "starts after it ends") {
			t.Errorf("NewDatetimeRange(%q, %q) error = %v, want an ordering error", tc[0], tc[1], err)
		}
	}
}

// TestDecodeFillerTypeValueMismatch checks that the type tag and the value are
// not independent: the CDDL of spec/01-data-model.md, "Molecules", is a
// discriminated union, so every pairing outside the five alternatives is
// rejected. A value that is well-formed under one alternative is still invalid
// under another — a text string is a legal type 3 value and an illegal type 0
// one.
func TestDecodeFillerTypeValueMismatch(t *testing.T) {
	digest := dcbor.Bytes(testDigest(0x11).Bytes())
	scalar := dcbor.Map{{Key: keyValue, Value: dcbor.Uint(1)}}

	// Types 0, 1 and 2 take a 32-byte digest and nothing else.
	for _, typ := range []FillerType{FillerAtom, FillerBond, FillerMolecule} {
		for _, tc := range []struct {
			name    string
			value   dcbor.Value
			wantErr string
		}{
			{"text", dcbor.Text("bafyrei"), "must be a byte string"},
			{"integer", dcbor.Uint(1), "must be a byte string"},
			{"scalar map", scalar, "must be a byte string"},
			{"array", dcbor.Array{digest}, "must be a byte string"},
			{"null", dcbor.Null, "must be a byte string"},
			{"empty bytes", dcbor.Bytes{}, "digest is 0 bytes"},
			{"31 bytes", dcbor.Bytes(make([]byte, 31)), "digest is 31 bytes"},
			{"33 bytes", dcbor.Bytes(make([]byte, 33)), "digest is 33 bytes"},
			// A 36-byte CID pasted where a digest belongs: the case
			// spec/03-encoding.md, "Internal references", exists to forbid.
			{"36-byte CID", dcbor.Bytes(make([]byte, 36)), "digest is 36 bytes"},
		} {
			t.Run(typ.String()+"/"+tc.name, func(t *testing.T) {
				b := dcbor.MustEncode(fillerMap(typ, tc.value))
				if _, err := DecodeFiller(b); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("DecodeFiller(%x) error = %v, want one containing %q", b, err, tc.wantErr)
				}
			})
		}
	}

	// Type 3 takes a non-empty text string; type 4 a scalar map.
	for _, tc := range []struct {
		name    string
		typ     FillerType
		value   dcbor.Value
		wantErr string
	}{
		{"ipfs/digest", FillerIPFSURI, digest, "must carry a text string"},
		{"ipfs/integer", FillerIPFSURI, dcbor.Uint(3), "must carry a text string"},
		{"ipfs/scalar map", FillerIPFSURI, scalar, "must carry a text string"},
		{"ipfs/null", FillerIPFSURI, dcbor.Null, "must carry a text string"},
		{"ipfs/empty string", FillerIPFSURI, dcbor.Text(""), "IPFS URI filler is empty"},
		{"scalar/digest", FillerScalar, digest, "must be a CBOR map"},
		{"scalar/text", FillerScalar, dcbor.Text("42"), "must be a CBOR map"},
		{"scalar/integer", FillerScalar, dcbor.Uint(42), "must be a CBOR map"},
		{"scalar/array", FillerScalar, dcbor.Array{dcbor.Uint(42)}, "must be a CBOR map"},
		{"scalar/null", FillerScalar, dcbor.Null, "must be a CBOR map"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := dcbor.MustEncode(fillerMap(tc.typ, tc.value))
			if _, err := DecodeFiller(b); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("DecodeFiller(%x) error = %v, want one containing %q", b, err, tc.wantErr)
			}
		})
	}
}

// TestDecodeFillerRejects is the wire-format rejection table for fillers and
// their scalar values.
func TestDecodeFillerRejects(t *testing.T) {
	d := dcbor.Bytes(testDigest(0x11).Bytes())
	short := dcbor.Bytes(make([]byte, 31))

	cases := []struct {
		name    string
		value   dcbor.Value
		wantErr string
	}{
		{"not a map", dcbor.Uint(0), "must be a CBOR map"},
		{"empty map", dcbor.Map{}, "want exactly 2"},
		{"missing value", dcbor.Map{{Key: keyType, Value: dcbor.Uint(0)}}, "want exactly 2"},
		{"missing type", dcbor.Map{{Key: keyValue, Value: d}}, "want exactly 2"},
		{"extra key", dcbor.Map{
			{Key: keyType, Value: dcbor.Uint(0)},
			{Key: keyValue, Value: d},
			{Key: "note", Value: dcbor.Text("x")},
		}, "want exactly 2"},
		{"type 5", fillerMap(FillerType(5), d), "not one of the five types"},
		{"type 255", fillerMap(FillerType(255), d), "not one of the five types"},
		{"atom value is text", fillerMap(FillerAtom, dcbor.Text("digest")), "must be a byte string"},
		{"atom value too short", fillerMap(FillerAtom, short), "digest is 31 bytes"},
		{"atom value too long", fillerMap(FillerAtom, dcbor.Bytes(make([]byte, 36))), "digest is 36 bytes"},
		{"bond value is a map", fillerMap(FillerBond, dcbor.Map{}), "must be a byte string"},
		{"molecule value is null", fillerMap(FillerMolecule, dcbor.Null), "must be a byte string"},
		{"ipfs value is bytes", fillerMap(FillerIPFSURI, d), "must carry a text string"},
		{"ipfs value empty", fillerMap(FillerIPFSURI, dcbor.Text("")), "IPFS URI filler is empty"},
		{"scalar value is bytes", fillerMap(FillerScalar, d), "must be a CBOR map"},
		{"scalar value is an integer", fillerMap(FillerScalar, dcbor.Uint(3)), "must be a CBOR map"},
		{"scalar empty map", fillerMap(FillerScalar, dcbor.Map{}), "must hold"},
		{"scalar value is text", fillerMap(FillerScalar, dcbor.Map{
			{Key: keyValue, Value: dcbor.Text("42")},
		}), "must be an integer or a tag 4 decimal fraction"},
		{"scalar number is bytes", fillerMap(FillerScalar, dcbor.Map{
			{Key: keyValue, Value: d},
		}), "must be an integer or a tag 4 decimal fraction"},
		{"scalar unit only", fillerMap(FillerScalar, dcbor.Map{
			{Key: keyUnit, Value: d},
		}), "must hold"},
		{"scalar unit too short", fillerMap(FillerScalar, dcbor.Map{
			{Key: keyValue, Value: dcbor.Uint(1)},
			{Key: keyUnit, Value: short},
		}), "digest is 31 bytes"},
		{"scalar unit is text", fillerMap(FillerScalar, dcbor.Map{
			{Key: keyValue, Value: dcbor.Uint(1)},
			{Key: keyUnit, Value: dcbor.Text("metre")},
		}), "must be a byte string"},
		{"scalar unknown extra key", fillerMap(FillerScalar, dcbor.Map{
			{Key: keyValue, Value: dcbor.Uint(1)},
			{Key: "scale", Value: dcbor.Uint(2)},
		}), `missing the "unit" key`},
		{"scalar three keys", fillerMap(FillerScalar, dcbor.Map{
			{Key: keyValue, Value: dcbor.Uint(1)},
			{Key: keyUnit, Value: d},
			{Key: "note", Value: dcbor.Text("x")},
		}), "it must hold"},
		{"range missing to", fillerMap(FillerScalar, dcbor.Map{
			{Key: keyFrom, Value: dcbor.Text("2026-02-20T00:00:00Z")},
		}), "want exactly 2"},
		{"range missing from", fillerMap(FillerScalar, dcbor.Map{
			{Key: keyTo, Value: dcbor.Text("2026-02-20T00:00:00Z")},
		}), "want exactly 2"},
		{"range with unit", fillerMap(FillerScalar, dcbor.Map{
			{Key: keyFrom, Value: dcbor.Text("2026-02-20T00:00:00Z")},
			{Key: keyTo, Value: dcbor.Text("2026-02-20T23:59:59Z")},
			{Key: keyUnit, Value: d},
		}), "want exactly 2"},
		{"range endpoint not text", fillerMap(FillerScalar, dcbor.Map{
			{Key: keyFrom, Value: dcbor.Uint(0)},
			{Key: keyTo, Value: dcbor.Text("2026-02-20T23:59:59Z")},
		}), "must be a text string"},
		{"range endpoint is a bare date", datetimeFiller("2026-02-20", "2026-02-20T23:59:59Z"),
			"YYYY-MM-DDTHH:MM:SSZ"},
		{"range endpoint empty", datetimeFiller("2026-02-20T00:00:00Z", ""),
			"timestamp is empty"},
		// The canonical profile, on the wire: every rejected spelling below
		// would otherwise be a second CID for an instant that already has one.
		{"range endpoint with a numeric offset", datetimeFiller("2026-02-19T23:00:00-01:00", "2026-02-20T23:59:59Z"),
			"is 25 bytes"},
		{"range endpoint with a zero offset", datetimeFiller("2026-02-20T00:00:00+00:00", "2026-02-20T23:59:59Z"),
			"is 25 bytes"},
		{"range endpoint with fractional seconds", datetimeFiller("2026-02-20T00:00:00.000Z", "2026-02-20T23:59:59Z"),
			"is 24 bytes"},
		{"range endpoint with lowercase t", datetimeFiller("2026-02-20t00:00:00Z", "2026-02-20T23:59:59Z"),
			`want "T"`},
		{"range endpoint with lowercase z", datetimeFiller("2026-02-20T00:00:00z", "2026-02-20T23:59:59Z"),
			`want "Z"`},
		{"range endpoint with a leap second", datetimeFiller("2026-02-20T00:00:00Z", "2026-12-31T23:59:60Z"),
			"leap second"},
		{"range endpoint is not a real day", datetimeFiller("2026-02-30T00:00:00Z", "2026-03-01T00:00:00Z"),
			"does not denote a real instant"},
		{"range runs backwards", datetimeFiller("2026-02-20T23:59:59Z", "2026-02-20T00:00:00Z"),
			"starts after it ends"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := dcbor.MustEncode(tc.value)
			if _, err := DecodeFiller(b); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("DecodeFiller(%x) error = %v, want one containing %q", b, err, tc.wantErr)
			}
		})
	}

	// A type field that is not an unsigned integer.
	for _, tc := range []struct {
		name  string
		value dcbor.Value
	}{
		{"type is text", dcbor.Text("atom")},
		{"type is negative", dcbor.Neg(0)},
		{"type is a decimal", dcbor.Decimal{Exponent: -1, Mantissa: 1}},
		{"type is bytes", dcbor.Bytes{0x00}},
		{"type is null", dcbor.Null},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := dcbor.MustEncode(dcbor.Map{
				{Key: keyType, Value: tc.value},
				{Key: keyValue, Value: d},
			})
			if _, err := DecodeFiller(b); err == nil || !strings.Contains(err.Error(), "must be an unsigned integer in 0-4") {
				t.Errorf("DecodeFiller(%x) error = %v, want a type-field error", b, err)
			}
		})
	}

	// A non-canonical decimal fraction inside a scalar filler is rejected by
	// the dCBOR layer before the entity rules apply.
	nonCanonical := "a2647479706504" + "6576616c7565" + "a16576616c7565" + "c4820019013a"
	raw, err := hex.DecodeString(nonCanonical)
	if err != nil {
		t.Fatalf("bad hex literal: %v", err)
	}
	if _, err := DecodeFiller(raw); err == nil || !strings.Contains(err.Error(), "is not negative") {
		t.Errorf("DecodeFiller(non-canonical decimal) error = %v, want a canonicalization error", err)
	}
}
