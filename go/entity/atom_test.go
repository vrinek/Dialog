package entity

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/dcbor"
)

// TestNewAtomRejects covers the atom validity rules of
// spec/01-data-model.md, "Atoms".
func TestNewAtomRejects(t *testing.T) {
	cases := []struct {
		name        string
		description string
		wantErr     string
	}{
		{"empty", "", "is empty"},
		{"invalid UTF-8", "Fran\xffce", "not valid UTF-8"},
		{"lone surrogate", "\xed\xa0\x80", "not valid UTF-8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewAtom(tc.description); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("NewAtom(%q) error = %v, want one containing %q", tc.description, err, tc.wantErr)
			}
		})
	}
}

// TestAtomDistinctness is the property the data model rests on: any
// difference in the description, however minor, is a different atom
// (spec/01-data-model.md, "Atoms"; spec/03-encoding.md, "Text strings and
// Unicode" for the no-normalization rule).
func TestAtomDistinctness(t *testing.T) {
	descriptions := []string{
		"Paris, the capital of France",
		"Paris, France",
		"France",
		"france",
		"New York",
		"New  York",
		"é",  // é as U+00E9
		"é", // é as e + combining acute
		" France",
		"France ",
	}
	seen := make(map[string]string, len(descriptions))
	for _, d := range descriptions {
		a := MustAtom(d)
		if prev, ok := seen[a.Digest().String()]; ok {
			t.Errorf("atoms %q and %q share the digest %s", prev, d, a.Digest())
		}
		seen[a.Digest().String()] = d
	}
}

// TestDecodeAtomRejects is the wire-format half of the atom rules.
func TestDecodeAtomRejects(t *testing.T) {
	cases := []struct {
		name    string
		value   dcbor.Value
		wantErr string
	}{
		{"empty map", dcbor.Map{}, "want exactly 1"},
		{"wrong key", dcbor.Map{{Key: "desc", Value: dcbor.Text("France")}}, `missing the "description" key`},
		{"extra key", dcbor.Map{
			{Key: keyDescription, Value: dcbor.Text("France")},
			{Key: "lang", Value: dcbor.Text("en")},
		}, "want exactly 1"},
		{"description not text", dcbor.Map{{Key: keyDescription, Value: dcbor.Uint(1)}}, "must be a text string"},
		{"description is bytes", dcbor.Map{{Key: keyDescription, Value: dcbor.Bytes("France")}}, "must be a text string"},
		{"description is null", dcbor.Map{{Key: keyDescription, Value: dcbor.Null}}, "must be a text string"},
		{"empty description", dcbor.Map{{Key: keyDescription, Value: dcbor.Text("")}}, "is empty"},
		{"not a map", dcbor.Text("France"), "must be a CBOR map"},
		{"array", dcbor.Array{dcbor.Text("France")}, "must be a CBOR map"},
		{"integer", dcbor.Uint(1), "must be a CBOR map"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := dcbor.MustEncode(tc.value)
			if _, err := DecodeAtom(b); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("DecodeAtom(%x) error = %v, want one containing %q", b, err, tc.wantErr)
			}
		})
	}
}

// TestDecodeAtomRejectsNonCanonical checks that the dCBOR profile's rules
// reach the entity API: a structurally correct atom encoded non-canonically
// is not an atom (spec/03-encoding.md, "Deterministic CBOR").
func TestDecodeAtomRejectsNonCanonical(t *testing.T) {
	cases := []struct {
		name    string
		hex     string
		wantErr string
	}{
		// {"description": "France"} with a one-byte length for the key.
		{"non-shortest key length", "a1780b6465736372697074696f6e664672616e6365", "shortest form"},
		// Indefinite-length map.
		{"indefinite map", "bf6b6465736372697074696f6e664672616e6365ff", "indefinite-length map"},
		// The canonical atom followed by a stray byte.
		{"trailing byte", franceCBOR + "00", "trailing byte"},
		// Invalid UTF-8 in the description, rejected by the decoder.
		{"invalid UTF-8", "a16b6465736372697074696f6e6180", "not valid UTF-8"},
		// A duplicate key.
		{"duplicate key", "a26b6465736372697074696f6e664672616e63656b6465736372697074696f6e664672616e6365", "duplicate map key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := hex.DecodeString(strings.ReplaceAll(tc.hex, " ", ""))
			if err != nil {
				t.Fatalf("bad hex literal: %v", err)
			}
			if _, err := DecodeAtom(b); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("DecodeAtom(%s) error = %v, want one containing %q", tc.hex, err, tc.wantErr)
			}
		})
	}
}

// TestAtomZeroValue checks that the zero Atom refuses to produce an
// identifier rather than handing back the digest of an empty structure.
func TestAtomZeroValue(t *testing.T) {
	var zero Atom
	if got := zero.String(); got != "atom(invalid)" {
		t.Errorf("zero Atom String() = %q", got)
	}
	assertPanics(t, "zero Atom Digest", func() { zero.Digest() })
	assertPanics(t, "zero Atom Bytes", func() { zero.Bytes() })
	assertPanics(t, "zero Atom CID", func() { zero.CID() })
}

// TestMustAtomPanics checks the Must* helpers fail loudly on invalid input.
func TestMustAtomPanics(t *testing.T) {
	assertPanics(t, "MustAtom(\"\")", func() { MustAtom("") })
	assertPanics(t, "MustBond(\"no vars\")", func() { MustBond("no vars") })
}
