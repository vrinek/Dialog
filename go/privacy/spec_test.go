package privacy

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/internal/vectorfile"
)

// The worked example of spec/04-cryptography.md, "Encrypting a private block",
// and the conformance vectors that pin this package's output.
//
// The example prints placeholders for the author's key, the signature and the
// ciphertext, so what it fixes exactly is (a) the plaintext — a dCBOR map of
// refs, ts and ops, every byte of which the example gives — and (b) the AAD's
// field set and layout. Those are assembled here by hand from the CBOR encoding
// reference of spec/03-encoding.md and compared against the package's output.
// The ciphertext depends on a key and a nonce the example does not give; the
// vectors below supply both, so that the bytes are reproducible and can be
// checked against any other implementation.
const (
	// spec/04's example timestamp, 1740067200 = 0x67b75180.
	specTSHex = "1a67b75180"

	// Encoded map keys and text values (spec/03-encoding.md, "CBOR encoding
	// reference").
	hexKeyV           = "6176"
	hexKeyTS          = "627473"
	hexKeyOps         = "636f7073"
	hexKeyPub         = "63707562"
	hexKeyPrev        = "6470726576"
	hexKeyRefs        = "6472656673"
	hexKeyType        = "6474797065"
	hexKeyOp          = "626f70"
	hexKeyDescription = "6b6465736372697074696f6e"
	hexPrivate        = "6770726976617465"
	hexCreateAtom     = "6b6372656174655f61746f6d"

	// "My private note", 15 bytes of UTF-8.
	hexPrivateNote = "6f4d792070726976617465206e6f7465"
)

// TestSpecPrivateBlockPlaintext reproduces step 1 of the example byte for byte:
//
//	plaintext = dCBOR({"refs": [], "ts": 1740067200,
//	                   "ops": [{"op": "create_atom",
//	                            "description": "My private note"}]})
//
// The three keys sort ts, ops, refs in the bytewise order of their encodings,
// which is what makes the plaintext a single fixed byte string for a given
// payload — and therefore what makes a conformance vector possible at all.
func TestSpecPrivateBlockPlaintext(t *testing.T) {
	p := block.Payload{
		Refs: nil, // the example's empty refs list
		TS:   1740067200,
		Ops:  []block.Operation{block.MustCreateAtom("My private note")},
	}
	want := "a3" +
		hexKeyTS + specTSHex +
		hexKeyOps + "81" + "a2" + hexKeyOp + hexCreateAtom + hexKeyDescription + hexPrivateNote +
		hexKeyRefs + "80"

	encoded, err := p.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got := hex.EncodeToString(encoded); got != want {
		t.Errorf("plaintext =\n%s\nwant\n%s", got, want)
	}
	back, err := block.DecodePayload(encoded)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if back.TS != p.TS || len(back.Ops) != 1 || len(back.Refs) != 0 {
		t.Errorf("round trip = %+v, want the example's payload", back)
	}
}

// TestSpecPrivateBlockAAD reproduces step 3 of the example:
//
//	aad = dCBOR({"v": 1, "type": "private", "pub": <32 bytes>,
//	             "prev": <digest of previous block, or null>})
//
// Both branches of prev are checked, since the example prints the choice and
// not a value.
func TestSpecPrivateBlockAAD(t *testing.T) {
	pub := testPub(t, 1)
	genesis := Header{Version: block.Version, Pub: pub}
	wantGenesis := "a4" +
		hexKeyV + "01" +
		hexKeyPub + "5820" + hex.EncodeToString(pub) +
		hexKeyPrev + "f6" +
		hexKeyType + hexPrivate

	aad, err := genesis.AAD()
	if err != nil {
		t.Fatalf("AAD: %v", err)
	}
	if got := hex.EncodeToString(aad); got != wantGenesis {
		t.Errorf("genesis AAD =\n%s\nwant\n%s", got, wantGenesis)
	}

	prev := cid.SumDigest([]byte("the previous block"))
	linked := Header{Version: block.Version, Pub: pub, Prev: &prev}
	wantLinked := "a4" +
		hexKeyV + "01" +
		hexKeyPub + "5820" + hex.EncodeToString(pub) +
		hexKeyPrev + "5820" + prev.String() +
		hexKeyType + hexPrivate
	aad, err = linked.AAD()
	if err != nil {
		t.Fatalf("AAD: %v", err)
	}
	if got := hex.EncodeToString(aad); got != wantLinked {
		t.Errorf("linked AAD =\n%s\nwant\n%s", got, wantLinked)
	}
}

