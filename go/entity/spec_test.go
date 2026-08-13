package entity

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/cid"
)

// The worked examples of spec/01-data-model.md, "Examples", and of
// spec/03-encoding.md, "Encoding an atom". Every value below is copied from
// the specification, with the line breaks of the hex dumps removed.
const (
	// spec/03-encoding.md, "Encoding an atom".
	franceDescription = "France"
	franceCBOR        = "a16b6465736372697074696f6e664672616e6365"
	franceDigest      = "e57761b439ee0cbb7ef79422b0cce927d7d0147e00a5281cc173b0475512b842"
	franceCIDString   = "bafyreihfo5q3iopobs5x554uekymz2jh27ibi7qauuubzqltwbdvkevyii"

	// spec/01-data-model.md, "Examples" — Atom.
	parisDescription = "Paris, the capital of France"
	parisCBOR        = "a16b6465736372697074696f6e781c50617269732c20746865206361706974616c206f66204672616e6365"
	parisDigest      = "6545050a23d42dbbd9af636a25fb6a373be6ae75f9d928539c594360965411fd"
	parisCIDHex      = "01711220" + parisDigest
	parisCIDString   = "bafyreidfiucqui6ufw55tl3dnis7w2rxhptk45pz3eufhhczinqjmvar7u"

	// spec/01-data-model.md, "Examples" — Bond.
	capitalTemplate = "_A_ is the capital of _B_"
	capitalCBOR     = "a16874656d706c61746578195f415f20697320746865206361706974616c206f66205f425f"
	capitalDigest   = "f295b89289597b4486784ad03d0be8bdab09a0d20070a893afa4f4d307811340"
	capitalCIDHex   = "01711220" + capitalDigest

	// spec/01-data-model.md, "Examples" — Molecule.
	capitalMoleculeCBOR = "a264626f6e645820f295b89289597b4486784ad03d0be8bdab09a0d20070a893" +
		"afa4f4d3078113406766696c6c65727382a26474797065006576616c7565582065" +
		"45050a23d42dbbd9af636a25fb6a373be6ae75f9d928539c594360965411fda264" +
		"74797065006576616c75655820e57761b439ee0cbb7ef79422b0cce927d7d0147e" +
		"00a5281cc173b0475512b842"
	capitalMoleculeDigest = "f9f124b06af6aa7d5f2381462afdeaca628fe3ac8b994253e5c08a3f5d128afb"
	capitalMoleculeCIDHex = "01711220" + capitalMoleculeDigest
)

// TestSpecAtomFrance reproduces the atom of spec/03-encoding.md, "Encoding an
// atom", which is also the atom the cid package's worked example builds by
// hand — the two must agree.
func TestSpecAtomFrance(t *testing.T) {
	a := MustAtom(franceDescription)
	checkBytes(t, "france atom", a.Bytes(), franceCBOR)
	checkDigest(t, "france atom", a.Digest(), franceDigest)
	if got := a.CID().String(); got != franceCIDString {
		t.Errorf("france atom CID text = %s, want %s", got, franceCIDString)
	}
}

// TestSpecAtomParis reproduces the atom example of spec/01-data-model.md,
// including the CID byte dump and the canonical text form the specification
// prints beside it.
func TestSpecAtomParis(t *testing.T) {
	a := MustAtom(parisDescription)
	checkBytes(t, "paris atom", a.Bytes(), parisCBOR)
	checkDigest(t, "paris atom", a.Digest(), parisDigest)
	if got := a.CID().HexString(); got != parisCIDHex {
		t.Errorf("paris atom CID bytes = %s, want %s", got, parisCIDHex)
	}
	if got := a.CID().String(); got != parisCIDString {
		t.Errorf("paris atom CID text = %s, want %s", got, parisCIDString)
	}

	decoded, err := DecodeAtom(a.Bytes())
	if err != nil {
		t.Fatalf("DecodeAtom: %v", err)
	}
	if decoded.Description() != parisDescription {
		t.Errorf("decoded description = %q, want %q", decoded.Description(), parisDescription)
	}
	if decoded.Digest() != a.Digest() {
		t.Errorf("decoded digest = %s, want %s", decoded.Digest(), a.Digest())
	}
}

