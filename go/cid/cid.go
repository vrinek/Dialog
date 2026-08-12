// Package cid implements Dialog's content identifiers: the raw 32-byte
// SHA-256 digest used for every reference inside a Dialog structure, and the
// 36-byte CIDv1 used for external identifiers, as specified in
// spec/03-encoding.md ("Content identifiers" and "Internal references").
//
// Both forms are fixed-parameter. A Dialog CID is always CIDv1 with the
// dag-cbor codec (0x71), SHA-256 (0x12) and a 32-byte digest (0x20); the
// parsers here reject anything else, as the specification requires.
//
// Digest is what appears on the wire inside Dialog structures — a block's
// prev field, each entry of its refs list, a molecule's bond field, and
// fillers of type 0, 1 and 2 — encoded as a CBOR byte string (5820 followed
// by the 32 digest bytes). CID is used only at the API and display boundary.
//
// A CID is written as text in the canonical multibase base32 form of
// spec/03-encoding.md, "Text representation": CID.String produces it and
// ParseCIDString reads it. The hex helpers (CID.HexString, ParseCIDHex,
// Digest.String, ParseDigestHex) render byte dumps, matching the
// illustrations in the specification, and are not an interchange format.
//
// This package depends on the standard library only. Callers encode an entity
// with the dcbor package and pass the resulting bytes to SumDigest or SumCID.
package cid

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"
)

// Fixed CID parameters (spec/03-encoding.md, "Content identifiers"). All four
// values are below 128, so each is a single-byte unsigned varint.
const (
	// Version is the CID version: CIDv1.
	Version byte = 0x01
	// CodecDAGCBOR is the dag-cbor multicodec content type.
	CodecDAGCBOR byte = 0x71
	// HashSHA256 is the SHA-256 multihash function code.
	HashSHA256 byte = 0x12
	// DigestSize is the SHA-256 digest length, in bytes and as the
	// multihash length byte.
	DigestSize = 0x20

	// PrefixSize is the number of bytes preceding the digest in a CID.
	PrefixSize = 4
	// Size is the total size of a Dialog CID in bytes.
	Size = PrefixSize + DigestSize
	// MultihashSize is the total size of a Dialog multihash in bytes.
	MultihashSize = 2 + DigestSize
)

// MultibaseBase32 is the multibase code for lowercase, unpadded RFC 4648
// base32 — the canonical text form of a CID (spec/03-encoding.md, "Text
// representation").
const MultibaseBase32 = "b"

// StringSize is the length of a Dialog CID in its canonical text form: the
// multibase prefix plus the base32 encoding of 36 bytes.
const StringSize = len(MultibaseBase32) + (Size*8+4)/5 // 1 + 58 = 59

// prefix is the fixed 4-byte CID prefix: varint(1) || varint(0x71) ||
// varint(0x12) || varint(0x20).
var prefix = [PrefixSize]byte{Version, CodecDAGCBOR, HashSHA256, DigestSize}

// base32Lower is the RFC 4648 base32 alphabet in lowercase, without padding,
// as the multibase "b" encoding requires.
var base32Lower = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// A Digest is a raw 32-byte SHA-256 digest: the form every reference inside a
// Dialog structure takes.
type Digest [DigestSize]byte

// A CID is a 36-byte Dialog CIDv1: the external form of an entity identifier.
// A CID value obtained from this package always carries the fixed parameters.
type CID [Size]byte

// SumDigest returns the SHA-256 digest of the dCBOR encoding of an entity.
// The argument must already be dCBOR bytes, as produced by dcbor.Encode.
func SumDigest(dcborBytes []byte) Digest {
	return Digest(sha256.Sum256(dcborBytes))
}

// SumCID returns the CID of an entity given its dCBOR encoding:
//
//	CID(entity) = 0x01 || 0x71 || 0x12 || 0x20 || SHA-256(dCBOR(entity))
func SumCID(dcborBytes []byte) CID {
	return SumDigest(dcborBytes).CID()
}

// CID returns the external 36-byte form of d.
func (d Digest) CID() CID {
	var c CID
	copy(c[:PrefixSize], prefix[:])
	copy(c[PrefixSize:], d[:])
	return c
}

// Multihash returns the 34-byte multihash of d: varint(0x12) || varint(0x20)
// || digest.
func (d Digest) Multihash() []byte {
	mh := make([]byte, 0, MultihashSize)
	mh = append(mh, HashSHA256, DigestSize)
	return append(mh, d[:]...)
}

// Bytes returns a copy of the digest as a slice, ready to be wrapped in a
// dcbor.Bytes value.
func (d Digest) Bytes() []byte {
	b := make([]byte, DigestSize)
	copy(b, d[:])
	return b
}

