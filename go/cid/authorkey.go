package cid

import (
	"fmt"
	"strings"
)

// The canonical text form of an author's Ed25519 public key
// (spec/03-encoding.md, "Text representation of author keys").
//
// Inside Dialog's CBOR structures a public key is 32 raw bytes with no prefix,
// and it stays that way. The text form is what a key looks like outside them —
// in an API, a log, a configuration file, or wherever a chain is named by its
// author — and it is the multibase base32 encoding of the key behind its
// multicodec prefix, the same alphabet and multibase code a CID uses.
const (
	// AuthorKeySize is the length of an Ed25519 public key in bytes.
	AuthorKeySize = 32

	// MulticodecEd25519Pub is the multicodec code for an Ed25519 public key.
	// It is 237, above the single-byte varint range, so it encodes as the two
	// bytes 0xed 0x01 — unlike every code used in a CID.
	MulticodecEd25519Pub = 0xed

	// AuthorKeyPrefixSize is the number of bytes preceding the key in its
	// prefixed form: varint(0xed) is two bytes.
	AuthorKeyPrefixSize = 2

	// AuthorKeyPrefixedSize is the length of a multicodec-prefixed author key.
	AuthorKeyPrefixedSize = AuthorKeyPrefixSize + AuthorKeySize

	// AuthorKeyTextSize is the length of an author key in its canonical text
	// form: the multibase prefix plus the base32 encoding of 34 bytes.
	AuthorKeyTextSize = len(MultibaseBase32) + (AuthorKeyPrefixedSize*8+4)/5 // 1 + 55 = 56
)

// authorKeyPrefix is varint(0xed), the multicodec ed25519-pub code.
var authorKeyPrefix = [AuthorKeyPrefixSize]byte{MulticodecEd25519Pub, 0x01}

// AuthorKeyText returns the canonical text form of an author's 32-byte Ed25519
// public key: "b" followed by the lowercase unpadded RFC 4648 base32 encoding
// of the 34 bytes 0xed 0x01 || key (spec/03-encoding.md, "Text representation
// of author keys").
//
// Every author key renders as 56 characters beginning "b5ua". The same 34
// bytes re-encoded in base58btc are the payload of a did:key identifier, so a
// Dialog author key and a did:key identity are the same key in two alphabets.
func AuthorKeyText(pub []byte) (string, error) {
	if len(pub) != AuthorKeySize {
		return "", fmt.Errorf("cid: author key is %d bytes, want %d", len(pub), AuthorKeySize)
	}
	prefixed := make([]byte, 0, AuthorKeyPrefixedSize)
	prefixed = append(prefixed, authorKeyPrefix[:]...)
	prefixed = append(prefixed, pub...)
	return MultibaseBase32 + base32Lower.EncodeToString(prefixed), nil
}

// ParseAuthorKeyText interprets s as an author key in its canonical text form
// and returns the 32 key bytes.
//
// The form is exact, because these strings are compared as strings: a missing
// or wrong multibase prefix, uppercase base32 (multibase "B"), padding, a
// wrong length, or a multicodec prefix other than ed25519-pub is an error. A
// decoder that accepted any of them would mint a second spelling of one
// identity (spec/03-encoding.md, Security Considerations).
func ParseAuthorKeyText(s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("cid: empty author key string")
	}
	if !strings.HasPrefix(s, MultibaseBase32) {
		return nil, fmt.Errorf("cid: author key string must start with the multibase code %q (base32, lowercase, unpadded), got %q", MultibaseBase32, s[:1])
	}
	body := s[len(MultibaseBase32):]
	if strings.Contains(body, "=") {
		return nil, fmt.Errorf("cid: author key string must not use base32 padding")
	}
	if body != strings.ToLower(body) {
		return nil, fmt.Errorf("cid: author key string must use the lowercase base32 alphabet")
	}
	if len(s) != AuthorKeyTextSize {
		return nil, fmt.Errorf("cid: author key string is %d characters, want %d", len(s), AuthorKeyTextSize)
	}
	b, err := base32Lower.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("cid: invalid base32 in author key string: %w", err)
	}
	return ParseAuthorKeyBytes(b)
}

// ParseAuthorKeyBytes interprets b as a multicodec-prefixed author key —
// 0xed 0x01 followed by 32 key bytes — and returns the key.
func ParseAuthorKeyBytes(b []byte) ([]byte, error) {
	if len(b) != AuthorKeyPrefixedSize {
		return nil, fmt.Errorf("cid: prefixed author key is %d bytes, want %d", len(b), AuthorKeyPrefixedSize)
	}
	if b[0] != authorKeyPrefix[0] || b[1] != authorKeyPrefix[1] {
		return nil, fmt.Errorf("cid: unsupported key multicodec 0x%02x 0x%02x, want 0x%02x 0x%02x (ed25519-pub)",
			b[0], b[1], authorKeyPrefix[0], authorKeyPrefix[1])
	}
	pub := make([]byte, AuthorKeySize)
	copy(pub, b[AuthorKeyPrefixSize:])
	return pub, nil
}
