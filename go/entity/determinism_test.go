package entity

import (
	"bytes"
	"testing"

	"github.com/vrinek/Dialog/go/cid"
)

// An entity's identity is the SHA-256 of its canonical dCBOR encoding
// (spec/03-encoding.md, "Content identifiers"). Two implementations that agree
// on a value and disagree on its bytes disagree on its identity, and so does
// one implementation that encodes the same value twice and gets two answers.
//
// dcbor/determinism_test.go makes the same argument about the encoder itself.
// This file makes it about the layer above, where the values are built: the
// order of a molecule's fillers is meaningful and fixed, the order of a map's
// keys is meaningless and fixed, and nothing in between may depend on anything
// the runtime is free to vary.

// determinismIterations is how many times each entity is rebuilt and
// re-encoded from scratch. Rebuilding, rather than re-encoding one instance,
// is what makes the test able to see a cached or memoised encoding that is
// only correct by accident.
const determinismIterations = 1000

// addressable is what every content-addressed entity can do.
type addressable interface {
	Bytes() []byte
	Digest() cid.Digest
}

type determinismCase struct {
	name string
	// build constructs the entity from nothing, so that every iteration
	// exercises the whole path from constructor to bytes.
	build func() addressable
	// decode parses the canonical encoding back, for the round-trip half.
	decode func([]byte) (addressable, error)
}

func determinismCases() []determinismCase {
	unit := testDigest(9)
	decimal, err := DecimalScalar(-2, 12345)
	if err != nil {
		panic(err) // a constant, canonical by construction
	}
	withUnit, err := IntScalar(180).WithUnit(unit)
	if err != nil {
		panic(err)
	}
	rangeScalar, err := NewDatetimeRange("2025-02-20T00:00:00Z", "2025-02-20T23:59:59Z")
	if err != nil {
		panic(err)
	}
	fillers := []Filler{
		AtomFiller(testDigest(1)),
		BondFiller(testDigest(2)),
		MoleculeFiller(testDigest(3)),
		mustFiller(IPFSFiller("ipfs://bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi")),
		mustFiller(ScalarFiller(decimal)),
		mustFiller(ScalarFiller(withUnit)),
		mustFiller(ScalarFiller(rangeScalar)),
	}

	return []determinismCase{
		{
			name:   "atom",
			build:  func() addressable { return MustAtom(franceDescription) },
			decode: func(b []byte) (addressable, error) { return DecodeAtom(b) },
		},
		{
			name:   "atom with combining characters",
			build:  func() addressable { return MustAtom("Ελλάδα — Kévin — 日本") },
			decode: func(b []byte) (addressable, error) { return DecodeAtom(b) },
		},
		{
			name:   "bond",
			build:  func() addressable { return MustBond(capitalTemplate) },
			decode: func(b []byte) (addressable, error) { return DecodeBond(b) },
		},
		{
			name:   "meta-bond",
			build:  func() addressable { return MustBond(TemplateSupersession) },
			decode: func(b []byte) (addressable, error) { return DecodeBond(b) },
		},
		{
			name: "molecule of every filler type",
			build: func() addressable {
				m, err := NewMolecule(MustBond(capitalTemplate).Digest(), fillers)
				if err != nil {
					panic(err)
				}
				return m
			},
			decode: func(b []byte) (addressable, error) { return DecodeMolecule(b) },
		},
		{
			name: "molecule of the spec's worked example",
			build: func() addressable {
				return MustMolecule(MustBond(capitalTemplate), []Filler{
					AtomFiller(MustAtom(parisDescription).Digest()),
					AtomFiller(MustAtom(franceDescription).Digest()),
				})
			},
			decode: func(b []byte) (addressable, error) { return DecodeMolecule(b) },
		},
	}
}

func mustFiller(f Filler, err error) Filler {
	if err != nil {
		panic(err)
	}
	return f
}

