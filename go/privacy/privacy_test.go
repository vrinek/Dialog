package privacy

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
)

// Test keys and nonces are derived from fixed seeds, so that every byte these
// tests produce — ciphertexts, wrapped keys, block digests — is reproducible
// run to run and machine to machine.
func testKey(t testing.TB, seed byte) ed25519.PrivateKey {
	t.Helper()
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
}

func testPub(t testing.TB, seed byte) ed25519.PublicKey {
	t.Helper()
	pub, ok := testKey(t, seed).Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("test key does not yield an Ed25519 public key")
	}
	return pub
}

func testBuilder(t testing.TB, seed byte) *block.Builder {
	t.Helper()
	b, err := block.NewBuilder(testKey(t, seed))
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	return b
}

// fixedRand is the injectable randomness source: an endless repetition of one
// byte. Nonce uniqueness is a MUST in production and the enemy of a pinned test
// vector, so the source is a parameter and this is what the tests pass.
type fixedRand byte

func (r fixedRand) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}

// failingRand stands in for an exhausted entropy source.
type failingRand struct{}

func (failingRand) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func testContentKey(t testing.TB, seed byte) Key {
	t.Helper()
	k, err := GenerateKey(fixedRand(seed))
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return k
}

func samplePayload() block.Payload {
	return block.Payload{
		TS:  1740067200,
		Ops: []block.Operation{block.MustCreateAtom("My private note")},
	}
}

// TestRoundTrip is the whole of the author-and-recipient path: a chain key is
// wrapped for two readers, the author seals a block under it, each reader
// unwraps and opens it, and a third party with a key of their own cannot.
func TestRoundTrip(t *testing.T) {
	authorPriv := testKey(t, 1)
	alice, bob, mallory := testKey(t, 2), testKey(t, 3), testKey(t, 4)

	key := testContentKey(t, 0x11)
	recipients, err := WrapFor(key, authorPriv, []ed25519.PublicKey{
		testPub(t, 2), testPub(t, 3),
	}, fixedRand(0x22))
	if err != nil {
		t.Fatalf("WrapFor: %v", err)
	}
	if len(recipients) != 2 {
		t.Fatalf("WrapFor returned %d recipients, want 2", len(recipients))
	}

	author := testBuilder(t, 1)
	b, err := SealBlock(author, key, samplePayload(), fixedRand(0x33))
	if err != nil {
		t.Fatalf("SealBlock: %v", err)
	}
	if b.Type() != block.TypePrivate {
		t.Fatalf("SealBlock produced a %s block", b.Type())
	}

	for i, priv := range []ed25519.PrivateKey{alice, bob} {
		unwrapped, err := recipients[i].Open(priv, testPub(t, 1))
		if err != nil {
			t.Fatalf("recipient %d: Open: %v", i, err)
		}
		if unwrapped != key {
			t.Fatalf("recipient %d unwrapped a different content key", i)
		}
		p, err := Open(b, unwrapped)
		if err != nil {
			t.Fatalf("recipient %d: Open block: %v", i, err)
		}
		if p.TS != 1740067200 || len(p.Ops) != 1 {
			t.Fatalf("recipient %d recovered %+v, want the sealed payload", i, p)
		}
		if got := p.Ops[0].(block.CreateAtom).Description(); got != "My private note" {
			t.Errorf("recipient %d recovered the operation %q", i, got)
		}
	}

	t.Run("non-recipient", func(t *testing.T) {
		for i := range recipients {
			if _, err := recipients[i].Open(mallory, testPub(t, 1)); !errors.Is(err, ErrAuthentication) {
				t.Errorf("recipient %d unwrapped for a non-recipient: %v", i, err)
			}
		}
		// And with no wrapped key at all, a guessed content key opens nothing.
		if _, err := Open(b, testContentKey(t, 0x99)); !errors.Is(err, ErrAuthentication) {
			t.Errorf("Open with the wrong content key = %v, want ErrAuthentication", err)
		}
	})

	t.Run("the block itself is opaque to a node without the key", func(t *testing.T) {
		// What the wire carries is a block whose refs, ts and ops are nowhere in
		// its bytes (spec/02-block-format.md, "Security Considerations").
		wire := b.Bytes()
		if bytes.Contains(wire, []byte("My private note")) {
			t.Error("the operation's text is readable in the block's encoding")
		}
		decoded, err := block.Decode(wire)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		p, err := Open(decoded, key)
		if err != nil {
			t.Fatalf("Open after a wire round trip: %v", err)
		}
		if p.TS != 1740067200 {
			t.Errorf("payload after a wire round trip = %+v", p)
		}
	})
}

