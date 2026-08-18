package vectors

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"math/big"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
	"github.com/vrinek/Dialog/go/privacy"
)

// The `invalid` section of privacy.json is the counterpart of blocks.json's
// and entities.json's: every rejection rule spec/04-cryptography.md states in
// prose, pinned as bytes, and — because two of its layers (the enc floor and
// the rotate_key scoping) are in fact stated in spec/02-block-format.md — the
// two places where this file's contract reaches one layer down.
//
// Each case is verified against this implementation before it is emitted, the
// same discipline invalidBlockCases and invalidInChainCases hold themselves
// to: a vector may never pin a rejection this package does not itself make.

// Rule labels for the invalid section, named as the specification states
// them.
const (
	rulePrivConversionCanonical = "spec/04-cryptography.md, Ed25519-to-X25519 conversion, public keys, step 2"
	rulePrivConversionIdentity  = "spec/04-cryptography.md, Ed25519-to-X25519 conversion, public keys, step 3"
	rulePrivSmallOrder          = "spec/04-cryptography.md, Ed25519-to-X25519 conversion, public keys (small-order agreement)"
	rulePrivWrapLength          = "spec/04-cryptography.md, Wrapped key format"
	rulePrivWrapAuth            = "spec/04-cryptography.md, Wrapped key format (authentication)"
	rulePrivDecryption          = "spec/04-cryptography.md, Decryption procedure"
	rulePrivEncFloor            = "spec/02-block-format.md, Private block"
	rulePrivPayloadDecode       = "spec/04-cryptography.md, Decryption procedure (payload), spec/03-encoding.md Deterministic CBOR"
	rulePrivRotateScope         = "spec/02-block-format.md, Validation dispatch"
)

// curvePrime is 2^255 - 19, the field of Curve25519. The privacy package
// holds the same constant, unexported; recomputing it here from the
// specification text, rather than reaching into the package under test, is
// what keeps this generator an independent check of it.
var curvePrime = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))

// hexPtr is hexOf behind a pointer, so that a zero-length case can be told
// apart in JSON from a field that does not apply (see PrivacyInvalidCase's
// WrappedKey).
func hexPtr(b []byte) *string {
	s := hexOf(b)
	return &s
}