// TestSpecPrivateBlockShape reproduces step 5: the block the example
// constructs carries exactly seven fields, and no plaintext refs, ts or ops.
func TestSpecPrivateBlockShape(t *testing.T) {
	key := testContentKey(t, 0x11)
	author := testBuilder(t, 1)
	b, err := SealBlock(author, key, block.Payload{
		TS:  1740067200,
		Ops: []block.Operation{block.MustCreateAtom("My private note")},
	}, fixedRand(0x33))
	if err != nil {
		t.Fatalf("SealBlock: %v", err)
	}

	enc, ok := b.Enc()
	if !ok || len(enc) < TagSize {
		t.Fatalf("enc is %d bytes, want at least the %d-byte tag", len(enc), TagSize)
	}
	nonce, ok := b.Nonce()
	if !ok || len(nonce) != NonceSize {
		t.Fatalf("nonce is %d bytes, want %d", len(nonce), NonceSize)
	}
	if len(b.Refs()) != 0 || b.TS() != 0 || b.Ops() != nil {
		t.Error("a private block must carry no plaintext refs, ts or ops")
	}
	// v, type, pub, sig, prev, enc, nonce — the a7 head of the block map.
	if got := hex.EncodeToString(b.Bytes())[:2]; got != "a7" {
		t.Errorf("block map head = %s, want a7 (seven fields)", got)
	}
	// The ciphertext is the plaintext's length plus the tag: XChaCha20 is a
	// stream cipher, so the payload's size is not hidden.
	plaintext, err := block.Payload{TS: 1740067200, Ops: []block.Operation{block.MustCreateAtom("My private note")}}.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(enc) != len(plaintext)+TagSize {
		t.Errorf("enc is %d bytes, want %d (%d of plaintext plus a %d-byte tag)", len(enc), len(plaintext)+TagSize, len(plaintext), TagSize)
	}
}

// The conformance vectors live in vectors/privacy.json, not in this file.
//
// They were once constants here, and a copy in each place is a copy that can
// drift: the vector set is generated from this package (cmd/genvectors), and
// this test verifies this package, so a divergence between the two would mean
// one of them silently stopped describing the protocol. There is therefore
// exactly one copy of every byte — the committed file — and this test reads
// it. Regenerate with `go run ./cmd/genvectors` and review the diff.
//
// Every input is fixed and comes from the file's inputs section: the author's
// Ed25519 seed is 32 bytes of 0x01, the recipient's 0x02 and the third
// party's 0x03, the content key is 32 bytes of 0x11, the block nonce 24 bytes
// of 0x33 and the key-wrap nonce 24 bytes of 0x22. The payload is the one
// spec/04's example encrypts.
//
// These bytes are the interop contract: a second implementation that produces
// them from the same inputs implements the same protocol, and one that does
// not has found either a bug or an ambiguity worth filing.
const vectorsPath = "../../vectors/privacy.json"

// specVectors is the committed file, indexed by case name.
type specVectors struct {
	inputs vectorfile.PrivacyInputs
	bytes  map[string]vectorfile.PrivacyCase
	x25519 map[string]vectorfile.X25519Case
	wraps  map[string]vectorfile.WrapCase
	block  vectorfile.BlockCase
}