// TestOpenAndValidate runs a decrypted block through the rules a key holder is
// the only one able to check: reachability of the entities its operations name
// (rule 4), through its own chain and through the refs graph.
func TestOpenAndValidate(t *testing.T) {
	store := block.NewMemStore()
	key := testContentKey(t, 0x11)

	// A foreign author publishes the bond in the clear.
	foreign := testBuilder(t, 9)
	bond := entity.MustBond("_A_ is the capital of _B_")
	provider, err := foreign.Public(1000, nil, block.MustCreateBond(bond.Template()))
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	store.MustAdd(provider)

	// The author's private chain: a genesis block defining two atoms, then a
	// block whose molecule uses them and the foreign bond.
	author := testBuilder(t, 1)
	paris, france := entity.MustAtom("Paris, the capital of France"), entity.MustAtom("France")
	genesis, err := SealBlock(author, key, block.Payload{
		TS:  1000,
		Ops: []block.Operation{block.MustCreateAtom(paris.Description()), block.MustCreateAtom(france.Description())},
	}, fixedRand(0x33))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	molecule := block.MustCreateMolecule(bond, []entity.Filler{
		entity.AtomFiller(paris.Digest()), entity.AtomFiller(france.Digest()),
	})
	second, err := SealBlock(author, key, block.Payload{
		TS:   2000,
		Refs: []cid.Digest{provider.Digest()},
		Ops:  []block.Operation{molecule},
	}, fixedRand(0x44))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	store.MustAdd(genesis, second)

	// Without the key, four rules go unchecked.
	report, err := block.Validate(second, store, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !slices.Equal(report.Unchecked, []int{4, 5, 6, 10}) {
		t.Fatalf("Unchecked = %v, want rules 4, 5, 6 and 10", report.Unchecked)
	}

	// With it, all ten are checked and nothing is left over.
	p, full, err := OpenAndValidate(second, key, store, nil)
	if err != nil {
		t.Fatalf("OpenAndValidate: %v", err)
	}
	if len(full.Unchecked) != 0 {
		t.Errorf("Unchecked = %v, want nothing left unchecked", full.Unchecked)
	}
	for _, w := range full.Warnings {
		if w.Msg == block.PrivateBlockNotice {
			t.Error("the report still carries the notice that the rules could not be checked")
		}
	}
	if full.Scanned != 1 {
		t.Errorf("Scanned = %d, want the one foreign block the refs graph needed", full.Scanned)
	}
	if len(p.Refs) != 1 || p.Refs[0] != provider.Digest() {
		t.Errorf("recovered refs = %v, want the foreign provider", p.Refs)
	}

	t.Run("unreachable entity", func(t *testing.T) {
		// The same molecule without the ref: rule 4 has nowhere to resolve the
		// bond, and only a key holder can find that out.
		lonely := testBuilder(t, 5)
		bad, err := SealBlock(lonely, key, block.Payload{
			TS:  1000,
			Ops: []block.Operation{molecule},
		}, fixedRand(0x55))
		if err != nil {
			t.Fatalf("SealBlock: %v", err)
		}
		other := block.NewMemStore()
		other.MustAdd(bad)
		if _, err := block.Validate(bad, other, nil); err != nil {
			t.Fatalf("a node without the key must still accept the block structurally: %v", err)
		}
		_, _, err = OpenAndValidate(bad, key, other, nil)
		var rule *block.RuleError
		if !errors.As(err, &rule) || rule.Rule != 4 {
			t.Errorf("OpenAndValidate = %v, want a rule 4 violation", err)
		}
	})

	t.Run("private refs may name a block of any type", func(t *testing.T) {
		// Rule 6 constrains a public block's refs only; a private block's may
		// name anything (spec/02-block-format.md, "Validation" rule 6).
		reader := testBuilder(t, 6)
		b, err := SealBlock(reader, key, block.Payload{
			TS:   3000,
			Refs: []cid.Digest{genesis.Digest()},
			Ops:  []block.Operation{block.MustCreateAtom("a note about a private block")},
		}, fixedRand(0x66))
		if err != nil {
			t.Fatalf("SealBlock: %v", err)
		}
		store.MustAdd(b)
		if _, _, err := OpenAndValidate(b, key, store, nil); err != nil {
			t.Errorf("a private block referencing a private block must be accepted: %v", err)
		}
	})
}

// TestTamper covers the AEAD's whole job: any change to the ciphertext, to the
// nonce, or to a field the AAD covers must make the block unopenable
// (spec/04-cryptography.md, "Decryption procedure": the block MUST be
// rejected).
func TestTamper(t *testing.T) {
	key := testContentKey(t, 0x11)
	author := testBuilder(t, 1)
	first, err := SealBlock(author, key, samplePayload(), fixedRand(0x33))
	if err != nil {
		t.Fatalf("SealBlock: %v", err)
	}
	enc, _ := first.Enc()
	nonce, _ := first.Nonce()

	// A tampered block is a block with a different signature, so each case is
	// re-signed by the author: what is under test is the AEAD, not Ed25519,
	// which block's own tests cover.
	reseal := func(t *testing.T, c block.Content) *block.Block {
		t.Helper()
		b, err := block.Sign(c, testKey(t, 1))
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		return b
	}

	t.Run("a flipped bit in enc", func(t *testing.T) {
		for _, i := range []int{0, len(enc) / 2, len(enc) - 1} {
			bad := slices.Clone(enc)
			bad[i] ^= 0x01
			c := first.Content()
			c.Enc = bad
			if _, err := Open(reseal(t, c), key); !errors.Is(err, ErrAuthentication) {
				t.Errorf("flipping byte %d of enc = %v, want ErrAuthentication", i, err)
			}
		}
	})

	t.Run("a flipped bit in the nonce", func(t *testing.T) {
		bad := slices.Clone(nonce)
		bad[0] ^= 0x01
		c := first.Content()
		c.Nonce = bad
		if _, err := Open(reseal(t, c), key); !errors.Is(err, ErrAuthentication) {
			t.Errorf("flipping a nonce byte = %v, want ErrAuthentication", err)
		}
	})

	t.Run("a changed prev", func(t *testing.T) {
		// The AAD covers prev, so a ciphertext cannot be moved to another
		// position in the chain — the payload-swapping attack spec/04 names.
		second, err := SealBlock(author, key, samplePayload(), fixedRand(0x44))
		if err != nil {
			t.Fatalf("SealBlock: %v", err)
		}
		c := second.Content()
		c.Enc, c.Nonce = enc, nonce // the genesis block's ciphertext, moved forward
		if _, err := Open(reseal(t, c), key); !errors.Is(err, ErrAuthentication) {
			t.Errorf("moving a ciphertext to another position = %v, want ErrAuthentication", err)
		}
	})

	t.Run("a changed author", func(t *testing.T) {
		// The AAD covers pub, so a ciphertext cannot be re-signed into another
		// author's chain even by someone who holds the content key.
		c := first.Content()
		c.Pub = testPub(t, 7)
		impostor, err := block.Sign(c, testKey(t, 7))
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if _, err := Open(impostor, key); !errors.Is(err, ErrAuthentication) {
			t.Errorf("re-signing a ciphertext as another author = %v, want ErrAuthentication", err)
		}
	})

	t.Run("a changed version", func(t *testing.T) {
		// v is in the AAD too. No *Block can carry another version, so the AAD
		// is compared directly.
		h, err := HeaderOf(first)
		if err != nil {
			t.Fatalf("HeaderOf: %v", err)
		}
		aad, err := h.AAD()
		if err != nil {
			t.Fatalf("AAD: %v", err)
		}
		h.Version = 2
		if _, err := h.AAD(); err == nil {
			t.Error("an AAD for an unrecognized protocol version must not be computed")
		}
		h.Version = block.Version
		again, err := h.AAD()
		if err != nil || !bytes.Equal(aad, again) {
			t.Errorf("AAD is not a function of the header alone: %v", err)
		}
	})

	t.Run("a truncated ciphertext", func(t *testing.T) {
		// Below the tag size the block is structurally invalid and never
		// reaches the AEAD (spec/02-block-format.md, "Private block").
		c := first.Content()
		c.Enc = enc[:TagSize-1]
		if _, err := block.Sign(c, testKey(t, 1)); err == nil {
			t.Error("a ciphertext shorter than the Poly1305 tag must not be signed into a block")
		}
		c.Enc = enc[:TagSize]
		if _, err := Open(reseal(t, c), key); !errors.Is(err, ErrAuthentication) {
			t.Errorf("a truncated ciphertext = %v, want ErrAuthentication", err)
		}
	})
}

// TestStrictDecodeOfPlaintext: a ciphertext that authenticates is not thereby
// well-formed. The dCBOR profile applies to what comes out of the AEAD exactly
// as it applies to what arrives on the wire.
func TestStrictDecodeOfPlaintext(t *testing.T) {
	key := testContentKey(t, 0x11)
	atom := block.MustCreateAtom("France").Value()

	// sealRaw encrypts arbitrary bytes as if they were a payload, so that the
	// decoder is what rejects them and not the encoder.
	sealRaw := func(t *testing.T, plaintext []byte) *block.Block {
		t.Helper()
		author := testBuilder(t, 1)
		h := Header{Version: block.Version, Pub: testPub(t, 1)}
		aad, err := h.AAD()
		if err != nil {
			t.Fatalf("AAD: %v", err)
		}
		aead, err := newXChaCha(key[:])
		if err != nil {
			t.Fatalf("newXChaCha: %v", err)
		}
		nonce := bytes.Repeat([]byte{0x33}, NonceSize)
		b, err := author.Private(aead.Seal(nil, nonce, plaintext, aad), nonce)
		if err != nil {
			t.Fatalf("Private: %v", err)
		}
		return b
	}

	canonical := dcbor.MustEncode(dcbor.Map{
		{Key: "refs", Value: dcbor.Array{}},
		{Key: "ts", Value: dcbor.Uint(1740067200)},
		{Key: "ops", Value: dcbor.Array{atom}},
	})
	if _, err := Open(sealRaw(t, canonical), key); err != nil {
		t.Fatalf("the canonical payload must open: %v", err)
	}

	// Each of these authenticates and none of them decodes.
	noncanonical := map[string][]byte{
		// "ts" sorts before "ops", which sorts before "refs"; this is the same
		// map with the keys in declaration order.
		"unsorted map keys": interior(canonical, func(b []byte) []byte {
			return concat([]byte{0xa3},
				[]byte{0x64}, []byte("refs"), []byte{0x80},
				[]byte{0x62}, []byte("ts"), []byte{0x1a, 0x67, 0xb7, 0x51, 0x80},
				[]byte{0x63}, []byte("ops"), []byte{0x81}, dcbor.MustEncode(atom))
		}),
		// ts = 1 written in two bytes instead of one.
		"a non-shortest integer": interior(canonical, func(b []byte) []byte {
			return concat([]byte{0xa3},
				[]byte{0x62}, []byte("ts"), []byte{0x18, 0x01},
				[]byte{0x63}, []byte("ops"), []byte{0x81}, dcbor.MustEncode(atom),
				[]byte{0x64}, []byte("refs"), []byte{0x80})
		}),
		// An indefinite-length ops array.
		"an indefinite length": interior(canonical, func(b []byte) []byte {
			return concat([]byte{0xa3},
				[]byte{0x62}, []byte("ts"), []byte{0x01},
				[]byte{0x63}, []byte("ops"), []byte{0x9f}, dcbor.MustEncode(atom), []byte{0xff},
				[]byte{0x64}, []byte("refs"), []byte{0x80})
		}),
		"trailing bytes":  append(slices.Clone(canonical), 0x00),
		"not a map":       dcbor.MustEncode(dcbor.Array{dcbor.Uint(1)}),
		"empty":           nil,
		"an empty ops":    dcbor.MustEncode(dcbor.Map{{Key: "refs", Value: dcbor.Array{}}, {Key: "ts", Value: dcbor.Uint(1)}, {Key: "ops", Value: dcbor.Array{}}}),
		"a missing field": dcbor.MustEncode(dcbor.Map{{Key: "ts", Value: dcbor.Uint(1)}, {Key: "ops", Value: dcbor.Array{atom}}}),
		"an extra field": dcbor.MustEncode(dcbor.Map{
			{Key: "refs", Value: dcbor.Array{}}, {Key: "ts", Value: dcbor.Uint(1)},
			{Key: "ops", Value: dcbor.Array{atom}}, {Key: "nonce", Value: dcbor.Bytes(make([]byte, NonceSize))},
		}),
		"a rotate_key operation": dcbor.MustEncode(dcbor.Map{
			{Key: "refs", Value: dcbor.Array{}}, {Key: "ts", Value: dcbor.Uint(1)},
			{Key: "ops", Value: dcbor.Array{block.MustRotateKey(testPub(t, 2)).Value()}},
		}),
	}
	for name, plaintext := range noncanonical {
		b := sealRaw(t, plaintext)
		p, err := Open(b, key)
		if err == nil {
			t.Errorf("%s: Open accepted a payload it must reject: %+v", name, p)
			continue
		}
		if errors.Is(err, ErrAuthentication) {
			t.Errorf("%s: the ciphertext must authenticate; the payload is what is wrong (%v)", name, err)
		}
	}
}

// interior applies f, ignoring the canonical bytes; it exists to keep the
// hand-written encodings above next to the canonical one they deviate from.
func interior(canonical []byte, f func([]byte) []byte) []byte { return f(canonical) }

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestSealRejections covers what cannot be sealed at all.
func TestSealRejections(t *testing.T) {
	key := testContentKey(t, 0x11)
	h := Header{Version: block.Version, Pub: testPub(t, 1)}

	t.Run("a payload with no operations", func(t *testing.T) {
		if _, _, err := Seal(h, key, block.Payload{TS: 1}, fixedRand(0x33)); err == nil {
			t.Error("a payload with no operations must not be sealed")
		}
	})

	t.Run("a rotate_key operation", func(t *testing.T) {
		p := block.Payload{TS: 1, Ops: []block.Operation{block.MustRotateKey(testPub(t, 2))}}
		if _, _, err := Seal(h, key, p, fixedRand(0x33)); err == nil {
			t.Error("a rotate_key operation must not be sealed into a private block")
		}
	})

	t.Run("a malformed header", func(t *testing.T) {
		if _, _, err := Seal(Header{Version: block.Version, Pub: []byte{1, 2, 3}}, key, samplePayload(), fixedRand(0x33)); err == nil {
			t.Error("a header with a malformed public key must not produce an AAD")
		}
		if _, _, err := Seal(Header{Version: 99, Pub: testPub(t, 1)}, key, samplePayload(), fixedRand(0x33)); err == nil {
			t.Error("a header with an unrecognized version must not produce an AAD")
		}
	})

	t.Run("no randomness", func(t *testing.T) {
		if _, _, err := Seal(h, key, samplePayload(), failingRand{}); err == nil {
			t.Error("Seal must fail when it cannot read a nonce")
		}
		if _, err := GenerateKey(failingRand{}); err == nil {
			t.Error("GenerateKey must fail when it cannot read a key")
		}
		if _, err := Wrap(key, testKey(t, 1), testPub(t, 2), failingRand{}); err == nil {
			t.Error("Wrap must fail when it cannot read a nonce")
		}
	})

	t.Run("a block of another type", func(t *testing.T) {
		public, err := testBuilder(t, 1).Public(1, nil, block.MustCreateAtom("France"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if _, err := HeaderOf(public); err == nil {
			t.Error("a public block has no AAD")
		}
		if _, err := Open(public, key); err == nil {
			t.Error("a public block cannot be opened")
		}
		if _, err := HeaderOf(nil); err == nil {
			t.Error("HeaderOf(nil) must be an error")
		}
	})

	t.Run("a private successor genesis", func(t *testing.T) {
		// The builder refuses it, so SealBlock does too
		// (spec/02-block-format.md, "Verifiable succession").
		old := testBuilder(t, 1)
		// A rotation block is never a genesis block (spec/02-block-format.md,
		// "Rotation block"), so the chain it ends is opened first.
		if _, err := old.Public(900, nil, block.MustCreateAtom("France")); err != nil {
			t.Fatalf("genesis: %v", err)
		}
		rotation, err := old.Rotation(1000, nil, testPub(t, 2))
		if err != nil {
			t.Fatalf("rotation: %v", err)
		}
		successor := testBuilder(t, 2)
		if err := successor.Succeeds(rotation); err != nil {
			t.Fatalf("Succeeds: %v", err)
		}
		if _, err := SealBlock(successor, key, samplePayload(), fixedRand(0x33)); err == nil {
			t.Error("a successor chain must not begin with a private block")
		}
		if _, err := SealBlock(nil, key, samplePayload(), nil); err == nil {
			t.Error("SealBlock(nil, ...) must be an error")
		}
	})
}

// TestKeyHandling covers the content key's own surface.
func TestKeyHandling(t *testing.T) {
	k := testContentKey(t, 0x11)
	if len(k.Bytes()) != KeySize {
		t.Errorf("Bytes() is %d bytes, want %d", len(k.Bytes()), KeySize)
	}
	parsed, err := ParseKey(k.Bytes())
	if err != nil || parsed != k {
		t.Errorf("ParseKey(Bytes()) = %v, %v, want the same key", parsed, err)
	}
	if _, err := ParseKey(make([]byte, KeySize-1)); err == nil {
		t.Error("a short content key must be rejected")
	}
	if s := k.String(); strings.Contains(s, "11") || !strings.Contains(s, "redacted") {
		t.Errorf("Key.String() = %q; a content key must not print itself", s)
	}
	// Bytes returns a copy: mutating it must not reach the key.
	b := k.Bytes()
	b[0] ^= 0xff
	if k.Bytes()[0] == b[0] {
		t.Error("Bytes() shares its array with the key")
	}
	if a, err := GenerateKey(nil); err != nil || a == (Key{}) {
		t.Errorf("GenerateKey(nil) = %v, %v, want a key from crypto/rand", a, err)
	}
}

// TestNonceDiscipline: every block takes a fresh nonce, and it is the size the
// specification fixes (spec/04-cryptography.md, "Security Considerations":
// implementations MUST generate unique nonces for every private block).
func TestNonceDiscipline(t *testing.T) {
	key := testContentKey(t, 0x11)
	author := testBuilder(t, 1)
	seen := make(map[string]bool)
	for i := 0; i < 8; i++ {
		b, err := SealBlock(author, key, samplePayload(), nil) // crypto/rand
		if err != nil {
			t.Fatalf("block %d: %v", i, err)
		}
		nonce, ok := b.Nonce()
		if !ok || len(nonce) != NonceSize {
			t.Fatalf("block %d: nonce is %d bytes, want %d", i, len(nonce), NonceSize)
		}
		if seen[string(nonce)] {
			t.Fatalf("block %d reuses a nonce; that breaks confidentiality outright", i)
		}
		seen[string(nonce)] = true
	}
	if NonceSize != block.NonceSize || TagSize != block.MinEncSize {
		t.Errorf("privacy and block disagree on the fixed sizes: %d/%d and %d/%d",
			NonceSize, block.NonceSize, TagSize, block.MinEncSize)
	}
	// The same payload sealed twice differs, since the nonce differs.
	one, err := SealBlock(author, key, samplePayload(), nil)
	if err != nil {
		t.Fatalf("SealBlock: %v", err)
	}
	two, err := SealBlock(author, key, samplePayload(), nil)
	if err != nil {
		t.Fatalf("SealBlock: %v", err)
	}
	encOne, _ := one.Enc()
	encTwo, _ := two.Enc()
	if bytes.Equal(encOne, encTwo) {
		t.Error("two blocks with the same payload have the same ciphertext; the nonce is not doing its work")
	}
}
