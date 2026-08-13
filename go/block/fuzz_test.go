package block

import (
	"bytes"
	"testing"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/entity"
)

// FuzzDecodeBlock asserts the two properties that make a decoded block
// trustworthy: Decode never panics, whatever the input, and every block it
// accepts is canonical and verified — re-encoding reproduces the input byte for
// byte, the digest is the hash of those bytes, and the signature checks out.
//
// The seeds are a valid block of each type plus a handful of near misses, so
// the fuzzer starts next to the boundaries rather than in random noise.
func FuzzDecodeBlock(f *testing.F) {
	author, err := NewBuilder(testKey(f, 1))
	if err != nil {
		f.Fatalf("NewBuilder: %v", err)
	}
	bond := entity.MustBond("_A_ is the capital of _B_")
	genesis, err := author.Public(1740067200, nil,
		MustCreateBond(bond.Template()),
		MustCreateAtom("Paris, the capital of France"),
		MustCreateAtom("France"),
		MustCreateMolecule(bond, []entity.Filler{
			entity.AtomFiller(entity.MustAtom("Paris, the capital of France").Digest()),
			entity.AtomFiller(entity.MustAtom("France").Digest()),
		}))
	if err != nil {
		f.Fatalf("genesis: %v", err)
	}
	second, err := author.Public(1740153600, []cid.Digest{genesis.Digest()}, MustCreateAtom("Germany"))
	if err != nil {
		f.Fatalf("second: %v", err)
	}
	rotation, err := author.Rotation(1740240000, nil, testPub(f, 2))
	if err != nil {
		f.Fatalf("rotation: %v", err)
	}
	private, err := mustBuilder(f, 3).Private([]byte("ciphertext"), bytes.Repeat([]byte{5}, NonceSize))
	if err != nil {
		f.Fatalf("private: %v", err)
	}
	for _, b := range []*Block{genesis, second, rotation, private} {
		f.Add(b.Bytes())
	}
	// Near misses: a truncation, a flipped signature bit and a bare CBOR map.
	f.Add(genesis.Bytes()[:len(genesis.Bytes())/2])
	flipped := genesis.Bytes()
	flipped[len(flipped)-1] ^= 0x01
	f.Add(flipped)
	f.Add([]byte{0xa0})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		b, err := Decode(data)
		if err != nil {
			return // rejected input carries no obligation
		}
		if got := b.Bytes(); !bytes.Equal(got, data) {
			t.Fatalf("re-encoding an accepted block gave %x, want %x", got, data)
		}
		if got, want := b.Digest(), cid.SumDigest(data); got != want {
			t.Fatalf("digest = %s, want the hash of the accepted bytes %s", got, want)
		}
		if err := b.Verify(); err != nil {
			t.Fatalf("Decode accepted a block whose signature does not verify: %v", err)
		}
		again, err := Decode(b.Bytes())
		if err != nil {
			t.Fatalf("Decode rejected its own output: %v", err)
		}
		if again.Digest() != b.Digest() {
			t.Fatalf("re-decoding changed the digest: %s -> %s", b.Digest(), again.Digest())
		}
	})
}