// leBytes writes v as n little-endian bytes, the encoding an Ed25519 public
// key's y coordinate uses (spec/04-cryptography.md, "Ed25519-to-X25519
// conversion").
func leBytes(v *big.Int, n int) []byte {
	out := make([]byte, n)
	v.FillBytes(out)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// privacyInvalidCases builds the thirteen cases: two of the X25519
// conversion's own rejections, the small-order agreement, four key-wrap
// rejections, three AEAD tamper cases, the enc floor, and two payloads that
// authenticate but must still be refused, one on strict decoding and one on
// the rotate_key scoping rule.
func privacyInvalidCases(key privacy.Key) ([]PrivacyInvalidCase, error) {
	authorPriv, authorPub := seedKey(seedAlice), seedPub(seedAlice)
	recipientPriv := seedKey(seedBob)

	cases := []PrivacyInvalidCase{}

	// --- Ed25519-to-X25519 conversion -----------------------------------

	nonCanonicalY := leBytes(curvePrime, ed25519.PublicKeySize) // y = p, sign bit clear
	if _, err := privacy.X25519PublicFromEd25519(ed25519.PublicKey(nonCanonicalY)); err == nil {
		return nil, errors.New("vectors: y_not_canonical: the reference implementation accepts y = p")
	}
	cases = append(cases, PrivacyInvalidCase{
		Name:      "y_not_canonical",
		Rule:      rulePrivConversionCanonical,
		Reason:    "y = 2^255 - 19, the field modulus: not reduced modulo p, so no Ed25519 public key produces this encoding.",
		PublicKey: hexOf(nonCanonicalY),
	})

	identityY := make([]byte, ed25519.PublicKeySize)
	identityY[0] = 1
	if _, err := privacy.X25519PublicFromEd25519(ed25519.PublicKey(identityY)); err == nil {
		return nil, errors.New("vectors: y_equals_one: the reference implementation accepts y = 1")
	}
	cases = append(cases, PrivacyInvalidCase{
		Name:      "y_equals_one",
		Rule:      rulePrivConversionIdentity,
		Reason:    "y = 1 is the identity point; 1 - y has no inverse mod p, so its Montgomery image is the point at infinity, which has no u coordinate to encode.",
		PublicKey: hexOf(identityY),
	})

	orderTwo := leBytes(new(big.Int).Sub(curvePrime, big.NewInt(1)), ed25519.PublicKeySize) // y = p - 1
	if _, err := privacy.WrappingKey(authorPriv, ed25519.PublicKey(orderTwo)); err == nil {
		return nil, errors.New("vectors: small_order_agreement: the reference implementation derives a wrapping key for a small-order peer")
	}
	cases = append(cases, PrivacyInvalidCase{
		Name:          "small_order_agreement",
		Rule:          rulePrivSmallOrder,
		Reason:        "peer_public_key is the Edwards point of order 2 (y = p - 1); its Montgomery image is u = 0, so every X25519 agreement with it is all-zero. spec/04 permits rejecting this at conversion time or at the agreement, but never deriving a wrapping key from it either way.",
		Own:           "author",
		PeerPublicKey: hexOf(orderTwo),
	})

	// --- Wrapped key format ---------------------------------------------

	validWrap, err := privacy.Wrap(key, authorPriv, seedPub(seedBob), fixedReader(wrapNonceByte))
	if err != nil {
		return nil, err
	}
	if len(validWrap) != privacy.WrappedKeySize {
		return nil, fmt.Errorf("vectors: a wrap is %d bytes, want %d", len(validWrap), privacy.WrappedKeySize)
	}
	for _, lc := range []struct {
		name    string
		wrapped []byte
	}{
		{"wrapped_key_length_71", validWrap[:71]},
		{"wrapped_key_length_73", append(bytes.Clone(validWrap), 0x00)},
		{"wrapped_key_length_0", []byte{}},
	} {
		if _, err := privacy.Unwrap(lc.wrapped, recipientPriv, authorPub); err == nil {
			return nil, fmt.Errorf("vectors: %s: the reference implementation unwraps a wrapped key of %d bytes", lc.name, len(lc.wrapped))
		} else if errors.Is(err, privacy.ErrAuthentication) {
			return nil, fmt.Errorf("vectors: %s: rejected by authentication, not by length: %w", lc.name, err)
		}
		cases = append(cases, PrivacyInvalidCase{
			Name:       lc.name,
			Rule:       rulePrivWrapLength,
			Reason:     fmt.Sprintf("%d bytes; every conforming wrap is exactly the fixed 72, so this MUST be rejected on length before any decryption is attempted.", len(lc.wrapped)),
			Own:        "author",
			Peer:       "recipient",
			WrappedKey: hexPtr(lc.wrapped),
		})
	}

	tamperedWrap := bytes.Clone(validWrap)
	tamperedWrap[40] ^= 0x01
	if _, err := privacy.Unwrap(tamperedWrap, recipientPriv, authorPub); !errors.Is(err, privacy.ErrAuthentication) {
		return nil, fmt.Errorf("vectors: wrapped_key_tampered: want an authentication failure, got %w", err)
	}
	cases = append(cases, PrivacyInvalidCase{
		Name:       "wrapped_key_tampered",
		Rule:       rulePrivWrapAuth,
		Reason:     "The 72-byte wrap of author_to_recipient with one bit of its ciphertext flipped. The length is right; the tag no longer authenticates.",
		Own:        "author",
		Peer:       "recipient",
		WrappedKey: hexPtr(tamperedWrap),
	})

	// --- AEAD tamper, over the worked example's own ciphertext ----------

	header := privacy.Header{Version: block.Version, Pub: authorPub}
	enc, nonce, err := privacy.Seal(header, key, samplePayload(), fixedReader(blockNonceByte))
	if err != nil {
		return nil, err
	}

	tamperedEnc := bytes.Clone(enc)
	tamperedEnc[0] ^= 0x01
	tamperedEncBlock, err := block.Sign(block.Content{
		Version: block.Version, Type: block.TypePrivate, Pub: authorPub,
		Enc: tamperedEnc, Nonce: nonce,
	}, authorPriv)
	if err != nil {
		return nil, err
	}
	if err := mustOpenFail(tamperedEncBlock, key, true); err != nil {
		return nil, fmt.Errorf("vectors: tampered_enc: %w", err)
	}

	tamperedNonce := bytes.Clone(nonce)
	tamperedNonce[0] ^= 0x01
	tamperedNonceBlock, err := block.Sign(block.Content{
		Version: block.Version, Type: block.TypePrivate, Pub: authorPub,
		Enc: enc, Nonce: tamperedNonce,
	}, authorPriv)
	if err != nil {
		return nil, err
	}
	if err := mustOpenFail(tamperedNonceBlock, key, true); err != nil {
		return nil, fmt.Errorf("vectors: tampered_nonce: %w", err)
	}

	otherPrev := entity.MustAtom("the previous block").Digest()
	tamperedAADBlock, err := block.Sign(block.Content{
		Version: block.Version, Type: block.TypePrivate, Pub: authorPub, Prev: &otherPrev,
		Enc: enc, Nonce: nonce,
	}, authorPriv)
	if err != nil {
		return nil, err
	}
	if err := mustOpenFail(tamperedAADBlock, key, true); err != nil {
		return nil, fmt.Errorf("vectors: tampered_aad_field: %w", err)
	}

	cases = append(cases,
		PrivacyInvalidCase{
			Name: "tampered_enc", Rule: rulePrivDecryption,
			Reason:     "private_genesis with one bit of enc flipped, re-signed by the author so the block still decodes and verifies; the ciphertext no longer authenticates under the unchanged AAD.",
			ContentKey: hexOf(key[:]), Block: hexOf(tamperedEncBlock.Bytes()),
		},
		PrivacyInvalidCase{
			Name: "tampered_nonce", Rule: rulePrivDecryption,
			Reason:     "private_genesis with one bit of nonce flipped, re-signed by the author; the same enc opened under a different nonce does not authenticate.",
			ContentKey: hexOf(key[:]), Block: hexOf(tamperedNonceBlock.Bytes()),
		},
		PrivacyInvalidCase{
			Name: "tampered_aad_field", Rule: rulePrivDecryption,
			Reason:     "private_genesis re-signed with prev changed from null to a 32-byte digest; enc and nonce are unchanged, so the AAD computed at open time no longer matches the one enc was sealed under.",
			ContentKey: hexOf(key[:]), Block: hexOf(tamperedAADBlock.Bytes()),
		},
	)

	// --- The enc floor: rejected before decryption, or even a valid
	// signature, is attempted -------------------------------------------

	shortEncMap := with(with(with(with(with(with(dcbor.Map{},
		"v", dcbor.Uint(block.Version)),
		"type", dcbor.Text(string(block.TypePrivate))),
		"pub", dcbor.Bytes(authorPub)),
		"prev", dcbor.Null),
		"enc", dcbor.Bytes(bytes.Repeat([]byte{0xaa}, 8))),
		"nonce", dcbor.Bytes(bytes.Repeat([]byte{blockNonceByte}, privacy.NonceSize)))
	shortEncHex, err := signRaw(authorPriv, shortEncMap)
	if err != nil {
		return nil, err
	}
	if _, err := block.Decode(mustHexBytes(shortEncHex)); err == nil {
		return nil, errors.New("vectors: enc_below_floor: the reference implementation decodes a block whose enc is 8 bytes")
	}
	cases = append(cases, PrivacyInvalidCase{
		Name: "enc_below_floor", Rule: rulePrivEncFloor,
		Reason:     "An 8-byte enc field, shorter than the 16-byte Poly1305 tag every XChaCha20-Poly1305 ciphertext carries; it cannot be the AEAD's output and is rejected without attempting decryption.",
		ContentKey: hexOf(key[:]), Block: shortEncHex,
	})

	// --- A plaintext that authenticates but is not canonical dCBOR ------
	//
	// The same three fields as the payload case, reordered refs/ops/ts
	// instead of the canonical ts/ops/refs. dcbor.Encode always sorts, so
	// these bytes are hand-assembled from the same encoded segments — the way
	// dcbor.json's own invalid section pins non-canonical bytes no encoder
	// here would produce.
	const (
		segTS   = "6274731a67b75180"
		segOps  = "636f707381a2626f706b6372656174655f61746f6d6b6465736372697074696f6e6f4d792070726976617465206e6f7465"
		segRefs = "647265667380"
	)
	nonCanonicalPlaintext := mustHexBytes("a3" + segRefs + segOps + segTS)
	if _, err := dcbor.Decode(nonCanonicalPlaintext); err == nil {
		return nil, errors.New("vectors: non_canonical_plaintext: the reordered payload decodes as canonical dCBOR")
	}
	plaintextNonce := bytes.Repeat([]byte{0x44}, privacy.NonceSize)
	plaintextEnc, err := sealRaw(header, key, nonCanonicalPlaintext, plaintextNonce)
	if err != nil {
		return nil, err
	}
	nonCanonicalBlock, err := block.Sign(block.Content{
		Version: block.Version, Type: block.TypePrivate, Pub: authorPub,
		Enc: plaintextEnc, Nonce: plaintextNonce,
	}, authorPriv)
	if err != nil {
		return nil, err
	}
	if err := mustOpenFail(nonCanonicalBlock, key, false); err != nil {
		return nil, fmt.Errorf("vectors: non_canonical_plaintext: %w", err)
	}
	cases = append(cases, PrivacyInvalidCase{
		Name: "non_canonical_plaintext", Rule: rulePrivPayloadDecode,
		Reason:     "The ciphertext authenticates: its plaintext is the payload's three fields keyed refs, ops, ts instead of the canonical ts, ops, refs order. Decryption succeeds; the strict dCBOR decode of the plaintext is what rejects it, which pins that the check happens after authentication and not before.",
		ContentKey: hexOf(key[:]), Block: hexOf(nonCanonicalBlock.Bytes()),
	})

	// --- An authentic plaintext carrying a rotate_key operation ---------
	//
	// Payload.Encode refuses to produce this on purpose (block.Payload.Validate
	// rejects a rotate_key operation), so the bytes are built from Value()
	// directly, bypassing the validation a conforming author's own encoder
	// would apply — exactly the shape a non-conforming or hostile author could
	// still transmit, and which spec/02-block-format.md requires a party that
	// decrypts the payload to refuse.
	rotatePayload := block.Payload{TS: tsGenesis, Ops: []block.Operation{block.MustRotateKey(seedPub(seedSuccessor))}}
	rotateBytes, err := dcbor.Encode(rotatePayload.Value())
	if err != nil {
		return nil, err
	}
	rotateNonce := bytes.Repeat([]byte{0x55}, privacy.NonceSize)
	rotateEnc, err := sealRaw(header, key, rotateBytes, rotateNonce)
	if err != nil {
		return nil, err
	}
	rotateBlock, err := block.Sign(block.Content{
		Version: block.Version, Type: block.TypePrivate, Pub: authorPub,
		Enc: rotateEnc, Nonce: rotateNonce,
	}, authorPriv)
	if err != nil {
		return nil, err
	}
	if err := mustOpenFail(rotateBlock, key, false); err != nil {
		return nil, fmt.Errorf("vectors: rotate_key_payload: %w", err)
	}
	cases = append(cases, PrivacyInvalidCase{
		Name: "rotate_key_payload", Rule: rulePrivRotateScope,
		Reason:     "The ciphertext authenticates to a well-formed payload whose one operation is rotate_key. A private chain's key never rotates inside a ciphertext: the rule confining rotate_key to a rotation block applies here exactly as spec/02-block-format.md states it for a party that decrypts the payload.",
		ContentKey: hexOf(key[:]), Block: hexOf(rotateBlock.Bytes()),
	})

	return cases, nil
}

// sealRaw seals plaintext exactly as Seal does, without routing it through
// block.Payload.Encode — which validates, and would refuse the very bytes the
// two payload-layer cases above exist to pin.
func sealRaw(h privacy.Header, key privacy.Key, plaintext, nonce []byte) ([]byte, error) {
	aad, err := h.AAD()
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, nonce, plaintext, aad), nil
}

// mustOpenFail checks that Open rejects b under key. When auth is true, the
// rejection must specifically be an authentication failure — a tampered
// ciphertext, nonce or AAD is indistinguishable from a wrong key at the AEAD
// layer, so that is as precise as the check can be; when it is false, Open
// must fail on something other than authentication, because the case is
// about the payload behind an AEAD that already authenticated it.
func mustOpenFail(b *block.Block, key privacy.Key, auth bool) error {
	_, err := privacy.Open(b, key)
	switch {
	case err == nil:
		return errors.New("the reference implementation opens the block")
	case auth && !errors.Is(err, privacy.ErrAuthentication):
		return fmt.Errorf("want an authentication failure, got %w", err)
	case !auth && errors.Is(err, privacy.ErrAuthentication):
		return fmt.Errorf("rejected by authentication, not by the payload rule under test: %w", err)
	}
	return nil
}