// TestSpecBondCapitalOf reproduces the bond example of spec/01-data-model.md.
func TestSpecBondCapitalOf(t *testing.T) {
	b := MustBond(capitalTemplate)
	checkBytes(t, "capital bond", b.Bytes(), capitalCBOR)
	checkDigest(t, "capital bond", b.Digest(), capitalDigest)
	if got := b.CID().HexString(); got != capitalCIDHex {
		t.Errorf("capital bond CID bytes = %s, want %s", got, capitalCIDHex)
	}
	if want := []string{"A", "B"}; !equalStrings(b.Variables(), want) {
		t.Errorf("variables = %v, want %v", b.Variables(), want)
	}
}

// TestSpecMoleculeCapitalOfFrance reproduces the molecule example of
// spec/01-data-model.md: "[Paris, the capital of France] is the capital of
// [France]", byte for byte, digest and CID included.
func TestSpecMoleculeCapitalOfFrance(t *testing.T) {
	paris := MustAtom(parisDescription)
	france := MustAtom(franceDescription)
	bond := MustBond(capitalTemplate)

	m, err := NewMoleculeFor(bond, []Filler{AtomFiller(paris.Digest()), AtomFiller(france.Digest())})
	if err != nil {
		t.Fatalf("NewMoleculeFor: %v", err)
	}
	checkBytes(t, "capital molecule", m.Bytes(), capitalMoleculeCBOR)
	checkDigest(t, "capital molecule", m.Digest(), capitalMoleculeDigest)
	if got := m.CID().HexString(); got != capitalMoleculeCIDHex {
		t.Errorf("capital molecule CID bytes = %s, want %s", got, capitalMoleculeCIDHex)
	}

	decoded, err := DecodeMolecule(m.Bytes())
	if err != nil {
		t.Fatalf("DecodeMolecule: %v", err)
	}
	if decoded.Digest() != m.Digest() {
		t.Errorf("decoded digest = %s, want %s", decoded.Digest(), m.Digest())
	}
	if err := decoded.ValidateAgainst(bond); err != nil {
		t.Errorf("ValidateAgainst(bond): %v", err)
	}
	if got := decoded.Bond(); got != bond.Digest() {
		t.Errorf("decoded bond = %s, want %s", got, bond.Digest())
	}
	fillers := decoded.Fillers()
	if len(fillers) != 2 {
		t.Fatalf("decoded %d filler(s), want 2", len(fillers))
	}
	for i, want := range []cid.Digest{paris.Digest(), france.Digest()} {
		if fillers[i].Type() != FillerAtom {
			t.Errorf("filler %d type = %s, want atom", i, fillers[i].Type())
		}
		if got, ok := fillers[i].Ref(); !ok || got != want {
			t.Errorf("filler %d ref = %s, want %s", i, got, want)
		}
	}
}

