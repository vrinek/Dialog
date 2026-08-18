package vectors

import (
	"bytes"
	"crypto/ed25519"
	"fmt"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
	"github.com/vrinek/Dialog/go/privacy"
)

// The fixed inputs of the private-block vectors. Nonce uniqueness is a MUST in
// production and the enemy of a reproducible vector, so every nonce here comes
// from a byte source that repeats one value, and the sealed bytes are
// reproducible exactly because these inputs are not secret.
const (
	// contentKeyByte fills the chain's 32-byte symmetric key.
	contentKeyByte = 0x11
	// wrapNonceByte fills the 24-byte nonce of every key wrap.
	wrapNonceByte = 0x22
	// blockNonceByte fills the 24-byte XChaCha20 nonce of the sealed block.
	blockNonceByte = 0x33

	// privateNote is the operation of the worked example in
	// spec/04-cryptography.md, "Encrypting a private block".
	privateNote = "My private note"
)

// fixedReader is an endless repetition of one byte: the injectable randomness
// source that makes a sealed block reproducible.
type fixedReader byte

func (r fixedReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}

func privacyFile() (File, error) {
	doc, err := privacyDocument()
	if err != nil {
		return File{}, err
	}
	return File{Name: "privacy.json", Doc: doc}, nil
}

// samplePayload is the payload spec/04's example encrypts: an empty refs list,
// the example's timestamp, and one create_atom operation.
func samplePayload() block.Payload {
	return block.Payload{
		TS:  tsGenesis,
		Ops: []block.Operation{block.MustCreateAtom(privateNote)},
	}
}

func privacyDocument() (Document, error) {
	payload := samplePayload()
	plaintext, err := payload.Encode()
	if err != nil {
		return Document{}, fmt.Errorf("vectors: private payload: %w", err)
	}

	author, recipient, other := seedKey(seedAlice), seedKey(seedBob), seedKey(seedSuccessor)
	key, err := privacy.GenerateKey(fixedReader(contentKeyByte))
	if err != nil {
		return Document{}, err
	}

	header := privacy.Header{Version: block.Version, Pub: seedPub(seedAlice)}
	aad, err := header.AAD()
	if err != nil {
		return Document{}, err
	}
	enc, nonce, err := privacy.Seal(header, key, payload, fixedReader(blockNonceByte))
	if err != nil {
		return Document{}, err
	}

	// A linked block's AAD, so that the binding of a ciphertext to its place
	// in the chain has a vector of its own: an enc lifted into another block
	// fails authentication because this byte string differs.
	linkedPrev := entity.MustAtom("the previous block").Digest()
	linked := privacy.Header{Version: block.Version, Pub: seedPub(seedAlice), Prev: &linkedPrev}
	linkedAAD, err := linked.AAD()
	if err != nil {
		return Document{}, err
	}

	sealed, err := sealedBlockCase(key)
	if err != nil {
		return Document{}, err
	}

	wraps, err := wrapCases(key, author, recipient, other)
	if err != nil {
		return Document{}, err
	}
	conversions, err := x25519Cases()
	if err != nil {
		return Document{}, err
	}
	invalid, err := privacyInvalidCases(key)
	if err != nil {
		return Document{}, err
	}

	return Document{
		Vectors: Format,
		Area:    "privacy",
		Description: "The private-block path of spec/04-cryptography.md: the plaintext payload, the AAD that binds it to its block, the XChaCha20-Poly1305 ciphertext, the Ed25519-to-X25519 conversions, and the per-recipient key wrap. " +
			"Every key and nonce is a fixed constant given in inputs, so an implementation reproduces these bytes exactly or does not implement this protocol.",
		Spec:   []string{"spec/04-cryptography.md", "spec/02-block-format.md"},
		Inputs: privacyInputs(key),
		Sections: []Section{
			{
				Name:        "payload",
				Description: "plaintext = dCBOR({\"refs\": refs, \"ts\": ts, \"ops\": ops}) — the three fields a private block encrypts, encoded exactly as a public block encodes them.",
				Cases: []PrivacyCase{{
					Name:  "plaintext",
					Note:  "The payload of the worked example: an empty refs list, the timestamp 1740067200, and one create_atom operation. The keys sort ts, ops, refs.",
					Value: describePointer(payload.Value()),
					Hex:   hexOf(plaintext),
				}},
			},
			{
				Name:        "aead",
				Description: "enc = XChaCha20-Poly1305(content key, nonce, plaintext, aad), where aad = dCBOR({\"v\": v, \"type\": \"private\", \"pub\": pub, \"prev\": prev}) — every plaintext field of the block but sig, enc and nonce.",
				Cases: []PrivacyCase{
					{
						Name:  "aad_genesis",
						Note:  "The AAD of a private genesis block: prev is null.",
						Value: describePointer(aadValue(header)),
						Hex:   hexOf(aad),
					},
					{
						Name:  "aad_linked",
						Note:  "The AAD of a private block whose prev is the digest of the atom {\"description\": \"the previous block\"}, used here only as a fixed 32-byte value. Binding the ciphertext to prev is what stops an enc from being lifted into another block.",
						Value: describePointer(aadValue(linked)),
						Hex:   hexOf(linkedAAD),
					},
					{
						Name: "nonce",
						Note: "The 24-byte XChaCha20 nonce of the sealed block. In production it MUST be unique per block under a given key; here it is fixed so the ciphertext is reproducible.",
						Hex:  hexOf(nonce),
					},
					{
						Name: "enc",
						Note: "The ciphertext: the 55-byte plaintext followed by the 16-byte Poly1305 tag. XChaCha20 is a stream cipher, so the payload's length is not hidden.",
						Hex:  hexOf(enc),
					},
				},
			},
			{
				Name:        "x25519",
				Description: "The birational map from an Ed25519 identity to the X25519 agreement (spec/04-cryptography.md, \"Ed25519-to-X25519 conversion\"). The private half is derived from SHA-512 over the seed, not from the seed itself — the single most common place for an implementation to go wrong.",
				Cases:       conversions,
			},
			{
				Name:        "key_wrap",
				Description: "wrapping key = HKDF-SHA-256(salt: empty, ikm: X25519(own, peer), info: \"dialog-v1-key-wrap\", 32); wrapped key = nonce || XChaCha20-Poly1305(wrapping key, nonce, content key, aad: empty), 72 bytes.",
				Cases:       wraps,
			},
			{
				Name:        "private_block",
				Description: "The whole thing assembled: a private genesis block carrying the ciphertext above, signed by the author. Only v, type, pub, sig and prev are in the clear.",
				Cases:       []BlockCase{sealed},
			},
			{
				Name:        "invalid",
				Description: "Every rejection rule spec/04-cryptography.md states in prose, pinned as bytes: both of the X25519 conversion's own refusals, the small-order agreement, four key-wrap rejections, three AEAD tamper cases, and two payloads that authenticate but must still be refused — one on strict decoding, one on the rotate_key scoping rule that in fact lives in spec/02-block-format.md, which the enc-floor case also reaches down to.",
				Cases:       invalid,
			},
		},
	}, nil
}

