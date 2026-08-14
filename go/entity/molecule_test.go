package entity

import (
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/dcbor"
)

// moleculeMap builds a molecule-shaped dCBOR map directly, so that malformed
// shapes can be expressed.
func moleculeMap(bond dcbor.Value, fillers dcbor.Value) dcbor.Value {
	return dcbor.Map{
		{Key: keyBond, Value: bond},
		{Key: keyFillers, Value: fillers},
	}
}

// TestMoleculeFillerCount checks where the rule "the number of fillers MUST
// equal the number of variables in the referenced bond template"
// (spec/01-data-model.md, "Molecules") is and is not enforceable.
//
// A molecule carries only the bond's digest, so NewMolecule and
// DecodeMolecule cannot check it; NewMoleculeFor and ValidateAgainst can,
// and the specification places the check at block validation
// (spec/02-block-format.md, "Validation" rule 5).
func TestMoleculeFillerCount(t *testing.T) {
	bond := MustBond(capitalTemplate) // two variables
	one := []Filler{AtomFiller(testDigest(1))}
	two := []Filler{AtomFiller(testDigest(1)), AtomFiller(testDigest(2))}
	three := []Filler{AtomFiller(testDigest(1)), AtomFiller(testDigest(2)), AtomFiller(testDigest(3))}

	if _, err := NewMoleculeFor(bond, one); err == nil || !strings.Contains(err.Error(), "1 filler(s) but bond") {
		t.Errorf("NewMoleculeFor with too few fillers: error = %v, want a count mismatch", err)
	}
	if _, err := NewMoleculeFor(bond, three); err == nil || !strings.Contains(err.Error(), "3 filler(s) but bond") {
		t.Errorf("NewMoleculeFor with too many fillers: error = %v, want a count mismatch", err)
	}
	if _, err := NewMoleculeFor(bond, two); err != nil {
		t.Errorf("NewMoleculeFor with the right count: %v", err)
	}

	// The same wrong-count molecule is structurally valid on its own, and is
	// only caught once the bond is resolved.
	m, err := NewMolecule(bond.Digest(), one)
	if err != nil {
		t.Fatalf("NewMolecule (structural validation only): %v", err)
	}
	decoded, err := DecodeMolecule(m.Bytes())
	if err != nil {
		t.Fatalf("DecodeMolecule (structural validation only): %v", err)
	}
	if err := decoded.ValidateAgainst(bond); err == nil || !strings.Contains(err.Error(), "1 filler(s) but bond") {
		t.Errorf("ValidateAgainst: error = %v, want a count mismatch", err)
	}

	// ValidateAgainst also rejects the wrong bond entirely.
	other := MustBond("_A_ founded _B_")
	good, err := NewMoleculeFor(bond, two)
	if err != nil {
		t.Fatalf("NewMoleculeFor: %v", err)
	}
	if err := good.ValidateAgainst(other); err == nil || !strings.Contains(err.Error(), "references bond") {
		t.Errorf("ValidateAgainst(wrong bond): error = %v, want a bond mismatch", err)
	}
	if err := good.ValidateAgainst(Bond{}); err == nil || !strings.Contains(err.Error(), "zero-value Bond") {
		t.Errorf("ValidateAgainst(zero Bond): error = %v", err)
	}
	if _, err := NewMoleculeFor(Bond{}, two); err == nil || !strings.Contains(err.Error(), "zero-value Bond") {
		t.Errorf("NewMoleculeFor(zero Bond): error = %v", err)
	}
}

// TestNewMoleculeRejects covers the constructor-side rules.
func TestNewMoleculeRejects(t *testing.T) {
	if _, err := NewMolecule(testDigest(1), nil); err == nil || !strings.Contains(err.Error(), "no fillers") {
		t.Errorf("NewMolecule with no fillers: error = %v", err)
	}
	if _, err := NewMolecule(testDigest(1), []Filler{}); err == nil || !strings.Contains(err.Error(), "no fillers") {
		t.Errorf("NewMolecule with an empty filler slice: error = %v", err)
	}
	// A filler built by hand rather than by a constructor can still be
	// invalid; NewMolecule catches it.
	if _, err := NewMolecule(testDigest(1), []Filler{{typ: FillerType(9)}}); err == nil || !strings.Contains(err.Error(), "not one of the five types") {
		t.Errorf("NewMolecule with an out-of-range filler type: error = %v", err)
	}
	if _, err := NewMolecule(testDigest(1), []Filler{{typ: FillerScalar}}); err == nil || !strings.Contains(err.Error(), "scalar has no value") {
		t.Errorf("NewMolecule with an empty scalar: error = %v", err)
	}
}