// TestSpecMetaBonds pins the five standard meta-bonds of spec/06-meta-bonds.md.
//
// The specification prints no digests for them, so these values are computed
// by this implementation from the template strings it quotes verbatim. They
// are the identifiers by which every implementation recognizes a
// meta-molecule (spec/06-meta-bonds.md, "Meta-molecules are regular
// molecules"), so pinning them here is what keeps them stable across changes
// to this package, and they belong in the conformance vectors.
func TestSpecMetaBonds(t *testing.T) {
	cases := []struct {
		name     string
		bond     Bond
		template string
		vars     []string
		cbor     string
		digest   string
		cidText  string
	}{
		{
			name: "equivalence", bond: MetaBondEquivalence, template: TemplateEquivalence,
			vars:   []string{"A", "B"},
			cbor:   "a16874656d706c617465765f415f206973207468652073616d65206173205f425f",
			digest: "6d6f0db1c36db5247eab0b51e16f22704b796ea1505d36e1e5e93e47d45d31fb",
			// spec/06-meta-bonds.md gives no text form; this is the canonical
			// multibase base32 of spec/03-encoding.md.
			cidText: "bafyreidnn4g3dq3nwush5kylkhqw6itqjn4w5ikqlu3odzpjhzd5ixjr7m",
		},
		{
			name: "truth assertion", bond: MetaBondTruthAssertion, template: TemplateTruthAssertion,
			vars:    []string{"A"},
			cbor:    "a16874656d706c6174656b5f415f2069732074727565",
			digest:  "d6dc10d7c503ab4ac9caa453eb3a32984abc885cfa850740e97c9f0881ba5be3",
			cidText: "bafyreigw3qinpridvnfmtsvekpvtumuyjk6iqxh2qudub2l4t4eidos34m",
		},
		{
			name: "truth retraction", bond: MetaBondTruthRetraction, template: TemplateTruthRetraction,
			vars:    []string{"A"},
			cbor:    "a16874656d706c6174656d5f415f20697320756e74727565",
			digest:  "f166a1ddfcdc18cbd101601b9ae717a940c0f176da3a1a66912bdcaf413ab06c",
			cidText: "bafyreihrm2q537g4ddf5caladonoof5jidapc5w2hingnejl3sxucovqnq",
		},
		{
			name: "contradiction", bond: MetaBondContradiction, template: TemplateContradiction,
			vars:    []string{"A", "B"},
			cbor:    "a16874656d706c617465735f415f20636f6e7472616469637473205f425f",
			digest:  "0e2b8fc4d1e53e5d5866b43b4054b18cc54f5077b313d9f657fd4d3c456d81e3",
			cidText: "bafyreiaofoh4jupfhzovqzvuhnafjmmmyvhva55tcpm7mv75ju6ek3mb4m",
		},
		{
			name: "supersession", bond: MetaBondSupersession, template: TemplateSupersession,
			vars:    []string{"A", "B"},
			cbor:    "a16874656d706c617465725f415f2073757065727365646573205f425f",
			digest:  "63d8f626dc5cbf19013da53635ed3cd37bcf0fbd33def852101ff4bc4cc0c66b",
			cidText: "bafyreidd3d3cnxc4x4mqcpnfgy262pgtpphq7pjt334feea76s6ezqggnm",
		},
	}

	if got := len(StandardMetaBonds()); got != len(cases) {
		t.Fatalf("StandardMetaBonds returned %d bonds, want %d", got, len(cases))
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.bond.Template() != tc.template {
				t.Errorf("template = %q, want %q", tc.bond.Template(), tc.template)
			}
			if !equalStrings(tc.bond.Variables(), tc.vars) {
				t.Errorf("variables = %v, want %v", tc.bond.Variables(), tc.vars)
			}
			checkBytes(t, tc.name, tc.bond.Bytes(), tc.cbor)
			checkDigest(t, tc.name, tc.bond.Digest(), tc.digest)
			if got := tc.bond.CID().String(); got != tc.cidText {
				t.Errorf("CID text = %s, want %s", got, tc.cidText)
			}
			if got := StandardMetaBonds()[i]; got.Digest() != tc.bond.Digest() {
				t.Errorf("StandardMetaBonds()[%d] = %s, want %s", i, got, tc.bond)
			}
			if !IsMetaBond(tc.bond.Digest()) {
				t.Errorf("IsMetaBond(%s) = false, want true", tc.bond.Digest())
			}
			looked, ok := LookupMetaBond(tc.bond.Digest())
			if !ok || looked.Template() != tc.template {
				t.Errorf("LookupMetaBond(%s) = (%v, %v), want the %s meta-bond", tc.bond.Digest(), looked, ok, tc.name)
			}
		})
	}

	// An ordinary bond is not a meta-bond.
	other := MustBond(capitalTemplate)
	if IsMetaBond(other.Digest()) {
		t.Errorf("IsMetaBond(%s) = true for a non-meta bond", other.Digest())
	}
	if _, ok := LookupMetaBond(other.Digest()); ok {
		t.Errorf("LookupMetaBond found a non-meta bond")
	}
}