// String returns the lowercase hex encoding of the digest.
func (d Digest) String() string { return hex.EncodeToString(d[:]) }

// Digest returns the 32-byte digest carried by c.
func (c CID) Digest() Digest {
	var d Digest
	copy(d[:], c[PrefixSize:])
	return d
}

// Bytes returns a copy of the CID as a slice.
func (c CID) Bytes() []byte {
	b := make([]byte, Size)
	copy(b, c[:])
	return b
}

// String returns the canonical text form of the CID: its multibase base32
// encoding, "b" followed by the lowercase unpadded RFC 4648 base32 encoding
// of all 36 CID bytes (spec/03-encoding.md, "Text representation").
//
// This is the standard CIDv1 text form, so a Dialog CID pastes directly into
// IPFS and multiformats tooling. Every Dialog CID renders as 59 characters
// beginning "bafyrei".
func (c CID) String() string {
	return MultibaseBase32 + base32Lower.EncodeToString(c[:])
}

// HexString returns the lowercase hex encoding of the 36 CID bytes.
//
// This is a byte dump, matching the illustrative hex listings in
// spec/03-encoding.md. It is not a CID text form: use String for anything
// that leaves the process.
func (c CID) HexString() string { return hex.EncodeToString(c[:]) }

// ParseDigest interprets b as a raw 32-byte digest.
func ParseDigest(b []byte) (Digest, error) {
	var d Digest
	if len(b) != DigestSize {
		return d, fmt.Errorf("cid: digest is %d bytes, want %d", len(b), DigestSize)
	}
	copy(d[:], b)
	return d, nil
}

// ParseDigestHex interprets s as the hex encoding of a raw 32-byte digest.
func ParseDigestHex(s string) (Digest, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return Digest{}, fmt.Errorf("cid: invalid digest hex: %w", err)
	}
	return ParseDigest(b)
}

// ParseCID interprets b as a Dialog CID, validating every fixed parameter.
// CIDs that use different parameters are rejected, as spec/03-encoding.md
// requires.
func ParseCID(b []byte) (CID, error) {
	var c CID
	if len(b) != Size {
		return c, fmt.Errorf("cid: CID is %d bytes, want %d", len(b), Size)
	}
	if b[0] != Version {
		return c, fmt.Errorf("cid: unsupported CID version 0x%02x, want 0x%02x (CIDv1)", b[0], Version)
	}
	if b[1] != CodecDAGCBOR {
		return c, fmt.Errorf("cid: unsupported content codec 0x%02x, want 0x%02x (dag-cbor)", b[1], CodecDAGCBOR)
	}
	if b[2] != HashSHA256 {
		return c, fmt.Errorf("cid: unsupported hash function 0x%02x, want 0x%02x (SHA-256)", b[2], HashSHA256)
	}
	if b[3] != DigestSize {
		return c, fmt.Errorf("cid: unsupported digest length 0x%02x, want 0x%02x (32 bytes)", b[3], DigestSize)
	}
	copy(c[:], b)
	return c, nil
}

// ParseCIDString interprets s as a CID in its canonical text form: the
// multibase base32 encoding of spec/03-encoding.md, "Text representation".
//
// The form is exact. A missing or wrong multibase prefix, uppercase base32
// (multibase "B"), padding, or a wrong length is an error, as is any string
// whose decoded bytes fail the fixed-parameter validation of ParseCID.
func ParseCIDString(s string) (CID, error) {
	if s == "" {
		return CID{}, fmt.Errorf("cid: empty CID string")
	}
	if !strings.HasPrefix(s, MultibaseBase32) {
		return CID{}, fmt.Errorf("cid: CID string must start with the multibase code %q (base32, lowercase, unpadded), got %q", MultibaseBase32, s[:1])
	}
	body := s[len(MultibaseBase32):]
	if strings.Contains(body, "=") {
		return CID{}, fmt.Errorf("cid: CID string must not use base32 padding")
	}
	if body != strings.ToLower(body) {
		return CID{}, fmt.Errorf("cid: CID string must use the lowercase base32 alphabet")
	}
	if len(s) != StringSize {
		return CID{}, fmt.Errorf("cid: CID string is %d characters, want %d", len(s), StringSize)
	}
	b, err := base32Lower.DecodeString(body)
	if err != nil {
		return CID{}, fmt.Errorf("cid: invalid base32 in CID string: %w", err)
	}
	return ParseCID(b)
}

// ParseCIDHex interprets s as the hex encoding of the 36 CID bytes. It is the
// inverse of CID.HexString and reads the byte dumps printed by
// spec/03-encoding.md; ParseCIDString reads the canonical text form.
func ParseCIDHex(s string) (CID, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return CID{}, fmt.Errorf("cid: invalid CID hex: %w", err)
	}
	return ParseCID(b)
}