func loadSpecVectors(t testing.TB) specVectors {
	t.Helper()
	doc, err := vectorfile.Read(vectorsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", vectorsPath, err)
	}
	inputs, err := vectorfile.DecodeInputs[vectorfile.PrivacyInputs](doc)
	if err != nil {
		t.Fatalf("%s: %v", vectorsPath, err)
	}
	v := specVectors{
		inputs: inputs,
		bytes:  map[string]vectorfile.PrivacyCase{},
		x25519: map[string]vectorfile.X25519Case{},
		wraps:  map[string]vectorfile.WrapCase{},
	}
	for _, name := range []string{"payload", "aead"} {
		for _, c := range sectionCases[vectorfile.PrivacyCase](t, doc, name) {
			v.bytes[c.Name] = c
		}
	}
	for _, c := range sectionCases[vectorfile.X25519Case](t, doc, "x25519") {
		v.x25519[c.Name] = c
	}
	for _, c := range sectionCases[vectorfile.WrapCase](t, doc, "key_wrap") {
		v.wraps[c.Name] = c
	}
	v.block = sectionCases[vectorfile.BlockCase](t, doc, "private_block")[0]
	return v
}

func sectionCases[T any](t testing.TB, doc vectorfile.Document, name string) []T {
	t.Helper()
	s, ok := doc.Section(name)
	if !ok {
		t.Fatalf("%s has no %q section", vectorsPath, name)
	}
	out, err := vectorfile.DecodeCases[T](s)
	if err != nil {
		t.Fatalf("%s, section %s: %v", vectorsPath, name, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s, section %s is empty", vectorsPath, name)
	}
	return out
}

// hexCase returns the pinned bytes of a named case.
func (v specVectors) hexCase(t testing.TB, name string) string {
	t.Helper()
	c, ok := v.bytes[name]
	if !ok {
		t.Fatalf("%s has no case %q", vectorsPath, name)
	}
	return c.Hex
}

// keyNamed rebuilds one of the file's Ed25519 identities from its seed, and
// checks the file's own public key against it — an inconsistent vector file is
// a bug wherever it comes from.
func (v specVectors) keyNamed(t testing.TB, name string) ed25519.PrivateKey {
	t.Helper()
	for _, k := range v.inputs.Keys {
		if k.Name != name {
			continue
		}
		seed := decodeHex(t, k.Seed)
		if len(seed) != ed25519.SeedSize {
			t.Fatalf("%s: the %s seed is %d bytes, want %d", vectorsPath, name, len(seed), ed25519.SeedSize)
		}
		priv := ed25519.NewKeyFromSeed(seed)
		pub, ok := priv.Public().(ed25519.PublicKey)
		if !ok {
			t.Fatalf("%s: the %s seed does not yield an Ed25519 public key", vectorsPath, name)
		}
		if got := hex.EncodeToString(pub); got != k.PublicKey {
			t.Fatalf("%s: the %s public key is %s, but its seed derives %s", vectorsPath, name, k.PublicKey, got)
		}
		return priv
	}
	t.Fatalf("%s has no key named %q", vectorsPath, name)
	return nil
}

func (v specVectors) pubNamed(t testing.TB, name string) ed25519.PublicKey {
	t.Helper()
	pub, ok := v.keyNamed(t, name).Public().(ed25519.PublicKey)
	if !ok {
		t.Fatalf("the %s key does not yield an Ed25519 public key", name)
	}
	return pub
}

func decodeHex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("%q is not hex: %v", s, err)
	}
	return b
}

// repeatedByte returns the byte a fixed nonce or key is made of, failing if
// the input is not one byte repeated — which is what makes it reproducible
// from a fixedRand source.
func repeatedByte(t testing.TB, what, hexBytes string, size int) byte {
	t.Helper()
	b := decodeHex(t, hexBytes)
	if len(b) != size {
		t.Fatalf("%s is %d bytes, want %d", what, len(b), size)
	}
	for _, c := range b {
		if c != b[0] {
			t.Fatalf("%s is not a single repeated byte, so this test cannot reproduce it", what)
		}
	}
	return b[0]
}

