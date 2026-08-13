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

// The conformance vectors. Every input is fixed: the author's Ed25519 seed is
// 32 bytes of 0x01, the recipients' 0x02 and 0x03, the content key is 32 bytes
// of 0x11, the block nonce is 24 bytes of 0x33 and the key-wrap nonces 24 bytes
// of 0x22. The payload is the one spec/04's example encrypts.
//
// These bytes are the interop contract: a second implementation that produces
// them from the same inputs implements the same protocol, and one that does not
// has found either a bug or an ambiguity worth filing.
const (
	// dCBOR({"refs": [], "ts": 1740067200,
	//        "ops": [{"op": "create_atom", "description": "My private note"}]})
	vectorPlaintextHex = "a3" +
		"627473" + "1a67b75180" +
		"636f7073" + "81a2626f706b6372656174655f61746f6d6b6465736372697074696f6e6f4d792070726976617465206e6f7465" +
		"64726566" + "7380"

	// dCBOR({"v": 1, "type": "private", "pub": <the seed-0x01 key>, "prev": null})
	vectorAADHex = "a4" +
		"6176" + "01" +
		"63707562" + "5820" + "8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c" +
		"6470726576" + "f6" +
		"6474797065" + "6770726976617465"

	// XChaCha20-Poly1305(key: 32×0x11, nonce: 24×0x33, plaintext, aad) — the
	// 55-byte plaintext followed by the 16-byte Poly1305 tag.
	vectorEncHex = "19c603fa620cb46602f747d4bd6c5cb4e437179d3ed615f88e3bf70eff7a9425" +
		"ca95bbbcc0bff62dae2db0201b49ce295064f97994eda3731bc7ebeefa011841" +
		"d9559fbcf77be720fe02305ea226863a"

	// The remaining values are the worked example of spec/04-cryptography.md,
	// "Wrapping a chain key", which gives every one of them in hex: the
	// specification pins these bytes and this test is what holds the package to
	// them.
	//
	// HKDF-SHA-256(salt: empty, ikm: X25519(sk₁, pk₂), info: "dialog-v1-key-wrap", 32).
	vectorWrappingKeyHex = "657dbd5e5d21dcb81a44415ddf3a8b9f9fa44c7d832d678c9962079aa01fe68d"

	// The content key (32×0x11) wrapped for the seed-0x02 recipient under a
	// nonce of 24×0x22: nonce || XChaCha20-Poly1305(wrapping key, nonce, key,
	// aad: empty), 72 bytes.
	vectorWrappedKeyHex = "222222222222222222222222222222222222222222222222" +
		"bdafc49fb94819665a9993f60272336caf98fd5fd4fb1b302e94cc2b5a8ccbd6" +
		"1b25f1def2b48d225f13e64d5f0ffa90"
)

// TestVectors pins the exact bytes this package produces for fixed inputs.
func TestVectors(t *testing.T) {
	key := testContentKey(t, 0x11)
	payload := block.Payload{TS: 1740067200, Ops: []block.Operation{block.MustCreateAtom("My private note")}}

	plaintext, err := payload.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got := hex.EncodeToString(plaintext); got != vectorPlaintextHex {
		t.Errorf("plaintext = %s, want %s", got, vectorPlaintextHex)
	}

	h := Header{Version: block.Version, Pub: testPub(t, 1)}
	aad, err := h.AAD()
	if err != nil {
		t.Fatalf("AAD: %v", err)
	}
	if got := hex.EncodeToString(aad); got != vectorAADHex {
		t.Errorf("aad = %s, want %s", got, vectorAADHex)
	}

	enc, nonce, err := Seal(h, key, payload, fixedRand(0x33))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if got, want := hex.EncodeToString(nonce), "333333333333333333333333333333333333333333333333"; got != want {
		t.Errorf("nonce = %s, want %s", got, want)
	}
	if got := hex.EncodeToString(enc); got != vectorEncHex {
		t.Errorf("enc = %s, want %s", got, vectorEncHex)
	}

	// The wrapping key for the author (seed 0x01) and the first recipient
	// (seed 0x02), and the wrapped content key under a nonce of 24 bytes of
	// 0x22.
	wrappingKey, err := WrappingKey(testKey(t, 1), testPub(t, 2))
	if err != nil {
		t.Fatalf("WrappingKey: %v", err)
	}
	if got := hex.EncodeToString(wrappingKey); got != vectorWrappingKeyHex {
		t.Errorf("wrapping key = %s, want %s", got, vectorWrappingKeyHex)
	}
	wrapped, err := Wrap(key, testKey(t, 1), testPub(t, 2), fixedRand(0x22))
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if got := hex.EncodeToString(wrapped); got != vectorWrappedKeyHex {
		t.Errorf("wrapped key = %s, want %s", got, vectorWrappedKeyHex)
	}
}

// TestX25519Conversion covers the Ed25519-to-X25519 conversion the key wrap
// rests on (spec/04-cryptography.md, "Key management").
func TestX25519Conversion(t *testing.T) {
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