// TestSpecMetaMoleculeEquivalence builds the atom-equivalence meta-molecule
// of spec/06-meta-bonds.md, "Declaring atom equivalence", and checks it
// against the two truncated CIDs the example prints for the atoms it
// references.
func TestSpecMetaMoleculeEquivalence(t *testing.T) {
	parisCapital := MustAtom(parisDescription)
	parisFrance := MustAtom("Paris, France")
	if got := parisCapital.CID().HexString(); !strings.HasPrefix(got, "017112206545050a") {
		t.Errorf(`"Paris, the capital of France" CID = %s, want the prefix 017112206545050a of spec/06`, got)
	}

	m, err := NewMoleculeFor(MetaBondEquivalence, []Filler{
		AtomFiller(parisCapital.Digest()),
		AtomFiller(parisFrance.Digest()),
	})
	if err != nil {
		t.Fatalf("NewMoleculeFor(equivalence): %v", err)
	}
	if !IsMetaBond(m.Bond()) {
		t.Errorf("the equivalence molecule's bond is not recognized as a meta-bond")
	}

	// The fillers must both be the same type (spec/06-meta-bonds.md, §1). That
	// rule is a meta-bond semantic, not a data-model rule, so this package
	// does not enforce it; the molecule below is structurally valid.
	mixed, err := NewMoleculeFor(MetaBondEquivalence, []Filler{
		AtomFiller(parisCapital.Digest()),
		BondFiller(MustBond(capitalTemplate).Digest()),
	})
	if err != nil {
		t.Fatalf("NewMoleculeFor(equivalence, mixed types): %v", err)
	}
	if mixed.Digest() == m.Digest() {
		t.Errorf("molecules with different fillers share a digest")
	}
}

// TestSpecFillerTypeTable walks the filler-type table of
// spec/01-data-model.md, "Filler types", checking the tag of each type and
// the shape of the value it carries.
func TestSpecFillerTypeTable(t *testing.T) {
	d := MustAtom(franceDescription).Digest()
	scalar, err := ScalarFiller(IntScalar(42))
	if err != nil {
		t.Fatalf("ScalarFiller: %v", err)
	}
	ipfs, err := IPFSFiller("bafyreihfo5q3iopobs5x554uekymz2jh27ibi7qauuubzqltwbdvkevyii")
	if err != nil {
		t.Fatalf("IPFSFiller: %v", err)
	}

	cases := []struct {
		filler Filler
		tag    FillerType
		isRef  bool
	}{
		{AtomFiller(d), 0, true},
		{BondFiller(d), 1, true},
		{MoleculeFiller(d), 2, true},
		{ipfs, 3, false},
		{scalar, 4, false},
	}
	for _, tc := range cases {
		t.Run(tc.tag.String(), func(t *testing.T) {
			if tc.filler.Type() != tc.tag {
				t.Errorf("type = %d, want %d", tc.filler.Type(), tc.tag)
			}
			if tc.tag.IsRef() != tc.isRef {
				t.Errorf("IsRef = %v, want %v", tc.tag.IsRef(), tc.isRef)
			}
			ref, ok := tc.filler.Ref()
			if ok != tc.isRef {
				t.Errorf("Ref ok = %v, want %v", ok, tc.isRef)
			}
			if tc.isRef && ref != d {
				t.Errorf("Ref = %s, want %s", ref, d)
			}
			got, err := DecodeFiller(tc.filler.Bytes())
			if err != nil {
				t.Fatalf("DecodeFiller: %v", err)
			}
			if got.Type() != tc.filler.Type() {
				t.Errorf("round-tripped type = %s, want %s", got.Type(), tc.filler.Type())
			}
		})
	}
}

// TestSpecDecimalFillerExample places the 3.14 decimal fraction of
// spec/03-encoding.md, "Encoding a decimal fraction", inside a scalar filler
// and checks the resulting bytes.
func TestSpecDecimalFillerExample(t *testing.T) {
	s, err := DecimalScalar(-2, 314)
	if err != nil {
		t.Fatalf("DecimalScalar: %v", err)
	}
	f, err := ScalarFiller(s)
	if err != nil {
		t.Fatalf("ScalarFiller: %v", err)
	}
	// a2                        map(2)
	//   64 74797065  04         "type": 4
	//   65 76616c7565           "value":
	//     a1 65 76616c7565      map(1) "value":
	//       c4 82 21 19013a     3.14 as tag 4 [-2, 314]
	want := "a2647479706504" + "6576616c7565" + "a16576616c7565" + "c4822119013a"
	checkBytes(t, "3.14 scalar filler", f.Bytes(), want)
}

func checkBytes(t *testing.T, what string, got []byte, wantHex string) {
	t.Helper()
	if h := hex.EncodeToString(got); h != wantHex {
		t.Errorf("%s dCBOR = %s, want %s", what, h, wantHex)
	}
}

func checkDigest(t *testing.T, what string, got cid.Digest, wantHex string) {
	t.Helper()
	if got.String() != wantHex {
		t.Errorf("%s digest = %s, want %s", what, got, wantHex)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