// TestVectors holds this package to the exact bytes of vectors/privacy.json.
func TestVectors(t *testing.T) {
	v := loadSpecVectors(t)
	author, recipient := v.keyNamed(t, "author"), v.keyNamed(t, "recipient")

	key, err := ParseKey(decodeHex(t, v.inputs.ContentKey))
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	blockNonce := repeatedByte(t, "the block nonce", v.inputs.BlockNonce, NonceSize)
	wrapNonce := repeatedByte(t, "the wrap nonce", v.inputs.WrapNonce, NonceSize)

	payload := samplePayload()
	plaintext, err := payload.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got, want := hex.EncodeToString(plaintext), v.hexCase(t, "plaintext"); got != want {
		t.Errorf("plaintext = %s, want %s", got, want)
	}

	h := Header{Version: block.Version, Pub: v.pubNamed(t, "author")}
	aad, err := h.AAD()
	if err != nil {
		t.Fatalf("AAD: %v", err)
	}
	if got, want := hex.EncodeToString(aad), v.hexCase(t, "aad_genesis"); got != want {
		t.Errorf("aad = %s, want %s", got, want)
	}

	enc, nonce, err := Seal(h, key, payload, fixedRand(blockNonce))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if got, want := hex.EncodeToString(nonce), v.inputs.BlockNonce; got != want {
		t.Errorf("nonce = %s, want %s", got, want)
	}
	if got, want := hex.EncodeToString(enc), v.hexCase(t, "enc"); got != want {
		t.Errorf("enc = %s, want %s", got, want)
	}

	// The wrapping key and the wrapped content key, for each recipient the
	// file names.
	for name, w := range v.wraps {
		t.Run("wrap/"+name, func(t *testing.T) {
			own, peer := v.keyNamed(t, w.Own), v.pubNamed(t, w.Peer)
			if w.Info != WrapInfo {
				t.Errorf("HKDF info = %q, want %q", w.Info, WrapInfo)
			}
			wrappingKey, err := WrappingKey(own, peer)
			if err != nil {
				t.Fatalf("WrappingKey: %v", err)
			}
			if got := hex.EncodeToString(wrappingKey); got != w.WrappingKey {
				t.Errorf("wrapping key = %s, want %s", got, w.WrappingKey)
			}
			wrapped, err := Wrap(key, own, peer, fixedRand(wrapNonce))
			if err != nil {
				t.Fatalf("Wrap: %v", err)
			}
			if got := hex.EncodeToString(wrapped); got != w.WrappedKey {
				t.Errorf("wrapped key = %s, want %s", got, w.WrappedKey)
			}
		})
	}

	// The sealed block: the ciphertext above inside a signed private block,
	// which the recipient opens with the key they unwrapped.
	sealed, err := SealBlock(mustBuilder(t, author), key, samplePayload(), fixedRand(blockNonce))
	if err != nil {
		t.Fatalf("SealBlock: %v", err)
	}
	if got := hex.EncodeToString(sealed.Bytes()); got != v.block.Block {
		t.Errorf("private block = %s, want %s", got, v.block.Block)
	}
	if got := sealed.Digest().String(); got != v.block.Digest {
		t.Errorf("private block digest = %s, want %s", got, v.block.Digest)
	}
	wrapped := decodeHex(t, v.wraps["author_to_recipient"].WrappedKey)
	unwrapped, err := Unwrap(wrapped, recipient, v.pubNamed(t, "author"))
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	opened, err := Open(sealed, unwrapped)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.TS != payload.TS || len(opened.Ops) != len(payload.Ops) {
		t.Errorf("the recipient opened %+v, want the sealed payload", opened)
	}
}

func mustBuilder(t testing.TB, priv ed25519.PrivateKey) *block.Builder {
	t.Helper()
	b, err := block.NewBuilder(priv)
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	return b
}