// TestEntityEncodingIsDeterministic rebuilds and re-encodes each entity
// determinismIterations times in one process and requires identical bytes and
// an identical digest every time.
func TestEntityEncodingIsDeterministic(t *testing.T) {
	for _, tc := range determinismCases() {
		t.Run(tc.name, func(t *testing.T) {
			first := tc.build()
			wantBytes, wantDigest := first.Bytes(), first.Digest()

			for i := range determinismIterations {
				e := tc.build()
				if got := e.Bytes(); !bytes.Equal(got, wantBytes) {
					t.Fatalf("iteration %d encoded to %x, the first encoding was %x", i, got, wantBytes)
				}
				if got := e.Digest(); got != wantDigest {
					t.Fatalf("iteration %d has digest %s, the first had %s", i, got, wantDigest)
				}
				// The digest of one instance must also not drift between
				// calls, which is the cheaper mistake to make.
				if got := e.Digest(); got != wantDigest {
					t.Fatalf("iteration %d changed its own digest between two calls", i)
				}
			}
		})
	}
}

// TestEntityEncodingSurvivesDecoding checks that decoding an entity and
// encoding it again reproduces the bytes exactly — and therefore the digest.
// An entity that changed identity by being transmitted would make content
// addressing useless.
func TestEntityEncodingSurvivesDecoding(t *testing.T) {
	for _, tc := range determinismCases() {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.build()
			encoded := e.Bytes()

			decoded, err := tc.decode(encoded)
			if err != nil {
				t.Fatalf("decoding our own encoding: %v", err)
			}
			if got := decoded.Bytes(); !bytes.Equal(got, encoded) {
				t.Fatalf("re-encoding after a decode gave %x, want %x", got, encoded)
			}
			if got := decoded.Digest(); got != e.Digest() {
				t.Fatalf("the decoded entity has digest %s, the original %s", got, e.Digest())
			}
		})
	}
}

// TestFillerEncodingIsDeterministic is the same property one level down. A
// filler is not content-addressed on its own — it has no digest — but it is
// part of the bytes a molecule is addressed by, so an unstable filler encoding
// is an unstable molecule identity.
func TestFillerEncodingIsDeterministic(t *testing.T) {
	unit := testDigest(9)
	withUnit, err := IntScalar(180).WithUnit(unit)
	if err != nil {
		t.Fatalf("WithUnit: %v", err)
	}
	decimal, err := DecimalScalar(-3, 1234)
	if err != nil {
		t.Fatalf("DecimalScalar: %v", err)
	}
	span, err := NewDatetimeRange("2025-02-20T00:00:00Z", "2025-02-21T00:00:00Z")
	if err != nil {
		t.Fatalf("NewDatetimeRange: %v", err)
	}

	cases := []struct {
		name string
		f    Filler
	}{
		{"atom ref", AtomFiller(testDigest(1))},
		{"bond ref", BondFiller(testDigest(2))},
		{"molecule ref", MoleculeFiller(testDigest(3))},
		{"ipfs uri", mustFiller(IPFSFiller("ipfs://bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"))},
		{"integer scalar", mustFiller(ScalarFiller(IntScalar(-42)))},
		{"decimal scalar", mustFiller(ScalarFiller(decimal))},
		{"scalar with a unit", mustFiller(ScalarFiller(withUnit))},
		{"datetime range", mustFiller(ScalarFiller(span))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.f.Bytes()
			for i := range determinismIterations {
				if got := tc.f.Bytes(); !bytes.Equal(got, want) {
					t.Fatalf("iteration %d encoded to %x, the first encoding was %x", i, got, want)
				}
			}
			back, err := DecodeFiller(want)
			if err != nil {
				t.Fatalf("DecodeFiller of our own encoding: %v", err)
			}
			if got := back.Bytes(); !bytes.Equal(got, want) {
				t.Fatalf("re-encoding after a decode gave %x, want %x", got, want)
			}
		})
	}
}

// TestSpecDigestsAreStable pins the digests the specification writes down, and
// checks them after a thousand rebuilds. The conformance vectors pin the same
// values from outside this package; this is the in-process half, and it is
// what fails first if an encoder change makes identity depend on the run.
func TestSpecDigestsAreStable(t *testing.T) {
	atoms := []struct{ description, digest string }{
		{franceDescription, franceDigest},
		{parisDescription, parisDigest},
	}
	for _, a := range atoms {
		for i := range determinismIterations {
			if got := MustAtom(a.description).Digest().String(); got != a.digest {
				t.Fatalf("iteration %d: digest of %q is %s, want %s", i, a.description, got, a.digest)
			}
		}
	}
	for i := range determinismIterations {
		if got := MustBond(capitalTemplate).Digest().String(); got != capitalDigest {
			t.Fatalf("iteration %d: digest of the capital-of bond is %s, want %s", i, got, capitalDigest)
		}
	}
}