// aadValue rebuilds the AAD map so that the vectors can show its structure as
// well as its bytes. It mirrors privacy.Header.AAD, which is what the hex
// beside it comes from; the conformance test compares the two.
func aadValue(h privacy.Header) dcbor.Value {
	prev := dcbor.Value(dcbor.Null)
	if h.Prev != nil {
		prev = dcbor.Bytes(h.Prev.Bytes())
	}
	return dcbor.Map{
		{Key: "v", Value: dcbor.Uint(h.Version)},
		{Key: "type", Value: dcbor.Text(string(block.TypePrivate))},
		{Key: "pub", Value: dcbor.Bytes(h.Pub)},
		{Key: "prev", Value: prev},
	}
}

func privacyInputs(key privacy.Key) PrivacyInputs {
	return PrivacyInputs{
		Note: "Every key comes from a seed of 32 equal bytes and every nonce from a byte repeated 24 times. These are test values with published secret material and MUST NOT be used for anything but conformance testing; in production a nonce MUST NOT repeat under one key.",
		Keys: []KeyCase{
			keyCase("author", seedAlice),
			keyCase("recipient", seedBob),
			keyCase("third_party", seedSuccessor),
		},
		ContentKey: hexOf(key[:]),
		BlockNonce: hexOf(bytes.Repeat([]byte{blockNonceByte}, privacy.NonceSize)),
		WrapNonce:  hexOf(bytes.Repeat([]byte{wrapNonceByte}, privacy.NonceSize)),
	}
}

func x25519Cases() ([]X25519Case, error) {
	cases := []X25519Case{}
	for _, k := range []struct {
		name string
		seed byte
		note string
	}{
		{"author", seedAlice, "The author of the private chain."},
		{"recipient", seedBob, "The reader the content key is wrapped for."},
		{"third_party", seedSuccessor, "A key that is not a recipient. Its agreement with the author differs, so it derives a different wrapping key and cannot unwrap."},
	} {
		priv, err := privacy.X25519PrivateFromEd25519(seedKey(k.seed))
		if err != nil {
			return nil, fmt.Errorf("vectors: %s: %w", k.name, err)
		}
		pub, err := privacy.X25519PublicFromEd25519(seedPub(k.seed))
		if err != nil {
			return nil, fmt.Errorf("vectors: %s: %w", k.name, err)
		}
		// The two halves must agree, or the conversion is wrong in a way no
		// single vector would catch.
		if !priv.PublicKey().Equal(pub) {
			return nil, fmt.Errorf("vectors: %s: the converted private key's public key is not the converted public key", k.name)
		}
		cases = append(cases, X25519Case{
			Name:             k.name,
			Note:             k.note,
			Seed:             hexOf(seedKey(k.seed).Seed()),
			Ed25519PublicKey: hexOf(seedPub(k.seed)),
			X25519PrivateKey: hexOf(priv.Bytes()),
			X25519PublicKey:  hexOf(pub.Bytes()),
		})
	}
	return cases, nil
}