// TestX25519Conversion covers the Ed25519-to-X25519 conversion the key wrap
// rests on (spec/04-cryptography.md, "Ed25519-to-X25519 conversion").
func TestX25519Conversion(t *testing.T) {
	// The conversions of the worked example in spec/04-cryptography.md,
	// "Wrapping a chain key": both halves for every party, and the shared
	// secret they agree on. These are the bytes an implementation that reads
	// only the specification must reproduce — the private half in particular,
	// which is SHA-512 over the seed and not the seed itself. They are read
	// from the conformance vectors, which is where they are written down.
	t.Run("worked example", func(t *testing.T) {
		v := loadSpecVectors(t)
		for name, tc := range v.x25519 {
			priv, err := X25519PrivateFromEd25519(v.keyNamed(t, name))
			if err != nil {
				t.Fatalf("%s: X25519PrivateFromEd25519: %v", name, err)
			}
			if got := hex.EncodeToString(priv.Bytes()); got != tc.X25519PrivateKey {
				t.Errorf("%s: X25519 private key = %s, want %s", name, got, tc.X25519PrivateKey)
			}
			pub, err := X25519PublicFromEd25519(v.pubNamed(t, name))
			if err != nil {
				t.Fatalf("%s: X25519PublicFromEd25519: %v", name, err)
			}
			if got := hex.EncodeToString(pub.Bytes()); got != tc.X25519PublicKey {
				t.Errorf("%s: X25519 public key = %s, want %s", name, got, tc.X25519PublicKey)
			}
		}
		for name, w := range v.wraps {
			own, err := X25519PrivateFromEd25519(v.keyNamed(t, w.Own))
			if err != nil {
				t.Fatalf("%s: X25519PrivateFromEd25519: %v", name, err)
			}
			peer, err := X25519PublicFromEd25519(v.pubNamed(t, w.Peer))
			if err != nil {
				t.Fatalf("%s: X25519PublicFromEd25519: %v", name, err)
			}
			shared, err := own.ECDH(peer)
			if err != nil {
				t.Fatalf("%s: ECDH: %v", name, err)
			}
			if got := hex.EncodeToString(shared); got != w.SharedSecret {
				t.Errorf("%s: shared secret = %s, want %s", name, got, w.SharedSecret)
			}
		}
	})

	// The two halves of the conversion must agree: the public key derived from
	// the converted private key must equal the birational image of the Ed25519
	// public key. Nothing else checks the map's correctness as sharply.
	for seed := byte(1); seed <= 8; seed++ {
		priv := testKey(t, seed)
		pub, _ := priv.Public().(ed25519.PublicKey)
		fromPrivate, err := X25519PrivateFromEd25519(priv)
		if err != nil {
			t.Fatalf("seed %d: X25519PrivateFromEd25519: %v", seed, err)
		}
		fromPublic, err := X25519PublicFromEd25519(pub)
		if err != nil {
			t.Fatalf("seed %d: X25519PublicFromEd25519: %v", seed, err)
		}
		if !fromPrivate.PublicKey().Equal(fromPublic) {
			t.Errorf("seed %d: the converted private key's public key is not the converted public key", seed)
		}
	}

	t.Run("agreement is symmetric", func(t *testing.T) {
		// The author and the recipient must arrive at the same wrapping key
		// from opposite ends, which is what makes Unwrap possible at all.
		author, recipient := testKey(t, 1), testKey(t, 2)
		a, err := WrappingKey(author, testPub(t, 2))
		if err != nil {
			t.Fatalf("WrappingKey: %v", err)
		}
		b, err := WrappingKey(recipient, testPub(t, 1))
		if err != nil {
			t.Fatalf("WrappingKey: %v", err)
		}
		if hex.EncodeToString(a) != hex.EncodeToString(b) {
			t.Error("the two ends of the agreement derive different wrapping keys")
		}
		// A third party derives a different one.
		c, err := WrappingKey(testKey(t, 3), testPub(t, 1))
		if err != nil {
			t.Fatalf("WrappingKey: %v", err)
		}
		if hex.EncodeToString(a) == hex.EncodeToString(c) {
			t.Error("a third party derives the same wrapping key")
		}
	})

	t.Run("malformed keys", func(t *testing.T) {
		if _, err := X25519PublicFromEd25519(make([]byte, 31)); err == nil {
			t.Error("a short Ed25519 public key must be rejected")
		}
		if _, err := X25519PrivateFromEd25519(make([]byte, 31)); err == nil {
			t.Error("a short Ed25519 private key must be rejected")
		}
		// y = 1 is the identity point; its Montgomery image is at infinity.
		identity := make([]byte, ed25519.PublicKeySize)
		identity[0] = 1
		if _, err := X25519PublicFromEd25519(identity); err == nil {
			t.Error("the identity point has no X25519 image and must be rejected")
		}
		// y = p is not a reduced coordinate.
		unreduced := make([]byte, ed25519.PublicKeySize)
		unreduced[0] = 0xed
		for i := 1; i < 31; i++ {
			unreduced[i] = 0xff
		}
		unreduced[31] = 0x7f
		if _, err := X25519PublicFromEd25519(unreduced); err == nil {
			t.Error("a y coordinate that is not reduced modulo p must be rejected")
		}
	})

	t.Run("small-order keys", func(t *testing.T) {
		// A public key of small order maps to a u of small order, and the
		// agreement with it is all zeroes. The specification requires the
		// rejection but lets an implementation place it either at the
		// conversion or at the agreement (spec/04-cryptography.md,
		// "Ed25519-to-X25519 conversion"); this package places it at the
		// agreement, so what must hold is that no wrapping key comes out.
		//
		// y = p - 1 is the point of order 2; y = 0 is a point of order 4.
		orderTwo := make([]byte, ed25519.PublicKeySize)
		orderTwo[0] = 0xec
		for i := 1; i < 31; i++ {
			orderTwo[i] = 0xff
		}
		orderTwo[31] = 0x7f
		orderFour := make([]byte, ed25519.PublicKeySize)

		for _, pub := range []ed25519.PublicKey{orderTwo, orderFour} {
			if _, err := WrappingKey(testKey(t, 1), pub); err == nil {
				t.Errorf("a wrapping key was derived for the small-order public key %x", []byte(pub))
			}
			if _, err := Wrap(testContentKey(t, 0x11), testKey(t, 1), pub, fixedRand(0x22)); err == nil {
				t.Errorf("a content key was wrapped for the small-order public key %x", []byte(pub))
			}
		}
	})

	t.Run("wrapped keys", func(t *testing.T) {
		key := testContentKey(t, 0x11)
		wrapped, err := Wrap(key, testKey(t, 1), testPub(t, 2), fixedRand(0x22))
		if err != nil {
			t.Fatalf("Wrap: %v", err)
		}
		// 72 bytes: 24 nonce, 32 ciphertext, 16 tag, in that order
		// (spec/04-cryptography.md, "Wrapped key format").
		if len(wrapped) != WrappedKeySize || WrappedKeySize != 72 {
			t.Fatalf("a wrapped key is %d bytes, want %d = 24 + 32 + 16", len(wrapped), WrappedKeySize)
		}
		if got, want := hex.EncodeToString(wrapped[:NonceSize]), strings.Repeat("22", NonceSize); got != want {
			t.Errorf("the wrap does not begin with its nonce: %s, want %s", got, want)
		}
		got, err := Unwrap(wrapped, testKey(t, 2), testPub(t, 1))
		if err != nil || got != key {
			t.Fatalf("Unwrap = %v, %v, want the content key", got, err)
		}
		// Every conforming wrap is exactly WrappedKeySize bytes, so any other
		// length is malformed and MUST be rejected — before any decryption is
		// attempted, which is why these are not authentication failures.
		for _, bad := range [][]byte{
			nil,
			{},
			wrapped[:len(wrapped)-1],
			wrapped[:NonceSize],
			append(slices.Clone(wrapped), 0),
		} {
			_, err := Unwrap(bad, testKey(t, 2), testPub(t, 1))
			if err == nil {
				t.Errorf("a wrapped key of %d bytes must be rejected", len(bad))
			} else if errors.Is(err, ErrAuthentication) {
				t.Errorf("a wrapped key of %d bytes must be rejected on its length, not by the AEAD", len(bad))
			}
		}
		for i := range wrapped {
			bad := append([]byte(nil), wrapped...)
			bad[i] ^= 0x01
			if _, err := Unwrap(bad, testKey(t, 2), testPub(t, 1)); err == nil {
				t.Fatalf("flipping byte %d of a wrapped key still unwrapped", i)
			}
		}
	})
}