// TestDecodeMoleculeRejects is the wire-format rejection table.
func TestDecodeMoleculeRejects(t *testing.T) {
	digest := dcbor.Bytes(testDigest(0x22).Bytes())
	filler := AtomFiller(testDigest(0x33)).Value()
	fillers := dcbor.Array{filler}

	cases := []struct {
		name    string
		value   dcbor.Value
		wantErr string
	}{
		{"not a map", dcbor.Array{}, "must be a CBOR map"},
		{"empty map", dcbor.Map{}, "want exactly 2"},
		{"missing fillers", dcbor.Map{{Key: keyBond, Value: digest}}, "want exactly 2"},
		{"missing bond", dcbor.Map{{Key: keyFillers, Value: fillers}}, "want exactly 2"},
		{"extra key", dcbor.Map{
			{Key: keyBond, Value: digest},
			{Key: keyFillers, Value: fillers},
			{Key: "author", Value: dcbor.Text("x")},
		}, "want exactly 2"},
		{"wrong key name", dcbor.Map{
			{Key: "bonds", Value: digest},
			{Key: keyFillers, Value: fillers},
		}, `missing the "bond" key`},
		{"bond is text", moleculeMap(dcbor.Text("f295b892"), fillers), "must be a byte string"},
		{"bond is null", moleculeMap(dcbor.Null, fillers), "must be a byte string"},
		{"bond too short", moleculeMap(dcbor.Bytes(make([]byte, 31)), fillers), "digest is 31 bytes"},
		{"bond is a CID", moleculeMap(dcbor.Bytes(make([]byte, 36)), fillers), "digest is 36 bytes"},
		{"fillers not an array", moleculeMap(digest, dcbor.Map{}), "must be an array"},
		{"fillers is text", moleculeMap(digest, dcbor.Text("x")), "must be an array"},
		{"fillers empty", moleculeMap(digest, dcbor.Array{}), "at least one filler"},
		{"filler not a map", moleculeMap(digest, dcbor.Array{dcbor.Uint(0)}), "filler 0"},
		{"second filler malformed", moleculeMap(digest, dcbor.Array{filler, dcbor.Map{
			{Key: keyType, Value: dcbor.Uint(0)},
			{Key: keyValue, Value: dcbor.Text("not a digest")},
		}}), "filler 1"},
		{"nested filler array", moleculeMap(digest, dcbor.Array{dcbor.Array{filler}}), "must be a CBOR map"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := dcbor.MustEncode(tc.value)
			if _, err := DecodeMolecule(b); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("DecodeMolecule(%x) error = %v, want one containing %q", b, err, tc.wantErr)
			}
		})
	}
}

// TestMoleculeAccessors checks copies are independent and the zero value
// refuses to produce an identifier.
func TestMoleculeAccessors(t *testing.T) {
	bond := MustBond(capitalTemplate)
	m := MustMolecule(bond, []Filler{AtomFiller(testDigest(1)), AtomFiller(testDigest(2))})

	fillers := m.Fillers()
	fillers[0] = MoleculeFiller(testDigest(9))
	if got := m.Fillers()[0]; got.Type() != FillerAtom {
		t.Errorf("Fillers() is not a copy: %v", got)
	}
	if m.Bond() != bond.Digest() {
		t.Errorf("Bond() = %s, want %s", m.Bond(), bond.Digest())
	}
	if !strings.Contains(m.String(), "2 filler(s)") {
		t.Errorf("String() = %q", m.String())
	}

	var zero Molecule
	if got := zero.String(); got != "molecule(invalid)" {
		t.Errorf("zero Molecule String() = %q", got)
	}
	assertPanics(t, "zero Molecule Digest", func() { zero.Digest() })
	assertPanics(t, "zero Molecule Bytes", func() { zero.Bytes() })
	assertPanics(t, "zero Molecule CID", func() { zero.CID() })
	assertPanics(t, "MustMolecule with a wrong filler count", func() {
		MustMolecule(bond, []Filler{AtomFiller(testDigest(1))})
	})
}

// TestMoleculeDistinctness checks that filler order and filler type are part
// of a molecule's identity.
func TestMoleculeDistinctness(t *testing.T) {
	bond := MustBond(capitalTemplate)
	a, b := testDigest(1), testDigest(2)

	ab := MustMolecule(bond, []Filler{AtomFiller(a), AtomFiller(b)})
	ba := MustMolecule(bond, []Filler{AtomFiller(b), AtomFiller(a)})
	typed := MustMolecule(bond, []Filler{AtomFiller(a), MoleculeFiller(b)})
	other := MustMolecule(MustBond("_A_ founded _B_"), []Filler{AtomFiller(a), AtomFiller(b)})

	digests := map[string]string{}
	for name, m := range map[string]Molecule{"ab": ab, "ba": ba, "typed": typed, "other bond": other} {
		if prev, ok := digests[m.Digest().String()]; ok {
			t.Errorf("molecules %q and %q share the digest %s", prev, name, m.Digest())
		}
		digests[m.Digest().String()] = name
	}

	// The same molecule built twice is the same molecule: content addressing
	// is author-independent (spec/01-data-model.md, "Content addressing").
	again := MustMolecule(bond, []Filler{AtomFiller(a), AtomFiller(b)})
	if again.Digest() != ab.Digest() {
		t.Errorf("the same molecule built twice has digests %s and %s", again.Digest(), ab.Digest())
	}
}