func wrapCases(key privacy.Key, author, recipient, other ed25519.PrivateKey) ([]WrapCase, error) {
	build := func(name, note, ownName, peerName string, own ed25519.PrivateKey, peer ed25519.PublicKey) (WrapCase, error) {
		ownX, err := privacy.X25519PrivateFromEd25519(own)
		if err != nil {
			return WrapCase{}, err
		}
		peerX, err := privacy.X25519PublicFromEd25519(peer)
		if err != nil {
			return WrapCase{}, err
		}
		shared, err := ownX.ECDH(peerX)
		if err != nil {
			return WrapCase{}, err
		}
		wrappingKey, err := privacy.WrappingKey(own, peer)
		if err != nil {
			return WrapCase{}, err
		}
		wrapped, err := privacy.Wrap(key, own, peer, fixedReader(wrapNonceByte))
		if err != nil {
			return WrapCase{}, err
		}
		if len(wrapped) != privacy.WrappedKeySize {
			return WrapCase{}, fmt.Errorf("vectors: a wrapped key is %d bytes, want %d", len(wrapped), privacy.WrappedKeySize)
		}
		return WrapCase{
			Name:         name,
			Note:         note,
			Own:          ownName,
			Peer:         peerName,
			SharedSecret: hexOf(shared),
			Info:         privacy.WrapInfo,
			WrappingKey:  hexOf(wrappingKey),
			Nonce:        hexOf(wrapped[:privacy.NonceSize]),
			WrappedKey:   hexOf(wrapped),
		}, nil
	}

	authorToRecipient, err := build(
		"author_to_recipient",
		"The worked example of spec/04-cryptography.md, \"Wrapping a chain key\". The agreement is symmetric: the recipient derives the same wrapping key from their own private key and the author's public key, which is what makes unwrapping possible.",
		"author", "recipient", author, seedPub(seedBob))
	if err != nil {
		return nil, err
	}
	authorToThirdParty, err := build(
		"author_to_third_party",
		"The same content key wrapped for a different reader. Every field differs from the case above, which is the point: a wrap is per recipient.",
		"author", "third_party", author, seedPub(seedSuccessor))
	if err != nil {
		return nil, err
	}

	// The wrap must actually open, and only for its recipient. A vector that
	// pinned an unopenable wrap would be worse than no vector.
	unwrapped, err := privacy.Unwrap(mustHexBytes(authorToRecipient.WrappedKey), recipient, seedPub(seedAlice))
	if err != nil {
		return nil, fmt.Errorf("vectors: the wrapped key does not unwrap: %w", err)
	}
	if unwrapped != key {
		return nil, fmt.Errorf("vectors: the wrapped key unwraps to the wrong content key")
	}
	if _, err := privacy.Unwrap(mustHexBytes(authorToRecipient.WrappedKey), other, seedPub(seedAlice)); err == nil {
		return nil, fmt.Errorf("vectors: a third party unwrapped the recipient's key")
	}

	return []WrapCase{authorToRecipient, authorToThirdParty}, nil
}

// sealedBlockCase builds the private block the ciphertext above belongs to.
func sealedBlockCase(key privacy.Key) (BlockCase, error) {
	builder, err := block.NewBuilder(seedKey(seedAlice))
	if err != nil {
		return BlockCase{}, err
	}
	b, err := privacy.SealBlock(builder, key, samplePayload(), fixedReader(blockNonceByte))
	if err != nil {
		return BlockCase{}, fmt.Errorf("vectors: sealing a private block: %w", err)
	}
	// What a key holder gets back must be the payload that went in.
	payload, err := privacy.Open(b, key)
	if err != nil {
		return BlockCase{}, fmt.Errorf("vectors: the sealed block does not open: %w", err)
	}
	if payload.TS != tsGenesis || len(payload.Ops) != 1 {
		return BlockCase{}, fmt.Errorf("vectors: the sealed block opened to the wrong payload")
	}
	return blockCase(
		"private_genesis",
		"A private genesis block by the author, carrying the ciphertext of the payload above. Its refs, ts and ops are inside enc; a node without the key validates rules 1, 2, 3, 7, 8 and 9 and reports 4, 5, 6 and 10 as unchecked.",
		"author", b)
}
